.PHONY: build build-all test docker-up docker-down \
       run-kafka run-redpanda run-both \
       run-producer-kafka run-producer-redpanda \
       run-consumer-kafka run-consumer-redpanda \
       clean

# Combined benchmark binary (producer + consumer in one)
BENCHMARK := bin/benchmark

# Standalone CLIs
PRODUCER := bin/producer
CONSUMER := bin/consumer

build:
	go build -o $(BENCHMARK) ./cmd/benchmark

build-all:
	go build -o $(BENCHMARK) ./cmd/benchmark
	go build -o $(PRODUCER) ./cmd/producer
	go build -o $(CONSUMER) ./cmd/consumer

test:
	go test ./... -v

docker-up:
	docker compose up -d
	@echo "Waiting for brokers to be healthy..."
	@docker compose ps --format "table {{.Name}}\t{{.Status}}"

docker-down:
	docker compose down -v

# --- Combined benchmark (producer + consumer together) ---

run-kafka: build
	./$(BENCHMARK) --broker=kafka --concurrency=4 --total=100000

run-redpanda: build
	./$(BENCHMARK) --broker=redpanda --concurrency=4 --total=100000

run-both: build
	@echo "=== Running Kafka benchmark ==="
	./$(BENCHMARK) --broker=kafka --concurrency=4 --total=100000
	@echo ""
	@echo "=== Running Redpanda benchmark ==="
	./$(BENCHMARK) --broker=redpanda --concurrency=4 --total=100000

# --- Standalone producer CLI ---

run-producer-kafka: build-all
	./$(PRODUCER) --broker=kafka --concurrency=4 --total=100000

run-producer-redpanda: build-all
	./$(PRODUCER) --broker=redpanda --concurrency=4 --total=100000

# --- Standalone consumer CLI ---

run-consumer-kafka: build-all
	./$(CONSUMER) --broker=kafka --total=100000

run-consumer-redpanda: build-all
	./$(CONSUMER) --broker=redpanda --total=100000

clean:
	rm -rf bin/ results.csv
