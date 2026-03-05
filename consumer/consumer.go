package consumer

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"

	"kafka-redpanda-poc/metrics"
)

type Config struct {
	Brokers     []string
	Topic       string
	GroupID     string
	Total       int
	Timeout     time.Duration
	Collector   *metrics.Collector
}

type Result struct {
	Consumed int64
}

func Run(ctx context.Context, cfg Config) (Result, error) {
	cl, err := kgo.NewClient(
		kgo.SeedBrokers(cfg.Brokers...),
		kgo.ConsumerGroup(cfg.GroupID),
		kgo.ConsumeTopics(cfg.Topic),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
	)
	if err != nil {
		return Result{}, fmt.Errorf("create consumer client: %w", err)
	}
	defer cl.Close()

	var consumed atomic.Int64
	target := int64(cfg.Total)
	lastMsg := time.Now()

	for {
		select {
		case <-ctx.Done():
			slog.Warn("consumer cancelled", "consumed", consumed.Load(), "target", target)
			return Result{Consumed: consumed.Load()}, nil
		default:
		}

		fetches := cl.PollFetches(ctx)
		if fetches.IsClientClosed() {
			break
		}

		errs := fetches.Errors()
		for _, e := range errs {
			slog.Error("consumer fetch error", "topic", e.Topic, "partition", e.Partition, "error", e.Err)
		}

		fetches.EachRecord(func(r *kgo.Record) {
			lastMsg = time.Now()
			consumed.Add(1)

			// Extract produce_ts header for e2e latency
			for _, h := range r.Headers {
				if h.Key == "produce_ts" {
					tsNano, err := strconv.ParseInt(string(h.Value), 10, 64)
					if err != nil {
						slog.Warn("invalid produce_ts header", "value", string(h.Value))
						continue
					}
					produceTime := time.Unix(0, tsNano)
					e2eLatency := time.Since(produceTime)
					cfg.Collector.Record(e2eLatency)
				}
			}
		})

		if consumed.Load() >= target {
			slog.Info("consumer reached target", "consumed", consumed.Load())
			break
		}

		// Timeout if no messages received for cfg.Timeout duration
		if time.Since(lastMsg) > cfg.Timeout {
			slog.Warn("consumer timed out waiting for messages",
				"consumed", consumed.Load(),
				"target", target,
				"timeout", cfg.Timeout,
			)
			break
		}
	}

	return Result{Consumed: consumed.Load()}, nil
}
