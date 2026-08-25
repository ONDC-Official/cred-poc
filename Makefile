APP_NAME := credential-service
GO := go

.PHONY: run build test fmt docker-build docker-up docker-down docker-logs

run:
	@bash -c 'set -a; [ -f .env ] && source .env; set +a; go run ./cmd/api'

build:
	@mkdir -p build
	$(GO) build -o build/$(APP_NAME) ./cmd/api

test:
	$(GO) test ./...

fmt:
	$(GO) fmt ./...

docker-build:
	docker compose build

docker-up:
	docker compose up --build -d

docker-down:
	docker compose down

docker-logs:
	docker compose logs -f credential-service
