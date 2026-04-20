package snowflakeid

import (
	"errors"
	"sync"
	"time"
)

// 位布局（共63位有效位）:
//
//	 1位  符号位 (0)
//	42位  毫秒偏移（精度1ms，可用约139年）
//	12位  机器ID（支持4096个节点）
//	 9位  序列号（每毫秒最多512个ID）
const (
	epoch3 = int64(1704067200000) // 2024-01-01 00:00:00 UTC（毫秒）

	machineBits3  = 12
	sequenceBits3 = 9

	maxMachineID3 = -1 ^ (-1 << machineBits3)  // 4095
	maxSequence3  = -1 ^ (-1 << sequenceBits3) // 511

	timeShift3    = machineBits3 + sequenceBits3 // 21
	machineShift3 = sequenceBits3                // 9
)

// Snowflake3 序列号9位版本：时间戳42位，可用约139年，每ms最多512个ID
type Snowflake3 struct {
	mu        sync.Mutex
	lastStamp int64
	machineID int64
	sequence  int64
}

func NewSnowflake3(machineID int64) (*Snowflake3, error) {
	if machineID < 0 || machineID > maxMachineID3 {
		return nil, errors.New("machineID out of range [0, 4095]")
	}
	return &Snowflake3{machineID: machineID}, nil
}

func (s *Snowflake3) MachineID() int64 { return s.machineID }

func (s *Snowflake3) NextID() (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tick := currentTick3()

	if tick < s.lastStamp {
		tick = s.waitNextTick(s.lastStamp)
	}

	if tick == s.lastStamp {
		s.sequence = (s.sequence + 1) & maxSequence3
		if s.sequence == 0 {
			tick = s.waitNextTick(tick)
		}
	} else {
		s.sequence = 0
	}

	s.lastStamp = tick
	return tick<<timeShift3 | s.machineID<<machineShift3 | s.sequence, nil
}

func (s *Snowflake3) waitNextTick(last int64) int64 {
	t := currentTick3()
	for t <= last {
		t = currentTick3()
	}
	return t
}

func currentTick3() int64 {
	return time.Now().UnixMilli() - epoch3
}
