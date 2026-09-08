.PHONY: tidy build test run migrate-up migrate-down docker-up docker-down vet lint

BIN_DIR := .bin

tidy:
	go mod tidy

build:
	mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/api ./cmd/api
	go build -o $(BIN_DIR)/migrate ./cmd/migrate
	go build -o $(BIN_DIR)/worker ./cmd/worker

vet:
	go vet ./...

test:
	go test ./...

run: build
	./$(BIN_DIR)/api

migrate-up: build
	./$(BIN_DIR)/migrate -command=up

migrate-down: build
	./$(BIN_DIR)/migrate -command=down

docker-up:
	docker compose up -d

docker-down:
	docker compose down

# Full local bootstrap: infra → migrate → api
bootstrap: docker-up
	@echo "waiting for postgres..."
	@sleep 3
	$(MAKE) migrate-up
	$(MAKE) run
