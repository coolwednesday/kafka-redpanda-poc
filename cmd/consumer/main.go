package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"kafka-redpanda-poc/broker"
	"kafka-redpanda-poc/consumer"
	"kafka-redpanda-poc/metrics"
)

func main() {
	brokerType := flag.String("broker", "kafka", "Broker type: kafka or redpanda")
	total := flag.Int("total", 100000, "Total messages to consume")
	kafkaAddr := flag.String("kafka-addr", "localhost:9092", "Kafka broker address")
	redpandaAddr := flag.String("redpanda-addr", "localhost:19092", "Redpanda broker address")
	topic := flag.String("topic", "benchmark", "Topic name")
	groupID := flag.String("group", "", "Consumer group ID (auto-generated if empty)")
	timeout := flag.Duration("timeout", 30*time.Second, "Timeout after last message received")

	flag.Parse()

	// Resolve broker address
	brokerAddr, err := broker.ResolveAddr(*brokerType, *kafkaAddr, *redpandaAddr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	brokers := []string{brokerAddr}

	// Auto-generate group ID if not provided
	if *groupID == "" {
		*groupID = fmt.Sprintf("bench-%d", time.Now().UnixNano())
	}

	slog.Info("starting consumer",
		"broker", *brokerType,
		"address", brokerAddr,
		"topic", *topic,
		"group", *groupID,
		"total", *total,
		"timeout", *timeout,
	)

	// Setup context with signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		slog.Warn("received signal, shutting down gracefully", "signal", sig)
		cancel()
	}()

	// Health check
	if err := broker.HealthCheck(ctx, brokers); err != nil {
		slog.Error("broker health check failed", "error", err)
		os.Exit(1)
	}
	slog.Info("broker health check passed")

	// Initialize E2E metrics collector
	collector := metrics.NewCollector()

	// Run consumer
	result, err := consumer.Run(ctx, consumer.Config{
		Brokers:   brokers,
		Topic:     *topic,
		GroupID:   *groupID,
		Total:     *total,
		Timeout:   *timeout,
		Collector: collector,
	})
	if err != nil {
		slog.Error("consumer error", "error", err)
		os.Exit(1)
	}

	slog.Info("consumer finished", "consumed", result.Consumed)

	// Print E2E latency results
	fmt.Println()
	fmt.Println("==================================================================")
	fmt.Println("  Consumer E2E Latency Results")
	fmt.Println("==================================================================")
	fmt.Printf("Broker:    %s\n", *brokerType)
	fmt.Printf("Consumed:  %d / %d\n", result.Consumed, *total)
	fmt.Printf("E2E p50:   %s\n", fmtDuration(collector.P50()))
	fmt.Printf("E2E p95:   %s\n", fmtDuration(collector.P95()))
	fmt.Println("==================================================================")
	fmt.Println()
}

func fmtDuration(d time.Duration) string {
	if d == 0 {
		return "N/A"
	}
	if d < time.Millisecond {
		return fmt.Sprintf("%.1fus", float64(d.Microseconds()))
	}
	return fmt.Sprintf("%.2fms", float64(d.Microseconds())/1000.0)
}
