BIN        := bin
GO         ?= go
BINARIES   := quote trade watch market
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

.PHONY: run-quote
run-quote: ## go run ./cmd/quote
	$(GO) run ./cmd/quote

.PHONY: run-market
run-market: ## go run ./cmd/market
	$(GO) run ./cmd/market

.PHONY: run-watch
run-watch: ## go run ./cmd/watch
	$(GO) run ./cmd/watch

.PHONY: clean
clean: ## Remove build output
	rm -rf $(BIN)

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'
