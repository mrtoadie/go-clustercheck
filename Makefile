# go-clustercheck build
# Static binary, stripped, version stamped - same conventions as your other Go repos

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BINARY  := clustercheck
BIN_DIR := bin

LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build clean

build:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) ./cmd/clustercheck

clean:
	rm -rf $(BIN_DIR)
