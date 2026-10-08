package main

// snowflakeid 命令行工具：生成与反解分布式 ID。
// 安装 / Install:
//
//	go install github.com/H-H1/snowflakeid@latest
//
// snowflakeid CLI: generate and decode distributed IDs.
import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"

	snowflakeid "github.com/H-H1/snowflakeid/snowflakeid"
)

// epochMs 与库内各版本一致：2024-01-01 00:00:00 UTC（毫秒）
// epochMs matches the library: 2024-01-01 00:00:00 UTC (milliseconds).
const epochMs = int64(1704067200000)

// layout 单个版本的位布局参数
// layout holds the bit-layout parameters of one version.
type layout struct {
	timeShift    int64 // 时间戳左移位数 / timestamp left-shift
	machineShift int64 // 机器ID左移位数 / machine ID left-shift
	machineMask  int64 // 机器ID掩码 / machine ID mask
	seqMask      int64 // 序列号掩码 / sequence mask
}

// layouts v1~v4 位布局（与库内常量一致）
// layouts for v1–v4 (mirrors the library constants).
var layouts = map[int]layout{
	1: {timeShift: 23, machineShift: 11, machineMask: 0xFFF, seqMask: 0x7FF},
	2: {timeShift: 20, machineShift: 8, machineMask: 0xFFF, seqMask: 0xFF},
	3: {timeShift: 21, machineShift: 9, machineMask: 0xFFF, seqMask: 0x1FF},
	4: {timeShift: 22, machineShift: 12, machineMask: 0x3FF, seqMask: 0xFFF},
}

func usage() {
	fmt.Fprint(os.Stderr, `snowflakeid 命令行工具 / CLI

用法 / Usage:
  snowflakeid [-v N] [-n N] [-pool]      生成 ID / generate IDs
  snowflakeid explain [-v N] <id>        反解 ID / decode an ID

选项 / Flags:
  -v int    位布局版本 1-4 / bit layout version 1-4 (default 1)
  -n int    生成数量 / count (default 1)
  -pool     使用分片池 / use the shard pool

示例 / Examples:
  snowflakeid                       v1 生成 1 个 / one v1 ID
  snowflakeid -v 4 -n 5              v4 生成 5 个 / five v4 IDs
  snowflakeid -v 4 -pool -n 8        v4 分片池生成 8 个 / eight via shard pool
  snowflakeid explain -v 4 12345678901234567
`)
}

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "explain":
			explainCmd(os.Args[2:])
			return
		case "-h", "-help", "--help", "help":
			usage()
			return
		}
	}
	genCmd(os.Args[1:])
}

// genCmd 生成模式
// genCmd is the generate mode.
func genCmd(args []string) {
	fs := flag.NewFlagSet("generate", flag.ExitOnError)
	ver := fs.Int("v", 1, "位布局版本 1-4 / layout version 1-4")
	n := fs.Int("n", 1, "生成数量 / count")
	pool := fs.Bool("pool", false, "使用分片池 / use the shard pool")
	fs.Parse(args)

	if err := validateVer(*ver); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if *n < 1 {
		fmt.Fprintln(os.Stderr, "-n must be >= 1")
		os.Exit(2)
	}

	// machineID 统一经 v1 Auto 派生（各版本 epoch/机器ID位宽不同，但取值范围兼容）
	// Derive the machine ID once via v1 Auto (valid range for every version).
	base, err := snowflakeid.NewSnowflakeAuto()
	if err != nil {
		fmt.Fprintln(os.Stderr, "derive machineID:", err)
		os.Exit(1)
	}
	mid := base.MachineID()

	var next func(i int) (int64, error)
	if *pool {
		next, err = poolGen(*ver, mid)
	} else {
		next, err = singleGen(*ver, mid)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	for i := range *n {
		id, err := next(i)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println(id)
	}
}

// singleGen 返回单实例发号函数
// singleGen returns a single-instance generator.
func singleGen(ver int, mid int64) (func(int) (int64, error), error) {
	switch ver {
	case 1:
		s, err := snowflakeid.NewSnowflake(mid)
		if err != nil {
			return nil, err
		}
		return func(int) (int64, error) { return s.NextID() }, nil
	case 2:
		s, err := snowflakeid.NewSnowflake2(mid)
		if err != nil {
			return nil, err
		}
		return func(int) (int64, error) { return s.NextID() }, nil
	case 3:
		s, err := snowflakeid.NewSnowflake3(mid)
		if err != nil {
			return nil, err
		}
		return func(int) (int64, error) { return s.NextID() }, nil
	case 4:
		s, err := snowflakeid.NewSnowflake4(mid & 0x3FF)
		if err != nil {
			return nil, err
		}
		return func(int) (int64, error) { return s.NextID() }, nil
	}
	return nil, fmt.Errorf("unsupported version %d", ver)
}

// poolGen 返回分片池发号函数（idx 任意，池内部取模）
// poolGen returns a shard-pool generator (any idx — the pool mods internally).
func poolGen(ver int, mid int64) (func(int) (int64, error), error) {
	switch ver {
	case 1:
		p, err := snowflakeid.NewShardPool(mid)
		if err != nil {
			return nil, err
		}
		return func(i int) (int64, error) { return p.NextID(int64(i)) }, nil
	case 2:
		p, err := snowflakeid.NewShardPool2(mid)
		if err != nil {
			return nil, err
		}
		return func(i int) (int64, error) { return p.NextID(int64(i)) }, nil
	case 3:
		p, err := snowflakeid.NewShardPool3(mid)
		if err != nil {
			return nil, err
		}
		return func(i int) (int64, error) { return p.NextID(int64(i)) }, nil
	case 4:
		p, err := snowflakeid.NewShardPool4(mid & 0x3FF)
		if err != nil {
			return nil, err
		}
		return func(i int) (int64, error) { return p.NextID(int64(i)) }, nil
	}
	return nil, fmt.Errorf("unsupported version %d", ver)
}

// explainCmd 反解模式：把 ID 拆回时间/机器ID/序列号
// explainCmd is the decode mode: split an ID back into time/machine/sequence.
func explainCmd(args []string) {
	fs := flag.NewFlagSet("explain", flag.ExitOnError)
	ver := fs.Int("v", 1, "位布局版本 1-4 / layout version 1-4")
	fs.Parse(args)

	if err := validateVer(*ver); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if fs.NArg() != 1 {
		usage()
		os.Exit(2)
	}
	id, err := strconv.ParseInt(fs.Arg(0), 10, 64)
	if err != nil {
		fmt.Fprintln(os.Stderr, "invalid id:", err)
		os.Exit(2)
	}

	l := layouts[*ver]
	ts := (id >> l.timeShift) + epochMs
	machine := (id >> l.machineShift) & l.machineMask
	seq := id & l.seqMask

	fmt.Printf("ID:            %d\n", id)
	fmt.Printf("版本 / ver:    v%d\n", *ver)
	fmt.Printf("时间 / time:   %s\n", time.UnixMilli(ts).Format("2006-01-02 15:04:05.000 -07:00"))
	fmt.Printf("机器 / machine: %d\n", machine)
	fmt.Printf("序列 / seq:    %d\n", seq)
}

func validateVer(v int) error {
	if v < 1 || v > 4 {
		return fmt.Errorf("-v must be 1-4, got %d", v)
	}
	return nil
}
