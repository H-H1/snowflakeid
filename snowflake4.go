package snowflakeid

import (
	"errors"
	"sync"
	"time"
)

// 位布局（共63位有效位，与 Twitter 原版/bwmarrin 一致）:
// Bit layout (63 effective bits total, identical to Twitter original / bwmarrin):
//
//	 1位  符号位 (0)                          | 1 bit   sign bit (0, ensures positive int64)
//	41位  毫秒偏移（精度1ms，可用约69年）       | 41 bits ms timestamp offset (~69 years from epoch)
//	10位  机器ID（支持1024个节点）              | 10 bits machine ID (up to 1024 nodes)
//	12位  序列号（每毫秒最多4096个ID）          | 12 bits sequence (up to 4096 IDs per ms)
const (
	epoch4 = int64(1704067200000) // 2024-01-01 00:00:00 UTC（毫秒 / milliseconds）

	machineBits4  = 10
	sequenceBits4 = 12

	maxMachineID4 = -1 ^ (-1 << machineBits4)  // 1023
	maxSequence4  = -1 ^ (-1 << sequenceBits4) // 4095

	timeShift4    = machineBits4 + sequenceBits4 // 22 — 时间戳左移位数 / timestamp left-shift
	machineShift4 = sequenceBits4                // 12 — 机器ID左移位数 / machine ID left-shift
)

// Snowflake4 大序列号版本：序列号12位（Twitter布局），每ms最多4096个ID，时间戳41位（~69年）
// Snowflake4 is the Twitter-layout variant:
// 12-bit sequence (4096/ms), 10-bit machine ID (1024 nodes), 41-bit timestamp (~69 years).
type Snowflake4 struct {
	mu        sync.Mutex
	lastStamp int64 // 上次生成ID的时间戳 / timestamp of the last generated ID
	machineID int64 // 机器ID / machine ID
	sequence  int64 // 当前毫秒内的序列号 / sequence counter within the current millisecond
}

// NewSnowflake4 创建 Snowflake4 生成器，machineID 范围 [0, 1023]
// NewSnowflake4 creates a Snowflake4 generator; machineID must be in [0, 1023].
func NewSnowflake4(machineID int64) (*Snowflake4, error) {
	if machineID < 0 || machineID > maxMachineID4 {
		return nil, errors.New("machineID out of range [0, 1023]")
	}
	return &Snowflake4{machineID: machineID}, nil
}

// NewSnowflake4Auto 自动从本机MAC地址、进程PID与启动时间派生 machineID（低10位）
// NewSnowflake4Auto derives the machineID automatically from the local MAC address, process ID and startup time (low 10 bits).
func NewSnowflake4Auto() (*Snowflake4, error) {
	mid, err := getMachineID4()
	if err != nil {
		return nil, err
	}
	return NewSnowflake4(mid)
}

// MachineID 返回当前实例使用的机器ID
// MachineID returns the machine ID used by this instance.
func (s *Snowflake4) MachineID() int64 { return s.machineID }

// NextID 生成下一个唯一ID
// NextID generates the next unique ID.
func (s *Snowflake4) NextID() (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tick := currentTick4()

	if tick < s.lastStamp {
		// 时钟回拨时等待追上
		// Clock rolled back; spin until it catches up.
		tick = s.waitNextTick(s.lastStamp)
	}

	if tick == s.lastStamp {
		s.sequence = (s.sequence + 1) & maxSequence4
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
	return tick<<timeShift4 | s.machineID<<machineShift4 | s.sequence, nil
}

// waitNextTick 自旋直到时钟超过 last
// waitNextTick spins until the current tick advances past last.
func (s *Snowflake4) waitNextTick(last int64) int64 {
	t := currentTick4()
	for t <= last {
		t = currentTick4()
	}
	return t
}

// currentTick4 返回距 epoch4 的毫秒偏移
// currentTick4 returns milliseconds elapsed since epoch4.
func currentTick4() int64 {
	return time.Now().UnixMilli() - epoch4
}

// getMachineID4 取 MAC低12位^PID^启动纳秒 的混合值，再截取低10位作为机器ID
// getMachineID4 derives the machine ID from the MAC/PID/startup-time mix
// (low 12 bits), further truncated to the low 10 bits.
func getMachineID4() (int64, error) {
	mid, err := getMachineID()
	if err != nil {
		return 0, err
	}
	return mid & maxMachineID4, nil
}
