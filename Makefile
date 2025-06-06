.PHONY: help lint lint-fix format test build clean install-tools run-server

help:
	@echo "Available commands:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

install-tools:
	@echo "Installing golangci-lint..."
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@echo "Installing gofumpt..."
	@go install mvdan.cc/gofumpt@latest

lint:
	@echo "Running golangci-lint on root module..."
	$(shell go env GOPATH)/bin/golangci-lint run
	@echo "Running golangci-lint on simulation-server module..."
	cd cmd/simulation-server && $(shell go env GOPATH)/bin/golangci-lint run
	@echo "Running golangci-lint on entity-client module..."
	cd cmd/entity-client && $(shell go env GOPATH)/bin/golangci-lint run

lint-fix:
	@echo "Running golangci-lint with auto-fix on root module..."
	$(shell go env GOPATH)/bin/golangci-lint run --fix
	@echo "Running golangci-lint with auto-fix on simulation-server module..."
	cd cmd/simulation-server && $(shell go env GOPATH)/bin/golangci-lint run --fix
	@echo "Running golangci-lint with auto-fix on entity-client module..."
	cd cmd/entity-client && $(shell go env GOPATH)/bin/golangci-lint run --fix

format:
	@echo "Formatting code in root module..."
	$(shell go env GOPATH)/bin/gofumpt -w .
	@echo "Formatting code in simulation-server module..."
	cd cmd/simulation-server && $(shell go env GOPATH)/bin/gofumpt -w .
	@echo "Formatting code in entity-client module..."
	cd cmd/entity-client && $(shell go env GOPATH)/bin/gofumpt -w .

test:
	@echo "Running tests in root module..."
	go test -v ./...
	@echo "Running tests in simulation-server module..."
	cd cmd/simulation-server && go test -v ./...
	@echo "Running tests in entity-client module..."
	cd cmd/entity-client && go test -v ./...

test-coverage:
	@echo "Running tests with coverage in root module..."
	go test -v -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Running tests with coverage in simulation-server module..."
	cd cmd/simulation-server && go test -v -coverprofile=coverage-server.out ./...
	cd cmd/simulation-server && go tool cover -html=coverage-server.out -o coverage-server.html
	@echo "Running tests with coverage in entity-client module..."
	cd cmd/entity-client && go test -v -coverprofile=coverage-client.out ./...
	cd cmd/entity-client && go tool cover -html=coverage-client.out -o coverage-client.html
	@echo "Coverage reports generated: coverage.html, cmd/simulation-server/coverage-server.html, cmd/entity-client/coverage-client.html"

build-all:
	@echo "Building all binaries..."
	@mkdir -p bin
	@echo "Building simulation server..."
	cd cmd/simulation-server && go build -o ../../bin/simulation-server .
	@echo "Building entity client..."
	cd cmd/entity-client && go build -o ../../bin/entity-client .

run-server:
	@echo "Running simulation server..."
	cd cmd/simulation-server && go run .

run-server-dev:
	@echo "Running simulation server in development mode..."
	cd cmd/simulation-server && go run . -dev=true

clean:
	@echo "Cleaning build artifacts..."
	rm -f bin/*
	rm -f coverage.out coverage.html
	rm -f simulation.exe

deps:
	@echo "Downloading dependencies for root module..."
	go mod download
	go mod tidy
	@echo "Downloading dependencies for simulation-server module..."
	cd cmd/simulation-server && go mod download
	cd cmd/simulation-server && go mod tidy
	@echo "Downloading dependencies for entity-client module..."
	cd cmd/entity-client && go mod download
	cd cmd/entity-client && go mod tidy

check: lint test

ci: format lint test build-all
