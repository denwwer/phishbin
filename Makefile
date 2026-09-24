.PHONY: run build-linux docker test lint mock

PB_VERSION ?= $(shell git describe --tags 2>/dev/null | sed 's/-.*//g' || echo "v0.0.0")
BUILD_FLAGS = -ldflags "-s -w -X github.com/phishbin/src/config.Version=$(PB_VERSION)"
DOCKER_REPOSITORY = "denwwer/phishbin"

# Run with loaded .env
run:
	@go tool godotenv go run .

# Build for current OS
build:
	@go build $(BUILD_FLAGS) -o phishbin main.go

docker:
	docker build -t denwwer/phishbin .
	docker run --name phishbin -p 4535:4535 -v ./data:/data denwwer/phishbin

# Run tests
test:
	@go test -race -v ./...
	@cd client && npm test


# Run linter
lint:
	golangci-lint run . --fix

# Generate mocks.
mock:
	@go tool mockery
