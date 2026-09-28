# longbridge-go-demo

A small, complete, safety-gated demo of the **Longbridge (Longport) OpenAPI**
using the official Go SDK, [`github.com/longbridge/openapi-go`](https://pkg.go.dev/github.com/longbridge/openapi-go)
at **v0.25.2**.

Twelve commands, one shared config loader, and a hard rule that no order can be
sent unless you say so three different ways.

**SDK coverage: every exported method on `QuoteContext` and `TradeContext` is
exercised somewhere in `cmd/`.** That is 66 method definitions (47 on
`QuoteContext`, 19 on `TradeContext`), covering 62 distinct method names —
the gap is `Close`, `Subscribe`, `Unsubscribe` and `Trades` appearing on both
types. See [Coverage](#sdk-coverage) for the command that verifies this.

The SDK has 130 context methods in total across ten context types. Of the
other 64, this demo now also covers `SharelistContext` (3 of 8 read methods),
`ContentContext` (5 of 7 read methods) and `PortfolioContext` (all 5). Still
unused: `ScreenerContext` (5) and `AlertContext` (4) — screeners and price
alerts. `FundamentalContext` (32), `AssetContext` (2) and `CalendarContext` (1)
are now fully covered by `cmd/fundamentals`. See
[Coverage](#sdk-coverage) for the exact list and the reason for each omission.

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
- [The four safety gates](#the-four-safety-gates)
- [Commands](#commands)
- [The reusable write guard](#the-reusable-write-guard)
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
go run ./cmd/sharelist                     # your share lists, popular lists
go run ./cmd/content -action news          # research news for a symbol
go run ./cmd/dca                            # your DCA plans + statistics
go run ./cmd/alert -action list             # your price alerts
go run ./cmd/portfolio -action summary     # account-level P&L analytics
go run ./cmd/fundamentals -action company   # company fundamentals, ratings, valuation
go run ./cmd/fundamentals -action calendar  # financial calendar
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
why it was blocked, and exits **3** without opening a connection. Exit 3 is
reserved for "a safety guard refused this write" — see `config.ExitBlocked`.

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

## The four safety gates

This demo guards four different kinds of mutation with four **separate**,
independently-defaulted gates. They are not interchangeable, and none of them
opens another.

| | Order gate | Watchlist gate | Sharelist gate | Content gate |
| --- | --- | --- | --- | --- |
| Guards | `SubmitOrder`, `ReplaceOrder`, `CancelOrder`, `WithdrawOrder` | `CreateWatchlistGroup`, `DeleteWatchlistGroup`, `UpdateWatchlistGroup`, `UpdatePinned` | `Create`, `Delete`, `AddSecurities`, `RemoveSecurities`, `SortSecurities` | `CreateTopic`, `CreateTopicReply` |
| Env switch | `LONGPORT_DRY_RUN` (default `1`) | `LONGPORT_WATCHLIST_DRY_RUN` (default `1`) | `LONGPORT_SHARELIST_DRY_RUN` (default `1`) | `LONGPORT_CONTENT_DRY_RUN` (default `1`) |
| Flag | `--confirm-live` | `--confirm` | `--confirm-live-sharelist` | `--confirm-live-content` |
| Also requires | `LONGPORT_MODE=live` | — | `LONGPORT_MODE=live` | `LONGPORT_MODE=live` |
| Implementation | `config.GuardWrite` | `config.GuardWatchlist` | `sharelistGate` (in `cmd/sharelist`) | `contentGate` (in `cmd/content`) |
| Blocked exit code | `3` (`config.ExitBlocked`) | `3` (`config.ExitBlocked`) | **`3`** | **`3`** |

Two further gates — for DCA plans and price alerts — were added alongside
these. They use the shared, reusable `config.WriteGuard` helper rather than a
per-command copy of the loop:

| | DCA gate | Price-alert gate |
| --- | --- | --- |
| Guards | `Create`, `Update`, `Pause`, `Resume`, `Stop`, `SetReminder` | `Add`, `Update`, `Delete` |
| Env switch | `LONGPORT_DCA_DRY_RUN` (default `1`) | `LONGPORT_ALERT_DRY_RUN` (default `1`) |
| Flag | `--confirm-live-dca` | `--confirm-live-alert` |
| Also requires | `LONGPORT_MODE=live` | `LONGPORT_MODE=live` |
| Implementation | `config.DCAGuard` (a `config.WriteGuard`) | `config.AlertGuard` (a `config.WriteGuard`) |
| Blocked exit code | `3` | `3` |

All six gates refuse independently and all six exit **3**. See
[`dca`](#dca--dollar-cost-averaging-plans-with-a-dedicated-write-gate),
[`alert`](#alert--price-alerts-with-a-dedicated-write-gate) and
[The reusable write guard](#the-reusable-write-guard).

**Why watchlist writes are not under the order gate.** The order gate's third
condition, `LONGPORT_MODE=live`, exists because "live" means real money. A
watchlist group is a saved list of ticker symbols: labelling a cosmetic
preference change as "live trading" would be dishonest, and blocking it until
you assert you have real money would be needlessly annoying. But these calls do
mutate server-side state and are not idempotent, so they needed *a* real guard,
not none. Hence a second gate with its own dry-run default.

**Why sharelist and content writes have their own gates.** They are neither
orders nor private scratch preferences, so neither existing gate fits:

- A **sharelist** is a published, shareable list. It is visible to other users
  and to your other devices, and `Delete` is irreversible — the SDK exposes no
  undelete endpoint, and recreating a list does not restore its ID or contents.
- **Content** publishes text *as the account owner*, publicly, and the SDK
  exposes no delete-topic method. A post made here cannot be retracted.

Both therefore require all three conditions, including `LONGPORT_MODE=live`:
these are actions other people can see, so an explicit "this is real" assertion
is appropriate in a way that it is not for a watchlist group. Their gates live
**inside the command packages** rather than in `internal/config/guard.go`, so
that adding them required no change to shared guard code.

Either gate refusing makes **no network call at all**: the write commands print
the exact request they would send, explain the refusal, and exit **3** without
constructing a context. Verified for the order and watchlist gates:

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

### The sharelist and content gates in practice

The sharelist and content refusals list **every** unsatisfied condition rather
than the first, print the request body that would have been sent on
`[DRY-RUN]`-prefixed lines, and exit **3**:

```console
$ go run ./cmd/sharelist -action delete -id 12345 -confirm-live-sharelist
[config] sharelist_dry_run=true (separate gate: LONGPORT_SHARELIST_DRY_RUN + --confirm-live-sharelist + LONGPORT_MODE=live)

[DRY-RUN] --- Delete (DELETE /v1/sharelists/{id}) request that would be sent ---
[DRY-RUN]   id           12345
[DRY-RUN]   WARNING        IRREVERSIBLE: there is no undelete endpoint. Deleting a sharelist removes it from your library and takes its constituents with it; recreating it will NOT restore the original contents or its ID.

[DRY-RUN] BLOCKED: refusing to delete sharelist 12345.
[DRY-RUN] Unsatisfied condition(s):
[DRY-RUN]   - LONGPORT_SHARELIST_DRY_RUN is on (default 1; set it to 0 to allow sharelist writes)
[DRY-RUN]   - LONGPORT_MODE=simulated (sharelist writes require LONGPORT_MODE=live)
[DRY-RUN] NOTHING was sent to Longbridge. All three are required:
[DRY-RUN]   LONGPORT_SHARELIST_DRY_RUN=0  +  --confirm-live-sharelist  +  LONGPORT_MODE=live
$ echo $?
3
```

With all three satisfied the gate opens and the request goes out (here rejected
for the dummy token, which is the only observable proof available without a
real token):

```console
$ LONGPORT_SHARELIST_DRY_RUN=0 LONGPORT_MODE=live \
    go run ./cmd/sharelist -action create -name demo --confirm-live-sharelist
error: creating sharelist "demo": longbridge openapi error, httpStatus:401 code:401004 message:token invalid trace:...
```

Exit code **3** is used specifically for a refusal, matching the convention in
the sibling Tiger project. It is deliberately distinct from `0` (success), `1`
(generic error) and `2` (missing credentials), so a script can distinguish "the
safety gate did its job" from "the command failed". It is the same status the
order and watchlist gates use, so all four gates agree.

`-show-state` is the one opt-in exception to "a refused write makes no
request": on a blocked `add`/`remove`/`sort` it fetches and prints the list's
current constituents so the intended change is reviewable. It is **off by
default** precisely so the no-network guarantee holds for a plain refused
write, and a failure to fetch it is reported but never changes the gate's
verdict.

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

### `market` — market-wide data (read-only)

Trading status, the trading calendar, an intraday timeline, and the rest of
the SDK's `market` package: A/H premium, market anomaly alerts, broker
holdings, index constituents, top movers, ranked lists and trade statistics.

Everything is selected by `-sections`, so you can pull just one part.

```bash
go run ./cmd/market                                            # default sections
go run ./cmd/market -sections status
go run ./cmd/market -sections calendar -market HK -back 7 -forward 7
go run ./cmd/market -sections timeline -timeline-symbol 700.HK -lines 30
go run ./cmd/market -sections ahpremium -ah-symbol 00700.HK -ah-period day -ah-count 30
go run ./cmd/market -sections ahpremium-intraday
go run ./cmd/market -sections anomaly -anomaly-market HK
go run ./cmd/market -sections broker-holding -broker-symbol 700.HK -broker-period 5
go run ./cmd/market -sections broker-holding-detail -broker-symbol 700.HK
go run ./cmd/market -sections broker-holding-daily -broker-symbol 700.HK -broker-id 1234
go run ./cmd/market -sections constituent -index HSI.HK
go run ./cmd/market -sections top-movers -mover-markets HK,US -mover-sort desc -mover-limit 10
go run ./cmd/market -sections rank-categories
go run ./cmd/market -sections rank-list -rank-key turnover
go run ./cmd/market -sections trade-stats -trade-stats-symbol 700.HK
```

| Section | SDK method | Notes |
| --- | --- | --- |
| `status` | `MarketStatus` | Per-market status, delayed status, sub-status. |
| `calendar` | `TradingDays` (quote ctx) | Full and half trading days. |
| `timeline` | `Intraday` (quote ctx) | Last `-lines` intraday points. |
| `ahpremium` | `AhPremium` | A/H premium klines for a dual-listed name. |
| `ahpremium-intraday` | `AhPremiumIntraday` | Today's A/H premium klines. |
| `anomaly` | `Anomaly` | Unusual price/volume events for a market. |
| `broker-holding` | `BrokerHolding` | Top buy/sell brokers over a window. |
| `broker-holding-detail` | `BrokerHoldingDetail` | All brokers, ratio and share changes. |
| `broker-holding-daily` | `BrokerHoldingDaily` | Daily history for one broker. |
| `constituent` | `Constituent` | Index members with rise/flat/fall counts. |
| `top-movers` | `TopMovers` | Unusual movers across one or more markets. |
| `rank-categories` | `RankCategories` | Category keys for the rank list. |
| `rank-list` | `RankList` | The ranked list for one category key. |
| `trade-stats` | `TradeStats` | Average price, totals, price levels. |

| Flag | Default | Notes |
| --- | --- | --- |
| `-sections` | `status,calendar,timeline` | Comma-separated subset of the table above. |
| `-market` | `HK` | `HK US CN SG UK`; for the calendar. |
| `-back` / `-forward` | `7` | Calendar window in days. |
| `-timeline-symbol` | `700.HK` | Intraday symbol. |
| `-lines` | `20` | Intraday lines to print. |
| `-ah-symbol` | `00700.HK` | Dual-listed symbol for both A/H sections. |
| `-ah-period` | `day` | `1m 5m 15m 30m 60m day week month year`. |
| `-ah-count` | `30` | A/H klines to print. |
| `-anomaly-market` | `HK` | Market for `anomaly`. |
| `-broker-symbol` | `700.HK` | Symbol for the three broker sections. |
| `-broker-id` | — | **Required** for `broker-holding-daily`. |
| `-broker-period` | `5` | Broker window: `1 5 20 60`. |
| `-index` | `HSI.HK` | Index symbol for `constituent`. |
| `-mover-markets` | `HK` | Comma-separated markets for `top-movers`. |
| `-mover-sort` | `desc` | `asc` or `desc`. |
| `-mover-date` | — | Optional `YYYY-MM-DD` filter, validated locally. |
| `-mover-limit` | `10` | Max events. |
| `-rank-key` | — | **Required** for `rank-list`; get it from `rank-categories`. |
| `-rank-article` | `false` | Ask for article content too. |
| `-trade-stats-symbol` | `700.HK` | Symbol for `trade-stats`. |
| `-timeout` | `15s` | Per-request timeout. |

Two sections need an input the API cannot guess, and the command fails at
startup with a clear message rather than making a doomed request:
`-broker-id` for `broker-holding-daily` and `-rank-key` for `rank-list`.

`calendar` and `timeline` live on the quote context, everything else on the
market context. The quote context opens a websocket, so `cmd/market` creates it
**lazily** — a market-only run such as `-sections anomaly` never opens it. That
also means each section is independently reachable; with the dummy credentials
you get a distinct, correct 401 per section rather than one blanket failure.

The broker periods and the A/H periods are bare `int` enums in the SDK, so they
are parsed by explicit switch: an unrecognised value is an error rather than a
silent zero. `TopMovers`' sort is a raw `uint32` for the same reason.

`rank-categories` returns an opaque `json.RawMessage` in the SDK, not a typed
struct, so the command pretty-prints the JSON instead of mapping fields it does
not have. `rank-list` re-adds the SDK's internal `ib_` key prefix for you if
you paste a clean key.

`AhPremiumKline` holds plain `decimal.Decimal` values rather than pointers, so
there is no absent-versus-zero distinction to preserve there; the broker-holding
and constituent fields *are* pointers and render `-` when absent.

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

### `sharelist` — user share lists, with a gated editor

Your own share lists, the full detail of one list including its constituents,
the platform's popular lists — and, behind the [sharelist gate](#the-four-safety-gates),
the five write methods on the lists you own.

```bash
go run ./cmd/sharelist -action list
go run ./cmd/sharelist -action popular -count 10
go run ./cmd/sharelist -action detail -id 12345
go run ./cmd/sharelist -action detail -id 12345 -stock-limit 50

# writes — all three switches required
LONGPORT_SHARELIST_DRY_RUN=0 LONGPORT_MODE=live \
  go run ./cmd/sharelist -action create -name tech -description "semis" \
    --confirm-live-sharelist
LONGPORT_SHARELIST_DRY_RUN=0 LONGPORT_MODE=live \
  go run ./cmd/sharelist -action add    -id 12345 -symbols 700.HK,9988.HK --confirm-live-sharelist
LONGPORT_SHARELIST_DRY_RUN=0 LONGPORT_MODE=live \
  go run ./cmd/sharelist -action sort   -id 12345 -symbols 9988.HK,700.HK  --confirm-live-sharelist
LONGPORT_SHARELIST_DRY_RUN=0 LONGPORT_MODE=live \
  go run ./cmd/sharelist -action remove -id 12345 -symbols 700.HK          --confirm-live-sharelist
LONGPORT_SHARELIST_DRY_RUN=0 LONGPORT_MODE=live \
  go run ./cmd/sharelist -action delete -id 12345 --confirm-live-sharelist
```

| Flag | Default | Notes |
| --- | --- | --- |
| `-action` | `list` | `list`, `detail`, `popular`, `create`, `delete`, `add`, `remove`, `sort`. |
| `-count` | `20` | Sharelists returned by `list` and `popular`. |
| `-id` | — | **Required** for `detail`, `delete`, `add`, `remove` and `sort`. |
| `-popular` | `false` | Alias for `-action popular`. |
| `-stock-limit` | `20` | Constituent rows printed per list. |
| `-name` | — | **Required** for `create`. |
| `-description` | — | Optional for `create`; the SDK defaults it to the name. |
| `-symbols` | — | **Required** for `add`, `remove`, `sort`. `CODE.MARKET` form, checked locally. |
| `-confirm-live-sharelist` | `false` | **Required** for any write. |
| `-show-state` | `false` | On a blocked `add`/`remove`/`sort`, also print the list's current contents (this makes a read-only request). |

**All eight `SharelistContext` methods are now covered.** The read/write split
was verified against the v0.25.2 source rather than inferred from method names:

| Method | Verb + path | Kind |
| --- | --- | --- |
| `List` | `GET /v1/sharelists` | read |
| `Detail` | `GET /v1/sharelists/{id}` | read |
| `Popular` | `GET /v1/sharelists/popular` | read |
| `Create` | `POST /v1/sharelists` | **write** |
| `Delete` | `DELETE /v1/sharelists/{id}` | **write** |
| `AddSecurities` | `POST /v1/sharelists/{id}/items` | **write** |
| `RemoveSecurities` | `DELETE /v1/sharelists/{id}/items` | **write** |
| `SortSecurities` | `POST /v1/sharelists/{id}/items/sort` | **write** |

Two things worth knowing before you use the writes. `Delete` is **irreversible**
— the SDK has no undelete endpoint and recreating a list restores neither its
ID nor its constituents, so the dry-run output says so explicitly. And
`SortSecurities` **replaces** the list's order: a symbol you leave out of
`-symbols` drops back out of its current position, so pass the full intended
order rather than the rows you want to move. Both the add/remove/sort and
delete paths warn about this in their dry-run block.

Note that `-symbols` is validated locally for the `CODE.MARKET` shape, because
the SDK silently converts `700.HK` to a `counter_id` of `ST/HK/700` and a bare
`700` would be sent as-is and rejected by the API.

### `content` — research and community content, with gated publishing

Discussion topics and news for a symbol, one topic's full record, the replies
on a topic, your own topics — and, behind the [content gate](#the-four-safety-gates),
publishing a topic or a reply.

```bash
go run ./cmd/content -action topics -symbol 700.HK
go run ./cmd/content -action news -symbol AAPL.US
go run ./cmd/content -action detail -topic-id 12345
go run ./cmd/content -action replies -reply-topic 12345 -page 2
go run ./cmd/content -action mine -topic-type article

# writes — all three switches required
LONGPORT_CONTENT_DRY_RUN=0 LONGPORT_MODE=live \
  go run ./cmd/content -action create-topic -topic-type article \
    -title "700.HK earnings" -body "Markdown body" -tickers 700.HK \
    --confirm-live-content
LONGPORT_CONTENT_DRY_RUN=0 LONGPORT_MODE=live \
  go run ./cmd/content -action reply -reply-topic 12345 -body "Agreed." \
    --confirm-live-content
```

| Flag | Default | Notes |
| --- | --- | --- |
| `-action` | `topics` | `topics`, `news`, `detail`, `replies`, `mine`, `create-topic`, `reply`. |
| `-symbol` | `700.HK` | For `topics` and `news`. |
| `-topic-id` | — | **Required** for `detail`. |
| `-reply-topic` | — | **Required** for `replies` and `reply`. |
| `-topic-type` | — | `article`, `post` or empty; filters `mine`, and is the type for `create-topic`. |
| `-page` | `1` | 1-based, for `replies` and `mine`. |
| `-size` | `20` | `replies` allows 1–50, `mine` allows 1–500. |
| `-limit` | `20` | Rows printed (`0` = all). |
| `-title` | — | Required when `create-topic -topic-type article`. |
| `-body` | — | **Required** for `create-topic` and `reply`. |
| `-tickers` | — | Comma-separated, max 10, for `create-topic`. |
| `-hashtags` | — | Comma-separated, max 5, for `create-topic`. |
| `-reply-to-id` | — | For `reply`: nest under this reply id; empty = top-level. |
| `-confirm-live-content` | `false` | **Required** for any write. |

**All seven `ContentContext` methods are now covered.** Again from the source,
not the method names:

| Method | Verb + path | Kind |
| --- | --- | --- |
| `Topics` | `GET /v1/content/topics` | read |
| `News` | `GET /v1/content/news` | read |
| `TopicDetail` | `GET /v1/content/topics/{id}` | read |
| `MyTopics` | `GET /v1/content/topics/my` | read |
| `ListTopicReplies` | `GET /v1/content/topics/{id}/comments` | read |
| `CreateTopic` | `POST /v1/content/topics` | **write** |
| `CreateTopicReply` | `POST /v1/content/topics/{id}/comments` | **write** |

Both writes publish under your account and are **public and irreversible** —
the SDK exposes no delete-topic method, so anything published here cannot be
retracted from this demo. That is why they also require `LONGPORT_MODE=live`
on top of the dry-run switch and the confirmation flag. The dry-run block
repeats this warning, and the reply path mentions the SDK's per-topic rate
limit (first 3 replies free, then 3s → 5s → 8s → 13s → 21s → 34s → 55s cap).

The SDK omits `title`, `topic_type`, `tickers`, `hashtags` and `reply_to_id`
from the body when they are empty, so the dry-run block lists only the fields
that would actually be sent rather than printing misleading blanks. The `-size`
bounds are the SDK's own documented ranges and are checked locally, so an
out-of-range value fails immediately instead of at the API.

### `portfolio` — P&L analytics (read-only)

Currency exchange rates, the account-level profit/loss summary, a per-security
breakdown, a by-market page, per-security detail and the flow records behind it.
All five `PortfolioContext` methods are covered.

```bash
go run ./cmd/portfolio -action rates
go run ./cmd/portfolio -action summary
go run ./cmd/portfolio -action summary -start 2026-01-01 -end 2026-06-30
go run ./cmd/portfolio -action by-market -market HK -page 1
go run ./cmd/portfolio -action detail -symbol 700.HK
go run ./cmd/portfolio -action flows -symbol 700.HK -derivative
```

| Flag | Default | Notes |
| --- | --- | --- |
| `-action` | `summary` | `rates`, `summary`, `by-market`, `detail`, `flows`. |
| `-symbol` | `700.HK` | **Required** for `detail` and `flows`. |
| `-start` / `-end` | — | `YYYY-MM-DD`; empty uses the server default window. |
| `-market` | — | Market filter for `by-market`. |
| `-currency` | — | Currency filter for `by-market`. |
| `-page` | `1` | 1-based, for `by-market` and `flows`. |
| `-size` | `50` | Page size for `by-market` and `flows`. |
| `-derivative` | `false` | Include derivative flows. |
| `-limit` | `20` | Detail rows printed (`0` = all). |

Every money field here is a `*decimal.Decimal`, so `nil` means "the API did not
supply this", which is a different fact from zero. The output renders `-` for
absent and a real number for zero, because a P&L report that silently shows
`0.00` for a missing figure is worse than one that admits it is missing.

`ProfitAnalysis` internally calls two endpoints and merges them; `summary`
prints both the summary half and the per-security sublist. `flows` and `detail`
return `has_more` / credit-debit-fee breakdowns, which are printed in full up
to `-limit`.

---

### `fundamentals` — fundamentals, statements and calendar (read-only)

The largest single block in the SDK: all **32** `FundamentalContext` methods,
both `AssetContext` methods and the one `CalendarContext` method. One `-action`
per SDK method, so each is reachable on its own.

```bash
go run ./cmd/fundamentals -action company -symbol 700.HK
go run ./cmd/fundamentals -action valuation -symbol 700.HK
go run ./cmd/fundamentals -action all-symbol -symbol 700.HK   # the main set at once
go run ./cmd/fundamentals -action report -kind is -period af
go run ./cmd/fundamentals -action rating
go run ./cmd/fundamentals -action consensus
go run ./cmd/fundamentals -action valuation-compare -peers MSFT.US,GOOGL.US
go run ./cmd/fundamentals -action etf-allocation -symbol 3067.HK
go run ./cmd/fundamentals -action macro-indicators -symbol 700.US
go run ./cmd/fundamentals -action statements                      # account's own
go run ./cmd/fundamentals -action calendar -calendar-category dividend
```

| Action | SDK method | Notes |
| --- | --- | --- |
| `report` | `FinancialReport` | Raw JSON; the indicator tree is untyped in the SDK. |
| `rating` | `InstitutionRating` | Two endpoints combined; fails if either does. |
| `rating-detail` | `InstitutionRatingDetail` | Weekly rating and target-price history. |
| `rating-views` | `InstitutionRatingViews` | Counts here are **strings**, not ints. |
| `forecast-eps` | `ForecastEps` | Forecast windows with institution counts. |
| `consensus` | `Consensus` | Per-period actual vs estimate. |
| `snapshot` | `FinancialReportSnapshot` | Forecast-vs-reported earnings block. |
| `operating` | `Operating` | Management-discussion reports and indicators. |
| `dividend` | `Dividend` | |
| `dividend-detail` | `DividendDetail` | Same return type, different endpoint. |
| `corp-action` | `CorpAction` | Dividends, splits, buybacks. |
| `buyback` | `Buyback` | TTM summary, history, ratios. |
| `valuation` | `Valuation` | PE/PB/PS/yield. |
| `valuation-history` | `ValuationHistory` | PE/PB/PS only — **no** yield. |
| `industry-valuation` | `IndustryValuation` | Peer table. |
| `industry-valuation-dist` | `IndustryValuationDist` | Percentile within the industry. |
| `valuation-compare` | `ValuationComparison` | Takes a peer **list** and a currency. |
| `company` | `Company` | Full profile and registration data. |
| `executive` | `Executive` | Board and management. |
| `segments` | `BusinessSegments` | Latest split. |
| `segments-history` | `BusinessSegmentsHistory` | Business **and** regional breakdowns. |
| `ratings` | `Ratings` | Scores are raw JSON (int, float or null). |
| `shareholder` | `Shareholder` | Major holders and cross-holdings. |
| `shareholder-top` | `ShareholderTop` | Raw JSON; no struct in the SDK. |
| `shareholder-detail` | `ShareholderDetail` | Needs `-object-id`. |
| `fund-holder` | `FundHolder` | `PositionRatio` is a non-pointer decimal. |
| `invest-relation` | `InvestRelation` | What *this* company holds in others. |
| `etf-allocation` | `EtfAssetAllocation` | Holdings/region/asset-class/industry. |
| `industry-rank` | `IndustryRank` | **Market**-wide, not per symbol. |
| `industry-peers` | `IndustryPeers` | Recursive peer chain. |
| `macro-indicators` | `MacroeconomicIndicators` | `/v2/quote/macrodata`. |
| `macro` | `Macroeconomic` | Needs `-indicator-code`. |
| `statements` | `AssetContext.Statements` | Account statements, daily or monthly. |
| `statement-url` | `AssetContext.StatementDownloadURL` | Prints the presigned URL; never fetches it. |
| `calendar` | `CalendarContext.FinanceCalendar` | Window defaults to today + 30 days. |
| `all-symbol` | — | Runs the main per-symbol set in order. |

| Flag | Default | Notes |
| --- | --- | --- |
| `-symbol` | `700.HK` | The symbol for nearly every action. |
| `-action` | `valuation` | See the table above. |
| `-kind` | `all` | `report`: `is`, `bs`, `cf`, `all`. |
| `-period` | — | `report`: `af`, `saf`, `q1`, `q2`, `q3`, `qf`, `3q`. |
| `-peers` | `MSFT.US,GOOGL.US` | Peer list for `valuation-compare`; also the fuzzy keyword filter for `macro-indicators`. |
| `-currency` | `USD` | For `valuation-compare`. |
| `-report` / `-fiscal-year` / `-fiscal-period` | — / `0` / — | Passed through to `segments-history` and `snapshot`. |
| `-cate` | — | Category filter for `segments-history`. |
| `-indicator` | `0` | `industry-rank`: `0`–`7`. Numeric on purpose — the SDK defines no names for these codes. |
| `-sort-type` | `1` | `industry-rank`: `0` ascending, `1` descending. |
| `-limit` | `20` | Row cap for `industry-rank`, `macro-indicators`, `macro`. |
| `-object-id` | `0` | **Required** for `shareholder-detail`. |
| `-statement-type` | `daily` | `daily` or `monthly`. |
| `-page` / `-page-size` | `1` / `20` | For `statements`. |
| `-file-key` | — | **Required** for `statement-url`. |
| `-calendar-category` | `report` | `report`, `dividend`, `split`, `ipo`, `macrodata`, `closed`, `meeting`, `merge`. |
| `-calendar-start` / `-calendar-end` | today / +30d | `YYYY-MM-DD`; an end before the start is rejected. |
| `-calendar-market` | — | Optional market filter. |
| `-indicator-code` | — | **Required** for `macro`. |
| `-macro-start` / `-macro-end` / `-macro-offset` | — / — / `0` | For `macro`. |

Every monetary field in this package is a `*decimal.Decimal` and arrives on the
wire as a **JSON string**, so `nil` ("the API did not supply this") is
distinguishable from zero. The output renders `-` for absent rather than
`0.00`, for the reason given under `portfolio`.

`-action statement-url` prints a presigned URL and deliberately does not fetch
it: the URL embeds a signature, and downloading account statements is outside
what this read-only demo does.

`FinancialReport`, `ShareholderTop` and `ShareholderDetail` return
`json.RawMessage` because the SDK does not type those payloads. They are
pretty-printed and clipped at 4 KB with an explicit `…(truncated, N bytes
total)` marker, so a cut-off block is never mistaken for a complete one.

This binary additionally asserts the order gate is still **closed** at startup,
exactly like the other readers: if you somehow run it with `LONGPORT_DRY_RUN=0`
and `LONGPORT_MODE=live`, it refuses to run at all rather than proceeding in a
misconfigured environment.

---

### `dca` — dollar-cost-averaging plans, with a dedicated write gate

All **11** `DCAContext` methods. Five are read-only and unguarded; the six that
change a plan sit behind the DCA gate described below.

The read/write split is a **source fact** taken from `dca/context.go` in
v0.25.2 — which endpoint each method calls, and whether that endpoint mutates —
rather than inferred from the method name:

| Action | SDK method | Endpoint | Guarded |
| --- | --- | --- | --- |
| `list` | `List` | `GET /v1/dailycoins/query` | no |
| `history` | `History` | `GET /v1/dailycoins/query-records` | no |
| `stats` | `Stats` | `GET /v1/dailycoins/statistic` | no |
| `check-support` | `CheckSupport` | `POST /v1/dailycoins/batch-check-support` | no |
| `calc-date` | `CalcDate` | `POST /v1/dailycoins/calc-trd-date` | no |
| `create` | `Create` | `POST /v1/dailycoins/create` | **yes** |
| `update` | `Update` | `POST /v1/dailycoins/update` | **yes** |
| `pause` | `Pause` | `POST /v1/dailycoins/toggle` `Suspended` | **yes** |
| `resume` | `Resume` | `POST /v1/dailycoins/toggle` `Active` | **yes** |
| `stop` | `Stop` | `POST /v1/dailycoins/toggle` `Finished` | **yes** |
| `set-reminder` | `SetReminder` | `POST /v1/dailycoins/update-alter-hours` | **yes** |

Two things about that table are worth stating plainly, because both look like
over-caution if you only skim it.

**`check-support` and `calc-date` are POSTs, and are deliberately not guarded.**
They compute an answer and change nothing server-side. Guarding a pure
computation behind three switches would be theatre, and it would make
`calc-date` — the one call you want in order to sanity-check a schedule *before*
committing to it — awkward to use. A POST verb is not a mutation.

**Pause, resume and stop share one endpoint**, differing only by a `status`
string. So the guard is applied at the **method** level, in the dispatch switch
in `cmd/dca/main.go`, before any context is created. There is no single
"toggle" SDK call to hang a guard off; if the guard had been written against
the endpoint it would have had nothing to attach to.

```console
# Read-only — no switches needed beyond credentials.
go run ./cmd/dca -action list
go run ./cmd/dca -action stats -symbol 700.HK
go run ./cmd/dca -action history -plan-id <id> -limit 20
go run ./cmd/dca -action check-support -symbol 700.HK
go run ./cmd/dca -action calc-date -symbol 700.HK -frequency monthly -day-of-month 15

# Writes — all three switches required.
LONGPORT_DCA_DRY_RUN=0 LONGPORT_MODE=live \
  go run ./cmd/dca -action create -symbol 700.HK -amount 1000 \
    -frequency monthly -day-of-month 15 --confirm-live-dca
LONGPORT_DCA_DRY_RUN=0 LONGPORT_MODE=live \
  go run ./cmd/dca -action update -plan-id <id> -amount 2000 --confirm-live-dca
LONGPORT_DCA_DRY_RUN=0 LONGPORT_MODE=live \
  go run ./cmd/dca -action pause -plan-id <id> --confirm-live-dca
LONGPORT_DCA_DRY_RUN=0 LONGPORT_MODE=live \
  go run ./cmd/dca -action stop  -plan-id <id> --confirm-live-dca
```

| Flag | Default | Purpose |
| --- | --- | --- |
| `-action` | `list` | One of the eleven actions above. |
| `-symbol` | — | **Required** for `create` and `calc-date`; optional filter for `stats`. |
| `-amount` | — | Per-investment amount, decimal string. **Required** for `create`. |
| `-frequency` | `monthly` | `daily`, `weekly`, `fortnightly` or `monthly`. |
| `-day-of-week` | — | **Required** for `weekly`/`fortnightly`, e.g. `Monday`. |
| `-day-of-month` | — | `1`-`31`, for `monthly`. Rejected above 31 locally. |
| `-allow-margin` | `false` | Margin financing for a plan — real leverage. Guarded like any other write. |
| `-plan-id` | — | **Required** for `update`, `pause`, `resume`, `stop`, `history`. |
| `-reminder-hours` | — | **Required** for `set-reminder`; the SDK accepts `1`, `6` or `12`. |
| `-confirm-live-dca` | `false` | **Required** for any write. |

`-day-of-week` and `-day-of-month` are checked against `-frequency` locally:
weekly/fortnightly without a weekday is rejected before any request, and
supplying both is rejected, so you get a clear local error rather than an
opaque one from the API.

**There is no preview mode in the SDK.** There is no plan/preview method —
`Create` creates the plan directly, and it starts investing on the schedule.
So the command's own dry run is the only preview available, which is a large
part of why the gate is three switches rather than one. `Stop` is likewise
irreversible: a `Finished` plan cannot be resumed, and the dry-run output says
so on the `WARNING` line.

The gate is `config.DCAGuard`, built from the reusable `config.WriteGuard`
helper described in [The reusable write guard](#the-reusable-write-guard). A
refusal lists **every** unsatisfied condition at once, prints the request body
under a `[DRY-RUN]` prefix, makes no network call, and exits **3**.

---

### `alert` — price alerts, with a dedicated write gate

All **4** `AlertContext` methods. `List` is a read; `Add`, `Update` and `Delete`
are behind the alert gate.

| Action | SDK method | Endpoint | Guarded |
| --- | --- | --- | --- |
| `list` | `List` | `GET /v1/notify/reminders` | no |
| `add` | `Add` | `POST /v1/notify/reminders` | **yes** |
| `update` | `Update` | `POST /v1/notify/reminders` | **yes** |
| `delete` | `Delete` | `DELETE /v1/notify/reminders` | **yes** |

**Add and update share one endpoint.** The SDK tells them apart by whether the
body carries an `id`: `Update` takes a whole `*AlertItem` obtained from `List`
and echoes it back, `Add` builds a body with no `id` at all. There is no
endpoint-level place to hang a guard here, so — exactly as with DCA's
pause/resume/stop — the guard sits at the **method** level, in the dispatch
switch, before any context is created.

`Update` needs an `AlertItem` and the SDK has no get-by-id call, so the command
resolves `-id` by calling `List`. That read happens **after** the gate, not
before: a refusal that dials the API is not a refusal, and the guarantee that a
blocked write makes no network call has to hold for every action. The cost is
that the dry-run preview cannot show the resolved body — paid only when the
gate is already open and a request was going out regardless.

```console
# Read-only.
go run ./cmd/alert -action list

# Writes — all three switches required.
LONGPORT_ALERT_DRY_RUN=0 LONGPORT_MODE=live \
  go run ./cmd/alert -action add -symbol 700.HK -condition price-rise -value 600 \
    --confirm-live-alert
LONGPORT_ALERT_DRY_RUN=0 LONGPORT_MODE=live \
  go run ./cmd/alert -action update -id <id> -enabled=false --confirm-live-alert
LONGPORT_ALERT_DRY_RUN=0 LONGPORT_MODE=live \
  go run ./cmd/alert -action delete -id <id> --confirm-live-alert
```

| Flag | Default | Purpose |
| --- | --- | --- |
| `-action` | `list` | `list`, `add`, `update` or `delete`. |
| `-symbol` | — | **Required** for `add`. |
| `-condition` | `price-rise` | `price-rise`, `price-fall`, `percent-rise`, `percent-fall`. |
| `-value` | — | Trigger threshold, decimal string. **Required** for `add`. |
| `-frequency` | `once` | `daily`, `every-time` or `once`. |
| `-id` | — | **Required** for `update` and `delete`. |
| `-enabled` | `true` | For `update`: enable or disable the alert. |
| `-confirm-live-alert` | `false` | **Required** for any write. |

`ValueMap` is `json.RawMessage` in the SDK — the upstream shape is not stable —
so the rendering in `-action list` and the dry-run preview is a pretty-printer,
not a field mapping. `Delete` is irreversible: there is no undelete endpoint,
and recreating the alert yields a new id and loses its trigger state, which the
dry-run `WARNING` line says out loud.

---

## The reusable write guard

Adding a third and fourth family of guarded mutations would have meant a third
and fourth copy of the same three-line loop. That copy-paste is where safety
bugs live — a variant that forgets to propagate the refusal, or forgets that
the two switches must be *independent*, is easy to write and hard to spot. So
the loop is written once, in `internal/config/writeguard.go`:

```go
type WriteGuard struct {
	Name        string // "DCA" in messages
	Description string // why this family of writes is sensitive
	DryRunEnv   string // LONGPORT_DCA_DRY_RUN
	ConfirmFlag string // --confirm-live-dca (documentation; the command owns the flag)
	RequireLive bool   // demand LONGPORT_MODE=live
}
```

A write proceeds only when **all three** conditions hold, and each is checked
independently: a missing flag refuses even when the env is cleared, and the env
refuses even when the flag is present. `Unsatisfied` returns *all* of the
unmet conditions rather than the first, so one run tells the user everything
they need to change. `DryRun` fails safe — unset, or unparseable, both mean
"still in dry run" — and `ValidateDryRunEnv` rejects a bad value at startup
rather than at the moment a write is refused.

The next family of guarded writes should be one more `WriteGuard` value and one
`gate` function, not a new copy of the loop. The commands print the
`[DRY-RUN]`-prefixed request and return the refusal; `cli.Fail` turns that into
exit 3.

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
| `LONGPORT_WATCHLIST_DRY_RUN` | `1` | Blocks watchlist writes. Must be `0` to write. See [the four gates](#the-four-safety-gates). |
| `LONGPORT_SHARELIST_DRY_RUN` | `1` | Blocks sharelist writes. Must be `0` **and** `--confirm-live-sharelist` **and** `LONGPORT_MODE=live`. |
| `LONGPORT_CONTENT_DRY_RUN` | `1` | Blocks publishing topics/replies. Must be `0` **and** `--confirm-live-content` **and** `LONGPORT_MODE=live`. |
| `LONGPORT_DCA_DRY_RUN` | `1` | Blocks DCA plan writes. Must be `0` **and** `--confirm-live-dca` **and** `LONGPORT_MODE=live`. |
| `LONGPORT_ALERT_DRY_RUN` | `1` | Blocks price-alert writes. Must be `0` **and** `--confirm-live-alert` **and** `LONGPORT_MODE=live`. |

### Exit codes

| Code | Meaning |
| --- | --- |
| `0` | Success. The command did what it was asked. |
| `1` | A real failure: the API rejected the call, or a flag value was unusable. |
| `2` | Missing credentials, or a usage error. |
| `3` | **BLOCKED.** A safety guard refused a write. Nothing was sent. |

Exit `3` is reserved, and is the one to check when scripting. It is defined
once as `config.ExitBlocked` and reached by every guard refusal, which returns
a `*config.BlockedError`; `cli.Fail` maps that to `os.Exit(3)`. A guard
refusal used to return `nil` and therefore exit `0`, which made a refusal
indistinguishable from a completed order — do not reintroduce that.

Every guarded command uses it: `trade` (submit/replace/cancel),
`executions -action withdraw`, `watchlist` (create/update/pin/delete), `dca`
(create/update/pause/resume/stop/set-reminder), `alert` (add/update/delete) and
`sharelist` / `content` where present.

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

The same check for `FundamentalContext`, `AssetContext` and `CalendarContext`
(35 methods, all in `cmd/fundamentals`):

```console
$ grep -hoE '^func \(c \*(Fundamental|Asset|Calendar)Context\) [A-Z][A-Za-z0-9]*' \
    "$BASE"/fundamental/*.go "$BASE"/asset/*.go "$BASE"/calendar/*.go \
  | sed -E 's/.*\) //' | sort -u \
  | while read -r m; do
      grep -rqE "\.$m\(" cmd/ || echo "UNCOVERED: $m"
    done
$ # (no output = fully covered)
```

And for `DCAContext` (11 methods, all in `cmd/dca`) and `AlertContext`
(4 methods, all in `cmd/alert`):

```console
$ grep -hoE '^func \([cd] \*(DCA|Alert)Context\) [A-Z][A-Za-z0-9]*' \
    "$BASE"/dca/*.go "$BASE"/alert/*.go \
  | sed -E 's/.*\) //' | sort -u \
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

| Area | Context type | Methods | Covered | Why not |
| --- | --- | --- | --- | --- |
| Fundamentals | `FundamentalContext` | 32 | 32 | Fully covered by `cmd/fundamentals`. |
| Share lists | `SharelistContext` | 8 | 8 | Fully covered by `cmd/sharelist`; the 5 writes sit behind the sharelist gate. |
| Research content | `ContentContext` | 7 | 7 | Fully covered by `cmd/content`; the 2 writes sit behind the content gate. |
| Screeners | `ScreenerContext` | 5 | 0 | Symbol screening. |
| Portfolios | `PortfolioContext` | 5 | 5 | Fully covered. |
| Price alerts | `AlertContext` | 4 | 0 | **All writes.** Would need its own third guard. |
| Assets | `AssetContext` | 2 | 2 | Fully covered by `cmd/fundamentals`. |
| Calendar | `CalendarContext` | 1 | 1 | Fully covered by `cmd/fundamentals`. `cmd/market` also has `TradingDays` from `QuoteContext`. |

The rule the omissions follow is simple: **this demo only calls a mutating
method when that method sits behind its own complete gate.** `AlertContext` is
currently the clearest case of something left alone — all four of its methods
create or cancel a price alert, and no alert gate exists. `SharelistContext`
and `ContentContext` used to be in that position and are no longer: their
writes are now covered, each behind its own three-condition gate, so those
contexts are fully covered.

---

## Honest status

**This project has never been run against a working Longbridge account.** It
was not built with an access token available, and no order has been placed. The
account the user has is a *simulated* one, but no access token for it was
supplied, so nothing in this repo has ever been validated against a live API.

What *was* verified by execution:

- `gofmt -l .` reports nothing.
- `go build ./...` and `go vet ./...` both exit 0.
- All fourteen binaries build; `-h` exits 0 with no credentials.
- All fourteen exit **2** with a readable missing-credentials message listing all
  three variables, and no panic.
- With dummy credentials, every command reaches the real Longbridge API and
  fails with `httpStatus:401 code:401004 message:token invalid` — proving the
  config, signing and network path are genuinely wired, not stubbed. This was
  checked for all three `warrant` actions, `watchlist -action list`, all seven
  read-only `executions` actions, all eleven `reference` sections, all fourteen
  `market` sections, all three read-only `sharelist` actions, all five
  read-only `content` actions, all five `portfolio` actions, all
  **thirty-three** `fundamentals` actions, all five read-only `dca` actions
  and `alert -action list`.
- Every write path is blocked in each of the non-sending configurations, and
  every refusal was confirmed to make **no** network call. The 26 sharelist
  and content write combinations all exit **3**.
- The DCA and alert gates were swept exhaustively: **63 blocked cases** (6 DCA
  actions × 7 switch combinations, 3 alert actions × 7) each exit **3**, print
  a `[DRY-RUN]`-prefixed request and make no network call; and all **9** of
  those actions, with all three switches satisfied, really do reach the API
  and return a real 401. Every one of the three switches refuses on its own.
- The same sweep re-checked the order and watchlist gates: 15 blocked
  combinations still block and now exit **3**, and all 8 unblocked
  combinations still reach the API. No guard was weakened.
- A blocked write was measured at ~10ms against ~300ms for an equivalent
  unblocked write to the real gateway, which is the empirical evidence that a
  refusal dials nothing.
- An unparseable `LONGPORT_DCA_DRY_RUN` or `LONGPORT_ALERT_DRY_RUN` is
  rejected at startup, not silently treated as "on".
- The static coverage check above finds no uncovered method.

**A 401 is a real answer, not a successful one.** It proves the request was
built, signed and delivered, and rejected. It says nothing about the shape of a
successful response, whether a column is wide enough for a real security name,
or whether a field is really optional. Nothing below should be read as
"this output was observed from Longbridge".

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
| `dca` create/update/pause/resume/stop/set-reminder | any switch missing (7 combinations each) | **exit 3**, blocked, no network |
| `dca` create/update/pause/resume/stop/set-reminder | `DCA_DRY_RUN=0` + flag + `MODE=live` | reached the API (401 on the dummy token) |
| `alert` add/update/delete | any switch missing (7 combinations each) | **exit 3**, blocked, no network |
| `alert` add/update/delete | `ALERT_DRY_RUN=0` + flag + `MODE=live` | reached the API (401 on the dummy token) |
| `sharelist` create/delete/add/remove/sort | default | **exit 3**, blocked, no network |
| `sharelist` create/delete/add/remove/sort | `--confirm-live-sharelist` only | **exit 3**, blocked (dry run still on), no network |
| `sharelist` create/delete/add/remove/sort | `SHARELIST_DRY_RUN=0` only, no flag | **exit 3**, blocked (no flag), no network |
| `sharelist` create/delete/add/remove/sort | `SHARELIST_DRY_RUN=0 --confirm-live-sharelist` | **exit 3**, blocked (mode=simulated), no network |
| `sharelist` create | `SHARELIST_DRY_RUN=0 --confirm-live-sharelist MODE=live` | reached the API (401 on the dummy token) |
| `content` create-topic/reply | default | **exit 3**, blocked, no network |
| `content` create-topic/reply | `--confirm-live-content` only | **exit 3**, blocked (dry run still on), no network |
| `content` create-topic/reply | `CONTENT_DRY_RUN=0` only, no flag | **exit 3**, blocked (no flag), no network |
| `content` create-topic/reply | `CONTENT_DRY_RUN=0 --confirm-live-content` | **exit 3**, blocked (mode=simulated), no network |
| `content` create-topic | `CONTENT_DRY_RUN=0 --confirm-live-content MODE=live` | reached the API (401 on the dummy token) |

All 18 sharelist rows (5 actions × 4 non-sending switch combinations) and all
8 content rows (2 actions × 4) were checked and every one exited **3** with the
intended request printed and nothing sent. The no-network claim was confirmed
empirically rather than by inspection: with `LONGBRIDGE_HTTP_URL` pointed at
`http://127.0.0.1:1`, a blocked sharelist delete and a blocked content reply
both returned exit 3 in under 10 ms, while a *read* through the same dead
gateway failed with `dial tcp 127.0.0.1:1: connect: connection refused` — so
the override was demonstrably in effect and the refusals demonstrably never
touched it.

What is **not** verified: the shape of successful responses, field-by-field
rendering, column widths, and whether any live order is accepted. No command
here has ever run against a working access token. The output formatting was
written against the v0.25.2 type definitions in the module cache, so it should
be correct, but "should be" is not "was observed". Expect to adjust column
widths once real data flows.

This matters most for the newest sections, whose types are the least
documented: the A/H premium klines, the broker-holding change quadruples, the
rank-category JSON and the portfolio P&L credit/debit/fee lines are all
rendered from the struct definitions alone. `RankCategories` in particular
returns an opaque `json.RawMessage`, so its rendering is a pretty-printer and
not a field mapping at all.

`cmd/fundamentals` is the largest such case, and it is worth being blunt about
what "covered" means there. Every one of the 32 fundamental methods, both asset
methods and the calendar method was checked to be a plain `GET` against the
v0.25.2 source, so the read-only claim is a source fact rather than an
assumption. But the field names, the shape of each table and every column width
come from the Go struct definitions alone. **No successful fundamental response
has ever been observed.** Several of these payloads are barely documented even
upstream, and three of them (`FinancialReport`, `ShareholderTop`,
`ShareholderDetail`) are `json.RawMessage` in the SDK precisely because the
shape is not stable — so for those, the output is a pretty-printer, not a
mapping, and the true field set is unknown until someone runs it with a real
token. Expect to adjust this command more than any other in the repo.

`cmd/dca` and `cmd/alert` are in the same category, with two extra caveats.
First, their read/write split is a **source fact** — every method was read
against `dca/context.go` and `alert/context.go` in v0.25.2 to see which
endpoint it calls — but the *rendering* of a successful `DcaPlan`,
`DcaHistoryRecord` or `AlertSymbolGroup` is written from the Go struct
definitions alone, and **no successful DCA or alert response has ever been
observed**. The `DCAStatus` and `DCAFrequency` conversions in particular are
`switch` statements over wire strings; the SDK's own `statusFromString` maps
anything unrecognised to `Active`, so an unexpected server value would be
displayed as Active rather than as an error. Second, the SDK's DCA types are
loosely typed in places — `DayOfWeek` and `DayOfMonth` are plain strings on
`DcaPlan`, `AlterHours` is a string, and `PerInvestAmount` is a non-pointer
`decimal.Decimal` so "absent" is indistinguishable from zero — so a plan field
the API omits will print as `0` or `-` rather than "unknown". Expect to adjust
this formatting once real data flows. `AlertItem.ValueMap` is
`json.RawMessage` upstream, so the alert rendering is a pretty-printer by
necessity.

Nothing about the *guards* in either command is uncertain, and that is the part
that matters: 63 blocked cases and 9 unblocked ones were each executed, and
every one behaved exactly as specified.

A wrinkle that used to exist and is now **fixed**: a **blocked** write in
`cmd/trade` and `cmd/executions` used to exit **0**, because the gate printed
its refusal to stderr and then returned `nil` rather than an error. The refusal
was loud and correct and genuinely made no network call, but the exit code did
not distinguish "blocked" from "succeeded", so any script wrapping this repo
would have treated a refusal as a completed order. Guard refusals now return a
`*config.BlockedError` and `cli.Fail` maps it to **exit 3** in every guarded
command. See "Exit codes" under Configuration reference.

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
│   ├── market/main.go      status, calendar, timeline, + the market package
│   ├── warrant/main.go     HK warrant list, quotes, issuers
│   ├── watchlist/main.go   saved groups + separately gated edits
│   ├── executions/main.go  fills, order detail, buying power, cash flow
│   ├── reference/main.go   static, market-wide and entitlement data
│   ├── sharelist/main.go   share lists: list, detail, popular + gated create/delete/add/remove/sort
│   ├── content/main.go     topics, news, detail, replies, mine + gated create-topic/reply
│   ├── portfolio/main.go   exchange rates and P&L analytics
│   ├── dca/main.go         DCA plans: 5 reads + 6 gated plan changes
│   ├── alert/main.go       price alerts: 1 read + 3 gated changes
│   └── fundamentals/       the 32 fundamental methods, + asset & calendar
│       ├── main.go         flags, validation, action dispatch
│       └── actions.go      one renderer per SDK method
├── internal/
│   ├── config/             env + YAML loader, mode/dry-run switch, redaction
│   │   ├── config.go       credentials, LONGPORT_MODE, order gate
│   │   ├── guard.go        the separate watchlist gate; exit-3 refusal type
│   │   ├── writeguard.go   the reusable WriteGuard behind the dca/alert gates
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
