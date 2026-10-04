# AGENTS.md — Agentic tooling for longbridge-go-demo

This file is for agents that operate this repository.
For human-readable documentation, see [README.md](README.md).

---

## Simulated vs Live

**There is no paper-trading flag.** The SDK exposes no `paper` / `simulated` /
`sandbox` mode. `LONGBRIDGE_ENV=staging` only repoints hosts at `*.longbridge.xyz`
— it does not create a simulated account.

> Simulated and live are distinguished **only by which credentials you pass**.
> There is no base URL that means "paper".

| Variable | Default | Meaning |
| --- | --- | --- |
| `LONGPORT_MODE` | `simulated` | `simulated`, `paper` (paper trading account), or `live` (real money) |
| `LONGPORT_DRY_RUN` | `1` | When on, no order write is possible |

If `LONGPORT_MODE=simulated`, writes stay blocked even with `dry_run=0`,
so an accidental `LONGPORT_DRY_RUN=0` cannot trade simulated money.
`LONGPORT_MODE=paper` allows writes to the paper trading endpoint;
`LONGPORT_MODE=live` allows writes to the live endpoint.

---

## Dry-Run Semantics

**Default is dry run. Nothing is sent.**

A write happens only when **all three** hold:

1. `LONGPORT_DRY_RUN=0` (or `false`)
2. `--confirm-live` passed on the command line
3. `LONGPORT_MODE=live` (or `=paper`)

Otherwise the command prints the exact request it *would* have sent,
prints why it was blocked, and exits **3** without opening a connection.

```console
$ go run ./cmd/trade -action submit -symbol 700.HK -qty 100 -price 400.50
--- SubmitOrder request ---
  order_type   LO
  price        400.5
  quantity     100
  side         Buy
  symbol       700.HK
  time_force   Day

BLOCKED: missing --confirm-live
DRY RUN: nothing was sent to Longbridge.
$ echo $?
3
```

The order gate is enforced at `internal/config.GuardWrite`, called by
`SubmitOrder`, `ReplaceOrder` and `CancelOrder` immediately before the SDK call.
Trade context is created **lazily** — a dry run makes no websocket or
authenticated round trip.

### Mode test is fail-closed

```go
if c.Mode != ModeLive { refuse }  // ✅ correct — empty/invented mode is blocked
// NOT:
if c.Mode == ModeSimulated { refuse }  // ❌ latent hole — invented mode passes
```

---

## The Six Safety Gates

This demo guards six kinds of mutation with six **independent**, default-on gates.
None opens another.

| | Order | Watchlist | Sharelist | Content | DCA | Price Alert |
| --- | --- | --- | --- | --- | --- | --- |
| Env switch (default `1`) | `LONGPORT_DRY_RUN` | `LONGPORT_WATCHLIST_DRY_RUN` | `LONGPORT_SHARELIST_DRY_RUN` | `LONGPORT_CONTENT_DRY_RUN` | `LONGPORT_DCA_DRY_RUN` | `LONGPORT_ALERT_DRY_RUN` |
| Flag | `--confirm-live` | `--confirm` | `--confirm-live-sharelist` | `--confirm-live-content` | `--confirm-live-dca` | `--confirm-live-alert` |
| Also requires | `LONGPORT_MODE=live` | — | `LONGPORT_MODE=live` | `LONGPORT_MODE=live` | `LONGPORT_MODE=live` | `LONGPORT_MODE=live` |
| Exit when blocked | `3` | `3` | `3` | `3` | `3` | `3` |

Every binary's `-h` prints all six dry-run switches in its `Safety:` block
(on stderr, not stdout: use `./bin/xxx -h 2>&1 | grep LONGPORT`).

### Why watchlist has its own gate

A watchlist group is a saved list of tickers. Labelling cosmetic preference
changes as "live trading" would be dishonest. But these calls mutate server-side
state and are not idempotent, so they need *a* real guard — hence a second gate
without the `mode=live` requirement.

### Why sharelist and content have their own gates

- **Sharelist** is published and visible to other users. `Delete` is
  irreversible — the SDK exposes no undelete endpoint.
- **Content** publishes text *as the account owner*, publicly, and the SDK
  exposes no delete-topic method.

Both require all three conditions including `LONGPORT_MODE=live`.

### Read-only invariant: nine binaries assert it

Nine binaries (`quote`, `watch`, `warrant`, `reference`, `portfolio`,
`fundamentals`, `market`, `screener`, `auth`) assert the order gate is closed
at startup. If open, they refuse to run and exit **1**:

```
error: internal invariant violated: market is read-only but the order gate is open
exit=1
```

The check is in `cli.AssertReadOnly(cfg, "<name>", "run the …")`.

---

## Configuration

### Credential variables

| Variable | Purpose |
| --- | --- |
| `LONGBRIDGE_APP_KEY` | App Key (required) |
| `LONGBRIDGE_APP_SECRET` | App Secret (required, secret) |
| `LONGBRIDGE_ACCESS_TOKEN` | Access Token (required, secret) |
| `LONGPORT_APP_KEY` / `LONGPORT_APP_SECRET` / `LONGPORT_ACCESS_TOKEN` | Deprecated fallbacks |

### Optional tuning

| Variable | Default | Purpose |
| --- | --- | --- |
| `LONGPORT_REGION` | auto | `cn` for mainland China access points |
| `LONGBRIDGE_HTTP_URL` | `https://openapi.longbridge.com` | HTTP API base |
| `LONGBRIDGE_QUOTE_URL` | `wss://openapi-quote.longbridge.com/v2` | Quote websocket |
| `LONGBRIDGE_TRADE_URL` | `wss://openapi-trade.longbridge.com/v2` | Trade websocket |
| `LONGBRIDGE_ENV` | unset | `staging` repoints at `*.longbridge.xyz` — not a paper account |
| `LONGPORT_LANGUAGE` | `en` | `en`, `zh-CN`, `zh-HK` |
| `LONGBRIDGE_HTTP_TIMEOUT` | `15s` | Demo default |

### Config file

Only `config.yaml` and `config.local.yaml` are auto-detected. An explicit
`-config` with any other extension is exit **1**.

Config file precedence: **environment variables beat YAML**, flag beats both.

`.gitignore` excludes `.env`, `.env.*`, `config.yaml`, `config.local.yaml`,
`config.yml`, `config.toml`. Keep `.env.example` and `config.example.yaml`.

---

## Exit Codes

| Code | Meaning |
| --- | --- |
| `0` | Success |
| `1` | Real failure: API rejected the call, bad flag, unknown flag, read-only binary with order gate open, or unusable config file |
| `2` | **Missing credentials, and nothing else** |
| `3` | **BLOCKED.** A safety guard refused a write. Nothing was sent. |

Exit `2` is reached by exactly one condition: a `*config.MissingCredentialError`.
A bad flag value exits **1**, not **3**.

---

## SDK Coverage

**206 exported methods** on all exported types in the LongBridge Go SDK v0.25.2:

- 184 referenced from `cmd/` — used directly by this demo
- 22 on an allow-list with written justification (`internal` / `not-used` / `unimportable`)

The check is enforced: `make coverage-check` exits non-zero if any method is
both unreferenced and unjustified. The count pin is in the Makefile.

Coverage by context:

| Context | Methods |
| --- | --- |
| QuoteContext | 47 |
| TradeContext | 19 |
| FundamentalContext | 32 |
| MarketContext | 12 |
| AssetContext | 2 |
| CalendarContext | 1 |
| DCAContext | 11 |
| AlertContext | 4 |
| SharelistContext | 8 |
| ContentContext | 7 |
| ScreenerContext | 5 |
| PortfolioContext | 5 |

---

## Project Layout

```
cmd/               # 16 binaries: quote, watch, warrant, trade, executions,
                   #   portfolio, fundamentals, market, screener, auth,
                   #   watchlist, sharelist, content, dca, alert, referenc
internal/
  cli/             # Shared CLI helpers: AssertReadOnly, Fail, GuardWrite
  config/          # Config loading, WriteGuard, safety gates, exit codes
  signer/          # Request signing
openspec/specs/    # Normative specifications (write-gates, exit-codes,
                   #   secret-handling, sdk-coverage, read-only-invariant)
```

For full detail on any of the above topics, see the corresponding section in
[README.md](README.md).
