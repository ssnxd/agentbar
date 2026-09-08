VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
BIN     := agentbar

.PHONY: build install test test-race vet fmt-check vuln lint ci clean release-check help

build: ## Build ./bin/agentbar
	go build -ldflags '$(LDFLAGS)' -o bin/$(BIN) ./cmd/agentbar

install: ## Build and install into ~/.local/bin
	go build -ldflags '$(LDFLAGS)' -o $(HOME)/.local/bin/$(BIN) ./cmd/agentbar

test: ## Run all tests (tmux integration tests skip when tmux is absent)
	go test ./...

test-race: ## Run all tests with the race detector
	go test -race ./...

vet:
	go vet ./...

fmt-check: ## Fail if any file is not gofmt-clean
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

vuln: ## Check for known vulnerabilities
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

lint: vet fmt-check

ci: fmt-check vet test-race vuln ## Everything CI runs

clean:
	rm -rf bin dist

release-check: ## Validate the goreleaser config locally
	goreleaser check

help:
	@grep -E '^[a-z-]+:.*##' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-16s %s\n", $$1, $$2}'
