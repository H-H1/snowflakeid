# Distributed ID Generator &nbsp;|&nbsp; <a href="README_zh.md">中文文档</a>

A high-performance Snowflake-based distributed ID generator in Go, with four bit-layout variants and a shard pool for near-lock-free throughput.

---

## Four Variants at a Glance

| Variant | Timestamp bits | Precision | Machine ID bits | Sequence bits | Time span | Single-instance | Shard pool |
|---------|---------------|-----------|-----------------|---------------|-----------|-----------------|------------|
| v1 (this project) | 40 | 1 ms | 12 (4096 nodes) | 11 (2048/ms) | ~34 yr | ~890K/s | ~9.76M/s |
| v2 | 43 | 1 ms | 12 (4096 nodes) | 8 (256/ms) | ~278 yr | ~120K/s | ~1.02M/s |
| v3 | 42 | 1 ms | 12 (4096 nodes) | 9 (512/ms) | ~139 yr | ~250K/s | ~1.70M/s |
| v4 | 41 | 1 ms | 10 (1024 nodes) | 12 (4096/ms) | ~69 yr | ~4M/s | ~12M/s |
| bwmarrin/snowflake | 41 | 1 ms | 10 (1024 nodes) | 12 (4096/ms) | ~69 yr | ~4M/s | — |
| sony/sonyflake | 39 | 10 ms | 16 (65536 nodes) | 8 (256/10ms) | ~174 yr | ~25K/s | — |
| Twitter original | 41 | 1 ms | 10 (1024 nodes) | 12 (4096/ms) | ~69 yr | — | — |

v1–v3 use 12-bit machine IDs (4096 nodes); v4 uses 10 bits (1024 nodes, Twitter layout).
All variants use 63 effective bits (int64 minus sign bit).
The trade-off is fixed: timestamp + machine ID + sequence = 63 bits.

---

## ID Structure (v1)

```
63 62                    23  22           11  10            0
  | |                      ||               ||              |
  0 [    timestamp(40)     ][ machineID(12) ][ sequence(11) ]
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

### Why subtract the epoch

```go
tick := time.Now().UnixMilli() - epoch
```

`UnixMilli()` returns absolute milliseconds since 1970-01-01 (currently ≈ 1.79e12). Subtracting the epoch (the UnixMilli of 2024-01-01) resets the counter to zero — the stored value is "milliseconds elapsed since 2024".

Storing the absolute timestamp would waste the bit budget on the 54 years that already passed (1970→2024). Take v3 (42-bit, ~139-year cap) as an example:

| Storage | Starts at | Overflows | Remaining from 2026 |
|---------|-----------|-----------|---------------------|
| Absolute Unix ms | 1970 | year 2109 | ~83 yr |
| Offset from epoch | 2024 | year 2163 | **~137 yr** |

Subtracting the epoch re-zeroes the counter so the entire bit field is reserved for the future — that is what maximizes the time span. To decode a timestamp back, add the epoch: `time.UnixMilli(epoch + id>>timeShift)`.

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

## Does a Wider Sequence Field Reduce Duplicates During Clock Rollback?

**No.** This is an intuition trap, for two reasons:

### Reason 1: This project spins and issues nothing during rollback

```go
if tick < s.lastStamp {
    // Clock rolled back: spin until the wall clock catches up; no IDs are issued.
    tick = s.waitNextTick(s.lastStamp)
}
```

On rollback the generator stalls until the wall clock passes `lastStamp` again. Not a single ID is issued inside the rolled-back region, so no duplicates can occur there. The sequence width is irrelevant on this path.

### Reason 2: Even "issuing through" restarts the sequence at 0

Suppose an implementation does not wait but instead advances virtually (`lastStamp++`), or a process resumes with stale state: the rollback revisits ticks that were already used. A duplicate requires the full triple `(tick, machineID, sequence)` to match — and this design **resets the sequence to 0 at every new tick, incrementing sequentially**. Revisiting a millisecond starts the sequence from 0 again: however many IDs that millisecond issued before, exactly that many are re-issued identically. **No sequence width can save that**:

```
Before rollback, tick=T issued 500 IDs (sequence 0..499)
After rollback, tick=T is revisited and the sequence starts from 0 again
→ The first 500 IDs are byte-identical to before, whether the sequence is 11 or 12 bits
```

### The one case where width helps: random starting point

Only if the sequence changes from **sequential** to a **random start** (or random allocation) does width matter — the chance of landing in the already-used region when revisiting a millisecond is `issued / 2^bits`, and v4 (12 bits / 4096) does halve that versus v1 (11 bits / 2048). But a random sequence sacrifices intra-ID monotonicity, and the cost outweighs simply spinning: **this project chose spinning**.

### The three defenses that actually prevent duplicates

The ID is assembled from three **non-overlapping bit fields** (`tick<<shift | machineID<<shift | sequence`), therefore:

```
IDs are equal ⟺ the full triple (tick, machineID, sequence) matches
```

If any one field differs, the IDs differ. Each defense guards one field:

#### Defense 1: tick — time only moves forward

Within an instance's lifetime, tick is **monotonically non-decreasing**, guaranteed by three mechanisms:

- Normal advance: every call reads the wall clock, so `tick >= lastStamp` always holds
- Clock rollback: `waitNextTick` spins until the clock catches up — zero issuance in the rolled-back region
- Sequence exhaustion: spin forward to the next tick instead of reusing the current one

A tick, once used, is never issued from again → the high bits never regress.

#### Defense 2: machineID — issuer identity isolation

The other half of collisions comes from *other issuers*. Even if two machines (or two processes on one machine) physically emit IDs in the same millisecond, their machineID bits differ → the IDs differ. The derivation formula plugs one hole per dimension:

```go
low 12 bits of MAC (machine) ^ PID (process) ^ boot-time nanos (lifetime)
```

- Two processes on one host: PIDs necessarily differ → machineIDs necessarily differ (XOR is a bijection for a fixed MAC)
- Same process across restarts: boot nanos necessarily differ → machineID differs
- Across machines: MACs differ, but after truncation to 12 bits they may collide — degrading to a random collision of ≈1/4096 per pair (1/1024 for v4), the inherent floor of any coordination-free scheme

#### Defense 3: sequence — no double-counting within one millisecond

When tick and machineID are both identical (same instance, same millisecond), the sequence distinguishes each call:

```go
s.sequence = (s.sequence + 1) & maxSequence
```

It increments on every call, so IDs within one millisecond never repeat; when the counter wraps (2048/4096 depending on version) it spins into the next millisecond and Defense 1 takes over. Note the precondition: **tick must never roll back** — Defense 3 is built on top of Defense 1, which is exactly why a wider sequence cannot prevent rollback duplicates (previous section).

#### Summary

| Defense | Field guarded | Threat | Mechanism |
|---------|---------------|--------|-----------|
| Stall on rollback | tick | Clock rollback reusing old ticks | `waitNextTick` spin |
| Identity isolation | machineID | Multi-host / multi-process / restart collisions | MAC ⊕ PID ⊕ nanos derivation |
| Sequential issuance | sequence | Multiple calls within one ms | `(seq+1) & mask` |

Bottom line: rollback-safety comes from the **issuing strategy and machine-ID stability**, not sequence width. v4's 12-bit sequence buys **single-instance throughput** (4096/ms vs 2048/ms), not rollback safety.

---

## Auto Machine ID Derivation

```go
// Last two bytes of the first non-loopback NIC's MAC (machine dimension)
// ^ process ID (process dimension) ^ startup time in nanoseconds
// (lifetime dimension), keep low 12 bits
val := int64(mac[len(mac)-2])<<8 | int64(mac[len(mac)-1])
return (val ^ int64(os.Getpid()) ^ time.Now().UnixNano()) & 0xFFF, nil
```

### Why mix in the PID and the startup time

With only the MAC low 12 bits, **multiple processes on one machine collide deterministically**: the machine ID is machine-level, so two processes get the same value, number independently, and produce duplicate IDs as soon as they hit the same millisecond with the same sequence number.

Adding just the PID still leaves two failure modes:

| Scenario | Why |
|----------|-----|
| Containers | PID namespaces isolate — every container sees its own PID as 1, so the PID dimension fails entirely |
| PID reuse | Linux recycles PIDs; a restart can draw the same PID again, reopening the collision window |

Hence the third dimension: **startup time in nanoseconds**. Time moves forward monotonically, so it naturally distinguishes two process lifetimes — after a restart the nanos differ, the machine ID changes, and the "restart within the same millisecond, sequence restarts at zero" window is sidestepped.

A collision requires the low 12 bits of all three factors to cancel out **simultaneously** — probability ≈ 1/4096 (1/1024 for v4). Every structured deterministic collision (same-machine multi-process, PID reuse, containers) is reduced to a uniform random one, which is near the theoretical floor for any coordination-free scheme: a 12-bit space holds only 4096 values, the collision lower bound is the birthday problem, and fleets of a few dozen instances should switch to coordinated assignment.

Notes:
- The machine ID differs on every launch — **do not persist it**
- `getMachineID` is no longer idempotent (time participates in the mix) — call it exactly once at initialization
- For large clusters, coordinated assignment is still recommended (manual config, K8s StatefulSet ordinal, etcd)

### Why AND with maxMachineID

`getMachineID` returns a 12-bit mixed value, but v4's machine ID is only 10 bits wide (`maxMachineID4 = 1023`) — passing a 12-bit value directly would be out of range. The bitmask keeps only the low 10 bits, mapping [0, 4095] onto the legal [0, 1023]:

```
mid           = 0b101100111010   (12 bits, 3619)
maxMachineID4 = 0b000011111111   (10-bit mask, 1023)
────────────────────────────────
result        = 0b000000111010   (10 bits, 58)
```

A machine ID is a bit field, not a numeric value — `& mask` directly expresses "extract the bit field" semantics (equivalent to `mid % 1024`) and is the standard Snowflake idiom.

The cost: 12→10-bit truncation drops the top 2 bits, making cross-machine collisions 4× more likely than v1–v3 (12 bits). This is the inherent trade-off of v4's Twitter layout (10-bit machine ID, 1024 nodes).

Note: `getMachineID` takes ~4.3 ms and allocates on the heap. Call it once at initialization — never on the hot path.

---

## Quick Start

```go
import snowflakeid "github.com/H-H1/snowflakeid"

// Single instance
sf, err := snowflakeid.NewSnowflakeAuto()
id, err := sf.NextID()

// Shard pool (recommended for high concurrency)
pool, err := snowflakeid.NewShardPool(sf.MachineID())
id, err := pool.NextID(goroutineIndex)

// v4 (Twitter layout, highest single-instance throughput)
sf4, err := snowflakeid.NewSnowflake4Auto()
id, err := sf4.NextID()
pool4, err := snowflakeid.NewShardPool4(sf4.MachineID())
id, err := pool4.NextID(goroutineIndex)
```

---

## CLI

The CLI lives in `cmd/snowflakeid`; the library stays at the module root (shortest import path):

```bash
go install github.com/H-H1/snowflakeid/cmd/snowflakeid@latest
```

```bash
snowflakeid                       # one v1 ID
snowflakeid -v 4 -n 5             # five v4 IDs
snowflakeid -v 4 -pool -n 8       # eight v4 IDs via the shard pool
snowflakeid explain -v 4 356088697336360960
# ID:            356088697336360960
# 版本 / ver:    v4
# 时间 / time:   2026-09-09 22:49:21.253 +08:00
# 机器 / machine: 63
# 序列 / seq:    0
```

---

## Run Benchmark

```bash
go run ./cmd/benchmark
```
