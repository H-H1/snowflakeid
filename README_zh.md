# 分布式 ID 生成器 &nbsp;|&nbsp; <a href="README.md">English</a>

基于 Snowflake 算法的高性能分布式 ID 生成器（Go 实现），提供四种位布局变体和分片池，实现近无锁吞吐。

---

## 四个版本对比

| 方案 | 时间戳位 | 时间精度 | 机器ID位 | 序列号位 | 时间跨度 | 单实例吞吐 | 分片池吞吐 |
|------|----------|----------|----------|----------|----------|------------|------------|
| v1（本项目） | 40 | 1ms | 12（4096节点） | 11（2048/ms） | ~34年 | ~89万/s | ~976万/s |
| v2 | 43 | 1ms | 12（4096节点） | 8（256/ms） | ~278年 | ~12万/s | ~102万/s |
| v3 | 42 | 1ms | 12（4096节点） | 9（512/ms） | ~139年 | ~25万/s | ~170万/s |
| v4 | 41 | 1ms | 10（1024节点） | 12（4096/ms） | ~69年 | ~400万/s | ~1205万/s |
| bwmarrin/snowflake | 41 | 1ms | 10（1024节点） | 12（4096/ms） | ~69年 | ~400万/s | 无分片 |
| sony/sonyflake | 39 | 10ms | 16（65536节点） | 8（256/10ms） | ~174年 | ~2.5万/s | 无分片 |
| Twitter原版 | 41 | 1ms | 10（1024节点） | 12（4096/ms） | ~69年 | — | — |

v1~v3 机器ID均为12位（4096节点），v4 为10位（1024节点，Twitter 布局）；总位数均为63位（int64去掉符号位）。
位数取舍固定：时间戳 + 机器ID + 序列号 = 63位。

---

## ID 结构（v1）

```
63 62                    23  22           11  10            0
  | |                      ||               ||              |
  0 [    timestamp(40)     ][ machineID(12) ][ sequence(11) ]
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

### 为什么减去 epoch

```go
tick := time.Now().UnixMilli() - epoch
```

`UnixMilli()` 返回距 1970-01-01 的绝对毫秒数（当前 ≈ 1.79e12），减去 epoch（2024-01-01 的 UnixMilli）后，tick 从 0 起算，存的是"距 2024 年过了多少毫秒"。

若直接存绝对时间戳，1970→2024 已过去的 54 年会白白占用位域量程。以 v3（42位，上限约 139 年）为例：

| 存法 | 起点 | 溢出时间 | 从 2026 年起剩余 |
|------|------|----------|------------------|
| 绝对 Unix 毫秒 | 1970 | 2109 年 | ~83 年 |
| 减 epoch 后的偏移 | 2024 | 2163 年 | **~137 年** |

减 epoch 相当于把计数器重新归零，整个位域全部留给未来，"时间跨度"才得以最大化。反解 ID 时加回 epoch 即可还原绝对时间：`time.UnixMilli(epoch + id>>timeShift)`。

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

## 时钟回拨时，序列号位数多能降低重复概率吗？

**不能。** 这是个直觉陷阱，两个原因：

### 原因一：本项目用自旋等待，回拨期间根本不发号

```go
if tick < s.lastStamp {
    // 时钟回拨：自旋等待时钟追上 lastStamp，期间不生成任何 ID
    tick = s.waitNextTick(s.lastStamp)
}
```

回拨时生成器停摆，直到墙钟重新超过 `lastStamp` 才恢复。回拨区间内的 tick 一个 ID 都不会发，自然无重复。序列号位数在此路径上完全无关。

### 原因二：即使“继续发号”，序列号也是从 0 重新数

假设某种实现不等待，而是用 `lastStamp++` 虚拟推进（或进程重启后带着旧状态继续）：回拨会重新覆盖已用过的 tick。重复 ID 要求三元组 `(tick, machineID, sequence)` 全同——而本设计**每个新 tick 序列号都从 0 开始顺序递增**，重新覆盖某个毫秒时又会从 0 发起。之前那个毫秒发过多少个 ID，前多少个就必然重复，**序列号字段再宽也救不了**：

```
回拨前 tick=T 发了 500 个 ID（sequence 0..499）
回拨后重新覆盖 tick=T，sequence 又从 0 开始
→ 前 500 个 ID 与之前完全相同，无论 sequence 是 11 位还是 12 位
```

### 序列号位数唯一能帮上的场景：随机起始

只有把序列号从**顺序递增**改为**随机起点**（或随机分配），宽度才有意义——重新覆盖同一毫秒时随机撞入已用区间的概率是 `已发数 / 2^位数`，v4（12位/4096）确实比 v1（11位/2048）碰撞概率减半。但随机序列牺牲了 ID 内的单调性保障，且代价远小于直接用自旋等待：**本项目选择了后者**。

### 真正防重复的三道防线

ID 由三个**互不重叠的位段**拼成（`tick<<shift | machineID<<shift | sequence`），因此：

```
ID 相同 ⟺ (tick, machineID, sequence) 三元组全部相同
```

只要任一字段不同，ID 就不同。三道防线各守一个字段：

#### 防线一：tick —— 时间维度只进不退

tick 在实例生命周期内**严格不减**，由三个机制保证：

- 正常推进：每次调用取当前墙钟，`tick >= lastStamp` 恒成立
- 时钟回拨：`waitNextTick` 自旋等待追上，回拨区间零发号
- 序列号耗尽：自旋推进到下一个 tick，而不是复用当前的

已用过的 tick 永远不会被第二次用来发号 → 高位永不回退。

#### 防线二：machineID —— 发号者身份隔离

冲突的另一半来自"别的发号器"。即使两台机器（或同机两个进程）在物理上同一毫秒各自发号，machineID 位不同 → ID 不同。派生公式三个维度各堵一个漏洞：

```go
MAC 低12位（机器维度） ^ PID（进程维度） ^ 启动纳秒（生命周期维度）
```

- 同机两进程：PID 必不同 → machineID 必不同（XOR 对固定 MAC 是双射）
- 同进程重启：纳秒必不同 → machineID 必不同
- 跨机器：MAC 不同，但截断到 12 位后可能相同——退化为随机碰撞，概率约 1/4096（v4 为 1/1024），这是无协调方案的固有下限

#### 防线三：sequence —— 同一毫秒内顺序不重

tick 与 machineID 都相同（同一实例、同一毫秒）时，靠序列号区分每次调用：

```go
s.sequence = (s.sequence + 1) & maxSequence
```

每次调用加一，同一毫秒内绝无重复；计数到顶（2048/4096，视版本）则自旋进入下一毫秒，由防线一接管。注意它成立的前提是**tick 永不回退**——防线三是建立在防线一之上的，这也是上一节"更宽的序列号防不了回拨重复"的原因。

#### 汇总

| 防线 | 守护字段 | 威胁 | 机制 |
|------|----------|------|------|
| 回拨停摆 | tick | 时钟回拨复用旧 tick | `waitNextTick` 自旋 |
| 身份隔离 | machineID | 多机/多进程/重启撞号 | MAC⊕PID⊕纳秒 派生 |
| 顺序不重 | sequence | 同毫秒内多次调用 | `(seq+1) & mask` |

结论：防回拨重复靠的是**发号策略和 machineID 稳定性**，不是序列号宽度。v4 的 12 位序列号带来的是**单实例吞吐**（4096/ms vs 2048/ms），而非回拨安全性。

---

## machineID 自动派生

```go
// 第一块非回环网卡 MAC 最后两字节（机器维度）
// ^ 进程PID（进程维度）^ 启动时刻纳秒（生命周期维度），截取低 12 位
val := int64(mac[len(mac)-2])<<8 | int64(mac[len(mac)-1])
return (val ^ int64(os.Getpid()) ^ time.Now().UnixNano()) & 0xFFF, nil
```

### 为什么要混入 PID 与启动时间

只取 MAC 低 12 位时，**同机多进程是确定性冲突**：machineID 是机器级的，两个进程得到相同值，各自独立发号，同一毫秒撞上相同序列号即产生重复 ID。

单加 PID 仍有两个失效场景：

| 场景 | 原因 |
|------|------|
| 容器 | PID namespace 隔离，每个容器内部看到的 PID 通常都是 1，PID 维度整体失效 |
| PID 复用 | Linux PID 循环分配，重启后可能拿到与上次相同的 PID，撞号窗口重新打开 |

因此引入第三个维度——**启动时刻纳秒**。时间单调向前，天然区分两次不同的进程生命周期：重启后纳秒必然不同，machineID 随之改变，同时规避了"同毫秒内重启、序列号从零重数"的撞号窗口。

冲突条件是三个因子的低 12 位**同时**抵消，概率 ≈ 1/4096（v4 为 1/1024）。所有结构性必然冲突（同机多进程、PID 复用、容器）都归结为均匀随机碰撞——这已接近无协调方案的理论下限：12 位空间容量只有 4096，冲突率下界由生日问题决定，实例规模接近几十个时应转向协调分配。

注意：
- machineID 每次启动都不同，**不可持久化**
- `getMachineID` 不再幂等（时间参与混合），只能在初始化时调用一次
- 大规模集群仍建议协调分配（手动配置、K8s StatefulSet 序号、etcd 发号）

### 为什么按位与 maxMachineID

`getMachineID` 返回 12 位混合值，但 v4 的机器ID只有 10 位（`maxMachineID4 = 1023`），12 位值直接传入会越界。位掩码截断只保留低 10 位，把 [0, 4095] 映射到合法的 [0, 1023]：

```
mid           = 0b101100111010   (12位, 3619)
maxMachineID4 = 0b000011111111   (10位掩码, 1023)
────────────────────────────────
结果          = 0b000000111010   (10位, 58)
```

机器ID本质是位字段而非数值，`& mask` 直接表达"截取位域"语义（等价于 `mid % 1024`），也是 Snowflake 实现的标准写法。

代价：12→10 位截断丢失高 2 位，跨机器冲突概率比 v1~v3（12 位）高 4 倍。这是 v4 采用 Twitter 布局（10 位机器ID、1024 节点）的固有取舍。

注意：`getMachineID` 单次调用约 4.3ms，有堆分配，只能在初始化时调用一次，不能放在热路径。

---

## 快速开始

```go
import snowflakeid "github.com/H-H1/snowflakeid"

// 单实例
sf, err := snowflakeid.NewSnowflakeAuto()
id, err := sf.NextID()

// 分片池（高并发推荐）
pool, err := snowflakeid.NewShardPool(sf.MachineID())
id, err := pool.NextID(goroutineIndex)

// v4（Twitter布局，单实例吞吐最高）
sf4, err := snowflakeid.NewSnowflake4Auto()
id, err := sf4.NextID()
pool4, err := snowflakeid.NewShardPool4(sf4.MachineID())
id, err := pool4.NextID(goroutineIndex)
```

---

## 命令行工具

CLI 位于 `cmd/snowflakeid`，库保持根目录（import 路径最短）：

```bash
go install github.com/H-H1/snowflakeid/cmd/snowflakeid@latest
```

```bash
snowflakeid                       # v1 生成 1 个 ID
snowflakeid -v 4 -n 5             # v4 生成 5 个
snowflakeid -v 4 -pool -n 8       # v4 分片池生成 8 个
snowflakeid explain -v 4 356088697336360960
# ID:            356088697336360960
# 版本 / ver:    v4
# 时间 / time:   2026-09-09 22:49:21.253 +08:00
# 机器 / machine: 63
# 序列 / seq:    0
```

---

## 运行基准测试

```bash
go run ./cmd/benchmark
```
