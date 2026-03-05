package consumer

import (
	"context"
	"testing"
	"time"

	"kafka-redpanda-poc/metrics"
)

func TestConfig_Fields(t *testing.T) {
	collector := metrics.NewCollector()
	cfg := Config{
		Brokers:   []string{"localhost:9092"},
		Topic:     "test-topic",
		GroupID:   "test-group",
		Total:     5000,
		Timeout:   30 * time.Second,
		Collector: collector,
	}

	if cfg.Topic != "test-topic" {
		t.Errorf("expected topic test-topic, got %s", cfg.Topic)
	}
	if cfg.GroupID != "test-group" {
		t.Errorf("expected group test-group, got %s", cfg.GroupID)
	}
	if cfg.Total != 5000 {
		t.Errorf("expected total 5000, got %d", cfg.Total)
	}
	if cfg.Timeout != 30*time.Second {
		t.Errorf("expected timeout 30s, got %v", cfg.Timeout)
	}
	if len(cfg.Brokers) != 1 || cfg.Brokers[0] != "localhost:9092" {
		t.Errorf("unexpected brokers: %v", cfg.Brokers)
	}
	if cfg.Collector == nil {
		t.Error("collector should not be nil")
	}
}

func TestResult_Fields(t *testing.T) {
	r := Result{Consumed: 9500}
	if r.Consumed != 9500 {
		t.Errorf("expected consumed 9500, got %d", r.Consumed)
	}
}

func TestRun_ContextCancelled(t *testing.T) {
	collector := metrics.NewCollector()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	cfg := Config{
		Brokers:   []string{"localhost:19999"},
		Topic:     "test",
		GroupID:   "test-cancelled",
		Total:     1000,
		Timeout:   1 * time.Second,
		Collector: collector,
	}

	result, err := Run(ctx, cfg)
	if err != nil {
		// Client creation can fail with cancelled context — acceptable
		return
	}

	// With cancelled context, should consume 0 messages
	if result.Consumed != 0 {
		t.Errorf("expected 0 consumed with cancelled context, got %d", result.Consumed)
	}
}

func TestRun_UnreachableBroker(t *testing.T) {
	collector := metrics.NewCollector()

	// Use a very short timeout so the test doesn't hang
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cfg := Config{
		Brokers:   []string{"localhost:19999"},
		Topic:     "nonexistent",
		GroupID:   "test-unreachable",
		Total:     100,
		Timeout:   1 * time.Second,
		Collector: collector,
	}

	result, err := Run(ctx, cfg)
	if err != nil {
		return // client creation failure is acceptable
	}

	// Should timeout with 0 consumed
	if result.Consumed != 0 {
		t.Errorf("expected 0 consumed for unreachable broker, got %d", result.Consumed)
	}
}
