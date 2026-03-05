package broker

import (
	"context"
	"fmt"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

// ResolveAddr returns the broker address based on the broker type.
func ResolveAddr(brokerType, kafkaAddr, redpandaAddr string) (string, error) {
	switch brokerType {
	case "kafka":
		return kafkaAddr, nil
	case "redpanda":
		return redpandaAddr, nil
	default:
		return "", fmt.Errorf("--broker must be 'kafka' or 'redpanda', got %q", brokerType)
	}
}

// HealthCheck verifies that the broker is reachable.
func HealthCheck(ctx context.Context, brokers []string) error {
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

// RecreateTopic deletes and recreates a topic with the given partition count.
func RecreateTopic(ctx context.Context, brokers []string, topic string, partitions int32) error {
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

	// Create topic with specified partitions, replication factor 1
	resp, err := admin.CreateTopics(ctx, int32(partitions), 1, nil, topic)
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
