package snowflakeid

//nolint

// ShardPool2 本项目2的分片池（序列号8位，时间戳43位，~278年）
// ShardPool2 is the shard pool for Snowflake2 (8-bit sequence, 43-bit timestamp, ~278 years).
type ShardPool2 struct {
	shards []*Snowflake2
	size   int64 // 分片数（等于物理核心数）/ shard count (equals physical core count)
}

// NewShardPool2 创建 Snowflake2 分片池，baseID 为机器ID基础值。
// NewShardPool2 creates a shard pool for Snowflake2; baseID is the base machine ID.
func NewShardPool2(baseID int64) (*ShardPool2, error) {
	n := physicalCores()
	shards := make([]*Snowflake2, n)
	for i := int64(0); i < n; i++ {
		sf, err := NewSnowflake2((baseID + i) & maxMachineID2)
		if err != nil {
			return nil, err
		}
		shards[i] = sf
	}
	return &ShardPool2{shards: shards, size: n}, nil
}

// NextID 根据调用方索引路由到对应分片生成 ID。
// NextID routes to the shard at idx % size and generates an ID.
func (p *ShardPool2) NextID(idx int64) (int64, error) {
	return p.shards[idx%p.size].NextID()
}

// Size 返回分片数（等于物理核心数）。
// Size returns the number of shards (equals physical core count).
func (p *ShardPool2) Size() int64 { return p.size }

// ShardPool3 本项目3的分片池（序列号9位，时间戳42位，~139年）
// ShardPool3 is the shard pool for Snowflake3 (9-bit sequence, 42-bit timestamp, ~139 years).
type ShardPool3 struct {
	shards []*Snowflake3
	size   int64 // 分片数（等于物理核心数）/ shard count (equals physical core count)
}

// NewShardPool3 创建 Snowflake3 分片池，baseID 为机器ID基础值。
// NewShardPool3 creates a shard pool for Snowflake3; baseID is the base machine ID.
func NewShardPool3(baseID int64) (*ShardPool3, error) {
	n := physicalCores()
	shards := make([]*Snowflake3, n)
	for i := int64(0); i < n; i++ {
		sf, err := NewSnowflake3((baseID + i) & maxMachineID3)
		if err != nil {
			return nil, err
		}
		shards[i] = sf
	}
	return &ShardPool3{shards: shards, size: n}, nil
}

// NextID 根据调用方索引路由到对应分片生成 ID。
// NextID routes to the shard at idx % size and generates an ID.
func (p *ShardPool3) NextID(idx int64) (int64, error) {
	return p.shards[idx%p.size].NextID()
}

// Size 返回分片数（等于物理核心数）。
// Size returns the number of shards (equals physical core count).
func (p *ShardPool3) Size() int64 { return p.size }
