BIN        := bin
GO         ?= go
BINARIES   := quote trade watch market warrant watchlist executions reference fundamentals sharelist content portfolio dca alert screener auth
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

# The SDK is scanned for every exported method on an exported type, not only
# the context types. The old pattern was `^func \([a-z]+ \*[A-Za-z]+Context\)`,
# which is narrower than the README's claim was and hid 53 methods: `Client`,
# `Config`, `OAuth`, `Signer`, `TradeStatus`, `DCAStatus`, `DCAFrequency`,
# `PinnedMode`, `CalendarCategory`, `ApiError`, the three `GetConfig` and the
# eleven request `Values()`. Ground truth is 206 `Type Method` pairs, and the
# `go doc` oracle agrees with the grep byte for byte.
#
# Three parts of the pattern are load-bearing. Breaking any one of them does
# not fail loudly; it quietly stops counting a class of method.
#   `\*?`      20 of the 206 are value receivers, including every TradeStatus
#              predicate. `\*` skips exactly the class most worth counting.
#   `[a-zA-Z_]` internal/signer/signer.go declares `func (_ *Signer) String()`;
#              `[a-z]+` misses the only blank receiver in the module.
#   `[A-Z]` on the TYPE excludes `core` (35 methods) and `store` (8) — the
#              unexported websocket internals the contexts embed. They are
#              unreachable from outside their package, so demanding a call
#              site for them is unsatisfiable. Do not drop it.
# `--exclude='*_test.go'` is belt-and-braces: no test file in the SDK declares
# a method today, and the first one that does would be a method on the type
# under test, not on the interface we mean to cover.
# LC_ALL=C on the sort: the list is fed to `comm`, which refuses unsorted input
# under a non-C locale. That failure was hit during this audit.
#
# The count is pinned, and that is the one thing here that catches a broken
# pattern rather than a broken claim. Every set assertion below can pass with a
# list that is too small: all 20 value-receiver methods are covered, so
# `\*` in place of `\*?` drops them and goes green. The count is what notices.
# It moves when go.mod bumps the SDK; the oracle below, not this grep, is how
# to find out by how much:
#
#   for p in $(go list github.com/longbridge/openapi-go/...); do \
#     go doc -all -short "$p" 2>/dev/null \
#       | grep -E '^func \([a-zA-Z_][A-Za-z0-9_]* \*?[A-Z][A-Za-z0-9_]*\) [A-Z]'; \
#   done | sed -E 's/^func \([a-zA-Z_][A-Za-z0-9_]* (\*?)([A-Z][A-Za-z0-9_]*)\) ([A-Za-z0-9_]*).*/\2 \3/' \
#     | LC_ALL=C sort -u | wc -l
#
# (`.*` at the end of that sed is what discards the parameter list `go doc`
# prints; without it the oracle's lines keep their signatures and diff against
# the grep output is not empty for reasons that have nothing to do with method
# coverage.)
SDK_METHOD_COUNT := 206

.PHONY: coverage-check
coverage-check: ## Fail if an exported SDK method is neither referenced in ./cmd nor allow-listed below
	@set -eu; \
	sdk=$$($(GO) list -m -f '{{.Version}}' github.com/longbridge/openapi-go); \
	base="$$($(GO) env GOMODCACHE)/github.com/longbridge/openapi-go@$$sdk"; \
	if [ ! -d "$$base" ]; then echo "SDK not in module cache: $$base"; exit 1; fi; \
	list=$$(mktemp); \
	allow=$$(mktemp); \
	names=$$(mktemp); \
	whys=$$(mktemp); \
	known=$$(mktemp); \
	bad=$$(mktemp); \
	trap 'rm -f "$$list" "$$allow" "$$names" "$$whys" "$$known" "$$bad"' EXIT; \
	grep -rhoE '^func \([a-zA-Z_][A-Za-z0-9_]* \*?[A-Z][A-Za-z0-9_]*\) [A-Z][A-Za-z0-9_]*' "$$base" \
	    --include='*.go' --exclude='*_test.go' \
	  | sed -E 's/^func \([a-zA-Z_][A-Za-z0-9_]* (\*?)([A-Z][A-Za-z0-9_]*)\) ([A-Za-z0-9_]*)/\2 \3/' \
	  | LC_ALL=C sort -u > "$$list"; \
	# KNOWN FALSE POSITIVES. The matcher is by method NAME, not by receiver \
	# type, so a call on an unrelated type counts as coverage. Verified today: \
	#   Client Delete           <- AlertContext.Delete, SharelistContext.Delete \
	#   ApiError Error          <- any err.Error(); `ApiError` appears nowhere \
	#                             in cmd/, so it is counted and never called \
	#   TradeStatus UnmarshalJSON <- encoding/json calls it implicitly for a \
	#                             field of type market.TradeStatus, so no \
	#                             `.UnmarshalJSON(` call site exists to find. The \
	#                             reference scan does include cmd/**/*_test.go, \
	#                             and cmd/market decodes through json.Unmarshal \
	#                             there, so the coverage is real and still \
	#                             invisible to a grep for the method name. \
	#   TradeStatus String      <- decimal.Decimal.String, bytes.Buffer.String. \
	#                             Deliberately not printed: the body is \
	#                             `return s.Name()` (cmd/market/main.go) \
	#   Signer String           <- the same .String( matches; unimportable anyway \
	#   CalendarCategory String <- bytes.Buffer.String in cmd/fundamentals; its \
	#                             only genuine call site is a _test.go \
	# A type-accurate check needs a parser, not grep. Read a green run as "no \
	# unallow-listed method is unreferenced", not "every method is called on its \
	# own type". The reference scan does include cmd/**/*_test.go. \
	# \
	# The allow-list, `Type Method:reason`, one per line. The three reason words: \
	#   internal      the SDK calls it itself as part of a higher-level \
	#                 operation we do make -- every context method routes \
	#                 through http.Client.Call; the eleven Values() are called \
	#                 by the context method that accepts them; the three \
	#                 GetConfig by config.New. Reached, just not by us. \
	#                 "Called by the SDK" means called by its LIBRARY \
	#                 packages, not by an example or integration program that \
	#                 ships inside the module; Client.GetOTP is `not-used` for \
	#                 exactly that reason, since only its v2 twin GetOTPV2 has \
	#                 a library caller (quote/core.go, trade/core.go). \
	#                 Config.Logger left this list when the SDK logger was \
	#                 wired up: internal/config now calls it, so it is a real \
	#                 call site rather than a claim about the SDK's plumbing. \
	# \
	# The scan root is `cmd/ internal/`, matching the sibling tiger-go-demo. \
	# It was `cmd/` alone until Config.WithHeader and Config.SetLogger were \
	# called from internal/config, where the shared plumbing belongs: with a \
	# cmd/-only root those two methods could never be honestly covered, and \
	# the alternative -- pushing a dummy call site into a command -- would \
	# have bought the number with a lie. \
	#   unimportable  it lives in the SDK's internal/ tree. Go's internal rule \
	#                 means module github.com/shing1211/longbridge-go-demo \
	#                 cannot import it at all, so a call site is unsatisfiable \
	#                 by construction, not by choice. Its own word so nobody \
	#                 "fixes" it by trying. \
	#   not-used      importable and user-facing, deliberately not reached. \
	#                 Deliberately kept distinct from `internal`: collapsing \
	#                 them would lose the fact that a user could call it. \
	# \
	# TWO `not-used` ENTRIES ARE NOW FALSE IN THE OTHER DIRECTION, and they \
	# stay on the list because this scan cannot see them. Config.WithHeader and \
	# Config.SetLogger ARE called, by internal/config (the extra headers from \
	# `-header`, and the SDK logger behind `-log-level`), because a flag every \
	# command shares has to be applied by the one loader they all call. The \
	# scan above greps cmd/ only, so it reports both as unreferenced; deleting \
	# the entries makes the check fail with "not on the allow-list and not \
	# referenced from cmd/", which was measured rather than assumed. Nothing \
	# here can honestly move them: a call site under cmd/ would have to be one \
	# command duplicating the shared loader's work, which is the drift this \
	# repo already paid for once (see cli.AssertReadOnly), and widening the \
	# scan root would move dozens of `internal` entries and change the pinned \
	# counts above. \
	printf '%s\n' \
	  'Client Call:internal' \
	  'Client Get:internal' \
	  'Client GetOTP:not-used' \
	  'Client GetOTPV2:internal' \
	  'Client Post:internal' \
	  'Client Put:internal' \
	  'EnvConfig GetConfig:internal' \
	  'GetAccountBalance Values:internal' \
	  'GetCashFlow Values:internal' \
	  'GetEstimateMaxPurchaseQuantity Values:internal' \
	  'GetFundPositions Values:internal' \
	  'GetHistoryExecutions Values:internal' \
	  'GetHistoryOrders Values:internal' \
	  'GetStatementDownloadURL Values:internal' \
	  'GetStatementList Values:internal' \
	  'GetStockPositions Values:internal' \
	  'GetTodayExecutions Values:internal' \
	  'GetTodayOrders Values:internal' \
	  'Signer Sign:unimportable' \
	  'TOMLConfig GetConfig:internal' \
	  'TradeStatus UnmarshalJSON:not-used' \
	  'YAMLConfig GetConfig:internal' \
	  > "$$allow"; \
	whylist='internal unimportable not-used'; \
	sed -E 's/:[^:]*$$//' "$$allow" | LC_ALL=C sort -u > "$$names"; \
	sed -E 's/^[^:]*://' "$$allow" | LC_ALL=C sort -u > "$$whys"; \
	printf '%s\n' $$whylist | LC_ALL=C sort -u > "$$known"; \
	: > "$$bad"; \
	LC_ALL=C comm -13 "$$list" "$$names" \
	  | sed -E 's/$$/: allow-list entry matches no method in the SDK list (typo, rename, or a new SDK version?)/' \
	  >> "$$bad"; \
	LC_ALL=C comm -23 "$$whys" "$$known" \
	  | while read -r w; do \
	      grep -F ":$$w" "$$allow" | sed -E 's/$$/: unknown reason word; use one of: internal, unimportable, not-used/'; \
	    done \
	  >> "$$bad"; \
	total=$$(wc -l < "$$list" | tr -d ' '); \
	if [ "$$total" -ne "$(SDK_METHOD_COUNT)" ]; then \
	  echo "expected $(SDK_METHOD_COUNT) exported methods, scanned $$total: the pinned SDK version moved or the scan pattern regressed (see the oracle above the target)" >> "$$bad"; \
	fi; \
	covered=0; \
	allowed=0; \
	while read -r typ m; do \
	  if grep -rqE "\.$$m\(" cmd/ internal/; then \
	    covered=$$((covered + 1)); \
	    if grep -qxF "$$typ $$m" "$$names"; then \
	      echo "$$typ $$m: now covered — delete from the allow-list above" >> "$$bad"; \
	    fi; \
	  elif grep -qxF "$$typ $$m" "$$names"; then \
	    allowed=$$((allowed + 1)); \
	  else \
	    echo "$$typ $$m: not on the allow-list and not referenced from cmd/" >> "$$bad"; \
	  fi; \
	done < "$$list"; \
	if [ -s "$$bad" ]; then \
	  echo "sdk coverage: $$covered/$$total exported methods referenced from cmd/, $$allowed allow-listed"; \
	  echo "coverage-check failed, $$(($$(wc -l < "$$bad" | tr -d ' '))) problem(s):"; \
	  cat "$$bad"; \
	  exit 1; \
	fi; \
	echo "sdk coverage: $$covered/$$total exported methods referenced from cmd/"; \
	echo "sdk allow-list: $$allowed of $$total methods justified, by reason:"; \
	for r in $$whylist; do printf '  %-12s %s\n' "$$r" "$$(grep -c ":$$r$$" "$$allow" || true)"; done

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

.PHONY: run-auth
run-auth: ## go run ./cmd/auth  (add -no-browser when there is no local browser)
	$(GO) run ./cmd/auth

.PHONY: clean
clean: ## Remove build output
	rm -rf $(BIN)

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'
