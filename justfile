_default:
    @just --list

mod examples './examples/justfile'

# Format code
fmt:
    go fmt ./...

# Run tests
test:
    go test ./... -v -race

# Get test coverage
coverage:
    go test ./... -race -coverprofile=coverage.out && \
    go tool cover -html=coverage.out && \
    rm coverage.out

# Run benchmarks
bench:
    go test -bench . -benchmem

# Run go vet
vet:
    go vet ./...

# Tidy dependencies
tidy:
    go mod tidy

# Run standard checks
check: fmt vet test

# Clean build caches
clean:
    go clean -cache -testcache
