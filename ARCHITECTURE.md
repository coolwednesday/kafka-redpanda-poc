# Architecture

## System Overview

```
+------------------------------------------------------------------+
|                        CLI (main.go)                             |
|  Flags: --broker, --concurrency, --total, --warmup, --csv       |
|                                                                  |
|  1. Validate flags                                               |
|  2. Health check broker                                          |
|  3. Delete + recreate topic                                      |
|  4. Launch consumer (background)                                 |
|  5. Launch producer (blocking)                                   |
|  6. Wait for consumer drain                                      |
|  7. Print summary + export CSV                                   |
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
|  | G1 | | G2 |..| GN |  |  |  fresh group per run    |
|  +--+-+ +--+-+  +--+-+  |  |                         |
|     |      |       |     |  |  Reads produce_ts       |
|     +------+-------+     |  |  header for E2E latency |
|            |             |  |                         |
+------------|-------------+  +------------|------------+
             |                             |
             v                             v
+-------------------------+  +-------------------------+
| Produce Latency         |  | E2E Latency             |
| Collector               |  | Collector               |
| (metrics/metrics.go)    |  | (metrics/metrics.go)    |
+------------+------------+  +------------+------------+
             |                             |
             +-------------+---------------+
                           |
                           v
              +------------------------+
              |    Summary + CSV       |
              |  - Throughput (msg/s)  |
              |  - p50 / p95 publish   |
              |  - p50 / p95 e2e      |
              |  - Error count         |
              +------------------------+
```

## Broker Topology

```
+-------------------+         +-------------------+
|   Kafka (KRaft)   |         |     Redpanda      |
|   Port: 9092      |         |   Port: 19092     |
|                   |         |                   |
|  Single node,     |         |  Single node,     |
|  no ZooKeeper     |         |  --smp 1          |
|                   |         |  --memory 512M    |
|  Topic:benchmark  |         |  Topic:benchmark  |
|  6 partitions     |         |  6 partitions     |
|  RF: 1            |         |  RF: 1            |
+-------------------+         +-------------------+
        ^                             ^
        |       --broker=kafka        |
        +-------  OR  ----------------+
                --broker=redpanda
```

## Data Flow

```
                    Produce Path
                    ============

  Payload Generator (seed=42)
  +------------------------------------------+
  | 70% OrderPlaced                          |
  | 20% PaymentSettled                       |
  | 10% InventoryAdjusted                    |
  | ~512 bytes JSON each                     |
  +--------------------+---------------------+
                       |
                       v
  +--------------------+---------------------+
  |           Kafka Record                    |
  |  Value:   {"type":"OrderPlaced",...}      |
  |  Header:  produce_ts = UnixNano          |
  +--------------------+---------------------+
                       |
          acks=all, linger=5ms,
          batch=16KB
                       |
                       v
  +--------------------+---------------------+
  |              Broker                       |
  |    (Kafka on :9092 / Redpanda on :19092) |
  +--------------------+---------------------+
                       |
                       v

                    Consume Path
                    ============

  +--------------------+---------------------+
  |         Consumer Group                    |
  |    (bench-<unix-nanos>, unique/run)       |
  |                                           |
  |  For each record:                         |
  |    1. Extract produce_ts header           |
  |    2. E2E latency = now() - produce_ts    |
  |    3. Record in E2E collector             |
  |                                           |
  |  Exit when: consumed == total OR timeout  |
  +-------------------------------------------+
```

## Producer Concurrency Model

```
                   Shared Atomic Counter
                   (0 -> total-1)
                          |
            +-------------+-------------+
            |             |             |
        +---v---+     +---v---+     +---v---+
        |  G0   |     |  G1   |     | G(N-1)|
        | seed  |     | seed  |     | seed  |
        | =42   |     | =43   |     |=42+N-1|
        +---+---+     +---+---+     +---+---+
            |             |             |
            |  idx < warmup?            |
            |  YES: discard latency     |
            |  NO:  record latency      |
            |             |             |
            +------+------+------+------+
                   |
                   v
           Produce Collector
           (thread-safe, mutex)
```

## Metrics Pipeline

```
  Raw Samples ([]time.Duration)
         |
         v
  Sort ascending
         |
         v
  +------+------+
  | p50 = [50%] |
  | p95 = [95%] |
  +------+------+
         |
         +----------+-----------+
         |                      |
         v                      v
  +------+------+      +-------+-------+
  | PrintSummary |      |  ExportCSV    |
  | (stdout)     |      | (append mode) |
  +--------------+      +---------------+
```

## File Map

```
kafka-redpanda-poc/
|
+-- main.go                 CLI entry, flag parsing, orchestration,
|                           signal handling, health check, topic mgmt
|
+-- producer/
|   +-- producer.go         N-goroutine producer, atomic counter,
|                           warmup discard, error tracking
|
+-- consumer/
|   +-- consumer.go         Single consumer, fresh group/run,
|                           E2E latency via produce_ts header
|
+-- payload/
|   +-- payload.go          Seeded RNG, weighted type selection,
|   |                       ~512B JSON padding
|   +-- payload_test.go     Distribution, size, determinism tests
|
+-- metrics/
|   +-- metrics.go          Thread-safe collector, percentiles,
|   |                       summary table, CSV export
|   +-- metrics_test.go     Percentile accuracy, CSV format tests
|
+-- docker-compose.yaml     Kafka KRaft + Redpanda, health checks
+-- Makefile                build, test, run-kafka, run-redpanda
+-- README.md               Quick start, flag reference
+-- ARCHITECTURE.md         This file
```
