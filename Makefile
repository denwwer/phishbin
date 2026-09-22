.PHONY: run build-linux docker test lint mock

PB_VERSION ?= $(shell git describe --tags 2>/dev/null | sed 's/-.*//g' || echo "v0.0.0")
BUILD_FLAGS = -ldflags "-s -w -X github.com/phishbin/src/config.Version=$(PB_VERSION)"
DOCKER_REPOSITORY = "denwwer/phishbin"

# Run with loaded .env
run:
	@go tool godotenv go run .

# Build for current OS
build:
	@go build $(BUILD_FLAGS) -o inboxbuffer main.go

# Build linux binaries using Docker (will be available in build/linux)
build-linux:
	docker build -f tools/linux-build/Dockerfile --output . .

docker:
	docker build -t denwwer/inboxbuffer .
	docker run --name inboxbuffer -p 1081:1081 -p 1082:1082 -p 1025:1025 -v ./data:/data denwwer/inboxbuffer

# Run tests
test:
	@go test -race -v ./...

# Run linter
lint:
	golangci-lint run . --fix

# Generate mocks.
mock:
	@go tool mockery
