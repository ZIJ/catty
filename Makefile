.PHONY: build build-cli build-api release clean

VERSION ?= 0.1.0
LDFLAGS := -ldflags="-s -w -X main.Version=$(VERSION)"

# Build all binaries
build: build-cli build-api

# Build CLI for current platform
build-cli:
	go build $(LDFLAGS) -o bin/catty ./cmd/catty

# Build API for current platform
build-api:
	go build $(LDFLAGS) -o bin/catty-api ./cmd/catty-api

# Build CLI for all platforms (for releases)
release:
	@mkdir -p dist
	# macOS
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o dist/catty-darwin-amd64 ./cmd/catty
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o dist/catty-darwin-arm64 ./cmd/catty

# Deploy API to Fly
deploy-api:
	fly deploy -c fly.api.toml

# Deploy executor to Fly
deploy-exec:
	fly deploy

# Clean build artifacts
clean:
	rm -rf bin dist
