package snowflakeid

import (
	"sync/atomic"
	"testing"

	bwsnow "github.com/bwmarrin/snowflake"
	"github.com/sony/sonyflake"
)

// ── 初始化共享实例 ────────────────────────────────────────────

var (
	benchSF, _   = NewSnowflake(1)
	benchPool, _ = NewShardPool(1)
	benchCompat  = NewSonyflakeCompat(1)
	benchBW, _   = bwsnow.NewNode(1)
	benchSony    = sonyflake.NewSonyflake(sonyflake.Settings{
		MachineID: func() (uint16, error) { return 1, nil },
	})
)

// ── 单次调用延迟（串行）────────────────────────────────────────

// BenchmarkSnowflake_NextID 本项目单实例，串行调用延迟
func BenchmarkSnowflake_NextID(b *testing.B) {
	for b.Loop() {
		benchSF.NextID()
	}
}

// BenchmarkShardPool_NextID 本项目分片池，串行调用延迟
func BenchmarkShardPool_NextID(b *testing.B) {
	for b.Loop() {
		benchPool.NextID(0)
	}
}

// BenchmarkSonyCompat_NextID SonyCompat（同位+sleep），串行调用延迟
func BenchmarkSonyCompat_NextID(b *testing.B) {
	for b.Loop() {
		benchCompat.NextID()
	}
}

// BenchmarkBwmarrin_NextID bwmarrin/snowflake，串行调用延迟
func BenchmarkBwmarrin_NextID(b *testing.B) {
	for b.Loop() {
		benchBW.Generate()
	}
}

// BenchmarkSony_NextID sony/sonyflake，串行调用延迟
func BenchmarkSony_NextID(b *testing.B) {
	for b.Loop() {
		benchSony.NextID()
	}
}

// ── 并发吞吐（-cpu 核心数）────────────────────────────────────

// BenchmarkSnowflake_NextID_Parallel 本项目单实例，并发吞吐
func BenchmarkSnowflake_NextID_Parallel(b *testing.B) {
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			benchSF.NextID()
		}
	})
}

// BenchmarkShardPool_NextID_Parallel 本项目分片池，并发吞吐
func BenchmarkShardPool_NextID_Parallel(b *testing.B) {
	var idx atomic.Int64
	b.RunParallel(func(pb *testing.PB) {
		i := idx.Add(1)
		for pb.Next() {
			benchPool.NextID(i)
		}
	})
}

// BenchmarkSonyCompat_NextID_Parallel SonyCompat，并发吞吐
func BenchmarkSonyCompat_NextID_Parallel(b *testing.B) {
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			benchCompat.NextID()
		}
	})
}

// BenchmarkBwmarrin_NextID_Parallel bwmarrin/snowflake，并发吞吐
func BenchmarkBwmarrin_NextID_Parallel(b *testing.B) {
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			benchBW.Generate()
		}
	})
}

// BenchmarkSony_NextID_Parallel sony/sonyflake，并发吞吐
func BenchmarkSony_NextID_Parallel(b *testing.B) {
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			benchSony.NextID()
		}
	})
}

// ── 辅助函数性能 ──────────────────────────────────────────────

// BenchmarkCurrentTick time.Now().UnixMicro() 调用开销
func BenchmarkCurrentTick(b *testing.B) {
	for b.Loop() {
		currentTick()
	}
}

// BenchmarkGetMachineID MAC地址派生机器ID开销
func BenchmarkGetMachineID(b *testing.B) {
	for b.Loop() {
		getMachineID()
	}
}
