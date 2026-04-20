package main

import (
	"fmt"
	"runtime"

	"github.com/shirou/gopsutil/v3/cpu"
)

func main() {
	physical, _ := cpu.Counts(false)
	logical, _ := cpu.Counts(true)
	fmt.Println("物理核心:", physical)
	fmt.Println("逻辑核心:", logical)
	fmt.Println("NumCPU: ", runtime.NumCPU())
	fmt.Println("GOMAXPROCS:", runtime.GOMAXPROCS(0))
}
