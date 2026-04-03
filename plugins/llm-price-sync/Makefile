PLUGIN_NAME := llm-price-sync
REGISTRY_PATH := studio-plugins/llm-price-sync
VERSION := 1.0.0
REG ?= ghcr.io/tyktechnologies
SIGN_KEY ?= cosign.key

.PHONY: build
build:
	GOOS=darwin GOARCH=arm64 go build -o $(PLUGIN_NAME)-darwin-arm64
	GOOS=linux GOARCH=amd64 go build -o $(PLUGIN_NAME)-linux-amd64

.PHONY: test
test:
	go test ./... -v

.PHONY: publish
publish:
	oras push $(REG)/$(REGISTRY_PATH):linux_amd64 \
		--artifact-type application/vnd.tyk.plugin.binary.v1 \
		./$(PLUGIN_NAME)-linux-amd64:application/vnd.tyk.plugin.layer.v1
	oras push $(REG)/$(REGISTRY_PATH):darwin_arm64 \
		--artifact-type application/vnd.tyk.plugin.binary.v1 \
		./$(PLUGIN_NAME)-darwin-arm64:application/vnd.tyk.plugin.layer.v1

.PHONY: sign
sign:
	cosign sign --key $(SIGN_KEY) $(REG)/$(REGISTRY_PATH):$(VERSION)

.PHONY: all
all: build publish sign

.PHONY: clean
clean:
	rm -f $(PLUGIN_NAME)-darwin-arm64 $(PLUGIN_NAME)-linux-amd64
