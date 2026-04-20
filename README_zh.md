# 分布式 ID 生成器 &nbsp;|&nbsp; <a href="README.md">English</a>

基于 Snowflake 算法的高性能分布式 ID 生成器（Go 实现），提供三种位布局变体和分片池，实现近无锁吞吐。

---

## 三个版本对比

| 方案 | 时间戳位 | 时间精度 | 机器ID位 | 序列号位 | 时间跨度 | 单实例吞吐 | 分片池吞吐 |
|------|----------|----------|----------|----------|----------|------------|------------|
| v1（本项目） | 40 | 1ms | 12（4096节点） | 11（2048/ms） | ~34年 | ~89万/s | ~976万/s |
| v2 | 43 | 1ms | 12（4096节点） | 8（256/ms） | ~278年 | ~12万/s | ~102万/s |
| v3 | 42 | 1ms | 12（4096节点） | 9（512/ms） | ~139年 | ~25万/s | ~170万/s |
| bwmarrin/snowflake | 41 | 1ms | 10（1024节点） | 12（4096/ms） | ~69年 | ~400万/s | 无分片 |
| sony/sonyflake | 39 | 10ms | 16（65536节点） | 8（256/10ms） | ~174年 | ~2.5万/s | 无分片 |
| Twitter原版 | 41 | 1ms | 10（1024节点） | 12（4096/ms） | ~69年 | — | — |

三个版本机器ID均为12位（4096节点），总位数均为63位（int64去掉符号位）。
位数取舍固定：时间戳 + 机器ID + 序列号 = 63位。

---

## ID 结构（v1）

```
 63      62                    23        22          11        10          0
  |       |                     |         |           |         |          |
  0  [       40位 毫秒时间戳偏移   ] [  12位机器ID  ] [  11位序列号  ]
```

| 字段 | 位数 | 范围 | 说明 |
|------|------|------|------|
| 符号位 | 1 | 固定 0 | 保证 ID 为正整数 |
| 时间戳 | 40 | 0 ~ 2⁴⁰-1 | 距 epoch(2024-01-01) 的毫秒偏移，可用约 34 年 |
| 机器ID | 12 | 0 ~ 4095 | 支持 4096 个分布式节点 |
| 序列号 | 11 | 0 ~ 2047 | 同一毫秒内最多生成 2048 个 ID |

### 位拼装

```go
id := tick<<23 | machineID<<11 | sequence
```

高位是时间戳，ID 天然有序，时间越晚数值越大，可直接 `ORDER BY id` 代替 `ORDER BY created_at`。

### 时间跨度计算

```
2⁴⁰ - 1 = 1,099,511,627,775 ms  ≈  34.8 年（从 2024-01-01 起，到约 2058 年）
```

到期前将 epoch 改为更近的时间点即可续期。

---

## NextID 生成逻辑

```
调用 NextID()
      │
      ▼
  加锁 (sync.Mutex)
      │
      ▼
  tick = time.Now().UnixMilli() - epoch
      │
      ├─ tick < lastStamp ──► 时钟回拨，自旋等待 tick > lastStamp
      │
      ├─ tick == lastStamp ─► 同一毫秒
      │        │
      │        ▼
      │   sequence = (sequence + 1) & 0x7FF
      │        │
      │        └─ sequence == 0 ──► 序列号耗尽，自旋等待下一个 tick
      │
      └─ tick > lastStamp ──► 新的毫秒，sequence 归零
              │
              ▼
      lastStamp = tick
      id = tick<<23 | machineID<<11 | sequence
      解锁，返回 id
```

**时钟回拨**：NTP 同步或虚拟机漂移时时钟可能向后跳，自旋等待时钟追上，适合回拨幅度小的场景。

**序列号耗尽**：同一毫秒内第 2049 次调用时序列号归零，自旋等待进入下一毫秒。自旋期间持锁，其他 goroutine 全部阻塞，这是单实例高并发下的瓶颈。

---

## 分片池设计

### 问题根源

`sync.Mutex` 是全局串行点，N 个 goroutine 并发时同一时刻只有 1 个能执行，并发越高锁竞争越激烈。

### 解决方案：按 CPU 核心数分片

```
                    ┌─ shard[0]  machineID = base+0  独立锁
goroutine 0,8,16 ──►│
                    ├─ shard[1]  machineID = base+1  独立锁
goroutine 1,9,17 ──►│
                    ├─ shard[2]  machineID = base+2  独立锁
goroutine 2,10,18──►│
                    │  ...
                    └─ shard[N-1]  独立锁
```

每个分片是独立的 `Snowflake` 实例，有自己的锁、序列号、lastStamp，互不阻塞。

```go
shard     = idx % size
machineID = (baseID + shardIndex) & 0xFFF  // 每个分片 machineID 不同，保证全局唯一
```

分片数 = `runtime.NumCPU()`，与 `GOMAXPROCS` 对齐，每个 CPU 核心对应一个分片，锁竞争降到最低。

### 性能数据（4核）

| 场景 | 延迟 | 说明 |
|------|------|------|
| 单实例串行 | 343 ns/op | 无竞争下的锁基础开销 |
| 单实例并发 | 435 ns/op | 8 goroutine 竞争，排队等锁 |
| 分片池串行 | 353 ns/op | 多了取模，与单实例持平 |
| 分片池并发 | **50 ns/op** | 竞争分散，接近无锁，提升 **8.6×** |

---

## 位数取舍规律

```
时间戳位数每 +1 位 → 时间跨度翻倍，机器ID或序列号少1位
序列号每少 1 位   → 每ms吞吐减半，时间戳可多1位
机器ID每少 1 位   → 支持节点数减半，时间戳可多1位
```

---

## UUID v4 与 ULID 有序性对比

### UUID v4 — 无序

完全随机，128位，无时间信息。

```
生成顺序:
  [0] e2649244-8c88-4a46-8011-6e104351a0f4
  [1] a384de78-6503-460b-9a74-cee55201ffe5
  [2] 13942ecc-8133-4f05-8c07-77288c8e6a3e
  ...
排序后顺序完全不同
```

- 数据库 B-Tree 索引随机落点，页分裂频繁，写入性能差
- 无法通过 ID 范围推断时间范围
- 需要额外 `created_at` 字段排序

### ULID — 字符串有序

128位，高48位毫秒时间戳 + 低80位随机数，字典序即时间顺序。

```
生成顺序（间隔10ms）:
  [0] 01KP0FWTYQ1F7P10V3ET41KDN8  时间部分: 01KP0FWTYQ
  [1] 01KP0FWTZ2PWP7YCH7EXDCV84D  时间部分: 01KP0FWTZ2
  ...
生成顺序即有序: true
```

字符串有序，索引比 UUID 友好，但存储和比较开销是整数的 2 倍。

### Snowflake — 整数有序

```
id1 < id2  ⟺  id1 生成时间早于 id2
```

整数索引最紧凑，`ORDER BY id` 直接代替 `ORDER BY created_at`，可从 ID 反解生成时间。

### 三者综合对比

| | UUID v4 | ULID | Snowflake |
|---|---|---|---|
| 类型 | 字符串(36字符) | 字符串(26字符) | 整数(int64) |
| 有序性 | 无 | 字符串有序 | 整数有序 |
| 分布式 | 天然支持 | 天然支持 | 需机器ID协调 |
| 数据库索引 | 差 | 较好 | 最好 |
| 可反解时间 | 否 | 是 | 是 |
| 存储空间 | 16字节 | 16字节 | 8字节 |
| 吞吐 | ~1400万/s | ~2800万/s | ~200万/s（单实例）~1480万/s（分片池） |

---

## sleep 策略 vs 自旋策略

`SonyflakeCompat` 与本项目位布局相同，但序列号耗尽时用 `sleep` 替代自旋：

```go
// 自旋（本项目）：持锁忙等，其他 goroutine 全部阻塞
for t <= last { t = currentTick() }

// sleep（SonyCompat）：释放锁让出调度，其他 goroutine 可继续
s.elapsedTime++
time.Sleep(time.Duration(overtime) * time.Millisecond)
```

并发下 sleep 反而更快，原因：自旋持锁期间所有 goroutine 阻塞；sleep 释放锁后其他 goroutine 可继续生产，整体吞吐并行推进。

分片池从根本上消除了这个问题，竞争压力降到 1/N，序列号耗尽概率极低，两种策略差异消失。

---

## machineID 自动派生

```go
// 取第一块非回环网卡 MAC 地址的最后两字节，截取低 12 位
val := int64(mac[len(mac)-2])<<8 | int64(mac[len(mac)-1])
return val & 0xFFF, nil
```

MAC 地址全球唯一，同一局域网内冲突概率极低，无需手动配置。

注意：`getMachineID` 单次调用约 4.3ms，有堆分配，只能在初始化时调用一次，不能放在热路径。

---

## 快速开始

```go
// 单实例
sf, err := snowflakeid.NewSnowflakeAuto()
id, err := sf.NextID()

// 分片池（高并发推荐）
pool, err := snowflakeid.NewShardPool(sf.MachineID())
id, err := pool.NextID(goroutineIndex)
```

---

## 运行基准测试

```bash
go run ./cmd/benchmark
```
