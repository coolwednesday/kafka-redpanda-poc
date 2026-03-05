package broker

import (
	"testing"
)

func TestResolveAddr_Kafka(t *testing.T) {
	addr, err := ResolveAddr("kafka", "localhost:9092", "localhost:19092")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if addr != "localhost:9092" {
		t.Errorf("expected localhost:9092, got %s", addr)
	}
}

func TestResolveAddr_Redpanda(t *testing.T) {
	addr, err := ResolveAddr("redpanda", "localhost:9092", "localhost:19092")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if addr != "localhost:19092" {
		t.Errorf("expected localhost:19092, got %s", addr)
	}
}

func TestResolveAddr_Invalid(t *testing.T) {
	_, err := ResolveAddr("rabbitmq", "localhost:9092", "localhost:19092")
	if err == nil {
		t.Fatal("expected error for invalid broker type")
	}
}

func TestResolveAddr_Empty(t *testing.T) {
	_, err := ResolveAddr("", "localhost:9092", "localhost:19092")
	if err == nil {
		t.Fatal("expected error for empty broker type")
	}
}

func TestResolveAddr_CustomAddresses(t *testing.T) {
	addr, err := ResolveAddr("kafka", "10.0.0.1:9093", "10.0.0.2:19093")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if addr != "10.0.0.1:9093" {
		t.Errorf("expected 10.0.0.1:9093, got %s", addr)
	}

	addr, err = ResolveAddr("redpanda", "10.0.0.1:9093", "10.0.0.2:19093")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if addr != "10.0.0.2:19093" {
		t.Errorf("expected 10.0.0.2:19093, got %s", addr)
	}
}

func TestHealthCheck_UnreachableBroker(t *testing.T) {
	// Use a port that's very unlikely to have a Kafka broker
	err := HealthCheck(t.Context(), []string{"localhost:19999"})
	if err == nil {
		t.Fatal("expected error for unreachable broker")
	}
}
