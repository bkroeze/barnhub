# Project commands. Run `just` to see the full list.

app := "barnhub-status"
bin-dir := "bin"
binary := bin-dir + "/" + app
coverage-file := "coverage.out"

# Show available commands.
default: list

list:
    @just --list --unsorted

# Format all Go packages.
fmt:
    go fmt ./...

# Fail if any Go file needs formatting.
fmt-check:
    @test -z "$(gofmt -l .)" || { \
        echo "Go files need formatting:"; \
        gofmt -l .; \
        exit 1; \
    }

# Run the test suite.
test:
    go test ./...

# Run tests and write an HTML coverage report.
coverage:
    go test -coverprofile={{coverage-file}} ./...
    go tool cover -html={{coverage-file}}

# Run static analysis.
vet:
    go vet ./...

# Run the checks expected before committing.
check: fmt-check test vet

# Build a reproducible local binary.
build:
    mkdir -p {{bin-dir}}
    go build -trimpath -o {{binary}} .

# Run the daemon, forwarding any extra arguments to it.
run *args:
    go run . {{args}}

# Install the command into Go's configured bin directory.
install:
    go install .

# Download module dependencies without changing go.mod or go.sum.
deps:
    go mod download

# Remove generated build and coverage artifacts.
clean:
    rm -rf {{bin-dir}} {{coverage-file}}
