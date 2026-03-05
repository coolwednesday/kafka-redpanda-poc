package producer

import (
	"context"
	"testing"
	"time"

	"kafka-redpanda-poc/metrics"
)

func TestConfig_Fields(t *testing.T) {
	collector := metrics.NewCollector()
	cfg := Config{
		Brokers:     []string{"localhost:9092"},
		Topic:       "test-topic",
		Concurrency: 4,
		Total:       1000,
		Warmup:      100,
		Collector:   collector,
	}

	if cfg.Concurrency != 4 {
		t.Errorf("expected concurrency 4, got %d", cfg.Concurrency)
	}
	if cfg.Total != 1000 {
		t.Errorf("expected total 1000, got %d", cfg.Total)
	}
	if cfg.Warmup != 100 {
		t.Errorf("expected warmup 100, got %d", cfg.Warmup)
	}
	if cfg.Topic != "test-topic" {
		t.Errorf("expected topic test-topic, got %s", cfg.Topic)
	}
	if len(cfg.Brokers) != 1 || cfg.Brokers[0] != "localhost:9092" {
		t.Errorf("unexpected brokers: %v", cfg.Brokers)
	}
	if cfg.Collector == nil {
		t.Error("collector should not be nil")
	}
}

func TestResult_Fields(t *testing.T) {
	r := Result{
		Produced: 10000,
		Errors:   5,
		Elapsed:  2 * time.Second,
	}

	if r.Produced != 10000 {
		t.Errorf("expected produced 10000, got %d", r.Produced)
	}
	if r.Errors != 5 {
		t.Errorf("expected errors 5, got %d", r.Errors)
	}
	if r.Elapsed != 2*time.Second {
		t.Errorf("expected elapsed 2s, got %v", r.Elapsed)
	}
}

func TestRun_ContextAlreadyCancelled(t *testing.T) {
	collector := metrics.NewCollector()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	cfg := Config{
		Brokers:     []string{"localhost:19999"},
		Topic:       "test",
		Concurrency: 2,
		Total:       100000,
		Warmup:      0,
		Collector:   collector,
	}

	result, err := Run(ctx, cfg)
	if err != nil {
		// Client creation can fail with cancelled context — acceptable
		return
	}

	// With a cancelled context, should not produce all messages
	if result.Produced == int64(cfg.Total) {
		t.Error("should not produce all messages with cancelled context")
	}
}

func TestRun_ZeroTotal(t *testing.T) {
	collector := metrics.NewCollector()

	cfg := Config{
		Brokers:     []string{"localhost:19999"},
		Topic:       "test",
		Concurrency: 2,
		Total:       0,
		Warmup:      0,
		Collector:   collector,
	}

	result, err := Run(context.Background(), cfg)
	if err != nil {
		return // client creation failure is acceptable
	}

	if result.Produced != 0 {
		t.Errorf("expected 0 produced for total=0, got %d", result.Produced)
	}
	if result.Errors != 0 {
		t.Errorf("expected 0 errors for total=0, got %d", result.Errors)
	}
}
