package snowflakeid

// SonyflakeCompat 模拟 sony/sonyflake 的实现风格（sleep等待而非自旋），
// 但采用与本项目相同的位布局：40位ms时间戳 | 12位机器ID | 11位序列号。
// 用于公平对比"sleep策略 vs 自旋策略"在相同位布局下的性能差异。
//
// SonyflakeCompat mimics the sony/sonyflake implementation style (sleep on sequence
// exhaustion instead of spinning), but uses the same bit layout as this project:
// 40-bit ms timestamp | 12-bit machine ID | 11-bit sequence.
// Its purpose is a fair apples-to-apples comparison of sleep vs spin strategies
// under an identical bit layout.

import (
	"sync"
	"time"
)

type SonyflakeCompat struct {
	mu          sync.Mutex
	elapsedTime int64 // 距 epoch 的毫秒偏移 / millisecond offset from epoch
	sequence    int64 // 当前毫秒内的序列号 / sequence counter within the current millisecond
	machineID   int64 // 机器ID / machine ID
}

// NewSonyflakeCompat 创建 SonyflakeCompat 实例，machineID 截取低12位。
// NewSonyflakeCompat creates a SonyflakeCompat instance; only the low 12 bits of machineID are used.
func NewSonyflakeCompat(machineID int64) *SonyflakeCompat {
	return &SonyflakeCompat{
		machineID: machineID & maxMachineID,
		sequence:  maxSequence, // 首次调用时归零 / will wrap to 0 on the first call
	}
}

// NextID 生成下一个唯一ID。
// 序列号耗尽时使用 sleep 让出调度（sony 风格），而非自旋持锁。
//
// NextID generates the next unique ID.
// When the sequence is exhausted it sleeps (sony style) instead of spinning,
// releasing the lock so other goroutines can proceed.
func (s *SonyflakeCompat) NextID() (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	current := currentTick() // 复用 snowflake.go 的 currentTick() / reuse currentTick() from snowflake.go

	if s.elapsedTime < current {
		// 新的毫秒，重置序列号
		// New millisecond — reset sequence.
		s.elapsedTime = current
		s.sequence = 0
	} else {
		s.sequence = (s.sequence + 1) & maxSequence
		if s.sequence == 0 {
			// 序列号耗尽：sony 风格用 sleep 等待，而非自旋
			// Sequence exhausted: sleep (sony style) instead of spinning.
			s.elapsedTime++
			overtime := s.elapsedTime - current
			// 等待 overtime 毫秒后继续 / sleep for overtime milliseconds before continuing
			time.Sleep(time.Duration(overtime) * time.Millisecond)
		}
	}

	id := s.elapsedTime<<timeShift | s.machineID<<machineShift | s.sequence
	return id, nil
}
