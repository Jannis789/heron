.PHONY: run build test lint tidy

run:
	go run .

build:
	go build -o bin/heron .

test:
	go test ./...

lint:
	go vet ./...

tidy:
	go mod tidy
