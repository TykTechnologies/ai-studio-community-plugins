# LLM Price Sync Plugin Makefile
# DEPRECATED for releases. Use `make plugin-publish NAME=llm-price-sync` from the repo
# root instead: it takes the version from manifest.json, uses it as the OCI tag,
# signs the index by digest and writes the marketplace entry from the digest the
# registry returned. The VERSION below is a hand-maintained copy and has drifted
# from manifest.json before, which is how tags and entries ended up disagreeing.
# These targets are kept for one-off local pushes only.
PLUGIN_NAME := llm-price-sync
REGISTRY_PATH := studio-plugins/llm-price-sync
VERSION := 1.0.0

# Build binaries for all platforms
.PHONY: build
build:
	GOOS=darwin GOARCH=arm64 go build -o $(PLUGIN_NAME)-darwin-arm64
	GOOS=linux GOARCH=amd64 go build -o $(PLUGIN_NAME)-linux-amd64

# Run tests
.PHONY: test
test:
	go test ./... -v

# Publish to OCI registry
.PHONY: publish
publish:
ifndef REG
	$(error REG environment variable is not set)
endif
	oras push $(REG)/$(REGISTRY_PATH):linux_amd64 \
		--artifact-type application/vnd.tyk.plugin.binary.v1 \
		--artifact-platform linux/amd64 \
		./$(PLUGIN_NAME)-linux-amd64:application/vnd.tyk.plugin.layer.v1
	oras push $(REG)/$(REGISTRY_PATH):darwin_arm64 \
		--artifact-type application/vnd.tyk.plugin.binary.v1 \
		--artifact-platform darwin/arm64 \
		./$(PLUGIN_NAME)-darwin-arm64:application/vnd.tyk.plugin.layer.v1
	oras manifest index create $(REG)/$(REGISTRY_PATH):$(VERSION) \
		darwin_arm64 linux_amd64

# Sign the manifest
.PHONY: sign
sign:
ifndef REG
	$(error REG environment variable is not set)
endif
ifndef SIGN_KEY
	$(error SIGN_KEY environment variable is not set)
endif
	cosign sign --key $(SIGN_KEY) $(REG)/$(REGISTRY_PATH):$(VERSION)

# Build, publish, and sign in one step
.PHONY: all
all: build publish sign

# Clean build artifacts
.PHONY: clean
clean:
	rm -f $(PLUGIN_NAME)-darwin-arm64 $(PLUGIN_NAME)-linux-amd64
