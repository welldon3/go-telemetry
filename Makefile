да.PHONY: test cover lint build example tidy

test:
	go test -race ./...

cover:
	go test -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out

lint:
	go vet ./...
	staticcheck ./...

build:
	go build ./...

example:
	go run ./examples/basic

tidy:
	go mod tidy