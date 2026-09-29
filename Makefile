BIN        := bin
GO         ?= go
BINARIES   := quote trade watch market warrant watchlist executions reference fundamentals sharelist content portfolio dca alert screener
CMDS       := $(addprefix ./cmd/,$(BINARIES))

.PHONY: all
all: fmt vet build

.PHONY: build
build: ## Compile every command into ./bin
	@mkdir -p $(BIN)
	@for c in $(CMDS); do \
		name=$$(basename $$c); \
		echo "  build $$name"; \
		$(GO) build -o $(BIN)/$$name ./$$c || exit 1; \
	done

.PHONY: fmt
fmt: ## Format all Go sources
	gofmt -w .

.PHONY: fmt-check
fmt-check: ## Fail if anything is unformatted
	@out=$$(gofmt -l .); \
	if [ -n "$$out" ]; then echo "unformatted files:"; echo "$$out"; exit 1; fi; \
	echo "gofmt clean"

.PHONY: vet
vet: ## Run go vet
	$(GO) vet ./...

.PHONY: tidy
tidy: ## Tidy go.mod / go.sum
	$(GO) mod tidy

.PHONY: test
test: ## Run unit tests with the race detector
	$(GO) test -race ./...

# Receiver-agnostic on purpose: MarketContext declares its methods on
# `m *MarketContext`, DCA on `d *DCAContext`, the rest on `c *XContext`.
# A `\(c \*` pattern silently misses all 12 MarketContext methods.
.PHONY: coverage-check
coverage-check: ## Fail if any SDK context method is unreferenced from ./cmd
	@set -eu; \
	sdk=$$($(GO) list -m -f '{{.Version}}' github.com/longbridge/openapi-go); \
	base="$$($(GO) env GOMODCACHE)/github.com/longbridge/openapi-go@$$sdk"; \
	if [ ! -d "$$base" ]; then echo "SDK not in module cache: $$base"; exit 1; fi; \
	list=$$(mktemp); \
	trap 'rm -f "$$list"' EXIT; \
	grep -rhoE '^func \([a-z]+ \*[A-Za-z]+Context\) [A-Z][A-Za-z0-9]*' "$$base" --include='*.go' \
	  | sed -E 's/^func \([a-z]+ \*([A-Za-z]+)Context\) ([A-Za-z0-9]*)/\1 \2/' \
	  | sort -u > $$list; \
	total=$$(wc -l < $$list | tr -d ' '); \
	missing=$$(while read -r ctx m; do grep -rqE "\.$$m\(" cmd/ || echo "UNCOVERED: $$ctx $$m"; done < $$list); \
	if [ -n "$$missing" ]; then \
		echo "sdk coverage: $$((total - $$(printf '%s\n' "$$missing" | wc -l)))/$$total"; \
		echo "uncovered context methods:"; echo "$$missing"; exit 1; \
	fi; \
	echo "sdk coverage: $$total/$$total context methods covered"

.PHONY: verify
verify: fmt-check vet test build coverage-check ## Everything CI should run

.PHONY: run-quote
run-quote: ## go run ./cmd/quote
	$(GO) run ./cmd/quote

.PHONY: run-market
run-market: ## go run ./cmd/market
	$(GO) run ./cmd/market

.PHONY: run-watch
run-watch: ## go run ./cmd/watch
	$(GO) run ./cmd/watch

.PHONY: run-warrant
run-warrant: ## go run ./cmd/warrant
	$(GO) run ./cmd/warrant

.PHONY: run-watchlist
run-watchlist: ## go run ./cmd/watchlist
	$(GO) run ./cmd/watchlist

.PHONY: run-executions
run-executions: ## go run ./cmd/executions
	$(GO) run ./cmd/executions

.PHONY: run-reference
run-reference: ## go run ./cmd/reference
	$(GO) run ./cmd/reference

.PHONY: run-fundamentals
run-fundamentals: ## go run ./cmd/fundamentals
	$(GO) run ./cmd/fundamentals

.PHONY: run-sharelist
run-sharelist: ## go run ./cmd/sharelist
	$(GO) run ./cmd/sharelist

.PHONY: run-content
run-content: ## go run ./cmd/content
	$(GO) run ./cmd/content

.PHONY: run-portfolio
run-portfolio: ## go run ./cmd/portfolio
	$(GO) run ./cmd/portfolio

.PHONY: run-dca
run-dca: ## go run ./cmd/dca
	$(GO) run ./cmd/dca

.PHONY: run-alert
run-alert: ## go run ./cmd/alert
	$(GO) run ./cmd/alert

.PHONY: run-screener
run-screener: ## go run ./cmd/screener
	$(GO) run ./cmd/screener

.PHONY: clean
clean: ## Remove build output
	rm -rf $(BIN)

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'
