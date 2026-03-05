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

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"

	"kafka-redpanda-poc/consumer"
	"kafka-redpanda-poc/metrics"
	"kafka-redpanda-poc/producer"
)

func main() {
	broker := flag.String("broker", "kafka", "Broker type: kafka or redpanda")
	concurrency := flag.Int("concurrency", 4, "Number of concurrent producers")
	total := flag.Int("total", 100000, "Total messages to produce")
	kafkaAddr := flag.String("kafka-addr", "localhost:9092", "Kafka broker address")
	redpandaAddr := flag.String("redpanda-addr", "localhost:19092", "Redpanda broker address")
	csvFile := flag.String("csv", "results.csv", "CSV output file")
	topic := flag.String("topic", "benchmark", "Topic name")
	warmup := flag.Int("warmup", 1000, "Warmup messages to discard from metrics")
	consumerTimeout := flag.Duration("consumer-timeout", 30*time.Second, "Consumer timeout after last message")

	flag.Parse()

	// Validate broker flag
	if *broker != "kafka" && *broker != "redpanda" {
		fmt.Fprintf(os.Stderr, "Error: --broker must be 'kafka' or 'redpanda', got %q\n", *broker)
		os.Exit(1)
	}

	// Resolve broker address
	var brokerAddr string
	switch *broker {
	case "kafka":
		brokerAddr = *kafkaAddr
	case "redpanda":
		brokerAddr = *redpandaAddr
	}
	brokers := []string{brokerAddr}

	slog.Info("starting benchmark",
		"broker", *broker,
		"address", brokerAddr,
		"concurrency", *concurrency,
		"total", *total,
		"warmup", *warmup,
		"topic", *topic,
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
	if err := healthCheck(ctx, brokers); err != nil {
		slog.Error("broker health check failed", "error", err)
		os.Exit(1)
	}
	slog.Info("broker health check passed")

	// Topic management: delete and recreate
	if err := recreateTopic(ctx, brokers, *topic); err != nil {
		slog.Error("topic setup failed", "error", err)
		os.Exit(1)
	}
	slog.Info("topic ready", "topic", *topic)

	// Wait briefly for topic to propagate
	time.Sleep(2 * time.Second)

	// Initialize metrics collectors
	produceCollector := metrics.NewCollector()
	e2eCollector := metrics.NewCollector()

	// Start consumer in background
	consumerGroupID := fmt.Sprintf("bench-%d", time.Now().UnixNano())
	consumerDone := make(chan consumer.Result, 1)

	go func() {
		result, err := consumer.Run(ctx, consumer.Config{
			Brokers:   brokers,
			Topic:     *topic,
			GroupID:   consumerGroupID,
			Total:     *total,
			Timeout:   *consumerTimeout,
			Collector: e2eCollector,
		})
		if err != nil {
			slog.Error("consumer error", "error", err)
		}
		consumerDone <- result
	}()

	// Give consumer time to join group
	time.Sleep(1 * time.Second)

	// Run producer
	slog.Info("starting producer")
	prodResult, err := producer.Run(ctx, producer.Config{
		Brokers:     brokers,
		Topic:       *topic,
		Concurrency: *concurrency,
		Total:       *total,
		Warmup:      *warmup,
		Collector:   produceCollector,
	})
	if err != nil {
		slog.Error("producer error", "error", err)
		os.Exit(1)
	}
	slog.Info("producer finished",
		"produced", prodResult.Produced,
		"errors", prodResult.Errors,
		"elapsed", prodResult.Elapsed,
	)

	// Wait for consumer to drain
	slog.Info("waiting for consumer to drain")
	consResult := <-consumerDone
	slog.Info("consumer finished", "consumed", consResult.Consumed)

	// Compute results
	throughput := float64(prodResult.Produced) / prodResult.Elapsed.Seconds()

	result := metrics.Result{
		Broker:      *broker,
		Concurrency: *concurrency,
		Total:       int(prodResult.Produced),
		Elapsed:     prodResult.Elapsed,
		Throughput:  throughput,
		P50:         produceCollector.P50(),
		P95:         produceCollector.P95(),
		E2EP50:      e2eCollector.P50(),
		E2EP95:      e2eCollector.P95(),
		Errors:      prodResult.Errors,
		Warmup:      *warmup,
	}

	// Print summary
	metrics.PrintSummary(result)

	// Export CSV
	if err := metrics.ExportCSV(*csvFile, result); err != nil {
		slog.Error("csv export failed", "error", err)
	} else {
		slog.Info("results exported", "file", *csvFile)
	}
}

func healthCheck(ctx context.Context, brokers []string) error {
	cl, err := kgo.NewClient(kgo.SeedBrokers(brokers...))
	if err != nil {
		return fmt.Errorf("create client: %w", err)
	}
	defer cl.Close()

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := cl.Ping(ctx); err != nil {
		return fmt.Errorf("ping broker: %w", err)
	}
	return nil
}

func recreateTopic(ctx context.Context, brokers []string, topic string) error {
	cl, err := kgo.NewClient(kgo.SeedBrokers(brokers...))
	if err != nil {
		return fmt.Errorf("create admin client: %w", err)
	}
	defer cl.Close()

	admin := kadm.NewClient(cl)

	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	// Delete topic (ignore errors if it doesn't exist)
	_, _ = admin.DeleteTopics(ctx, topic)
	time.Sleep(1 * time.Second)

	// Create topic with 6 partitions, replication factor 1
	resp, err := admin.CreateTopics(ctx, 6, 1, nil, topic)
	if err != nil {
		return fmt.Errorf("create topic: %w", err)
	}

	for _, t := range resp {
		if t.Err != nil {
			return fmt.Errorf("create topic %s: %w", t.Topic, t.Err)
		}
	}

	return nil
}
