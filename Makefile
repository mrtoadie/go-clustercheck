# go-clustercheck build
# Static binary, stripped, version stamped, UPX-compressed.
# Set COMPRESS=0 to skip UPX (e.g. for debugging symbols or testing).

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BINARY  := clustercheck
BIN_DIR := bin
UPX     ?= upx --best --lzma

LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build compress clean

build: compress

compress:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) ./cmd/clustercheck
	$(UPX) $(BIN_DIR)/$(BINARY)
ifdef COMPRESS
	ifeq ($(COMPRESS),0)
		@echo "Skipping UPX compression"
	else
		$(UPX) $(BIN_DIR)/$(BINARY)
	endif
else
	$(UPX) $(BIN_DIR)/$(BINARY)
endif

clean:
	rm -rf $(BIN_DIR)
