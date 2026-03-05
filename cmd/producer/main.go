package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"kafka-redpanda-poc/broker"
	"kafka-redpanda-poc/metrics"
	"kafka-redpanda-poc/producer"
)

func main() {
	brokerType := flag.String("broker", "kafka", "Broker type: kafka or redpanda")
	concurrency := flag.Int("concurrency", 4, "Number of concurrent producers")
	total := flag.Int("total", 100000, "Total messages to produce")
	kafkaAddr := flag.String("kafka-addr", "localhost:9092", "Kafka broker address")
	redpandaAddr := flag.String("redpanda-addr", "localhost:19092", "Redpanda broker address")
	csvFile := flag.String("csv", "results.csv", "CSV output file")
	topic := flag.String("topic", "benchmark", "Topic name")
	warmup := flag.Int("warmup", 1000, "Warmup messages to discard from metrics")
	setupTopic := flag.Bool("setup-topic", true, "Delete and recreate topic before producing")

	flag.Parse()

	// Resolve broker address
	brokerAddr, err := broker.ResolveAddr(*brokerType, *kafkaAddr, *redpandaAddr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	brokers := []string{brokerAddr}

	slog.Info("starting producer",
		"broker", *brokerType,
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
	if err := broker.HealthCheck(ctx, brokers); err != nil {
		slog.Error("broker health check failed", "error", err)
		os.Exit(1)
	}
	slog.Info("broker health check passed")

	// Topic setup
	if *setupTopic {
		if err := broker.RecreateTopic(ctx, brokers, *topic, 6); err != nil {
			slog.Error("topic setup failed", "error", err)
			os.Exit(1)
		}
		slog.Info("topic ready", "topic", *topic)
	}

	// Initialize metrics collector
	collector := metrics.NewCollector()

	// Run producer
	slog.Info("producing messages")
	result, err := producer.Run(ctx, producer.Config{
		Brokers:     brokers,
		Topic:       *topic,
		Concurrency: *concurrency,
		Total:       *total,
		Warmup:      *warmup,
		Collector:   collector,
	})
	if err != nil {
		slog.Error("producer error", "error", err)
		os.Exit(1)
	}

	slog.Info("producer finished",
		"produced", result.Produced,
		"errors", result.Errors,
		"elapsed", result.Elapsed,
	)

	// Compute and print results
	throughput := float64(result.Produced) / result.Elapsed.Seconds()

	res := metrics.Result{
		Broker:      *brokerType,
		Concurrency: *concurrency,
		Total:       int(result.Produced),
		Elapsed:     result.Elapsed,
		Throughput:  throughput,
		P50:         collector.P50(),
		P95:         collector.P95(),
		Errors:      result.Errors,
		Warmup:      *warmup,
	}

	metrics.PrintSummary(res)

	if *csvFile != "" {
		if err := metrics.ExportCSV(*csvFile, res); err != nil {
			slog.Error("csv export failed", "error", err)
		} else {
			slog.Info("results exported", "file", *csvFile)
		}
	}
}
