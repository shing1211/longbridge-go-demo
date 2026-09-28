# longbridge-go-demo

A small, complete, safety-gated demo of the **Longbridge (Longport) OpenAPI**
using the official Go SDK, [`github.com/longbridge/openapi-go`](https://pkg.go.dev/github.com/longbridge/openapi-go)
at **v0.25.2**.

Four commands, one shared config loader, and a hard rule that no order can be
sent unless you say so three different ways.

> **Status: not yet tested against the live API.** See
> [Honest status](#honest-status). Everything below was verified by building,
> vetting, and running against the real Longbridge endpoints with deliberately
> invalid credentials. No output in this README is copied from a live session.

---

## Contents

- [Quick start](#quick-start)
- [What you need to fill in](#what-you-need-to-fill-in)
- [Simulated vs live — read this](#simulated-vs-live--read-this)
- [Dry-run semantics and the safety gate](#dry-run-semantics-and-the-safety-gate)
- [Commands](#commands)
- [Configuration reference](#configuration-reference)
- [Honest status](#honest-status)
- [Troubleshooting](#troubleshooting)
- [Project layout](#project-layout)

---

## Quick start

```bash
git clone <this-repo> longbridge-go-demo
cd longbridge-go-demo

# 1. Build everything and check it compiles cleanly.
make all          # fmt + vet + build into ./bin
make fmt-check    # gofmt must report nothing

# 2. Create your credentials (see the next section), then:
cp .env.example .env
$EDITOR .env      # paste app key, app secret and access token

# 3. Read-only market data. Requires credentials, places no orders.
go run ./cmd/quote -symbols 700.HK,AAPL.US -period day -count 5
go run ./cmd/market
go run ./cmd/watch -symbols 700.HK

# 4. Account state.
go run ./cmd/trade -action balance
go run ./cmd/trade -action positions
go run ./cmd/trade -action today-orders
```

Every binary prints usage with `-h` and needs **no** credentials for that:

```bash
go run ./cmd/quote -h
```

With no credentials at all, every binary exits **2** with a message listing
exactly what is missing — no panic, no stack trace:

```console
$ go run ./cmd/quote
error: missing Longbridge credentials (checked via environment):

  - LONGBRIDGE_ACCESS_TOKEN
  - LONGBRIDGE_APP_KEY
  - LONGBRIDGE_APP_SECRET

How to fix:
  1. Sign in at https://open.longbridge.com/ -> User Center ->
     "Application credential" and copy App Key, App Secret and
     Access Token. ...
```

---

## What you need to fill in

Get all three values from **<https://open.longbridge.com/> → User Center →
Application credential** (应用凭证).

| Env var | Required | What it is |
| --- | --- | --- |
| `LONGBRIDGE_APP_KEY` | yes | App Key. Not secret alone; treat as private. |
| `LONGBRIDGE_APP_SECRET` | yes | App Secret. **Secret.** With the App Key it can sign requests. |
| `LONGBRIDGE_ACCESS_TOKEN` | yes | **Secret.** This is what authorises *trading*. |

**For a simulated account you need exactly one more thing than you already
have: the `LONGBRIDGE_ACCESS_TOKEN` issued for that simulated account.** Use
the *simulated account's own* App Key / App Secret / Access Token triple.
Longbridge issues a completely separate credential set for simulated accounts;
it is not a flag you toggle on live credentials.

Two easy-to-confuse notes:

- `LONGBRIDGE_ACCESS_TOKEN` is the **legacy Access Token** from User Center.
  It is *not* an OAuth access token, and *not* a refresh token. This demo uses
  legacy app-key auth, not OAuth.
- Anyone who obtains the Access Token can trade your account through the API.
  Treat it like a password.

`.env` is read automatically: the SDK imports `godotenv/autoload`, so a `.env`
in the working directory is picked up with no extra shell setup.

---

## Simulated vs live — read this

**The Go SDK has no paper-trading flag.** This was verified against the actual
v0.25.2 source, not assumed:

```console
$ grep -rniE "paper|simulat|sandbox|demo|test_env|virtual" \
    /home/tchan/go/pkg/mod/github.com/longbridge/openapi-go@v0.25.2/ --include=*.go
(no matches)
```

The only environment selector the SDK exposes is `LONGBRIDGE_ENV=staging`,
which just repoints the hosts at `*.longbridge.xyz` (`config/config.go:31-42`).
It does **not** create or select a simulated account.

So the truth is:

> Simulated and live are distinguished **only by which credentials you pass**.
> There is no base URL that means "paper".

Because of that, this demo cannot verify the switch either. What it does
instead is make your intent explicit and enforce the safety gate around it:

| Var | Default | Meaning |
| --- | --- | --- |
| `LONGPORT_MODE` | `simulated` | Your *stated* account type. `simulated` or `live`. |
| `LONGPORT_DRY_RUN` | `1` | When on, no order write is possible. |

If `LONGPORT_MODE=simulated`, writes stay blocked **even with dry run off**,
so an accidental `LONGPORT_DRY_RUN=0` cannot trade your simulated money. That
is the whole point of the extra switch: a typo in one variable is not enough to
place a real order.

Note the different prefixes on purpose: `LONGBRIDGE_*` are the SDK's own
variables, `LONGPORT_*` are this demo's. The SDK also still accepts
`LONGPORT_APP_KEY` / `LONGPORT_APP_SECRET` / `LONGPORT_ACCESS_TOKEN` as
fallbacks for the credentials, which is a mild trap — the demo prefers
`LONGBRIDGE_*` and documents the old names as deprecated.

---

## Dry-run semantics and the safety gate

**Default is dry run. Nothing is sent.**

A write happens only when **all three** of these hold:

1. `LONGPORT_DRY_RUN=0` (or `false`), and
2. `--confirm-live` passed on the command line, and
3. `LONGPORT_MODE=live`

Otherwise the command prints the exact request it *would* have sent, prints
why it was blocked, and exits **0** without opening a connection.

Verified matrix (credentials were dummy, so anything that got through would
have failed at auth — it did not):

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

$ LONGPORT_DRY_RUN=0 go run ./cmd/trade ... --confirm-live
BLOCKED: refusing to submit an order: mode is "simulated" but dry run is disabled.
No order was sent. Either restore LONGPORT_DRY_RUN=1, or set
  LONGPORT_MODE=live
to state that these are real-money credentials.

$ LONGPORT_DRY_RUN=0 LONGPORT_MODE=live go run ./cmd/trade ... --confirm-live
error: creating trade context: ... httpStatus:401 code:401004 message:token invalid
```

That last line is the proof the wiring is real: it reached Longbridge's servers
and was rejected for the dummy token.

Dry run is enforced at the single choke point `internal/config.GuardWrite`,
called by every one of `SubmitOrder`, `ReplaceOrder` and `CancelOrder`
immediately before the SDK call. The trade context is created **lazily**, so a
dry run does not open a websocket or make an authenticated round trip. If you
add a new write operation, copy the existing `gate(...)` shape.

---

## Commands

### `quote` — market data snapshot (read-only)

Quotes, candlesticks, order-book depth and option chain for a HK name and a US
name.

```bash
go run ./cmd/quote                                  # 700.HK + AAPL.US, daily
go run ./cmd/quote -symbols 700.HK,9988.HK,AAPL.US
go run ./cmd/quote -symbols 700.HK -period 5m -count 20
go run ./cmd/quote -chain 700.HK -us AAPL.US         # option chains
go run ./cmd/quote -depth 10 -no-adjust
```

| Flag | Default | Notes |
| --- | --- | --- |
| `-symbols` | `700.HK,AAPL.US` | Comma-separated. |
| `-period` | `day` | `1m 5m 15m 30m 60m day week month year`. |
| `-count` | `5` | Candle count. |
| `-depth` | `5` | Book levels per side. |
| `-chain` | `700.HK` | Option chain underlying. |
| `-us` | `AAPL.US` | US option chain underlying. |
| `-no-adjust` | `false` | Disable forward adjustment. |
| `-timeout` | `15s` | Per-request timeout. |

### `trade` — account state and gated orders

```bash
go run ./cmd/trade -action balance
go run ./cmd/trade -action positions
go run ./cmd/trade -action positions -symbol 700.HK
go run ./cmd/trade -action today-orders
go run ./cmd/trade -action history-orders -days 30

# Writes — dry run unless you override all three switches.
go run ./cmd/trade -action submit -symbol 700.HK -qty 100 -price 400.50
go run ./cmd/trade -action replace -order-id 1234567890 -qty 200 -price 401.00
go run ./cmd/trade -action cancel -order-id 1234567890

# Actually send (simulated account, real sandbox money):
LONGPORT_DRY_RUN=0 LONGPORT_MODE=live \
  go run ./cmd/trade -action submit -symbol 700.HK -qty 100 -price 400.50 --confirm-live
```

| Flag | Default | Notes |
| --- | --- | --- |
| `-action` | `balance` | `balance positions today-orders history-orders submit replace cancel`. |
| `-symbol` | — | Required for `submit`; optional filter elsewhere. |
| `-order-id` | — | Required for `replace` and `cancel`. |
| `-qty` | `0` | Required for `submit` and `replace`. |
| `-price` | — | Decimal string. |
| `-side` | `Buy` | `Buy` or `Sell`. |
| `-type` | `LO` | `LO ELO MO AO ALO ODD LIT MIT TSLPAMT TSLPPCT TSMAMT TSMPCT SLO`. |
| `-tif` | `Day` | `Day`, `GTC`, `GTD`. |
| `-remark` | — | Free text. |
| `-days` | `7` | Lookback for `history-orders`. |
| `-confirm-live` | `false` | Required acknowledgement for writes. |

### `watch` — realtime stream (read-only)

Streams quotes, ticks, depth and broker queues over the quote websocket.
Stops cleanly on `Ctrl-C`: it unsubscribes, closes the context and exits 0.

```bash
go run ./cmd/watch -symbols 700.HK,AAPL.US
go run ./cmd/watch -symbols 700.HK -trade=false -depth=false   # quotes only
go run ./cmd/watch -symbols 700.HK -brokers -interval 5s
```

| Flag | Default | Notes |
| --- | --- | --- |
| `-symbols` | `700.HK,AAPL.US` | Comma-separated. |
| `-quote` | `true` | Last-done quotes. |
| `-trade` | `true` | Individual ticks. |
| `-depth` | `true` | Order book. |
| `-brokers` | `false` | Broker queues (noisy). |
| `-interval` | `2s` | Throttle for depth prints. |

Depth is throttled because the book updates many times a second and
unthrottled output is unreadable.

### `market` — reference data (read-only)

Market-wide trading status, trading calendar, intraday timeline.

```bash
go run ./cmd/market
go run ./cmd/market -sections status
go run ./cmd/market -sections calendar -market HK -back 7 -forward 7
go run ./cmd/market -sections timeline -timeline-symbol 700.HK -lines 30
```

| Flag | Default | Notes |
| --- | --- | --- |
| `-sections` | `status,calendar,timeline` | Any subset, comma-separated. |
| `-market` | `HK` | `HK US CN SG UK`. |
| `-back` / `-forward` | `7` | Calendar window in days. |
| `-timeline-symbol` | `700.HK` | Intraday symbol. |
| `-lines` | `20` | Intraday lines to print. |

---

## Configuration reference

Precedence: **environment variables beat the YAML file**, and the YAML file is
only consulted for values the environment did not supply. The file is
auto-detected as `config.yaml`, `config.local.yaml`, `config.yml` or
`config.toml`; `-config PATH` overrides the search. An explicit `-config` path
that does not exist is an error, so a typo cannot silently fall back to env.

Copy `config.example.yaml` to `config.yaml`. The top-level key **must** be
`longbridge:` — the SDK errors with `Longbridge config is not exist in yaml
file` otherwise.

### Credential variables

| Variable | Purpose |
| --- | --- |
| `LONGBRIDGE_APP_KEY` | App Key (required). |
| `LONGBRIDGE_APP_SECRET` | App Secret (required, secret). |
| `LONGBRIDGE_ACCESS_TOKEN` | Access Token (required, secret). |
| `LONGPORT_APP_KEY` / `LONGPORT_APP_SECRET` / `LONGPORT_ACCESS_TOKEN` | Deprecated fallbacks. |

### Optional endpoints and tuning

| Variable | Default | Purpose |
| --- | --- | --- |
| `LONGPORT_REGION` | auto | `cn` for mainland China access points. |
| `LONGBRIDGE_HTTP_URL` | `https://openapi.longbridge.com` | HTTP API base. |
| `LONGBRIDGE_QUOTE_URL` | `wss://openapi-quote.longbridge.com/v2` | Quote websocket. |
| `LONGBRIDGE_TRADE_URL` | `wss://openapi-trade.longbridge.com/v2` | Trade websocket. |
| `LONGBRIDGE_ENV` | unset | `staging` repoints at `*.longbridge.xyz`. Not a paper account. |
| `LONGPORT_LANGUAGE` | `en` | `en`, `zh-CN`, `zh-HK`. |
| `LONGPORT_ENABLE_OVERNIGHT` | `false` | 24h US overnight trading. |
| `LONGBRIDGE_HTTP_TIMEOUT` | `15s` | Demo default. |
| `LONGBRIDGE_TIMEOUT` / `LONGBRIDGE_AUTH_TIMEOUT` | SDK default | Per-request timeouts. |
| `LONGBRIDGE_LOG_LEVEL` | unset | `trace debug info warn error`. |
| `LONGBRIDGE_READ_QUEUE_SIZE` / `WRITE_QUEUE_SIZE` / `READ_BUFFER_SIZE` / `MIN_GZIP_SIZE` | SDK default | Protocol tuning. |

### Demo-only safety variables

| Variable | Default | Purpose |
| --- | --- | --- |
| `LONGPORT_DRY_RUN` | `1` | Blocks all writes. Must be `0` to write. |
| `LONGPORT_MODE` | `simulated` | `simulated` or `live`. |

### Secret handling

`app_secret` and `access_token` are never printed or logged, not even
redacted — they are pure credentials with no diagnostic value. The startup
banner redacts the App Key only, keeping at most 4 leading characters:

```console
[config] mode=simulated  (expected: credentials from a SIMULATED account) dry_run=true app_key=dumm******78 http=https://openapi.longbridge.com (SDK default) ...
```

`.gitignore` excludes `.env`, `.env.*`, `config.yaml`, `config.local.yaml`,
`config.yml` and `config.toml`, while explicitly keeping `.env.example` and
`config.example.yaml`.

---

## Honest status

**This project has never been run against a working Longbridge account.** It
was not built with an access token available, and no order has been placed.

What *was* verified by execution:

- `gofmt -l .` reports nothing.
- `go build ./...` and `go vet ./...` both exit 0.
- All four binaries build; `-h` exits 0 with no credentials.
- All four exit **2** with a readable missing-credentials message and no panic.
- With dummy credentials, all four reach the real Longbridge API and fail
  with `httpStatus:401 code:401004 message:token invalid` — proving the config,
  signing and network path are genuinely wired, not stubbed.
- Every write path is blocked in each of the three non-sending configurations.

What is **not** verified: the shape of successful responses, field-by-field
rendering, option-chain content, intraday data, and whether any live order is
accepted. The output formatting was written against the v0.25.2 type
definitions in the module cache, so it should be correct, but "should be" is
not "was observed". Expect to adjust column widths.

There is no automated test suite. There should be — the config loader in
particular is testable without credentials and is the natural first target.

---

## Troubleshooting

**`missing Longbridge credentials`** — expected when unset. Follow the
printed instructions. Remember the SDK reads `.env` only from the *current
working directory*.

**`httpStatus:401 code:401004 message:token invalid`** — the credentials were
rejected. Check, in order: (1) the Access Token is the legacy one from User
Center, not an OAuth token; (2) all three values are from the *same* account
(mixing a live App Key with a simulated token is the usual mistake); (3) the
token has not been revoked. Turn on `LONGBRIDGE_LOG_LEVEL=debug` for detail.

**`Longbridge config is not exist in yaml file`** — the YAML is missing the
top-level `longbridge:` key, or the file extension is not `.yaml`/`.yml`
(the SDK infers the format from the extension).

**`config file "./x.yaml": no such file or directory`** — an explicit
`-config` path does not exist. This is intentional, so a typo cannot silently
fall back to environment variables.

**Websockets connect but nothing streams** — the symbol may not be subscribed
to that data type, or the market is closed. Check with
`go run ./cmd/market -sections status`. US symbols outside regular hours only
produce pre/post-market data.

**`unknown -tif "Day"`** — should not happen; if you see it you have a stale
binary. Rebuild with `make build`. (It was a real bug during development: the
SDK's `TimeTypeDay` is `"Day"`, not `"DAY"`.)

**Timeouts** — mainland China users should set `LONGPORT_REGION=cn` for the
`.cn` access points. Note `.cn` has **no route to the US data centre**, so US
accounts must use `.com`, including for login. See the
[access point vs data centre](https://open.longbridge.com/docs/getting-started)
table in Longbridge's docs.

---

## Project layout

```
longbridge-go-demo/
├── cmd/
│   ├── quote/main.go     read-only market data snapshot
│   ├── trade/main.go     account state + gated order writes
│   ├── watch/main.go     realtime stream, graceful SIGINT
│   └── market/main.go    status, calendar, intraday timeline
├── internal/
│   ├── config/           env + YAML loader, mode/dry-run switch, redaction
│   │   ├── config.go
│   │   └── file.go
│   └── cli/              shared flag parsing, credential errors, panic guard
├── .env.example          every LONGPORT_*/LONGBRIDGE_* var, fully commented
├── config.example.yaml   YAML template, `longbridge:` block
├── Makefile              build, fmt, fmt-check, vet, tidy
├── go.mod / go.sum
└── README.md
```

### A note on the module path

The SDK was renamed. Older examples on the web say
`github.com/longportapp/openapi-go`, but at v0.25.2 that path **fails to
build**:

```console
$ go get github.com/longportapp/openapi-go@v0.25.2
go: github.com/longportapp/openapi-go@v0.25.2: parsing go.mod:
        module declares its path as: github.com/longbridge/openapi-go
                but was required as: github.com/longportapp/openapi-go
```

The correct import path is `github.com/longbridge/openapi-go`. This repo uses
that. Old `longport` package names are deprecated; the `LONGPORT_*`
environment variables still work, which is the one place the old name lingers.

---

## License

The SDK is dual Apache-2.0 / MIT. This demo is provided as-is, with no
warranty of any kind — and in particular no guarantee that it will not place a
real order if you configure it to.
