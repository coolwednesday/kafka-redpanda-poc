# Technical Architecture

## Summary

The benchmark is a single Go binary composed of four internal packages. The CLI layer (`main.go`) orchestrates the execution flow: verify broker connectivity, prepare the topic, launch a consumer in the background, run N concurrent producer goroutines, wait for the consumer to drain, then compute and output results.

Both brokers are accessed through the same client library ([franz-go](https://github.com/twmb/franz-go)), use identical producer configurations, and receive the same deterministic payload sequence. The only variable between runs is the broker address.

```
CLI (main.go / cmd/benchmark/main.go)
 ├── Validate flags and resolve broker address
 ├── Health-check broker (metadata ping)
 ├── Delete and recreate topic (clean state)
 ├── Start consumer (background goroutine)  →  E2E Latency Collector
 ├── Start producer (N goroutines, blocking) →  Publish Latency Collector
 ├── Wait for consumer to drain
 └── Print summary table + export CSV
```

**Source:** [`cmd/benchmark/main.go`](https://github.com/coolwednesday/kafka-redpanda-poc/blob/main/cmd/benchmark/main.go) | [`main.go`](https://github.com/coolwednesday/kafka-redpanda-poc/blob/main/main.go)

---

## Table of Contents

1. [System Overview](#1-system-overview)
2. [Broker Topology](#2-broker-topology)
3. [Data Flow](#3-data-flow)
4. [Producer Concurrency Model](#4-producer-concurrency-model)
5. [Consumer Design](#5-consumer-design)
6. [Metrics Pipeline](#6-metrics-pipeline)
7. [Graceful Shutdown](#7-graceful-shutdown)
8. [Design Decisions](#8-design-decisions)
9. [File Reference](#9-file-reference)

---

## 1. System Overview

The application follows a linear orchestration model. Each phase completes before the next begins, with the exception of the consumer, which runs concurrently with the producer.

```
+------------------------------------------------------------------+
|                        CLI (main.go)                             |
|  Flags: --broker, --concurrency, --total, --warmup, --csv       |
|                                                                  |
|  Phase 1: Validate flags                                         |
|  Phase 2: Health-check broker (metadata ping, 10s timeout)       |
|  Phase 3: Delete + recreate topic (6 partitions, RF=1)           |
|  Phase 4: Launch consumer (background goroutine)                 |
|  Phase 5: Launch producer (blocking, N goroutines)               |
|  Phase 6: Wait for consumer to drain (or timeout)                |
|  Phase 7: Compute percentiles and throughput                     |
|  Phase 8: Print summary table + export CSV row                   |
+---------------------------+--------------------------------------+
                            |
              +-------------+-------------+
              |                           |
              v                           v
+-------------------------+  +-------------------------+
|    Producer (N workers)  |  |       Consumer          |
|    producer/producer.go  |  |  consumer/consumer.go   |
|                          |  |                         |
|  +----+ +----+  +----+  |  |  Single consumer with   |
|  | G0 | | G1 |..| GN |  |  |  unique group per run   |
|  +--+-+ +--+-+  +--+-+  |  |                         |
|     |      |       |     |  |  Extracts produce_ts    |
|     +------+-------+     |  |  header for E2E latency |
|            |             |  |                         |
+------------|-------------+  +------------|------------+
             |                             |
             v                             v
+-------------------------+  +-------------------------+
| Publish Latency         |  | E2E Latency             |
| Collector               |  | Collector               |
| (metrics/metrics.go)    |  | (metrics/metrics.go)    |
+------------+------------+  +------------+------------+
             |                             |
             +-------------+---------------+
                           |
                           v
              +------------------------+
              |  Output                |
              |  - Throughput (msg/s)  |
              |  - p50 / p95 publish   |
              |  - p50 / p95 E2E      |
              |  - Error count         |
              |  - CSV row             |
              +------------------------+
```

---

## 2. Broker Topology

Both brokers run as single-node Docker containers on separate ports. They are never benchmarked simultaneously; each run targets one broker.

```
+------------------------+         +------------------------+
|    Apache Kafka        |         |       Redpanda         |
|    Port: 9092          |         |    Port: 19092         |
|                        |         |                        |
|  Image:                |         |  Image:                |
|  apache/kafka:3.7.0    |         |  redpandadata/         |
|                        |         |  redpanda:v24.1.1      |
|  Mode: KRaft           |         |                        |
|  (no ZooKeeper)        |         |  Flags:                |
|                        |         |  --smp 1               |
|  Topic: benchmark      |         |  --memory 512M         |
|  Partitions: 6         |         |                        |
|  Replication Factor: 1 |         |  Topic: benchmark      |
|                        |         |  Partitions: 6         |
+------------------------+         |  Replication Factor: 1 |
        ^                          +------------------------+
        |                                  ^
        |       --broker=kafka             |
        +-----------  OR  -----------------+
                    --broker=redpanda
```

**Source:** [`docker-compose.yaml`](https://github.com/coolwednesday/kafka-redpanda-poc/blob/main/docker-compose.yaml) | [`broker/broker.go`](https://github.com/coolwednesday/kafka-redpanda-poc/blob/main/broker/broker.go)

**KRaft Mode (Kafka):** Traditional Kafka deployments require a separate ZooKeeper ensemble for metadata management. KRaft (Kafka Raft) internalizes consensus, reducing the deployment to a single container. This is the recommended mode for new Kafka deployments; ZooKeeper support is deprecated.

**Single Shard (Redpanda):** The `--smp 1` flag limits Redpanda to one CPU shard. Since Kafka is also running as a single-node instance without specific CPU pinning, this constraint maintains a fair comparison of per-core throughput.

---

## 3. Data Flow

**Source:** [`producer/producer.go`](https://github.com/coolwednesday/kafka-redpanda-poc/blob/main/producer/producer.go) | [`payload/payload.go`](https://github.com/coolwednesday/kafka-redpanda-poc/blob/main/payload/payload.go)

### 3.1 Produce Path

```
  Payload Generator (seed=42)
  +-----------------------------------------------+
  | Deterministic RNG selects message type:       |
  |   random < 0.70  →  OrderPlaced      (70%)   |
  |   random < 0.90  →  PaymentSettled   (20%)   |
  |   otherwise      →  InventoryAdjusted (10%)  |
  |                                               |
  | JSON payload padded to ~512 bytes             |
  +----------------------+------------------------+
                         |
                         v
  +----------------------+------------------------+
  |              Kafka Record                      |
  |                                                |
  |  Value:   {"type":"OrderPlaced",               |
  |            "id":"OrderPlaced-42",              |
  |            "ts":"2026-03-05T...",               |
  |            "data":"xxxxxxx..."}                |
  |                                                |
  |  Header:  produce_ts = 1772724937018234 (ns)  |
  +----------------------+------------------------+
                         |
            Producer Configuration:
            ├── acks = all
            ├── linger = 5ms
            ├── batch.max.bytes = 16KB
            └── max.buffered.records = 10,000
                         |
                         v
  +----------------------+------------------------+
  |                  Broker                        |
  |    Kafka (:9092) or Redpanda (:19092)         |
  +-----------------------------------------------+
```

### 3.2 Consume Path ([`consumer/consumer.go`](https://github.com/coolwednesday/kafka-redpanda-poc/blob/main/consumer/consumer.go))

```
  +-----------------------------------------------+
  |            Consumer Group                      |
  |    ID: bench-<unix-nanoseconds>               |
  |    (unique per run, ensures clean offsets)     |
  |                                                |
  |  For each consumed record:                    |
  |    1. Extract "produce_ts" record header      |
  |    2. Parse nanosecond Unix timestamp         |
  |    3. Compute: E2E latency = now() - ts       |
  |    4. Record duration in E2E collector        |
  |                                                |
  |  Exit conditions:                             |
  |    - consumed count == total, OR              |
  |    - no messages received for 30s (timeout)   |
  +-----------------------------------------------+
```

---

## 4. Producer Concurrency Model

**Source:** [`producer/producer.go`](https://github.com/coolwednesday/kafka-redpanda-poc/blob/main/producer/producer.go)

The producer employs N goroutines (default: 4) that draw work from a shared atomic counter. This implements a lock-free work distribution pattern without the overhead of a channel-based queue.

```
                   Shared Atomic Counter
                   counter.Add(1) returns next index
                   (0, 1, 2, ... total-1)
                          |
            +-------------+-------------+
            |             |             |
        +---v---+     +---v---+     +---v---+
        |  G0   |     |  G1   |     | G(N-1)|
        | seed  |     | seed  |     | seed  |
        | =42   |     | =43   |     |=42+N-1|
        +---+---+     +---+---+     +---+---+
            |             |             |
            |  if idx < warmup (1000):  |
            |    produce, but discard   |
            |    latency sample         |
            |  else:                    |
            |    produce and record     |
            |    latency sample         |
            |             |             |
            +------+------+------+------+
                   |
                   v
           Publish Latency Collector
           (sync.Mutex-protected slice)
```

**Atomic Counter:** `sync/atomic.Int64.Add(1)` provides lock-free work distribution. Each goroutine atomically claims the next message index. When the counter exceeds the total, the goroutine exits. This is more efficient than a channel-based approach for simple monotonic index assignment.

**Per-Goroutine Generators:** Go's `math/rand.Rand` is not safe for concurrent use. Each goroutine receives its own generator with a derived seed (`42 + goroutine_index`). The aggregate distribution across all goroutines still approximates the target 70/20/10 ratio.

**Warm-up Exclusion:** Messages with index < 1,000 are produced normally but their latencies are not recorded. This eliminates cold-start artifacts: TCP connection establishment, Kafka partition leader discovery, and JVM JIT compilation warmup on the Kafka side.

---

## 5. Consumer Design

**Source:** [`consumer/consumer.go`](https://github.com/coolwednesday/kafka-redpanda-poc/blob/main/consumer/consumer.go)

The consumer is a single goroutine running a synchronous poll loop. Simplicity is intentional — the benchmark measures broker performance, not consumer scalability.

```
Start
  │
  v
Create franz-go client with unique consumer group
  │
  v
┌──> Poll fetches (blocks until data arrives or context is cancelled)
│    │
│    v
│    For each record in the fetch:
│      ├── Increment consumed counter
│      ├── Extract "produce_ts" header
│      ├── Parse nanosecond Unix timestamp
│      ├── Compute E2E latency: time.Now() - produce_ts
│      └── Record duration in E2E collector
│    │
│    v
│    consumed >= total? ──YES──> Exit (success)
│    │
│    NO
│    │
│    v
│    Time since last message > 30s? ──YES──> Exit (timeout, log warning)
│    │
│    NO
└────┘
```

**Single Consumer:** Using one consumer ensures all E2E latency measurements originate from a single clock source and avoids partition rebalancing noise that would occur with multiple consumers.

**Fresh Consumer Group:** Each run generates a unique group ID (`bench-<unix-nanoseconds>`). This prevents committed offsets from prior runs from causing the consumer to skip or replay messages.

---

## 6. Metrics Pipeline

**Source:** [`metrics/metrics.go`](https://github.com/coolwednesday/kafka-redpanda-poc/blob/main/metrics/metrics.go)

The metrics system is self-contained with no external dependencies. At the scale of this benchmark (99,000 samples after warmup exclusion), sort-based percentile computation is both accurate and performant (~1ms sort time).

```
  Recording Phase (concurrent, hot path):
  ──────────────────────────────────────────────
  Goroutine 0 ─┐
  Goroutine 1 ─┼──> mutex.Lock() → append(samples, duration) → Unlock()
  Goroutine 2 ─┘

  Computation Phase (single-threaded, post-benchmark):
  ──────────────────────────────────────────────
  Raw Samples: []time.Duration (e.g., 99,000 entries)
         │
         v
  Copy slice → sort ascending
         │
         v
  ┌──────┴──────┐
  │ p50 = samples[len * 0.50] │
  │ p95 = samples[len * 0.95] │
  └──────┬──────┘
         │
         ├──────────────┬────────────────┐
         v              v                v
  ┌──────────────┐  ┌────────────────┐  ┌───────────┐
  │ PrintSummary │  │   ExportCSV    │  │  Result   │
  │ (stdout,     │  │ (append mode,  │  │  struct   │
  │  formatted)  │  │  auto-header)  │  │ (reuse)   │
  └──────────────┘  └────────────────┘  └───────────┘
```

**Thread Safety:** The collector uses `sync.Mutex` to protect a `[]time.Duration` slice from concurrent appends during the recording phase. The mutex is held only for the duration of a single `append` operation.

**CSV Append Mode:** Each benchmark run appends one row to the CSV file. If the file does not exist, a header row is written first. This allows multiple runs (varying concurrency, message counts, or brokers) to accumulate results for trend analysis.

---

## 7. Graceful Shutdown

**Source:** [`cmd/benchmark/main.go`](https://github.com/coolwednesday/kafka-redpanda-poc/blob/main/cmd/benchmark/main.go)

The application traps `SIGINT` and `SIGTERM` signals and propagates cancellation through Go's `context.Context` mechanism. This ensures clean resource release and partial result output even when a benchmark is interrupted.

```
Signal received (Ctrl+C / SIGTERM)
       │
       v
signal.Notify captures signal
       │
       v
context.Cancel() invoked
       │
       ├──────────────────┬──────────────────┐
       v                  v                  v
  Producer goroutines   Consumer poll      Client flush
  detect ctx.Done()     returns on         sends buffered
  and exit loops        context cancel     records
       │                  │                  │
       └──────────────────┴──────────────────┘
                          │
                          v
             Partial results computed and printed
             (based on messages produced/consumed before cancellation)
```

This design ensures the tool never exits silently. Partial results are useful for validating broker connectivity and configuration before committing to a full benchmark run.

---

## 8. Design Decisions

| Decision | Rationale |
|---|---|
| **franz-go as the client library** | Pure Go implementation with high performance. Compatible with both Kafka and Redpanda via the standard Kafka wire protocol, eliminating the need for broker-specific code paths. |
| **In-process metrics collection** | Removes the dependency on external monitoring infrastructure (Prometheus, Grafana). The tool is fully self-contained and portable. |
| **Deterministic RNG with fixed seed (42)** | Ensures identical payload distribution across runs and brokers, enabling fair comparison and reproducible results. |
| **Topic recreation before each run** | Prevents stale messages from prior runs from contaminating E2E latency measurements. Guarantees a clean measurement baseline. |
| **Unique consumer group per run** | Eliminates committed offset interference between runs. Each run begins consuming from the start of the topic. |
| **Warm-up exclusion (1,000 messages)** | Removes cold-start latency artifacts from statistical calculations, including TCP establishment, partition discovery, and JIT compilation. |
| **`context.Context` for lifecycle management** | Provides a single, idiomatic mechanism for propagating cancellation across all goroutines, consistent with Go best practices. |
| **Atomic counter for work distribution** | `sync/atomic.Int64` offers lock-free work distribution with lower overhead than channel-based alternatives for simple index assignment. |
| **Per-goroutine payload generators** | Avoids mutex contention on a shared `rand.Rand` instance, which is not safe for concurrent use in Go. |
| **6 partitions with RF=1** | Provides sufficient parallelism for 4 producer goroutines without introducing replication overhead that would complicate the comparison. |

---

## 9. File Reference

| File | Description |
|---|---|
| [`main.go`](https://github.com/coolwednesday/kafka-redpanda-poc/blob/main/main.go) | Root entry point. Delegates to combined benchmark. |
| [`cmd/benchmark/main.go`](https://github.com/coolwednesday/kafka-redpanda-poc/blob/main/cmd/benchmark/main.go) | Combined benchmark CLI. Flag parsing, signal handling, orchestration. |
| [`cmd/producer/main.go`](https://github.com/coolwednesday/kafka-redpanda-poc/blob/main/cmd/producer/main.go) | Standalone producer CLI. |
| [`cmd/consumer/main.go`](https://github.com/coolwednesday/kafka-redpanda-poc/blob/main/cmd/consumer/main.go) | Standalone consumer CLI. |
| [`broker/broker.go`](https://github.com/coolwednesday/kafka-redpanda-poc/blob/main/broker/broker.go) | Shared broker utilities: address resolution, health check, topic management. |
| [`broker/broker_test.go`](https://github.com/coolwednesday/kafka-redpanda-poc/blob/main/broker/broker_test.go) | Unit tests for broker utilities. |
| [`producer/producer.go`](https://github.com/coolwednesday/kafka-redpanda-poc/blob/main/producer/producer.go) | Concurrent producer engine. N goroutines, atomic counter, produce_ts stamping, warm-up exclusion. |
| [`producer/producer_test.go`](https://github.com/coolwednesday/kafka-redpanda-poc/blob/main/producer/producer_test.go) | Unit tests for producer. |
| [`consumer/consumer.go`](https://github.com/coolwednesday/kafka-redpanda-poc/blob/main/consumer/consumer.go) | Single-goroutine consumer. Unique group per run, E2E latency from produce_ts headers. |
| [`consumer/consumer_test.go`](https://github.com/coolwednesday/kafka-redpanda-poc/blob/main/consumer/consumer_test.go) | Unit tests for consumer. |
| [`payload/payload.go`](https://github.com/coolwednesday/kafka-redpanda-poc/blob/main/payload/payload.go) | Deterministic message generator. Seeded RNG, weighted 70/20/10 distribution, ~512B JSON. |
| [`payload/payload_test.go`](https://github.com/coolwednesday/kafka-redpanda-poc/blob/main/payload/payload_test.go) | Unit tests: distribution accuracy, payload size, determinism. |
| [`metrics/metrics.go`](https://github.com/coolwednesday/kafka-redpanda-poc/blob/main/metrics/metrics.go) | Thread-safe latency collector. Sort-based p50/p95, summary table, CSV export. |
| [`metrics/metrics_test.go`](https://github.com/coolwednesday/kafka-redpanda-poc/blob/main/metrics/metrics_test.go) | Unit tests: percentile accuracy, empty collector, CSV append. |
| [`docker-compose.yaml`](https://github.com/coolwednesday/kafka-redpanda-poc/blob/main/docker-compose.yaml) | Kafka 3.7.0 (KRaft, port 9092) + Redpanda v24.1.1 (port 19092) with health checks. |
| [`Makefile`](https://github.com/coolwednesday/kafka-redpanda-poc/blob/main/Makefile) | Build, test, start/stop containers, run benchmarks. |
| [`go.mod`](https://github.com/coolwednesday/kafka-redpanda-poc/blob/main/go.mod) | Go module definition. Dependencies: franz-go, franz-go/pkg/kadm. |
