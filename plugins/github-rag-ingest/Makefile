# GitHub RAG Ingest Plugin Makefile
PLUGIN_NAME := github-rag-ingest
REGISTRY_PATH := studio-plugins/github-rag-ingest
VERSION := 1.0
BUILD_DIR := server

# Build UI components (must run before Go build)
.PHONY: build-ui
build-ui:
	cd $(BUILD_DIR)/ui && npm install && npm run build

# Build binaries for all platforms
.PHONY: build
build: build-ui
	cd $(BUILD_DIR) && GOOS=darwin GOARCH=arm64 go build -o ../$(PLUGIN_NAME)-darwin-arm64
	cd $(BUILD_DIR) && GOOS=linux GOARCH=amd64 go build -o ../$(PLUGIN_NAME)-linux-amd64

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
	cd $(BUILD_DIR)/ui && npm run clean
