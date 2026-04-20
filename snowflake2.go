package snowflakeid

import (
	"errors"
	"sync"
	"time"
)

// 位布局（共63位有效位）:
//
//	 1位  符号位 (0)
//	43位  毫秒偏移（精度1ms，可用约278年）
//	12位  机器ID（支持4096个节点）
//	 8位  序列号（每毫秒最多256个ID）
const (
	epoch2 = int64(1704067200000) // 2024-01-01 00:00:00 UTC（毫秒）

	machineBits2  = 12
	sequenceBits2 = 8

	maxMachineID2 = -1 ^ (-1 << machineBits2)  // 4095
	maxSequence2  = -1 ^ (-1 << sequenceBits2) // 255

	timeShift2    = machineBits2 + sequenceBits2 // 20
	machineShift2 = sequenceBits2                // 8
)

// Snowflake2 压缩序列号版本：序列号8位，时间戳43位，可用约278年
type Snowflake2 struct {
	mu        sync.Mutex
	lastStamp int64
	machineID int64
	sequence  int64
}

func NewSnowflake2(machineID int64) (*Snowflake2, error) {
	if machineID < 0 || machineID > maxMachineID2 {
		return nil, errors.New("machineID out of range [0, 4095]")
	}
	return &Snowflake2{machineID: machineID}, nil
}

func (s *Snowflake2) MachineID() int64 { return s.machineID }

func (s *Snowflake2) NextID() (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tick := currentTick2()

	if tick < s.lastStamp {
		tick = s.waitNextTick(s.lastStamp)
	}

	if tick == s.lastStamp {
		s.sequence = (s.sequence + 1) & maxSequence2
		if s.sequence == 0 {
			tick = s.waitNextTick(tick)
		}
	} else {
		s.sequence = 0
	}

	s.lastStamp = tick
	return tick<<timeShift2 | s.machineID<<machineShift2 | s.sequence, nil
}

func (s *Snowflake2) waitNextTick(last int64) int64 {
	t := currentTick2()
	for t <= last {
		t = currentTick2()
	}
	return t
}

func currentTick2() int64 {
	return time.Now().UnixMilli() - epoch2
}
