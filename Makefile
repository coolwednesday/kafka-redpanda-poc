.PHONY: build test run-kafka run-redpanda docker-up docker-down clean

BINARY := kafka-redpanda-poc

build:
	go build -o $(BINARY) .

test:
	go test ./... -v

docker-up:
	docker compose up -d
	@echo "Waiting for brokers to be healthy..."
	@docker compose ps --format "table {{.Name}}\t{{.Status}}"

docker-down:
	docker compose down -v

run-kafka: build
	./$(BINARY) --broker=kafka --concurrency=4 --total=100000

run-redpanda: build
	./$(BINARY) --broker=redpanda --concurrency=4 --total=100000

run-both: build
	@echo "=== Running Kafka benchmark ==="
	./$(BINARY) --broker=kafka --concurrency=4 --total=100000
	@echo ""
	@echo "=== Running Redpanda benchmark ==="
	./$(BINARY) --broker=redpanda --concurrency=4 --total=100000

clean:
	rm -f $(BINARY) results.csv
