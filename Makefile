SHELL := /bin/bash

BINARY_NAME=phoenix
BUILD_DIR=bin

GREEN := \033[30;42m
RED   := \033[97;41m
RESET := \033[0m

build:
	go build -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/phoenix

run:
	go run ./cmd/phoenix

# Rewrite every file in place with gofmt.
fmt:
	gofmt -w .

# Fail if anything is unformatted, naming the offending files.
fmt-check:
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		printf "$(RED) ❌ $(RESET) gofmt: not formatted (run 'make fmt'):\n"; \
		echo "$$unformatted" | sed 's/^/         /'; \
		exit 1; \
	fi; \
	printf "$(GREEN) ✅ $(RESET) gofmt: every file is formatted\n"

vet:
	@if ! out=$$(go vet ./... 2>&1); then \
		printf "$(RED) ❌ $(RESET) go vet reported problems:\n"; \
		echo "$$out"; \
		exit 1; \
	fi; \
	printf "$(GREEN) ✅ $(RESET) go vet: no problems found\n"

test: fmt-check vet
	@set -o pipefail; \
	go test -v ./... 2>&1 | awk -f scripts/format_test_output.awk

clean:
	rm -rf $(BUILD_DIR)

.PHONY: build run fmt fmt-check vet test clean
