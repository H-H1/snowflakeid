package main

import (
	"fmt"
	"runtime"

	"github.com/shirou/gopsutil/v3/cpu"
)

func main() {
	physical, _ := cpu.Counts(false) // 物理核心数 / physical core count
	logical, _ := cpu.Counts(true)   // 逻辑核心数（含超线程）/ logical core count (includes hyper-threads)
	fmt.Println("物理核心 / Physical cores:", physical)
	fmt.Println("逻辑核心 / Logical cores: ", logical)
	fmt.Println("NumCPU:                  ", runtime.NumCPU())
	fmt.Println("GOMAXPROCS:              ", runtime.GOMAXPROCS(0))
}
