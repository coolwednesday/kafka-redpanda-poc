# Kafka vs Redpanda Performance PoC

A self-contained Go benchmark CLI that compares Apache Kafka and Redpanda throughput & latency using identical code paths.

## Prerequisites

- Go 1.22+
- Docker & Docker Compose

## Quick Start

```bash
# Start both brokers
make docker-up

# Wait ~30s for health checks to pass, then:
make run-kafka
make run-redpanda

# Or run both sequentially:
make run-both

# View results
cat results.csv

# Clean up
make docker-down
```

## CLI Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--broker` | `kafka` | Broker type: `kafka` or `redpanda` |
| `--concurrency` | `4` | Number of concurrent producer goroutines |
| `--total` | `100000` | Total messages to produce |
| `--kafka-addr` | `localhost:9092` | Kafka broker address |
| `--redpanda-addr` | `localhost:19092` | Redpanda broker address |
| `--csv` | `results.csv` | CSV output file |
| `--topic` | `benchmark` | Topic name |
| `--warmup` | `1000` | Warmup messages excluded from metrics |
| `--consumer-timeout` | `30s` | Consumer timeout after last message |

## Payload Mix

Deterministic (seed=42) distribution:
- 70% `OrderPlaced`
- 20% `PaymentSettled`
- 10% `InventoryAdjusted`
- ~512 bytes each

## Sample Output

```
==================================================================
  Kafka vs Redpanda Benchmark Results
==================================================================
Broker       Concurrency  Total      Throughput     p50        p95
------------------------------------------------------------------
kafka        4            100000     45231/s        1.20ms     3.80ms
------------------------------------------------------------------
E2E Latency (consumer):  p50 = 5.10ms  |  p95 = 12.40ms
Errors: 0  |  Warm-up discarded: 1000
==================================================================
```

## Architecture

- **franz-go**: Single Kafka protocol library that works with both Kafka and Redpanda
- **Producer**: N concurrent goroutines with shared atomic counter
- **Consumer**: Single consumer with fresh group per run for clean state
- **Metrics**: In-process latency collection with sorted percentile calculation
