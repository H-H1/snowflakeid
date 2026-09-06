package snowflakeid

import (
	"errors"
	"net"
	"os"
	"sync"
	"time"
)

// 位布局（共63位有效位）:
// Bit layout (63 effective bits total):
//
//	 1位  符号位 (0)                          | 1 bit   sign bit (0, ensures positive int64)
//	40位  毫秒偏移（精度1ms，可用约34年）       | 40 bits ms timestamp offset (~34 years from epoch)
//	12位  机器ID（支持4096个节点）              | 12 bits machine ID (up to 4096 nodes)
//	11位  序列号（每毫秒最多2048个ID）          | 11 bits sequence (up to 2048 IDs per ms)
const (
	epoch = int64(1704067200000) // 2024-01-01 00:00:00 UTC（毫秒 / milliseconds）

	machineBits  = 12
	sequenceBits = 11

	maxMachineID = -1 ^ (-1 << machineBits)  // 4095
	maxSequence  = -1 ^ (-1 << sequenceBits) // 2047

	timeShift    = machineBits + sequenceBits // 23 — 时间戳左移位数 / timestamp left-shift
	machineShift = sequenceBits               // 11 — 机器ID左移位数 / machine ID left-shift
)

// Snowflake 分布式ID生成器（毫秒精度）
// Snowflake is a distributed ID generator with millisecond precision.
type Snowflake struct {
	mu        sync.Mutex
	lastStamp int64 // 上次生成ID的时间戳 / timestamp of the last generated ID
	machineID int64 // 机器ID / machine ID
	sequence  int64 // 当前毫秒内的序列号 / sequence counter within the current millisecond
}

// NewSnowflake 创建生成器，machineID 范围 [0, 4095]
// NewSnowflake creates a generator; machineID must be in [0, 4095].
func NewSnowflake(machineID int64) (*Snowflake, error) {
	if machineID < 0 || machineID > maxMachineID {
		return nil, errors.New("machineID out of range [0, 4095]")
	}
	return &Snowflake{machineID: machineID}, nil
}

// NewSnowflakeAuto 自动从本机MAC地址与进程PID派生 machineID
// NewSnowflakeAuto derives the machineID automatically from the local MAC address and process ID.
func NewSnowflakeAuto() (*Snowflake, error) {
	mid, err := getMachineID()
	if err != nil {
		return nil, err
	}
	return NewSnowflake(mid)
}

// MachineID 返回当前实例使用的机器ID
// MachineID returns the machine ID used by this instance.
func (s *Snowflake) MachineID() int64 {
	return s.machineID
}

// NextID 生成下一个唯一ID
// NextID generates the next unique ID.
func (s *Snowflake) NextID() (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tick := currentTick()

	if tick < s.lastStamp {
		// 时钟回拨时等待追上
		// Clock rolled back; spin until it catches up.
		tick = s.waitNextTick(s.lastStamp)
	}

	if tick == s.lastStamp {
		s.sequence = (s.sequence + 1) & maxSequence
		if s.sequence == 0 {
			// 当前 tick 序列号耗尽，等待下一个 tick
			// Sequence exhausted for this tick; spin to the next one.
			tick = s.waitNextTick(tick)
		}
	} else {
		// 新的毫秒，序列号归零
		// New millisecond — reset sequence.
		s.sequence = 0
	}

	s.lastStamp = tick

	id := tick<<timeShift | s.machineID<<machineShift | s.sequence
	return id, nil
}

// waitNextTick 自旋直到时钟超过 last
// waitNextTick spins until the current tick advances past last.
func (s *Snowflake) waitNextTick(last int64) int64 {
	t := currentTick()
	for t <= last {
		t = currentTick()
	}
	return t
}

// currentTick 返回距 epoch 的毫秒偏移
// currentTick returns milliseconds elapsed since epoch.
func currentTick() int64 {
	return time.Now().UnixMilli() - epoch
}

// getMachineID 用 MAC 低12位异或进程PID派生机器ID：
// MAC 区分不同机器，PID 区分同一机器上的多个进程。
// XOR 对固定 MAC 是双射，同机不同进程必然得到不同 machineID，
// 消除"同机多进程"的确定性冲突。代价：machineID 随进程重启而变化，不可持久化。
// getMachineID derives the machine ID from the low 12 bits of the first
// non-loopback NIC's MAC address, XORed with the process ID:
// the MAC separates machines, the PID separates processes on the same machine.
// XOR with a fixed MAC is a bijection, so distinct processes on one machine
// always get distinct machine IDs. Note: the ID changes across restarts —
// do not persist it.
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
			// MAC末两字节（机器维度）异或PID（进程维度），截取低12位
			// Last two MAC bytes (machine) XOR the PID (process), keep low 12 bits.
			val := int64(mac[len(mac)-2])<<8 | int64(mac[len(mac)-1])
			return (val ^ int64(os.Getpid())) & maxMachineID, nil
		}
	}
	return 0, errors.New("no valid network interface found")
}
