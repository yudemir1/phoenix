NAME=phoenix
DIR=bin

build:
	go build -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/phoenix

run:
	go run ./cmd/phoenix

clean:
	rm -rf $(BUILD_DIR)

.PHONY: build run clean