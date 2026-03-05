package producer

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"

	"kafka-redpanda-poc/metrics"
	"kafka-redpanda-poc/payload"
)

type Config struct {
	Brokers     []string
	Topic       string
	Concurrency int
	Total       int
	Warmup      int
	Collector   *metrics.Collector
}

type Result struct {
	Produced int64
	Errors   int64
	Elapsed  time.Duration
}

func Run(ctx context.Context, cfg Config) (Result, error) {
	cl, err := kgo.NewClient(
		kgo.SeedBrokers(cfg.Brokers...),
		kgo.DefaultProduceTopic(cfg.Topic),
		kgo.RequiredAcks(kgo.AllISRAcks()),
		kgo.ProducerLinger(5*time.Millisecond),
		kgo.ProducerBatchMaxBytes(16*1024),
		kgo.MaxBufferedRecords(10000),
	)
	if err != nil {
		return Result{}, fmt.Errorf("create producer client: %w", err)
	}
	defer cl.Close()

	var (
		counter   atomic.Int64
		produced  atomic.Int64
		errCount  atomic.Int64
		wg        sync.WaitGroup
	)

	start := time.Now()

	for i := 0; i < cfg.Concurrency; i++ {
		wg.Add(1)
		// Each goroutine gets its own generator to avoid data races.
		// Derived seed ensures deterministic distribution per goroutine.
		gen := payload.NewGenerator(42 + int64(i))
		go func() {
			defer wg.Done()
			for {
				idx := int(counter.Add(1) - 1)
				if idx >= cfg.Total {
					return
				}

				select {
				case <-ctx.Done():
					return
				default:
				}

				data, _ := gen.Generate(idx)
				rec := &kgo.Record{
					Value: data,
					Headers: []kgo.RecordHeader{
						{Key: "produce_ts", Value: []byte(strconv.FormatInt(time.Now().UnixNano(), 10))},
					},
				}

				sendStart := time.Now()
				cl.Produce(ctx, rec, func(r *kgo.Record, err error) {
					if err != nil {
						errCount.Add(1)
						slog.Error("produce failed", "index", idx, "error", err)
						return
					}
					_ = r

					latency := time.Since(sendStart)
					produced.Add(1)

					// Skip warmup samples
					if idx >= cfg.Warmup {
						cfg.Collector.Record(latency)
					}
				})
			}
		}()
	}

	wg.Wait()

	// Flush any buffered records
	if err := cl.Flush(ctx); err != nil {
		slog.Error("flush failed", "error", err)
	}

	elapsed := time.Since(start)

	return Result{
		Produced: produced.Load(),
		Errors:   errCount.Load(),
		Elapsed:  elapsed,
	}, nil
}
