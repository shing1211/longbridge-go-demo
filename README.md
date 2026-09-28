# longbridge-go-demo

A small, complete, safety-gated demo of the **Longbridge (Longport) OpenAPI**
using the official Go SDK, [`github.com/longbridge/openapi-go`](https://pkg.go.dev/github.com/longbridge/openapi-go)
at **v0.25.2**.

Eight commands, one shared config loader, and a hard rule that no order can be
sent unless you say so three different ways.

**SDK coverage: every exported method on `QuoteContext` and `TradeContext` is
exercised somewhere in `cmd/`.** That is 66 method definitions (47 on
`QuoteContext`, 19 on `TradeContext`), covering 62 distinct method names —
the gap is `Close`, `Subscribe`, `Unsubscribe` and `Trades` appearing on both
types. See [Coverage](#sdk-coverage) for the command that verifies this.

The SDK has 130 context methods in total across ten context types. The other
64 belong to `FundamentalContext` (32), `SharelistContext` (8), `ContentContext`
(7), `ScreenerContext` (5), `PortfolioContext` (5), `AlertContext` (4),
`AssetContext` (2) and `CalendarContext` (1), none of which this demo uses —
they cover fundamental data, share lists, research content, screeners,
portfolios, price alerts and the trading calendar.

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
- [The two safety gates](#the-two-safety-gates)
- [Commands](#commands)
- [Configuration reference](#configuration-reference)
- [SDK coverage](#sdk-coverage)
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

# 5. The rest of the read-only surface.
go run ./cmd/warrant                      # HK derivative warrants
go run ./cmd/watchlist -action list        # saved watchlist groups
go run ./cmd/executions -action today-executions
go run ./cmd/reference                     # static + market-wide reference data
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

Once that simulated access token is available, every read-only command in this
repo can be run against it directly — drop the triple into `.env` and go. This
is the recommended way to exercise the demo end to end, including the
order paths, which will trade simulated money in a sandbox rather than real
money. Nothing in the code needs to change to switch accounts: the credentials
are the only thing that differs.

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

## The two safety gates

This demo guards two different kinds of mutation with two **separate**,
independently-defaulted gates. They are not interchangeable, and neither one
opens the other.

| | Order gate | Watchlist gate |
| --- | --- | --- |
| Guards | `SubmitOrder`, `ReplaceOrder`, `CancelOrder`, `WithdrawOrder` | `CreateWatchlistGroup`, `DeleteWatchlistGroup`, `UpdateWatchlistGroup`, `UpdatePinned` |
| Env switch | `LONGPORT_DRY_RUN` (default `1`) | `LONGPORT_WATCHLIST_DRY_RUN` (default `1`) |
| Flag | `--confirm-live` | `--confirm` |
| Also requires | `LONGPORT_MODE=live` | — |
| Implementation | `config.GuardWrite` | `config.GuardWatchlist` |

**Why watchlist writes are not under the order gate.** The order gate's third
condition, `LONGPORT_MODE=live`, exists because "live" means real money. A
watchlist group is a saved list of ticker symbols: labelling a cosmetic
preference change as "live trading" would be dishonest, and blocking it until
you assert you have real money would be needlessly annoying. But these calls do
mutate server-side state and are not idempotent, so they needed *a* real guard,
not none. Hence a second gate with its own dry-run default.

Either gate refusing makes **no network call at all**: the write commands print
the exact request they would send, explain the refusal, and exit 0 without
constructing a context. Verified for both:

```console
$ go run ./cmd/watchlist -action create -name demo -symbols 700.HK
[config] watchlist_dry_run=true (separate gate: LONGPORT_WATCHLIST_DRY_RUN + --confirm)

--- CreateWatchlistGroup request ---
  name         demo
  symbols      700.HK

BLOCKED: missing --confirm
DRY RUN: nothing was sent to Longbridge.
Watchlist writes are guarded separately from orders; pass
  --confirm   together with   LONGPORT_WATCHLIST_DRY_RUN=0

$ go run ./cmd/watchlist -action create -name demo -symbols 700.HK --confirm
[config] watchlist_dry_run=true (separate gate: LONGPORT_WATCHLIST_DRY_RUN + --confirm)

--- CreateWatchlistGroup request ---
  name         demo
  symbols      700.HK

BLOCKED: refusing to create a watchlist group: watchlist DRY RUN is active.
No change was sent. Watchlist writes mutate your account's
saved groups, so they are guarded separately from orders.
To allow them set
  LONGPORT_WATCHLIST_DRY_RUN=0
and pass --confirm. Both are required.
DRY RUN: nothing was sent to Longbridge.

$ LONGPORT_WATCHLIST_DRY_RUN=0 go run ./cmd/watchlist -action create -name demo --confirm
--- CreateWatchlistGroup request ---
  name         demo
  symbols      -

error: creating quote context: ... httpStatus:401 code:401004 message:token invalid
```

The last line is the proof the write path is genuinely wired: with both
switches set it stops refusing and reaches the real API (here rejected for the
dummy token).

Both switches are required and either alone still blocks. A bad
`LONGPORT_WATCHLIST_DRY_RUN` value is rejected at startup rather than being
silently treated as "on", and an unparseable value at runtime fails safe.

Note that `executions -action withdraw` is an **order** write (`WithdrawOrder`
is an alias of `CancelOrder` in the SDK), so it uses the order gate and
`--confirm-live` — not the watchlist gate.

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

# Order updates instead of quote data (trade websocket, not quote):
go run ./cmd/watch -orders
go run ./cmd/watch -orders -topics trade,order
```

| Flag | Default | Notes |
| --- | --- | --- |
| `-symbols` | `700.HK,AAPL.US` | Comma-separated. |
| `-quote` | `true` | Last-done quotes. |
| `-trade` | `true` | Individual ticks. |
| `-depth` | `true` | Order book. |
| `-brokers` | `false` | Broker queues (noisy). |
| `-orders` | `false` | Stream ORDER updates instead; takes a separate path. |
| `-topics` | `trade` | Trade topics for `-orders`. |
| `-interval` | `2s` | Throttle for depth prints. |

Depth is throttled because the book updates many times a second and
unthrottled output is unreadable.

`-orders` uses a `TradeContext` rather than a `QuoteContext`, because order
pushes arrive on a different websocket (`wss://openapi-trade...`). It is still
read-only: subscribing to a push feed changes nothing server-side. It reports
per-topic subscribe failures explicitly, because a partial subscribe still
"successfully" returns and silently streaming nothing is the confusing outcome.

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

### `warrant` — HK derivative warrants (read-only)

```bash
go run ./cmd/warrant                                # warrants over 700.HK
go run ./cmd/warrant -underlying 9988.HK -count 20
go run ./cmd/warrant -type call,put -expiry gt12 -sort-by change_rate -sort-order asc
go run ./cmd/warrant -action quote -warrant-symbols 12181.HK,67408.HK
go run ./cmd/warrant -action issuers
```

| Flag | Default | Notes |
| --- | --- | --- |
| `-action` | `list` | `list`, `quote` or `issuers`. |
| `-underlying` | `700.HK` | Underlying stock for `list`. |
| `-warrant-symbols` | `12181.HK,67408.HK` | Contracts for `quote`. |
| `-sort-by` | `last_done` | `last_done change_rate change_val volume turnover expiry_date strike_price outstanding_qty implied_volatility delta status`. |
| `-sort-order` | `desc` | `asc` or `desc`. |
| `-count` | `10` | `sort_count`. |
| `-offset` | `0` | `sort_offset`, for paging. |
| `-type` | — | `call put bull bear inline`, comma-separated. |
| `-expiry` | — | `lt3 bt3_6 bt6_12 gt12`. |
| `-moneyness` | — | `in` or `out`. |
| `-status` | — | `suspend listed normal`. |
| `-language` | `zh-hk` | `zh-cn`, `en` or `zh-hk`. |

Warrants are a HK-only product, so `-underlying` must be a `.HK` symbol. All
the filter enums are bare `int32` in the SDK, so they are parsed by explicit
switch: an unrecognised value is an error rather than a silent zero, because a
wrong cast would quietly return the wrong warrants.

### `watchlist` — saved groups, with a gated editor

`-action list` is a pure read. The other four actions mutate your account's
saved groups and are behind the [watchlist gate](#the-two-safety-gates).

```bash
go run ./cmd/watchlist -action list

# Writes — dry run unless you set BOTH switches.
go run ./cmd/watchlist -action create -name tech -symbols 700.HK,AAPL.US
go run ./cmd/watchlist -action update -group-id 12345 -symbols MSFT.US -update-mode add
go run ./cmd/watchlist -action update -group-id 12345 -update-mode remove -symbols 700.HK
go run ./cmd/watchlist -action pin -symbols 700.HK -pin-mode add
go run ./cmd/watchlist -action delete -group-id 12345 -purge

# Actually apply (account state changes, no orders involved):
LONGPORT_WATCHLIST_DRY_RUN=0 \
  go run ./cmd/watchlist -action create -name tech -symbols 700.HK --confirm
```

| Flag | Default | Notes |
| --- | --- | --- |
| `-action` | `list` | `list create delete update pin`. |
| `-name` | — | Required for `create`; optional rename on `update`. |
| `-group-id` | — | Required for `update`, `delete`. |
| `-symbols` | — | Required for `create` and `pin`. |
| `-update-mode` | `add` | `add`, `remove` or `replace`. |
| `-pin-mode` | `add` | `add` or `remove`. |
| `-purge` | `false` | For `delete`: also delete the group's symbols. |
| `-confirm` | `false` | **Required** for every write. |

### `executions` — fills, order detail and buying power

Read-only except `-action withdraw`, which is an order write under the order
gate.

```bash
go run ./cmd/executions -action today-executions
go run ./cmd/executions -action history-executions -days 30 -symbol 700.HK
go run ./cmd/executions -action order-detail -order-id 1234567890
go run ./cmd/executions -action max-purchase -symbol 700.HK -price 400.50
go run ./cmd/executions -action margin-ratio -symbol 700.HK
go run ./cmd/executions -action fund-positions
go run ./cmd/executions -action cash-flow -days 7 -balance-type cash

# Write (an order close — needs dry_run=0 AND --confirm-live AND mode=live):
go run ./cmd/executions -action withdraw -order-id 1234567890
```

| Flag | Default | Notes |
| --- | --- | --- |
| `-action` | `today-executions` | See list above. |
| `-symbol` | — | Filter, or required for `max-purchase` and `margin-ratio`. |
| `-order-id` | — | Required for `order-detail` and `withdraw`. |
| `-price` | `0` | Unit price for `max-purchase`; 0 means at market. |
| `-side` | `Buy` | For `max-purchase`. |
| `-type` | `LO` | Order type for `max-purchase`. |
| `-days` | `7` | Lookback for `history-executions` and `cash-flow`. |
| `-balance-type` | — | `cash`, `stock` or `fund`. |
| `-page` / `-size` | `0` / `50` | `cash-flow` paging. |
| `-confirm-live` | `false` | **Required** for `withdraw`. |

`order-detail` prints the conditional-order fields (trigger price/status,
trailing amounts), the commission-free and deduction status that decide final
cost, the charge breakdown, and the history snapshot. Note the SDK's
`OrderDetail.History` is a single `OrderHistoryDetail` struct, not a list, so
one snapshot is printed.

### `reference` — static and market-wide data

Everything here is read-only, selectable by `-sections` so you can pull just
the part you want.

```bash
go run ./cmd/reference                                       # default sections
go run ./cmd/reference -sections static,list -market HK -list-limit 50
go run ./cmd/reference -sections index -indexes last_done,pe_ttm,pb
go run ./cmd/reference -sections flow,distribution -symbol 700.HK
go run ./cmd/reference -sections session,participants,profile
go run ./cmd/reference -sections realtime,history -symbol 700.HK
go run ./cmd/reference -sections optionvol,filings -symbol 700.HK
go run ./cmd/reference -sections short,counter
```

| Section | SDK methods | Notes |
| --- | --- | --- |
| `static` | `StaticInfo` | Lot size, share counts, EPS, dividend yield. |
| `list` | `SecurityList` | Whole security list for a market. |
| `index` | `CalcIndex` | Computed indices; names via `-indexes`. |
| `flow` | `CapitalFlow` | Intraday capital flow, last 20 points. |
| `distribution` | `CapitalDistribution` | Daily large/medium/small in and out. |
| `session` | `TradingSession` | Per-market sessions, times as `HH:MM`. |
| `participants` | `Participants` | Exchange broker ids and names. |
| `profile` | `Profile` | Entitlements and rate limits (no HTTP call). |
| `realtime` | `RealtimeQuote`, `RealtimeDepth`, `RealtimeTrades`, `Brokers` | HTTP snapshots of the push feeds. |
| `history` | `HistoryCandlesticksByOffset`, `HistoryCandlesticksByDate` | Both paging styles. |
| `optionvol` | `OptionVolume`, `OptionVolumeDaily` | Aggregate and daily put/call. |
| `filings` | `Filings` | Regulatory documents and URLs. |
| `brokers` | `RealtimeBrokers` | Broker queue snapshot. |
| `short` | `ShortPositions`, `ShortTrades` | Short interest, US and HK. |
| `counter` | `SymbolToCounterIds`, `ResolveCounterIds` | Both directions, chained. |

| Flag | Default | Notes |
| --- | --- | --- |
| `-sections` | `static,index,session,participants,profile` | Comma-separated subset. |
| `-symbols` | `700.HK,AAPL.US` | For `static`, `index`, `flow`, `realtime`, `counter`. |
| `-symbol` | `700.HK` | Single symbol for the others. |
| `-market` | `HK` | For `list`: `HK US CN SG UK`. |
| `-indexes` | `last_done,change_rate,volume,turnover,pe_ttm,pb` | See `parseCalcIndexes` in the source for all ~45 names. |
| `-history-count` | `10` | Candles for `history`. |
| `-history-days` | `5` | Day range for `history` and `optionvol`. |
| `-forward` | `false` | Look forward from today instead of back. |
| `-short-count` | `10` | Records for `short`. |
| `-list-limit` | `20` | Rows printed for `list`. |

`profile` is the odd one out: `Profile()` takes no context and returns no
error, because the entitlement arrives over the websocket at connect time. It
therefore cannot fail the way the HTTP sections do, and may legitimately be
empty on a very fast exit.

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
| `LONGPORT_DRY_RUN` | `1` | Blocks all order writes. Must be `0` to write. |
| `LONGPORT_MODE` | `simulated` | `simulated` or `live`. |
| `LONGPORT_WATCHLIST_DRY_RUN` | `1` | Blocks watchlist writes. Must be `0` to write. See [the two gates](#the-two-safety-gates). |

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

## SDK coverage

This demo covers **every exported method on `QuoteContext` and
`TradeContext`** — 66 method definitions, 62 distinct names. You can check
that claim yourself without trusting this README:

```console
$ BASE=$(go env GOMODCACHE)/github.com/longbridge/openapi-go@v0.25.2
$ grep -hoE '^func \(c \*(Quote|Trade)Context\) [A-Z][A-Za-z0-9]*' \
    "$BASE"/quote/*.go "$BASE"/trade/*.go | sed -E 's/.*\) //' | sort -u \
  | while read -r m; do
      grep -rqE "\.$m\(" cmd/ || echo "UNCOVERED: $m"
    done
$ # (no output = fully covered)
```

This is a **static** check: it proves every method is *referenced*, not that
every method was *exercised against a live account*. No command in this repo
has been run with a working access token, so no successful response has ever
been observed. See [Honest status](#honest-status).

Not covered, and why:

| Area | Context type | Methods | Why not |
| --- | --- | --- | --- |
| Fundamentals | `FundamentalContext` | 32 | Financial statements and valuation; needs a paid data entitlement to be useful. |
| Share lists | `SharelistContext` | 8 | User share lists; write-shaped, same category as the watchlist. |
| Research content | `ContentContext` | 7 | News and research documents. |
| Screeners | `ScreenerContext` | 5 | Symbol screening. |
| Portfolios | `PortfolioContext` | 5 | Multi-account portfolio aggregation. |
| Price alerts | `AlertContext` | 4 | **All writes.** Would need its own third guard. |
| Assets | `AssetContext` | 2 | Fund/NAV reference. |
| Calendar | `CalendarContext` | 1 | `cmd/market` uses `TradingDays` from `QuoteContext` instead. |

`AlertContext` is the interesting omission: every one of its four methods
creates or cancels a price alert, so covering it would mean adding a third
write gate. That is out of scope here rather than something that was forgotten.

---

## Honest status

**This project has never been run against a working Longbridge account.** It
was not built with an access token available, and no order has been placed.

What *was* verified by execution:

- `gofmt -l .` reports nothing.
- `go build ./...` and `go vet ./...` both exit 0.
- All eight binaries build; `-h` exits 0 with no credentials.
- All eight exit **2** with a readable missing-credentials message listing all
  three variables, and no panic.
- With dummy credentials, every command reaches the real Longbridge API and
  fails with `httpStatus:401 code:401004 message:token invalid` — proving the
  config, signing and network path are genuinely wired, not stubbed. This was
  checked for all three `warrant` actions, `watchlist -action list`, all seven
  read-only `executions` actions, and all eleven `reference` sections.
- Every write path is blocked in each of the non-sending configurations, and
  every refusal was confirmed to make **no** network call.
- The static coverage check above finds no uncovered method.

Specifically verified about the guards:

| Command | Switches | Result |
| --- | --- | --- |
| `trade -action submit` | default | blocked: missing `--confirm-live`, no network |
| `trade -action submit` | `DRY_RUN=0 --confirm-live` | blocked: mode is simulated, no network |
| `trade -action cancel` | `--confirm-live` | blocked: dry run active, no network |
| `executions -action withdraw` | default | blocked: missing `--confirm-live`, no network |
| `executions -action withdraw` | `DRY_RUN=0 --confirm-live` | blocked: mode is simulated, no network |
| `watchlist -action create` | default | blocked: missing `--confirm`, no network |
| `watchlist -action create` | `--confirm` | blocked: watchlist dry run, no network |
| `watchlist` create/update/pin/delete | `WATCHLIST_DRY_RUN=0 --confirm` | reached the API (401 on the dummy token) |

What is **not** verified: the shape of successful responses, field-by-field
rendering, column widths, and whether any live order is accepted. No command
here has ever run against a working access token. The output formatting was
written against the v0.25.2 type definitions in the module cache, so it should
be correct, but "should be" is not "was observed". Expect to adjust column
widths once real data flows.

This matters most for the newer sections, whose types are the least
documented: the short-sale fields, the capital-flow points, the trading-session
minute encoding and the option-volume strings are all rendered from the struct
definitions alone.

There is no automated test suite. There should be — the config loader and both
guards in particular are testable without credentials and are the natural
first target.

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
│   ├── quote/main.go       read-only market data snapshot
│   ├── trade/main.go       account state + gated order writes
│   ├── watch/main.go       realtime stream, graceful SIGINT
│   ├── market/main.go      status, calendar, intraday timeline
│   ├── warrant/main.go     HK warrant list, quotes, issuers
│   ├── watchlist/main.go   saved groups + separately gated edits
│   ├── executions/main.go  fills, order detail, buying power, cash flow
│   └── reference/main.go   static, market-wide and entitlement data
├── internal/
│   ├── config/             env + YAML loader, mode/dry-run switch, redaction
│   │   ├── config.go       credentials, LONGPORT_MODE, order gate
│   │   ├── guard.go        the separate watchlist gate
│   │   └── file.go
│   └── cli/                shared flag parsing, credential errors, panic guard
├── .env.example            every LONGPORT_*/LONGBRIDGE_* var, fully commented
├── config.example.yaml     YAML template, `longbridge:` block
├── Makefile                build, fmt, fmt-check, vet, tidy
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
