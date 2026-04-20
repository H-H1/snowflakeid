package snowflakeid

import (
	"errors"
	"sync"
	"time"
)

// 位布局（共63位有效位）:
// Bit layout (63 effective bits total):
//
//	 1位  符号位 (0)                          | 1 bit   sign bit (0, ensures positive int64)
//	42位  毫秒偏移（精度1ms，可用约139年）      | 42 bits ms timestamp offset (~139 years from epoch)
//	12位  机器ID（支持4096个节点）             | 12 bits machine ID (up to 4096 nodes)
//	 9位  序列号（每毫秒最多512个ID）           | 9 bits  sequence (up to 512 IDs per ms)
const (
	epoch3 = int64(1704067200000) // 2024-01-01 00:00:00 UTC（毫秒 / milliseconds）

	machineBits3  = 12
	sequenceBits3 = 9

	maxMachineID3 = -1 ^ (-1 << machineBits3)  // 4095
	maxSequence3  = -1 ^ (-1 << sequenceBits3) // 511

	timeShift3    = machineBits3 + sequenceBits3 // 21 — 时间戳左移位数 / timestamp left-shift
	machineShift3 = sequenceBits3                // 9  — 机器ID左移位数 / machine ID left-shift
)

// Snowflake3 序列号9位版本：时间戳42位，可用约139年，每ms最多512个ID
// Snowflake3 is a balanced variant: 9-bit sequence (512/ms), 42-bit timestamp (~139 years).
type Snowflake3 struct {
	mu        sync.Mutex
	lastStamp int64 // 上次生成ID的时间戳 / timestamp of the last generated ID
	machineID int64 // 机器ID / machine ID
	sequence  int64 // 当前毫秒内的序列号 / sequence counter within the current millisecond
}

// NewSnowflake3 创建 Snowflake3 生成器，machineID 范围 [0, 4095]
// NewSnowflake3 creates a Snowflake3 generator; machineID must be in [0, 4095].
func NewSnowflake3(machineID int64) (*Snowflake3, error) {
	if machineID < 0 || machineID > maxMachineID3 {
		return nil, errors.New("machineID out of range [0, 4095]")
	}
	return &Snowflake3{machineID: machineID}, nil
}

// MachineID 返回当前实例使用的机器ID
// MachineID returns the machine ID used by this instance.
func (s *Snowflake3) MachineID() int64 { return s.machineID }

// NextID 生成下一个唯一ID
// NextID generates the next unique ID.
func (s *Snowflake3) NextID() (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tick := currentTick3()

	if tick < s.lastStamp {
		// 时钟回拨时等待追上
		// Clock rolled back; spin until it catches up.
		tick = s.waitNextTick(s.lastStamp)
	}

	if tick == s.lastStamp {
		s.sequence = (s.sequence + 1) & maxSequence3
		if s.sequence == 0 {
			// 序列号耗尽，等待下一个 tick
			// Sequence exhausted; spin to the next tick.
			tick = s.waitNextTick(tick)
		}
	} else {
		// 新的毫秒，序列号归零
		// New millisecond — reset sequence.
		s.sequence = 0
	}

	s.lastStamp = tick
	return tick<<timeShift3 | s.machineID<<machineShift3 | s.sequence, nil
}

// waitNextTick 自旋直到时钟超过 last
// waitNextTick spins until the current tick advances past last.
func (s *Snowflake3) waitNextTick(last int64) int64 {
	t := currentTick3()
	for t <= last {
		t = currentTick3()
	}
	return t
}

// currentTick3 返回距 epoch3 的毫秒偏移
// currentTick3 returns milliseconds elapsed since epoch3.
func currentTick3() int64 {
	return time.Now().UnixMilli() - epoch3
}
