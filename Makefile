.PHONY: run test build docker-build swagger

run:
	go run ./cmd/api

test:
	go test ./tests/integration/... -v

build:
	go build -o bin/server ./cmd/api

docker-build:
	docker build -t gateway-microcontroller .

swagger:
	swag init -g cmd/api/main.go -o docs
