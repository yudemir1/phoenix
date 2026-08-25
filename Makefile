SHELL := /bin/bash

BINARY_NAME=phoenix
BUILD_DIR=bin

build:
	go build -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/phoenix

run:
	go run ./cmd/phoenix

test:
	@set -o pipefail; \
	go test -v ./... 2>&1 | awk -f scripts/format_test_output.awk

clean:
	rm -rf $(BUILD_DIR)

.PHONY: build run test clean
