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
//	43位  毫秒偏移（精度1ms，可用约278年）      | 43 bits ms timestamp offset (~278 years from epoch)
//	12位  机器ID（支持4096个节点）             | 12 bits machine ID (up to 4096 nodes)
//	 8位  序列号（每毫秒最多256个ID）           | 8 bits  sequence (up to 256 IDs per ms)
const (
	epoch2 = int64(1704067200000) // 2024-01-01 00:00:00 UTC（毫秒 / milliseconds）

	machineBits2  = 12
	sequenceBits2 = 8

	maxMachineID2 = -1 ^ (-1 << machineBits2)  // 4095
	maxSequence2  = -1 ^ (-1 << sequenceBits2) // 255

	timeShift2    = machineBits2 + sequenceBits2 // 20 — 时间戳左移位数 / timestamp left-shift
	machineShift2 = sequenceBits2                // 8  — 机器ID左移位数 / machine ID left-shift
)

// Snowflake2 压缩序列号版本：序列号8位，时间戳43位，可用约278年
// Snowflake2 trades sequence bits for a longer timestamp:
// 8-bit sequence (256/ms), 43-bit timestamp (~278 years).
type Snowflake2 struct {
	mu        sync.Mutex
	lastStamp int64 // 上次生成ID的时间戳 / timestamp of the last generated ID
	machineID int64 // 机器ID / machine ID
	sequence  int64 // 当前毫秒内的序列号 / sequence counter within the current millisecond
}

// NewSnowflake2 创建 Snowflake2 生成器，machineID 范围 [0, 4095]
// NewSnowflake2 creates a Snowflake2 generator; machineID must be in [0, 4095].
func NewSnowflake2(machineID int64) (*Snowflake2, error) {
	if machineID < 0 || machineID > maxMachineID2 {
		return nil, errors.New("machineID out of range [0, 4095]")
	}
	return &Snowflake2{machineID: machineID}, nil
}

// MachineID 返回当前实例使用的机器ID
// MachineID returns the machine ID used by this instance.
func (s *Snowflake2) MachineID() int64 { return s.machineID }

// NextID 生成下一个唯一ID
// NextID generates the next unique ID.
func (s *Snowflake2) NextID() (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tick := currentTick2()

	if tick < s.lastStamp {
		// 时钟回拨时等待追上
		// Clock rolled back; spin until it catches up.
		tick = s.waitNextTick(s.lastStamp)
	}

	if tick == s.lastStamp {
		s.sequence = (s.sequence + 1) & maxSequence2
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
	return tick<<timeShift2 | s.machineID<<machineShift2 | s.sequence, nil
}

// waitNextTick 自旋直到时钟超过 last
// waitNextTick spins until the current tick advances past last.
func (s *Snowflake2) waitNextTick(last int64) int64 {
	t := currentTick2()
	for t <= last {
		t = currentTick2()
	}
	return t
}

// currentTick2 返回距 epoch2 的毫秒偏移
// currentTick2 returns milliseconds elapsed since epoch2.
func currentTick2() int64 {
	return time.Now().UnixMilli() - epoch2
}
