VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
BIN     := workflow

.PHONY: build install test vet fmt lint clean release-check

build: ## Build ./bin/workflow
	go build -ldflags '$(LDFLAGS)' -o bin/$(BIN) ./cmd/workflow

install: ## Build and install into $$GOPATH/bin
	go build -ldflags '$(LDFLAGS)' -o $(shell go env GOPATH)/bin/$(BIN) ./cmd/workflow

test: ## Run all tests (tmux integration tests skip when tmux is absent)
	go test ./...

vet:
	go vet ./...

fmt: ## Fail if any file is not gofmt-clean
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

lint: vet fmt

clean:
	rm -rf bin dist

release-check: ## Validate the goreleaser config locally
	goreleaser check

help:
	@grep -E '^[a-z-]+:.*##' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-16s %s\n", $$1, $$2}'
