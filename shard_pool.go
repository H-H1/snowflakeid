package snowflakeid

import (
	"runtime"

	"github.com/shirou/gopsutil/v3/cpu"
)

// physicalCores 返回物理核心数，失败时降级到 GOMAXPROCS
func physicalCores() int64 {
	n, err := cpu.Counts(false)
	if err != nil || n <= 0 {
		return int64(runtime.GOMAXPROCS(0))
	}
	return int64(n)
}

// ShardPool 按物理核心数分片的 Snowflake 池，每个分片独立加锁，消除竞争
// 物理核心数比逻辑核心数更准确，超线程共享执行单元，分片数对齐物理核心竞争最低
type ShardPool struct {
	shards []*Snowflake
	size   int64
}

// NewShardPool 创建分片池，baseID 为机器ID基础值，分片数等于物理核心数。
// 每个分片的 machineID = (baseID + shardIndex) & 0xFFF，保证全局唯一。
func NewShardPool(baseID int64) (*ShardPool, error) {
	n := physicalCores()
	shards := make([]*Snowflake, n)
	for i := int64(0); i < n; i++ {
		sf, err := NewSnowflake((baseID + i) & maxMachineID)
		if err != nil {
			return nil, err
		}
		shards[i] = sf
	}
	return &ShardPool{shards: shards, size: n}, nil
}

// NextID 根据调用方索引路由到对应分片生成 ID，idx 通常为 goroutine 编号。
func (p *ShardPool) NextID(idx int64) (int64, error) {
	return p.shards[idx%p.size].NextID()
}

// Size 返回分片数（等于物理核心数）。
func (p *ShardPool) Size() int64 { return p.size }
