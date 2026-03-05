# Kafka vs Redpanda — Performance Benchmark Report

## Executive Summary

This project implements a self-contained Go benchmark tool that evaluates the performance characteristics of Apache Kafka and Redpanda under identical conditions. The tool sends 100,000 messages through each broker using the same client library, producer configuration, and deterministic payload, then measures throughput and latency at both the producer and end-to-end levels.

**Benchmark Results (single-node, local Docker, 4 concurrent producers):**

| Metric | Apache Kafka | Redpanda | Delta |
|---|---|---|---|
| Throughput | 122,667 msgs/s | 188,745 msgs/s | Redpanda +54% |
| Publish Latency (p50) | 55.29 ms | 11.94 ms | Redpanda 4.6x lower |
| Publish Latency (p95) | 291.33 ms | 348.52 ms | Kafka 16% lower |
| End-to-End Latency (p50) | 2,014.61 ms | 14.97 ms | Redpanda 134x lower |
| End-to-End Latency (p95) | 2,413.97 ms | 352.65 ms | Redpanda 6.8x lower |
| Produce Errors | 0 | 0 | Equal |

**Key Finding:** Redpanda delivered 54% higher throughput and significantly lower end-to-end latency in this single-node benchmark. Kafka exhibited a tighter p95 publish latency. The performance delta is primarily attributed to Redpanda's C++/thread-per-core architecture versus Kafka's JVM-based runtime. See [Section 5](#5-performance-analysis) and [Section 6](#6-caveats--limitations) for detailed analysis and important caveats.

**Reproduce in 3 commands:**

```bash
make docker-up        # Start both brokers
make run-both         # Benchmark Kafka, then Redpanda
cat results.csv       # Compare results side-by-side
```

---

## Table of Contents

1. [Objective](#1-objective)
2. [Test Environment](#2-test-environment)
3. [Methodology](#3-methodology)
4. [Results](#4-results)
5. [Performance Analysis](#5-performance-analysis)
6. [Caveats & Limitations](#6-caveats--limitations)
7. [Setup & Usage Guide](#7-setup--usage-guide)
8. [CLI Reference](#8-cli-reference)
9. [Project Structure](#9-project-structure)
10. [References](#10-references)

---

## 1. Objective

The objective of this proof-of-concept is to generate simple, comparable performance metrics — specifically throughput and tail latency (p95) — for Apache Kafka and Redpanda using identical code paths, configurations, and workloads.

This benchmark does not aim to declare an absolute winner between the two systems. Instead, it provides a repeatable, controlled comparison on a local development setup, highlighting the architectural differences that influence performance under a specific workload profile.

---

## 2. Test Environment

| Component | Specification |
|---|---|
| **Language** | Go 1.22+ |
| **Client Library** | [franz-go](https://github.com/twmb/franz-go) v1.20.7 — a pure Go Kafka protocol client compatible with both brokers |
| **Apache Kafka** | v3.7.0, KRaft mode (no ZooKeeper dependency), single broker |
| **Redpanda** | v24.1.1, single node, 1 CPU shard (`--smp 1`), 512 MB memory |
| **Infrastructure** | Docker containers on local machine (shared CPU and memory) |
| **Topic Configuration** | 6 partitions, replication factor 1 |
| **Producer Configuration** | `acks=all`, `linger=5ms`, `batch.max.bytes=16KB`, `max.buffered.records=10,000` |
| **Concurrency** | 4 concurrent producer goroutines |
| **Message Volume** | 100,000 messages per run |
| **Message Size** | ~512 bytes (JSON) |
| **Warm-up** | First 1,000 messages excluded from latency measurements |

---

## 3. Methodology

### 3.1 Fairness Controls

The following controls ensure an apples-to-apples comparison between the two brokers:

| Control | Implementation |
|---|---|
| **Unified client library** | franz-go communicates via the Kafka wire protocol, which both Kafka and Redpanda implement natively. No broker-specific code paths exist in the benchmark. |
| **Identical producer settings** | Acknowledgment mode, batching parameters, linger duration, and buffer limits are configured identically for both brokers. |
| **Deterministic payload** | A seeded random number generator (`seed=42`) produces the same sequence of message types and content for every run. |
| **Clean state per run** | The benchmark topic is deleted and recreated before each execution. A unique consumer group ID (timestamped) prevents stale offset interference. |
| **Warm-up exclusion** | The first 1,000 messages are produced but excluded from latency calculations, eliminating cold-start artifacts (TCP handshake, partition leader discovery, JIT compilation). |

### 3.2 Workload Profile

The benchmark simulates a simplified e-commerce event stream with three message types in a fixed distribution:

| Event Type | Weight | Description |
|---|---|---|
| `OrderPlaced` | 70% | Represents a new customer order |
| `PaymentSettled` | 20% | Represents a payment confirmation |
| `InventoryAdjusted` | 10% | Represents a stock level change |

Each message is serialized as JSON and padded to approximately 512 bytes:

```json
{
  "type": "OrderPlaced",
  "id": "OrderPlaced-42",
  "ts": "2026-03-05T15:35:00.000000000Z",
  "data": "xxxxxxxxxxxx..."
}
```

### 3.3 Metrics Collected

| Metric | Definition |
|---|---|
| **Throughput** (msgs/s) | Total messages successfully produced divided by total elapsed wall-clock time |
| **Publish Latency (p50)** | Median duration from `Produce()` invocation to broker acknowledgment, across all non-warmup messages |
| **Publish Latency (p95)** | 95th percentile of the same measurement |
| **E2E Latency (p50)** | Median duration from the producer-stamped `produce_ts` header to consumer receipt |
| **E2E Latency (p95)** | 95th percentile of the same measurement |
| **Error Count** | Number of produce calls that returned a non-nil error |

Percentiles are computed by sorting all collected latency samples and selecting the value at the corresponding index. At 99,000 samples (100K minus 1K warmup), this approach is both accurate and performant.

### 3.4 Execution Flow

```
1. Parse and validate CLI flags; resolve broker address
2. Health-check broker connectivity (metadata ping, 10s timeout)
3. Delete and recreate the benchmark topic (6 partitions, RF=1)
4. Start the consumer in a background goroutine
5. Start the producer with N concurrent goroutines (blocks until completion)
6. Wait for the consumer to drain all messages (or timeout after 30s of inactivity)
7. Compute throughput and latency percentiles
8. Print formatted summary table to stdout
9. Append results as a row to the CSV output file
```

---

## 4. Results

### 4.1 Terminal Output

**Apache Kafka:**

```
==================================================================
  Kafka vs Redpanda Benchmark Results
==================================================================
Broker       Concurrency  Total      Throughput     p50        p95
------------------------------------------------------------------
kafka        4            100000     122667/s       55.29ms    291.33ms
------------------------------------------------------------------
E2E Latency (consumer):  p50 = 2014.61ms  |  p95 = 2413.97ms
Errors: 0  |  Warm-up discarded: 1000
==================================================================
```

**Redpanda:**

```
==================================================================
  Kafka vs Redpanda Benchmark Results
==================================================================
Broker       Concurrency  Total      Throughput     p50        p95
------------------------------------------------------------------
redpanda     4            100000     188745/s       11.94ms    348.52ms
------------------------------------------------------------------
E2E Latency (consumer):  p50 = 14.97ms  |  p95 = 352.65ms
Errors: 0  |  Warm-up discarded: 1000
==================================================================
```

### 4.2 CSV Output

```csv
broker,concurrency,total,throughput_msg_s,p50_ms,p95_ms,e2e_p50_ms,e2e_p95_ms,errors
kafka,4,100000,122667,55.29,291.33,2014.61,2413.97,0
redpanda,4,100000,188745,11.94,348.52,14.97,352.65,0
```

### 4.3 Comparative Summary

| Metric | Apache Kafka | Redpanda | Observation |
|---|---|---|---|
| **Throughput** | 122,667 msgs/s | 188,745 msgs/s | Redpanda completed the workload in 530ms vs Kafka's 815ms |
| **Publish p50** | 55.29 ms | 11.94 ms | Redpanda's median acknowledgment latency was 4.6x lower |
| **Publish p95** | 291.33 ms | 348.52 ms | Kafka exhibited a 16% lower tail latency at the publish level |
| **E2E p50** | 2,014.61 ms | 14.97 ms | Most significant gap — attributed to commit-to-delivery pipeline speed |
| **E2E p95** | 2,413.97 ms | 352.65 ms | Kafka's consumer received messages with significantly higher delay |
| **Errors** | 0 | 0 | Both brokers processed all 100,000 messages without failure |

---

## 5. Performance Analysis

### 5.1 Throughput

Redpanda processed 54% more messages per second than Kafka. This difference is consistent with Redpanda's architectural advantages for single-node, low-latency workloads: native C++ execution avoids JVM overhead, and the thread-per-core model eliminates lock contention.

### 5.2 Publish Latency

Redpanda's p50 publish latency (11.94ms) was 4.6x lower than Kafka's (55.29ms), reflecting the absence of JVM garbage collection pauses in Redpanda's runtime.

The p95 reversal (Kafka at 291ms vs Redpanda at 349ms) is explained by batching dynamics. Both brokers use `linger=5ms`, which causes messages to accumulate in batches. Tail latency is driven by batch flush timing, which varies with internal scheduling. This metric alone should not be interpreted as a Kafka advantage.

### 5.3 End-to-End Latency

The E2E latency gap (Kafka p50 at 2,014ms vs Redpanda p50 at 15ms) is the most significant finding. Two factors contribute:

1. **Commit-to-delivery pipeline speed** — Redpanda's C++ engine commits writes and makes them available to consumers with lower internal latency than Kafka's JVM-based pipeline.

2. **Burst workload effect** — In a burst benchmark, the producer completes before the consumer has processed all messages. Messages produced early sit in the broker until the consumer reaches them. Since Kafka's producer is slower overall, the consumer falls further behind, amplifying the time-since-produce-timestamp for each message.

### 5.4 Root Cause: Architectural Differences

| Factor | Apache Kafka | Redpanda |
|---|---|---|
| **Runtime** | JVM (Java) | Native C++ |
| **Garbage Collection** | Stop-the-world pauses (10–50ms) | No GC; manual memory management |
| **Threading Model** | Shared-state, lock-based threads | Seastar framework; thread-per-core, shared-nothing |
| **I/O Subsystem** | Java NIO (kernel-managed) | `io_uring` / direct I/O (fewer syscalls, fewer copies) |
| **Startup Behavior** | JIT requires warm-up; full optimization after sustained load | Immediately at full speed |

### 5.5 Scenarios Where Kafka Narrows the Gap

This benchmark exercised a workload profile that favors Redpanda's strengths. The following scenarios, not covered in this PoC, would likely narrow or reverse the gap:

| Scenario | Rationale |
|---|---|
| Multi-node clusters (3–5 brokers) | Kafka's replication protocol is mature and battle-tested at scale |
| Long-running sustained workloads | JIT compiler fully optimizes hot paths over time |
| Large JVM heap allocation (8 GB+) | GC pauses become infrequent with properly tuned heap |
| Tiered storage (S3 offloading) | Kafka has more mature support for tiered storage |
| Stream processing (Kafka Streams / ksqlDB) | Redpanda does not provide a native stream processing API |
| Connector ecosystem | Kafka Connect offers 200+ pre-built connectors |

---

## 6. Caveats & Limitations

| Limitation | Impact on Results |
|---|---|
| **Single-node deployment** | No replication overhead measured; production deployments typically use 3+ nodes with RF=3 |
| **Local Docker execution** | CPU, memory, and I/O are shared between the benchmark client and the broker; no network latency |
| **Burst workload (100K in <1s)** | Does not reflect sustained production throughput under steady state |
| **Replication factor = 1** | Eliminates replication cost; RF=3 would slow both brokers and may change the relative gap |
| **Same-host clock** | E2E latency relies on `time.Now()` on one machine; no clock skew, unlike distributed deployments |
| **No TLS or authentication** | Production security layers add latency; both brokers would be affected |
| **No schema registry** | Production workloads often use Avro/Protobuf with schema validation overhead |
| **Consumer starts before producer** | Does not model real-world consumer lag or late-joining consumers |

**Conclusion:** These results are directionally useful for understanding architectural differences between the two systems. Production performance will vary based on hardware specifications, network topology, cluster size, replication factor, security configuration, and workload characteristics.

---

## 7. Setup & Usage Guide

### 7.1 Prerequisites

| Requirement | Version |
|---|---|
| Go | 1.22 or later |
| Docker | 20.10+ with Compose V2 |
| Make | GNU Make (standard on macOS/Linux) |

### 7.2 Quick Start

```bash
# Clone the repository
git clone https://github.com/coolwednesday/kafka-redpanda-poc.git
cd kafka-redpanda-poc

# Start both brokers (allow ~40 seconds for health checks to pass)
make docker-up

# Run unit tests
make test

# Benchmark Apache Kafka
make run-kafka

# Benchmark Redpanda
make run-redpanda

# Run both benchmarks sequentially
make run-both

# View comparative results
cat results.csv

# Tear down containers and volumes
make docker-down
```

### 7.3 Custom Configurations

```bash
# Increase concurrency and message volume
go run . --broker=kafka --concurrency=8 --total=500000

# Use a custom topic name
go run . --broker=redpanda --topic=load-test --total=50000

# Target a remote broker
go run . --broker=kafka --kafka-addr=192.168.1.100:9092

# Disable warm-up exclusion
go run . --broker=redpanda --warmup=0
```

### 7.4 Makefile Targets

| Target | Description |
|---|---|
| `make build` | Compile the benchmark binary |
| `make test` | Run all unit tests with verbose output |
| `make docker-up` | Start Kafka and Redpanda containers |
| `make docker-down` | Stop containers and remove associated volumes |
| `make run-kafka` | Build and execute the benchmark against Kafka |
| `make run-redpanda` | Build and execute the benchmark against Redpanda |
| `make run-both` | Execute both benchmarks sequentially |
| `make clean` | Remove the compiled binary and results CSV |

---

## 8. CLI Reference

| Flag | Type | Default | Description |
|---|---|---|---|
| `--broker` | `string` | `kafka` | Target broker: `kafka` or `redpanda` |
| `--concurrency` | `int` | `4` | Number of concurrent producer goroutines |
| `--total` | `int` | `100000` | Total number of messages to produce |
| `--kafka-addr` | `string` | `localhost:9092` | Kafka broker address |
| `--redpanda-addr` | `string` | `localhost:19092` | Redpanda broker address |
| `--csv` | `string` | `results.csv` | Path for CSV output file |
| `--topic` | `string` | `benchmark` | Topic name used for the benchmark |
| `--warmup` | `int` | `1000` | Number of initial messages excluded from latency metrics |
| `--consumer-timeout` | `duration` | `30s` | Consumer exits after this duration of inactivity |

---

## 9. Project Structure

```
kafka-redpanda-poc/
├── main.go                  # CLI entry point, orchestration, signal handling
├── producer/
│   └── producer.go          # Concurrent producer with latency tracking
├── consumer/
│   └── consumer.go          # Consumer with end-to-end latency measurement
├── payload/
│   ├── payload.go           # Deterministic message generator (seeded RNG)
│   └── payload_test.go      # Unit tests: distribution, size, determinism
├── metrics/
│   ├── metrics.go           # Latency collector, percentile computation, CSV export
│   └── metrics_test.go      # Unit tests: percentile accuracy, CSV formatting
├── docker-compose.yaml      # Kafka (KRaft) and Redpanda container definitions
├── Makefile                 # Build, test, and run automation
├── ARCHITECTURE.md          # Technical architecture documentation
├── README.md                # This report
├── go.mod                   # Go module definition
└── go.sum                   # Dependency integrity checksums
```

For detailed technical architecture, component diagrams, data flow documentation, and design decision rationale, see [ARCHITECTURE.md](ARCHITECTURE.md).

---

## 10. References

| Resource | Link |
|---|---|
| Apache Kafka Documentation | https://kafka.apache.org/documentation/ |
| Redpanda Documentation | https://docs.redpanda.com/ |
| franz-go Client Library | https://github.com/twmb/franz-go |
| KRaft (Kafka without ZooKeeper) | https://developer.confluent.io/learn/kraft/ |
| Seastar Framework (Redpanda's foundation) | https://seastar.io/ |
