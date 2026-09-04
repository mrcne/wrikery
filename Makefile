BINARY := wrike-tui

.PHONY: build test lint check clean

build:
	CGO_ENABLED=0 go build $(LDFLAGS) -o $(BINARY) ./cmd/wrike-tui

test:
	go test ./...

lint:
	golangci-lint run

check: lint test

clean:
	rm -rf $(BINARY) dist
