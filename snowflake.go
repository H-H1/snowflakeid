package snowflakeid

import (
	"errors"
	"net"
	"sync"
	"time"
)

// 位布局（共63位有效位）:
//
//	 1位  符号位 (0)
//	40位  毫秒偏移（精度1ms，可用约34年）
//	12位  机器ID（支持4096个节点）
//	11位  序列号（每毫秒最多2048个ID）
const (
	epoch = int64(1704067200000) // 2024-01-01 00:00:00 UTC（毫秒）

	machineBits  = 12
	sequenceBits = 11

	maxMachineID = -1 ^ (-1 << machineBits)  // 4095
	maxSequence  = -1 ^ (-1 << sequenceBits) // 2047

	timeShift    = machineBits + sequenceBits // 23
	machineShift = sequenceBits               // 11
)

// Snowflake 分布式ID生成器（毫秒精度）
type Snowflake struct {
	mu        sync.Mutex
	lastStamp int64
	machineID int64
	sequence  int64
}

// NewSnowflake 创建生成器，machineID 范围 [0, 4095]
func NewSnowflake(machineID int64) (*Snowflake, error) {
	if machineID < 0 || machineID > maxMachineID {
		return nil, errors.New("machineID out of range [0, 4095]")
	}
	return &Snowflake{machineID: machineID}, nil
}

// NewSnowflakeAuto 自动从本机MAC地址派生 machineID
func NewSnowflakeAuto() (*Snowflake, error) {
	mid, err := getMachineID()
	if err != nil {
		return nil, err
	}
	return NewSnowflake(mid)
}

// MachineID 返回当前实例使用的机器ID
func (s *Snowflake) MachineID() int64 {
	return s.machineID
}

// NextID 生成下一个唯一ID
func (s *Snowflake) NextID() (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tick := currentTick()

	if tick < s.lastStamp {
		// 时钟回拨时等待追上
		tick = s.waitNextTick(s.lastStamp)
	}

	if tick == s.lastStamp {
		s.sequence = (s.sequence + 1) & maxSequence
		if s.sequence == 0 {
			// 当前 tick 序列号耗尽，等待下一个 tick
			tick = s.waitNextTick(tick)
		}
	} else {
		s.sequence = 0
	}

	s.lastStamp = tick

	id := tick<<timeShift | s.machineID<<machineShift | s.sequence
	return id, nil
}

func (s *Snowflake) waitNextTick(last int64) int64 {
	t := currentTick()
	for t <= last {
		t = currentTick()
	}
	return t
}

func currentTick() int64 {
	return time.Now().UnixMilli() - epoch
}

// getMachineID 取第一块非回环网卡 MAC 地址的低12位作为机器ID
func getMachineID() (int64, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return 0, err
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if len(iface.HardwareAddr) >= 6 {
			mac := iface.HardwareAddr
			// 取MAC最后两字节，截取低12位
			val := int64(mac[len(mac)-2])<<8 | int64(mac[len(mac)-1])
			return val & maxMachineID, nil
		}
	}
	return 0, errors.New("no valid network interface found")
}
