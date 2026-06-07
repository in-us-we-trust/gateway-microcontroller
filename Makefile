.PHONY: run test build docker-build swagger

run:
	go run ./cmd/api

test:
	go test ./tests/integration/... ./internal/... -v

test-mtls:
	go test ./tests/mtls/... ./internal/middleware/... -v

test-all:
	go test ./... -v

build:
	go build -o bin/server ./cmd/api

docker-build:
	docker build -t gateway-microcontroller .

swagger:
	swag init -g cmd/api/main.go -o docs
