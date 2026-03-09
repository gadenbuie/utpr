version := `git describe --tags --always --dirty 2>/dev/null || echo "dev"`
commit := `git rev-parse --short HEAD 2>/dev/null || echo "none"`
date := `date -u +%Y-%m-%dT%H:%M:%SZ`
ldflags := "-s -w -X main.version=" + version + " -X main.commit=" + commit + " -X main.date=" + date

# Build the binary
build:
    go build -ldflags '{{ldflags}}' -o utpr .

# Install to $GOPATH/bin
install:
    go install -ldflags '{{ldflags}}' .

# Run unit tests
test:
    go test ./...

# Run all tests (unit + integration)
test-all:
    go test -tags integration ./...

# Run go vet
vet:
    go vet ./...

# Run golangci-lint
lint:
    go run github.com/golangci/golangci-lint/cmd/golangci-lint@latest run

# Run vet, lint, and tests
check: vet lint test

# Format code
fmt:
    go fmt ./...

# Remove build artifacts
clean:
    rm -f utpr
