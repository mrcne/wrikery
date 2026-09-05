BINARY := wrikery
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -ldflags "-X main.version=$(VERSION)"
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64

.PHONY: build test lint check cross clean

build:
	CGO_ENABLED=0 go build $(LDFLAGS) -o $(BINARY) ./cmd/wrikery

test:
	CGO_ENABLED=0 go test ./...

lint:
	golangci-lint run

check: lint test

cross:
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; \
		echo "building $$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build $(LDFLAGS) -o dist/$(BINARY)-$$os-$$arch ./cmd/wrikery || exit 1; \
	done

clean:
	rm -rf $(BINARY) dist
