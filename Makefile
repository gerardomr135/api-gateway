# Makefile: shortcuts for the lab. Run `make help` to list targets.
# Note: recipe lines must be indented with a TAB, not spaces.

BIN     := bin
CONFIG  := configs/routes.yaml
COMPOSE := docker compose -f deploy/docker-compose.yaml

.PHONY: help build test vet run toy lab-up lab-down lab-logs clean

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-10s %s\n", $$1, $$2}'

build: ## Compile gateway and toysvc into ./bin
	go build -o $(BIN)/gateway ./cmd/gateway
	go build -o $(BIN)/toysvc ./cmd/toysvc

test: ## Run all tests with the race detector (always on, see M3/M6/M7)
	go test -race -count=1 ./...

vet: ## Static checks
	go vet ./...

run: ## Run the gateway locally against the lab
	go run ./cmd/gateway -config $(CONFIG)

toy: ## Run one toy backend locally (override: make toy NAME=orders PORT=9002)
	go run ./cmd/toysvc -name $(or $(NAME),users) -port $(or $(PORT),9001)

lab-up: ## Start the toy backends in docker compose
	$(COMPOSE) up --build -d

lab-down: ## Stop the lab
	$(COMPOSE) down

lab-logs: ## Tail lab logs
	$(COMPOSE) logs -f

clean: ## Remove build output
	rm -rf $(BIN)
