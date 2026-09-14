APP_NAME := credential-service
GO := go

.PHONY: run build test fmt docker-network docker-build docker-up docker-down docker-logs

run:
	@bash -c 'set -a; [ -f .env ] && source .env; set +a; go run ./cmd/api'

build:
	@mkdir -p build
	$(GO) build -o build/$(APP_NAME) ./cmd/api

test:
	$(GO) test ./...

fmt:
	$(GO) fmt ./...

docker-network:
	@docker network inspect cred_network >/dev/null 2>&1 || docker network create cred_network
	@[ -f .env ] || cp .env.sample .env

docker-build:
	docker compose build

docker-up: docker-network
	docker compose up --build -d

docker-down:
	docker compose down

docker-logs:
	docker compose logs -f credential-service
