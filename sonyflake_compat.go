package snowflakeid

// SonyflakeCompat 模拟 sony/sonyflake 的实现风格（sleep等待而非自旋），
// 但采用与本项目相同的位布局：39位ms时间戳 | 12位机器ID | 12位序列号。
// 用于公平对比"sleep策略 vs 自旋策略"在相同位布局下的性能差异。

import (
	"sync"
	"time"
)

type SonyflakeCompat struct {
	mu          sync.Mutex
	elapsedTime int64 // 距 epoch 的微秒偏移
	sequence    int64
	machineID   int64
}

func NewSonyflakeCompat(machineID int64) *SonyflakeCompat {
	return &SonyflakeCompat{
		machineID: machineID & maxMachineID,
		sequence:  maxSequence, // 首次调用时归零
	}
}

func (s *SonyflakeCompat) NextID() (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	current := currentTick() // 复用 snowflake.go 的 currentTick()

	if s.elapsedTime < current {
		s.elapsedTime = current
		s.sequence = 0
	} else {
		s.sequence = (s.sequence + 1) & maxSequence
		if s.sequence == 0 {
			// 序列号耗尽：sony 风格用 sleep 等待，而非自旋
			s.elapsedTime++
			overtime := s.elapsedTime - current
			// overtime 个毫秒后再继续
			time.Sleep(time.Duration(overtime) * time.Millisecond)
		}
	}

	id := s.elapsedTime<<timeShift | s.machineID<<machineShift | s.sequence
	return id, nil
}
