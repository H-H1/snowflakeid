package snowflakeid

//nolint

// ShardPool2 本项目2的分片池（序列号8位，时间戳43位，~278年）
type ShardPool2 struct {
	shards []*Snowflake2
	size   int64
}

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

func (p *ShardPool2) NextID(idx int64) (int64, error) {
	return p.shards[idx%p.size].NextID()
}

func (p *ShardPool2) Size() int64 { return p.size }

// ShardPool3 本项目3的分片池（序列号9位，时间戳42位，~139年）
type ShardPool3 struct {
	shards []*Snowflake3
	size   int64
}

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

func (p *ShardPool3) NextID(idx int64) (int64, error) {
	return p.shards[idx%p.size].NextID()
}

func (p *ShardPool3) Size() int64 { return p.size }
