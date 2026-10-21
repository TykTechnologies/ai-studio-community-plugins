# Prompt Compressor Plugin Makefile
# Releases go through `make plugin-publish NAME=prompt-compressor` from the AI
# Studio repo root: it takes the version from manifest.json, signs the index
# by digest and writes the marketplace entry. These targets are for local
# builds and tests only.
PLUGIN_NAME := prompt-compressor

# Build binaries for all platforms
.PHONY: build
build:
	GOOS=darwin GOARCH=arm64 go build -o $(PLUGIN_NAME)-darwin-arm64
	GOOS=linux GOARCH=amd64 go build -o $(PLUGIN_NAME)-linux-amd64

# Run tests
.PHONY: test
test:
	go vet ./...
	go test -race ./...

# Clean build artifacts
.PHONY: clean
clean:
	rm -f $(PLUGIN_NAME) $(PLUGIN_NAME)-darwin-arm64 $(PLUGIN_NAME)-linux-amd64
