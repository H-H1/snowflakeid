# Distributed ID Generator &nbsp;|&nbsp; <a href="README_zh.md">中文文档</a>

A high-performance Snowflake-based distributed ID generator in Go, with three bit-layout variants and a shard pool for near-lock-free throughput.

---

## Three Variants at a Glance

| Variant | Timestamp bits | Precision | Machine ID bits | Sequence bits | Time span | Single-instance | Shard pool |
|---------|---------------|-----------|-----------------|---------------|-----------|-----------------|------------|
| v1 (this project) | 40 | 1 ms | 12 (4096 nodes) | 11 (2048/ms) | ~34 yr | ~890K/s | ~9.76M/s |
| v2 | 43 | 1 ms | 12 (4096 nodes) | 8 (256/ms) | ~278 yr | ~120K/s | ~1.02M/s |
| v3 | 42 | 1 ms | 12 (4096 nodes) | 9 (512/ms) | ~139 yr | ~250K/s | ~1.70M/s |
| bwmarrin/snowflake | 41 | 1 ms | 10 (1024 nodes) | 12 (4096/ms) | ~69 yr | ~4M/s | — |
| sony/sonyflake | 39 | 10 ms | 16 (65536 nodes) | 8 (256/10ms) | ~174 yr | ~25K/s | — |
| Twitter original | 41 | 1 ms | 10 (1024 nodes) | 12 (4096/ms) | ~69 yr | — | — |

All three variants use 12-bit machine IDs and 63 effective bits (int64 minus sign bit).
The trade-off is fixed: timestamp + machine ID + sequence = 63 bits.

---

## ID Structure (v1)

```
 63      62                    23        22          11        10          0
  |       |                     |         |           |         |          |
  0  [       40-bit ms timestamp  ] [  12-bit machine ID  ] [  11-bit sequence  ]
```

| Field | Bits | Range | Notes |
|-------|------|-------|-------|
| Sign | 1 | fixed 0 | guarantees positive int64 |
| Timestamp | 40 | 0 ~ 2⁴⁰-1 | ms offset from epoch (2024-01-01), ~34 years |
| Machine ID | 12 | 0 ~ 4095 | up to 4096 distributed nodes |
| Sequence | 11 | 0 ~ 2047 | up to 2048 IDs per millisecond |

### Bit assembly

```go
id := tick<<23 | machineID<<11 | sequence
```

Because the timestamp occupies the high bits, IDs are naturally time-ordered — `ORDER BY id` replaces `ORDER BY created_at`.

### Time span

```
2⁴⁰ - 1 = 1,099,511,627,775 ms  ≈  34.8 years  (from 2024-01-01, until ~2058)
```

When the epoch approaches expiry, update it to a more recent date to extend the range.

---

## NextID Logic

```
Call NextID()
      │
      ▼
  Lock (sync.Mutex)
      │
      ▼
  tick = time.Now().UnixMilli() - epoch
      │
      ├─ tick < lastStamp ──► clock rollback: spin until tick > lastStamp
      │
      ├─ tick == lastStamp ─► same millisecond
      │        │
      │        ▼
      │   sequence = (sequence + 1) & 0x7FF
      │        │
      │        └─ sequence == 0 ──► sequence exhausted: spin to next tick
      │
      └─ tick > lastStamp ──► new millisecond, reset sequence to 0
              │
              ▼
      lastStamp = tick
      id = tick<<23 | machineID<<11 | sequence
      Unlock, return id
```

**Clock rollback** — caused by NTP sync or VM clock drift. The generator spins until the clock catches up. Suitable for small rollback amounts.

**Sequence exhaustion** — on the 2049th call within the same millisecond, the sequence wraps to 0 and the goroutine spins (while holding the lock) until the next millisecond. This is the single-instance bottleneck under high concurrency.

---

## Shard Pool Design

### Root cause

`sync.Mutex` is a global serialization point. Under N concurrent goroutines, only one executes at a time — the higher the concurrency, the worse the lock contention.

### Solution: shard by physical CPU count

```
                    ┌─ shard[0]  machineID = base+0  own lock
goroutine 0,8,16 ──►│
                    ├─ shard[1]  machineID = base+1  own lock
goroutine 1,9,17 ──►│
                    ├─ shard[2]  machineID = base+2  own lock
goroutine 2,10,18──►│
                    │  ...
                    └─ shard[N-1]  own lock
```

Each shard is an independent `Snowflake` instance with its own lock, sequence counter, and `lastStamp`. They never block each other.

```go
shard     = idx % size
machineID = (baseID + shardIndex) & 0xFFF  // unique per shard, globally unique IDs
```

Shard count = `runtime.NumCPU()`, aligned with `GOMAXPROCS`. One shard per CPU core minimizes lock contention.

### Benchmark (4-core machine)

| Scenario | Latency | Notes |
|----------|---------|-------|
| Single-instance, serial | 343 ns/op | baseline lock overhead, no contention |
| Single-instance, concurrent | 435 ns/op | 8 goroutines competing |
| Shard pool, serial | 353 ns/op | modulo overhead, on par with single |
| Shard pool, concurrent | **50 ns/op** | contention spread, near lock-free, **8.6× faster** |

---

## Bit Trade-off Rules

```
timestamp bits +1  →  time span ×2,   machine ID or sequence bits -1
sequence bits  -1  →  throughput /2,  timestamp bits +1 available
machine ID bits -1 →  node count /2,  timestamp bits +1 available
```

---

## UUID v4 vs ULID vs Snowflake

### UUID v4 — unordered

Fully random 128-bit value, no time information.

```
Generated order:
  [0] e2649244-8c88-4a46-8011-6e104351a0f4
  [1] a384de78-6503-460b-9a74-cee55201ffe5
  [2] 13942ecc-8133-4f05-8c07-77288c8e6a3e
  ...
Sorted order is completely different
```

- Random B-Tree insertions cause frequent page splits → poor write performance
- Cannot infer time range from ID range
- Requires a separate `created_at` column for ordering

### ULID — lexicographically ordered

128-bit: high 48 bits = ms timestamp, low 80 bits = random. Lexicographic order = time order.

```
Generated order (10 ms apart):
  [0] 01KP0FWTYQ1F7P10V3ET41KDN8  time part: 01KP0FWTYQ
  [1] 01KP0FWTZ2PWP7YCH7EXDCV84D  time part: 01KP0FWTZ2
  ...
In-order: true
```

String ordering is index-friendly compared to UUID, but storage and comparison cost is 2× that of an integer.

### Snowflake — integer ordered

```
id1 < id2  ⟺  id1 was generated before id2
```

Integer index is the most compact. `ORDER BY id` directly replaces `ORDER BY created_at`, and the generation time can be decoded from the ID.

### Comparison

| | UUID v4 | ULID | Snowflake |
|---|---|---|---|
| Type | string (36 chars) | string (26 chars) | int64 |
| Ordering | none | lexicographic | integer |
| Distributed | native | native | requires machine ID coordination |
| DB index | poor | good | best |
| Decode time | no | yes | yes |
| Storage | 16 bytes | 16 bytes | 8 bytes |
| Throughput | ~14M/s | ~28M/s | ~2M/s (single) / ~14.8M/s (shard pool) |

---

## Spin vs Sleep Strategy

`SonyflakeCompat` uses the same bit layout as v1 but replaces spin-wait with `sleep` on sequence exhaustion:

```go
// Spin (this project): holds lock, all other goroutines block
for t <= last { t = currentTick() }

// Sleep (SonyCompat): releases lock, other goroutines can proceed
s.elapsedTime++
time.Sleep(time.Duration(overtime) * time.Millisecond)
```

Under high concurrency, sleep can actually be faster: while spinning holds the lock and blocks everyone, sleeping releases it so other goroutines keep producing IDs in parallel.

The shard pool eliminates this problem entirely — contention drops to 1/N, sequence exhaustion becomes rare, and the two strategies converge.

---

## Auto Machine ID Derivation

```go
// Take the last two bytes of the first non-loopback NIC's MAC address, keep low 12 bits
val := int64(mac[len(mac)-2])<<8 | int64(mac[len(mac)-1])
return val & 0xFFF, nil
```

MAC addresses are globally unique, so collision probability within the same LAN is negligible. No manual configuration needed.

Note: `getMachineID` takes ~4.3 ms and allocates on the heap. Call it once at initialization — never on the hot path.

---

## Quick Start

```go
// Single instance
sf, err := snowflakeid.NewSnowflakeAuto()
id, err := sf.NextID()

// Shard pool (recommended for high concurrency)
pool, err := snowflakeid.NewShardPool(sf.MachineID())
id, err := pool.NextID(goroutineIndex)
```

---

## Run Benchmark

```bash
go run ./cmd/benchmark
```
