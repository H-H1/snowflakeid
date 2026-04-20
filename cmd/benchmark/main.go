package main

import (
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	snowflakeid "github.com/H-H1/snowflakeid"
	bwsnow "github.com/bwmarrin/snowflake"
	"github.com/google/uuid"
	"github.com/oklog/ulid/v2"
	"github.com/sony/sonyflake"
	_ "go.uber.org/automaxprocs"
)

func benchmark(name string, num int, genFn func(idx int) (int64, error)) {
	var (
		wg    sync.WaitGroup
		total atomic.Uint64
	)
	results := make([][]int64, num)
	start := time.Now()
	for i := range num {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			local := make([]int64, 0, num)
			for range num {
				id, err := genFn(idx)
				if err != nil {
					return
				}
				local = append(local, id)
				total.Add(1)
			}
			results[idx] = local
		}(i)
	}
	wg.Wait()
	elapsed := time.Since(start)
	ids := make(map[int64]struct{}, num*num)
	for _, batch := range results {
		for _, id := range batch {
			ids[id] = struct{}{}
		}
	}
	printResult(name, total.Load(), uint64(len(ids)), elapsed)
}

func benchmarkStr(name string, num int, genFn func(idx int) string) {
	var (
		wg    sync.WaitGroup
		total atomic.Uint64
	)
	results := make([][]string, num)
	start := time.Now()
	for i := range num {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			local := make([]string, 0, num)
			for range num {
				local = append(local, genFn(idx))
				total.Add(1)
			}
			results[idx] = local
		}(i)
	}
	wg.Wait()
	elapsed := time.Since(start)
	ids := make(map[string]struct{}, num*num)
	for _, batch := range results {
		for _, id := range batch {
			ids[id] = struct{}{}
		}
	}
	printResult(name, total.Load(), uint64(len(ids)), elapsed)
}

func printResult(name string, total, unique uint64, elapsed time.Duration) {
	dup := " \u2713 \u65e0\u91cd\u590d"
	if total != unique {
		dup = fmt.Sprintf(" \u2717 \u91cd\u590d%d\u4e2a", total-unique)
	}
	fmt.Printf("%-20s \u603b\u6570:%-8d \u8017\u65f6:%-12v \u5438\u5410:%10.0f ID/s%s\n",
		"["+name+"]", total, elapsed.Round(time.Millisecond), float64(total)/elapsed.Seconds(), dup)
}

func main() {
	const num = 1000

	node, err := snowflakeid.NewSnowflakeAuto()
	if err != nil {
		panic(err)
	}
	pool, err := snowflakeid.NewShardPool(node.MachineID())
	if err != nil {
		panic(err)
	}

	bwNode, err := bwsnow.NewNode(1)
	if err != nil {
		panic(err)
	}

	sony := sonyflake.NewSonyflake(sonyflake.Settings{})
	if sony == nil {
		panic("sonyflake init failed")
	}

	fmt.Printf("\u672c\u9879\u76ee\u673a\u5668ID: %d  \u5206\u7247\u6570: %d\n\n", node.MachineID(), pool.Size())
	fmt.Printf("%-20s %-8s %-12s %-18s\n", "\u65b9\u6848", "\u603b\u6570", "\u8017\u65f6", "\u5438\u5410(ID/s)")
	fmt.Println("\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500")

	var counter atomic.Int64
	benchmark("\u539f\u5b50\u81ea\u589e(\u57fa\u51c6)", num, func(_ int) (int64, error) {
		return counter.Add(1), nil
	})
	fmt.Println()

	benchmark("\u672c\u9879\u76ee \u5355\u5b9e\u4f8b", num, func(_ int) (int64, error) {
		return node.NextID()
	})
	benchmark("\u672c\u9879\u76ee \u5206\u7247\u6c60", num, func(idx int) (int64, error) {
		return pool.NextID(int64(idx))
	})
	fmt.Println()

	sf2, err := snowflakeid.NewSnowflake2(node.MachineID())
	if err != nil {
		panic(err)
	}
	pool2, err := snowflakeid.NewShardPool2(node.MachineID())
	if err != nil {
		panic(err)
	}
	benchmark("\u672c\u9879\u76ee2 \u5355\u5b9e\u4f8b", num, func(_ int) (int64, error) {
		return sf2.NextID()
	})
	benchmark("\u672c\u9879\u76ee2 \u5206\u7247\u6c60", num, func(idx int) (int64, error) {
		return pool2.NextID(int64(idx))
	})

	sf3, err := snowflakeid.NewSnowflake3(node.MachineID())
	if err != nil {
		panic(err)
	}
	pool3, err := snowflakeid.NewShardPool3(node.MachineID())
	if err != nil {
		panic(err)
	}
	benchmark("\u672c\u9879\u76ee3 \u5355\u5b9e\u4f8b", num, func(_ int) (int64, error) {
		return sf3.NextID()
	})
	benchmark("\u672c\u9879\u76ee3 \u5206\u7247\u6c60", num, func(idx int) (int64, error) {
		return pool3.NextID(int64(idx))
	})
	fmt.Println()

	benchmark("bwmarrin/snowflake", num, func(_ int) (int64, error) {
		return bwNode.Generate().Int64(), nil
	})
	benchmark("sony/sonyflake", num, func(_ int) (int64, error) {
		id, err := sony.NextID()
		return int64(id), err
	})

	sfCompat := snowflakeid.NewSonyflakeCompat(node.MachineID())
	benchmark("SonyCompat(\u540c\u4f4d+sleep)", num, func(_ int) (int64, error) {
		return sfCompat.NextID()
	})
	fmt.Println()

	benchmarkStr("UUID v4", num, func(_ int) string {
		return uuid.New().String()
	})

	ulidEntropies := make([]*ulid.MonotonicEntropy, num)
	for i := range num {
		ulidEntropies[i] = ulid.Monotonic(rand.New(rand.NewSource(time.Now().UnixNano()+int64(i))), 0)
	}
	benchmarkStr("ULID", num, func(idx int) string {
		return ulid.MustNew(ulid.Timestamp(time.Now()), ulidEntropies[idx]).String()
	})
}
