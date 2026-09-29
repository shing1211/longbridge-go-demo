# longbridge-go-demo

A small, complete, safety-gated demo of the **Longbridge (Longport) OpenAPI**
using the official Go SDK, [`github.com/longbridge/openapi-go`](https://pkg.go.dev/github.com/longbridge/openapi-go)
at **v0.25.2**.

Fifteen commands, one shared config loader, and a hard rule that no order can be
sent unless you say so three different ways.

**SDK coverage: every exported method on all twelve context types is exercised
somewhere in `cmd/` — 153 of 153.** See [Coverage](#sdk-coverage) for the one
command that verifies this, and for the caveat that it is a static check.

The counts, in full, are: 47 on `QuoteContext`, 32 on `FundamentalContext`, 19
on `TradeContext`, 12 on `MarketContext`, 11 on `DCAContext`, 8 on
`SharelistContext`, 7 on `ContentContext`, and 5 each on `ScreenerContext` and
`PortfolioContext`, plus 4 on `AlertContext`, 2 on `AssetContext` and 1 on
`CalendarContext`.

> **Status: not yet tested against the live API.** See
> [Honest status](#honest-status). Everything below was verified by building,
> vetting, running the test suite, and running against the real Longbridge
> endpoints with deliberately invalid credentials. No output in this README is
> copied from a live session.

---

## Contents

- [Quick start](#quick-start)
- [What you need to fill in](#what-you-need-to-fill-in)
- [Simulated vs live — read this](#simulated-vs-live--read-this)
- [Dry-run semantics and the safety gate](#dry-run-semantics-and-the-safety-gate)
- [The six safety gates](#the-six-safety-gates)
- [Commands](#commands)
- [The reusable write guard](#the-reusable-write-guard)
- [Configuration reference](#configuration-reference)
- [SDK coverage](#sdk-coverage)
- [Honest status](#honest-status)
- [Development](#development)
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
go run ./cmd/screener -action indicators    # the screener indicator catalogue
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

### The mode test is fail-closed

Condition 3 above is implemented as `if c.Mode != ModeLive { refuse }`, not as
`if c.Mode == ModeSimulated { refuse }`. The distinction matters:

| Written as | An empty, mistyped or invented `Mode` |
|---|---|
| `== ModeSimulated` → refuse | **passes** the check and the gate opens |
| `!= ModeLive` → refuse | refuses |

`config.Load` cannot currently produce a `Config` whose `Mode` is anything other
than `simulated` or `live` — it rejects any other value outright — so the old
permissive form was a **latent** hole rather than a live one. It was also
inconsistent with `WriteGuard.Unsatisfied` in the same package, which has always
used the deny-by-default shape. A latent hole in the one function that guards
money is still a hole, so the deny-by-default form wins. `TestGuardWrite_UnknownModeIsBlocked`
and `TestWriteGuard_UnsatisfiedAlreadyDeniesUnknownModes` pin it, and the
doc comment on `GuardWrite` says not to "simplify" it back.

Two smaller fail-closed rules follow the same principle. A `WriteGuard`'s
`DryRun()` treats an **unset** variable and an **unparseable** one identically —
both mean "still in dry run" — because guessing the other way is the dangerous
default. And `strconv.ParseBool` is used rather than a truthiness test, so only
an exact `0`/`false` (and `1`/`true`) is honoured; `LONGPORT_DRY_RUN=maybe` is
a startup error, not a silent default.

---

## The six safety gates

This demo guards six different kinds of mutation with six **separate**,
independently-defaulted gates. They are not interchangeable, and none of them
opens another.

| | Order gate | Watchlist gate | Sharelist gate | Content gate |
| --- | --- | --- | --- | --- |
| Guards | `SubmitOrder`, `ReplaceOrder`, `CancelOrder`, `WithdrawOrder` | `CreateWatchlistGroup`, `DeleteWatchlistGroup`, `UpdateWatchlistGroup`, `UpdatePinned` | `Create`, `Delete`, `AddSecurities`, `RemoveSecurities`, `SortSecurities` | `CreateTopic`, `CreateTopicReply` |
| Env switch | `LONGPORT_DRY_RUN` (default `1`) | `LONGPORT_WATCHLIST_DRY_RUN` (default `1`) | `LONGPORT_SHARELIST_DRY_RUN` (default `1`) | `LONGPORT_CONTENT_DRY_RUN` (default `1`) |
| Flag | `--confirm-live` | `--confirm` | `--confirm-live-sharelist` | `--confirm-live-content` |
| Also requires | `LONGPORT_MODE=live` | — | `LONGPORT_MODE=live` | `LONGPORT_MODE=live` |
| Implementation | `config.GuardWrite` | `config.GuardWatchlist` | `config.SharelistGuard` (a `config.WriteGuard`) | `config.ContentGuard` (a `config.WriteGuard`) |
| Blocked exit code | `3` (`config.ExitBlocked`) | `3` (`config.ExitBlocked`) | **`3`** | **`3`** |

Two further gates — for DCA plans and price alerts — use the same shared,
reusable `config.WriteGuard` helper as the sharelist and content gates above,
rather than a per-command copy of the loop:

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

Every binary's `-h` prints **all six** dry-run switches plus `LONGPORT_MODE` in
its `Safety:` block, so the set of switches is discoverable from any command
without reading this file:

```console
$ ./bin/screener -h 2>&1 | grep 'LONGPORT_.*_DRY_RUN'
  LONGPORT_WATCHLIST_DRY_RUN   1/true (default) blocks watchlist writes
  LONGPORT_DCA_DRY_RUN          1/true (default) blocks DCA plan writes.
  LONGPORT_ALERT_DRY_RUN        1/true (default) blocks price-alert writes.
  LONGPORT_SHARELIST_DRY_RUN   1/true (default) blocks sharelist writes.
  LONGPORT_CONTENT_DRY_RUN     1/true (default) blocks content publishes.
$ # five *_DRY_RUN lines here; LONGPORT_DRY_RUN itself is the sixth and sits above them
```

Note the block is written to **stderr**, not stdout — `flag` prints usage to
`fs.Output()`, which defaults to stderr. So `./bin/screener -h | grep …` finds
nothing and you want `./bin/screener -h 2>&1 | grep …`.

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
is appropriate in a way that it is not for a watchlist group. Both are declared
as `config.WriteGuard` values in `internal/config/writeguard.go` and are
listed in `declaredGuards()`, which `config.Load` validates at startup — so a
bad `LONGPORT_SHARELIST_DRY_RUN` or `LONGPORT_CONTENT_DRY_RUN` is now rejected
by **every** command in the repo, not only by the two that own those switches.

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

Both gates were refactored onto the shared `config.WriteGuard`, so their refusals
now come from one code path. Each one lists **every** unsatisfied condition
rather than the first, adds a `No change was sent, because <reason>.` line drawn
from the guard's own description, prints the request that would have been sent
on `[DRY-RUN]`-prefixed lines, and exits **3**. These transcripts are pasted from
real runs, not written by hand:

```console
$ go run ./cmd/sharelist -action delete -id 12345 --confirm-live-sharelist
[config] mode=simulated  (expected: credentials from a SIMULATED account) dry_run=true app_key=abcd******kl http=https://openapi.longbridge.com (SDK default) quote_ws=wss://openapi-quote.longbridge.com/v2 (SDK default) trade_ws=wss://openapi-trade.longbridge.com/v2 (SDK default)
[config] sharelist_dry_run=true (separate gate: LONGPORT_SHARELIST_DRY_RUN + --confirm-live-sharelist + LONGPORT_MODE=live)

[DRY-RUN] --- Delete (DELETE /v1/sharelists/{id}) request that would be sent ---
[DRY-RUN]   id           12345
[DRY-RUN]   WARNING        IRREVERSIBLE: there is no undelete endpoint. Deleting a sharelist removes it from your library and takes its constituents with it; recreating it will NOT restore the original contents or its ID.

[DRY-RUN] BLOCKED: refusing to delete sharelist 12345.
No change was sent, because a sharelist is account state other devices read, and Delete takes its constituents with it.

Unsatisfied condition(s):
  - LONGPORT_SHARELIST_DRY_RUN is on (default 1; set it to 0 to allow sharelist writes)
  - LONGPORT_MODE=simulated (sharelist writes require LONGPORT_MODE=live)

NOTHING was sent to Longbridge. To actually perform this write, set:
  LONGPORT_SHARELIST_DRY_RUN=0  +  --confirm-live-sharelist  +  LONGPORT_MODE=live
All of them are required; any one alone still blocks the write.
error: delete sharelist 12345 BLOCKED by the sharelist safety gate. Nothing was sent to Longbridge.
See the [DRY-RUN] output above for the request and the full list
of unsatisfied conditions. To perform it:
  LONGPORT_SHARELIST_DRY_RUN=0  +  --confirm-live-sharelist  +  LONGPORT_MODE=live
$ echo $?
3
```

Note the shape of the last three lines: `gate()` prints the detailed block and
returns a short `Blockedf(...)`, which `cli.Fail` turns into exit **3**. The
mechanism moved from a local `os.Exit(3)` to `return Blockedf(...)`, so the
refusal now travels up the same path as every other guard refusal in the repo.

The content refusal is the same shape, with the content guard's own wording:

```console
$ go run ./cmd/content -action create-topic -topic-type article \
    -title "700.HK earnings" -body "Markdown body" -tickers 700.HK --confirm-live-content
[config] mode=simulated  (expected: credentials from a SIMULATED account) dry_run=true app_key=abcd******kl http=https://openapi.longbridge.com (SDK default) quote_ws=wss://openapi-quote.longbridge.com/v2 (SDK default) trade_ws=wss://openapi-trade.longbridge.com/v2 (SDK default)
[config] content_dry_run=true (separate gate: LONGPORT_CONTENT_DRY_RUN + --confirm-live-content + LONGPORT_MODE=live)

[DRY-RUN] --- CreateTopic (POST /v1/content/topics) request that would be sent ---
[DRY-RUN]   body         Markdown body
[DRY-RUN]   title        700.HK earnings
[DRY-RUN]   topic_type   article
[DRY-RUN]   tickers      700.HK
[DRY-RUN]   WARNING        PUBLIC AND IRREVERSIBLE: this is published under your account and visible to other users. The SDK exposes no delete-topic method, so it cannot be retracted from this demo.

[DRY-RUN] BLOCKED: refusing to publish a new topic.
No change was sent, because a published topic or reply is public, attributed to your account, and cannot be retracted.

Unsatisfied condition(s):
  - LONGPORT_CONTENT_DRY_RUN is on (default 1; set it to 0 to allow content writes)
  - LONGPORT_MODE=simulated (content writes require LONGPORT_MODE=live)

NOTHING was sent to Longbridge. To actually perform this write, set:
  LONGPORT_CONTENT_DRY_RUN=0  +  --confirm-live-content  +  LONGPORT_MODE=live
All of them are required; any one alone still blocks the write.
error: publish a new topic BLOCKED by the content safety gate. Nothing was sent to Longbridge.
See the [DRY-RUN] output above for the request and the full list
of unsatisfied conditions. To perform it:
  LONGPORT_CONTENT_DRY_RUN=0  +  --confirm-live-content  +  LONGPORT_MODE=live
$ echo $?
3
```

Two things about the shared wording are worth stating so the text does not look
like a typo: the condition strings say **"content writes"**, not "publishing",
because they are generated from the guard's `Name` field and read
`set it to 0 to allow <Name> writes` / `<Name> writes require LONGPORT_MODE=live`
for every `WriteGuard`. And the trailer is `To actually perform this write,
set:` followed by the recipe and `All of them are required; any one alone still
blocks the write.` — the same three lines for all four `WriteGuard`-backed
gates, differing only in the variable and flag names.

With all three conditions satisfied the gate opens and the request goes out (here
rejected for the dummy token, which is the only observable proof available
without a real token):

```console
$ LONGPORT_SHARELIST_DRY_RUN=0 LONGPORT_MODE=live \
    go run ./cmd/sharelist -action create -name demo --confirm-live-sharelist
error: creating sharelist "demo": longbridge openapi error, httpStatus:401 code:401004 message:token invalid trace:...
```

Exit code **3** is used specifically for a refusal, matching the convention in
the sibling Tiger project. It is deliberately distinct from `0` (success), `1`
(a real failure) and `2` (missing credentials), so a script can distinguish "the
safety gate did its job" from "the command failed". It is the same status the
order and watchlist gates use, so all six gates agree.

`-show-state` is the one opt-in exception to "a refused write makes no
request": on a blocked `add`/`remove`/`sort` it fetches and prints the list's
current constituents so the intended change is reviewable. It is **off by
default** precisely so the no-network guarantee holds for a plain refused
write, and a failure to fetch it is reported but never changes the gate's
verdict.

Note that `executions -action withdraw` is an **order** write (`WithdrawOrder`
is an alias of `CancelOrder` in the SDK), so it uses the order gate and
`--confirm-live` — not the watchlist gate.

### The read-only invariant: eight binaries assert it

Separately from the six write gates, **eight** of the fifteen binaries assert
the **order** gate is still closed at startup. If the order gate is open they
refuse to run at all, rather than proceeding in a misconfigured environment:

| | |
| --- | --- |
| Binaries | `quote`, `watch`, `warrant`, `reference`, `portfolio`, `fundamentals`, `market`, `screener` |
| Check | `if cfg.GuardWrite("run the …") == nil { cli.Fail(…) }` |
| Exit | **1**, `internal invariant violated: <name> is read-only but the order gate is open` |

```console
$ LONGPORT_DRY_RUN=0 LONGPORT_MODE=live ./bin/market
[config] mode=live dry_run=false app_key=abcd******gh http=https://openapi.longbridge.com (SDK default) …
error: internal invariant violated: market is read-only but the order gate is open
```

`cmd/market` and `cmd/watch` joined the set in this pass. Both were verified
before being added rather than assumed:

- **`cmd/market` is read-only.** All eleven `MarketContext` methods are `GET`;
  the twelfth, `TopMovers`, is a `POST` that computes and mutates nothing
  (it is the same shape as `ScreenerContext.Search`, and the same argument
  applies). The two websocket-backed sections, `calendar` and `timeline`, are
  query commands on the quote context. There is no write method in the file.
- **`cmd/watch` has no write path at all.** Its complete SDK surface is
  `Subscribe`, `Unsubscribe`, `Subscriptions` and the `On*` push callbacks; it
  never touches the HTTP client. Its `-orders` mode holds a `TradeContext` for
  the push subscription only, and a subscription changes nothing server-side.

The other seven binaries that gate writes but do **not** carry the assertion are
`trade`, `executions`, `watchlist`, `sharelist`, `content`, `dca` and `alert` —
correctly, because for them an open order gate is a legitimate state.

**This assertion is not test-covered, in any of the eight.** It lives in
`main()`, and `main()` is not reachable from a test. Deleting any one of the
eight checks leaves the suite green; that was checked by mutation. Do not read
the test suite as evidence that this holds.

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
| `-symbol` | `""` | Required for `submit`; optional filter elsewhere. Padded values are trimmed; a whitespace-only one is an error, not an omission. |
| `-order-id` | `""` | Required for `replace` and `cancel`. Same trimming rule. |
| `-qty` | `0` | Required for `submit` and `replace`. |
| `-price` | `""` | Decimal string. Same trimming rule — and a blank one is refused rather than read as **zero**. |
| `-side` | `Buy` | `Buy` or `Sell`. |
| `-type` | `LO` | `LO ELO MO AO ALO ODD LIT MIT TSLPAMT TSLPPCT TSMAMT TSMPCT SLO`. |
| `-tif` | `Day` | `Day`, `GTC`, `GTD`. |
| `-remark` | — | Free text. |
| `-days` | `7` | Lookback for `history-orders`. |
| `-confirm-live` | `false` | Required acknowledgement for writes. |

**A blank flag is a flag error, not a gate refusal.** `-symbol "   "`,
`-order-id "   "` and `-price "   "` used to be trimmed to `""` and read as
"not supplied". For `-symbol` and `-order-id` that sent a blank value to the
gate, which refused it with a message about the three switches and named none of
the flags the user actually got wrong — so a `-symbol "   "` **exited 3**, the
one code in this repo that means "a gate protected you". For `-price` it was
worse: the blank became `decimal.Zero`, a **zero limit price**. All three are now
exit **1** and name the flag. Padded values (`-symbol " 700.HK "`) are still
trimmed and accepted, because a stray space is a typo, not a value. The six
enum parsers in this file (`-side`, `-type`, `-tif`, and the rest) have always
folded case and trimmed; this makes the string flags agree with them.

The `truncate` helper that clips a position's security name to 24 bytes is also
safe at the boundary: a width of 0 or less returns an ellipsis rather than
slicing at `-1`. The input was unreachable from the CLI, and the test that used
to document the panic has been inverted to assert the new behaviour. This
mirrors `cli.Truncate` in `internal/cli`, which guards the same boundary for the
other fourteen binaries.

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

**It also carries the read-only invariant assertion** described under
[the read-only invariant](#the-read-only-invariant-eight-binaries-assert-it):
if the order gate is open, the binary refuses to start. That claim was verified
rather than assumed. The complete SDK surface this file touches is `Subscribe`,
`Unsubscribe`, `Subscriptions` and the `On*` push callbacks — it never touches
the HTTP client at all, so there is no request it could make that would mutate
anything. The `-orders` path holds a `TradeContext` for exactly one reason, the
push subscription, and never calls `SubmitOrder`, `ReplaceOrder` or
`CancelOrder`.

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

**The `-broker-period` heading is mapped, not formatted.** `BrokerHoldingPeriod`
is a 0-based `iota` while the API parameter is `rct_1` / `rct_5` / `rct_20` /
`rct_60`, so the old heading — `fmt.Sprintf("rct_%d", int(period))` — printed
the **enum index**, not the window: `rct_0` for a one-day window, `rct_1` for
five-day, `rct_5` for twenty-day, and so on. It was wrong for every value. The
*request* was always correct throughout, because the SDK does its own
`BrokerHoldingPeriod.toAPIString` for the query parameter; only the label lied.
It is now a `switch` over the four constants with an `unknown(n)` default,
never a formula, and a test asserts all four:

```console
$ go run ./cmd/market -sections broker-holding -broker-period 5
=== Broker holding 700.HK (rct_5) ===
```

`broker-holding-detail` and `broker-holding-daily` take no period at all and
were never affected.

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
| `-sort-order` | `desc` | `asc` or `desc`. Surrounding whitespace is ignored, like the other five filter parsers in this file. |
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

All six parsers — `-type`, `-expiry`, `-moneyness`, `-status`, `-sort-by` and
`-sort-order` — now fold case **and** trim, so `-sort-order "  asc  "` is
accepted exactly as `-sort-order asc` is. `-sort-order` was the one that did
not, which meant the same value was valid or invalid depending on which parser
saw it. A test asserts each accepted value maps to the specific SDK constant,
and that a value belonging to a *different* parser in the same file is refused
rather than cast.

### `watchlist` — saved groups, with a gated editor

`-action list` is a pure read. The other four actions mutate your account's
saved groups and are behind the [watchlist gate](#the-six-safety-gates).

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
the platform's popular lists — and, behind the [sharelist gate](#the-six-safety-gates),
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
| `-symbols` | — | **Required** for `add`, `remove`, `sort`. Each entry must be exactly `CODE.MARKET`; see below. |
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

`-symbols` is validated locally, strictly, before any request. The SDK does no
validation of its own: `symbolToCounterID` splits the entry on the **last** dot
and builds `ST/<MARKET>/<CODE>` from whatever is left, so a malformed entry is
not caught here — it is sent, and the API then rejects the whole request with a
message naming neither the flag nor the entry. The check requires a non-empty
code, **exactly one** dot, a non-empty market, and no whitespace inside the
entry, and each error names the specific defect:

```console
$ go run ./cmd/sharelist -action add -id 12345 -symbols .HK --confirm-live-sharelist
error: symbol ".HK" has an empty code before the dot: the SDK would build a counter_id with no instrument code, which nothing can match; want CODE.MARKET, e.g. 700.HK or TSLA.US

$ go run ./cmd/sharelist -action add -id 12345 -symbols 700. --confirm-live-sharelist
error: symbol "700." has an empty market after the dot: the SDK would build a counter_id with no market, which the API cannot resolve; want CODE.MARKET, e.g. 700.HK or TSLA.US

$ go run ./cmd/sharelist -action add -id 12345 -symbols 700.HK.US --confirm-live-sharelist
error: symbol "700.HK.US" has 2 dots: the SDK splits on the last one to build the counter_id, so the market and the code are not the two halves you typed; want CODE.MARKET, e.g. 700.HK or TSLA.US

$ go run ./cmd/sharelist -action add -id 12345 -symbols "700 .HK" --confirm-live-sharelist
error: symbol "700 .HK" contains whitespace: the SDK keeps it inside the counter_id it builds, so the request can never match an instrument; want CODE.MARKET, e.g. 700.HK or TSLA.US with no spaces
```

**This check was `strings.Contains(s, ".")` until this pass, and that was
materially worse than it looks.** All four of the entries above passed it. The
brief summary of the trade-off, because the stricter rule is a real narrowing
and not a free improvement:

- **What is lost.** The SDK splits on the *last* dot, so a class share such as
  `BRK.B.US` — which becomes `ST/US/BRK.B` — and a bare index like `.DJI.US` —
  which becomes `ST/US/.DJI` — were converted **correctly** before and are now
  **refused**. Neither is reachable from the shapes this command is used for,
  and a symbol the SDK can convert but this command cannot is a worse
  experience than a clear refusal, so the refusal is the right call — but it is
  a narrowing, and it is pinned by a test so it stays a recorded decision rather
  than an accident.
- **What is gained.** `.HK`, `700.`, `700.HK.US` and `700 .HK` no longer
  produce a malformed `counter_id` and an opaque server error. Each one is now
  named locally, before the gate, before the request.
- **A correction to an earlier claim here.** `700.HK.US` does **not** become
  `ST/US/HK` — the SDK's `symbolToCounterID` splits on the last dot, so it
  becomes `ST/US/700.HK`, with the *first* half left intact inside the code.
  That is exactly why the entry is wrong rather than merely unusual, and why
  the local check has to reject it rather than pass it through.

### `content` — research and community content, with gated publishing

Discussion topics and news for a symbol, one topic's full record, the replies
on a topic, your own topics — and, behind the [content gate](#the-six-safety-gates),
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
| `-indicator` | `0` | `industry-rank`: `0`–`7`, one digit. Numeric on purpose — the SDK defines no names for these codes. Whitespace around it is ignored; `00`, `1.0` and `1e0` are refused. Checked at startup, not at the end of the run. |
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
misconfigured environment. See
[the read-only invariant](#the-read-only-invariant-eight-binaries-assert-it).

### Five validation fixes in this command

This is the largest `cmd/*` package, and it is also where the flag validation and
the request construction had drifted apart. All five of the following are now
fixed, and each is worth stating because two of them were silent-wrong rather
than loud-wrong.

**1. `-indicator` is range-checked at startup.** It used to be parsed inside
`printIndustryRank`, so a bad value was discovered at the *end* of a long
industry-rank run — after the contexts were built and the requests were made.
`validateFlags` now runs the same parser, so `-indicator 9` costs nothing:

```console
$ go run ./cmd/fundamentals -action industry-rank -indicator 9
error: unknown -indicator "9": want 0 through 7 (the SDK defines no names for these codes)
```

**2. `validateFlags` delegates to the command's own parsers.** It used to keep a
second, hand-written copy of each word list — and that copy lower-cased *without
trimming* while every parser trimmed. So `-sort-type " 1 "` was reported as
**unknown** at startup and would have been **valid** one layer down. The two
layers can no longer disagree, because there is only one. This is what a startup
error being *trustworthy* requires, and it is worth being explicit that the
divergence was a correctness bug, not just a tidiness one: `printStatements`
compared the **raw string** to `"monthly"`, so a padded
`-statement-type "  monthly  "` accepted at startup would have requested
**daily**. It now calls the same `parseStatementType`, so that is no longer
reachable.

**3. `marketFromSymbol` validates the market suffix.** `industry-rank` and
`industry-peers` take a *market*, not a security, and the suffix was taken as
given. The no-suffix fallback to HK is kept — it is the documented behaviour for
a bare symbol — but the suffix is now checked against the SDK's five markets, so
`700.XX` and `700.` are refused before a market-wide endpoint answers "no data"
and reads as an empty market:

```console
$ go run ./cmd/fundamentals -action industry-rank -symbol 700.XX
error: cannot read a market from -symbol "700.XX": "XX" is not a market code, want HK, US, CN, SG or UK (a symbol with no suffix means HK)
```

The macro country filter is deliberately **not** tightened the same way. A
country is a different vocabulary from a market code — it has `EU` and `JP` and
no `UK` — and it only narrows a list, so an unrecognised suffix such as the `SH`
of `000001.SH` yields no filter rather than refusing the action.

**4. `parseImportance` marks an unrecognised value.** The SDK's
`MacroeconomicImportance` is a bare `int32`, so an absent level decodes to `0`,
which the enum does not define. It used to print that `0` in the `IMPORT.`
column, where it reads as a real measurement — and it now returns
`unknown(n)`, exactly as `parseRecommend` and `parseElementType` already did for
their siblings. The cases name the SDK constants rather than `1`/`2`/`3`, so a
renumbering upstream shows up here instead of silently redefining what "high"
means.

**5. `fmtTimePtr` treats a zero time as absent.** A `*time.Time` field the API
omits decodes to the zero time rather than to `nil`, so a nil check alone printed
`0001-01-01 00:00:00` — a plausible-looking date. It is now `IsZero()`, and the
renderer prints `-`, which is what `cmd/content` and `cmd/sharelist` already did
in their `fmtTime` helpers.

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
| `-symbol` | `""` | **Required** for `create` and `calc-date`; optional filter for `stats`; comma-separated for `check-support`. Padded values are trimmed; a whitespace-only one is an error, not an omission. |
| `-amount` | `""` | Per-investment amount, decimal string. **Required** for `create`, optional on `update` (absent = leave the amount alone). Padded values are trimmed; a whitespace-only one is an error. |
| `-frequency` | `""` (effective default: `monthly`) | `daily`, `weekly`, `fortnightly` or `monthly`. See below — the **literal** flag default is the empty string and it must stay that way. |
| `-day-of-week` | `""` | Weekday name, e.g. `Monday`. **Required** for `weekly`/`fortnightly`, **rejected** for `monthly` and `daily`. Padded values are trimmed; a whitespace-only one is an error. |
| `-day-of-month` | `0` (unset) | `1`-`31`, for `monthly`. Rejected above 31 first, then rejected outright for `weekly`, `fortnightly` and `daily`. |
| `-allow-margin` | `false` | Margin financing for a plan — real leverage. Guarded like any other write. |
| `-plan-id` | `""` | **Required** for `update`, `pause`, `resume`, `stop`, `history`. Trimmed; whitespace-only is an error. |
| `-reminder-hours` | `""` | **Required** for `set-reminder`; the SDK accepts `1`, `6` or `12`. Trimmed; whitespace-only is an error. |
| `-confirm-live-dca` | `false` | **Required** for any write. |

#### `-frequency` has no default, and that is deliberate

The table above gives the **literal flag default** as `""`. The **effective**
default is still monthly, because the validation resolves `""` to monthly before
`parseFrequency` ever sees it. Those are two different things and the difference
is the whole point:

`doUpdate` builds a sparse `*dca.UpdateOptions` in which a nil pointer means
"leave this field alone" — the SDK's own contract, *"Nil/zero fields are not
sent"* (`dca/context.go`). The only way to express "leave the schedule alone" is
therefore `if frequency != ""`. That guard used to be **always true**, because
the flag layer handed it `"monthly"` when the user passed nothing at all. Every
`-action update` therefore sent `invest_frequency: monthly`, and the command
line

```bash
go run ./cmd/dca -action update -plan-id <id> -amount 2000 --confirm-live-dca
```

silently converted a **weekly** plan to a monthly one. The default is `""` now,
and `TestRegisterFlags_TheDefaultsLeaveAnUpdateableScheduleUnspecified` pins it
through the real flag set, including that exact command line. See
[Honest status](#honest-status) for why the old unit tests passed while
asserting the opposite.

#### The schedule is cross-validated for every frequency

The 1–31 range check on `-day-of-month` runs first. Then each frequency accepts
**exactly its own** day field, and nothing else:

| `-frequency` | `-day-of-week` | `-day-of-month` |
| --- | --- | --- |
| `daily` | rejected | rejected |
| `weekly` | **required** | rejected |
| `fortnightly` | **required** | rejected |
| `monthly` (and `""`) | rejected | optional — the server picks the day |

The error names the flag to drop, and all of this happens before any request:

```console
$ go run ./cmd/dca -action create -symbol 700.HK -amount 1000 \
    -frequency weekly -day-of-week Monday -day-of-month 15
error: -day-of-week and -day-of-month are mutually exclusive: -frequency weekly takes -day-of-week, so drop -day-of-month

$ go run ./cmd/dca -action create -symbol 700.HK -amount 1000 \
    -frequency monthly -day-of-week Monday
error: -day-of-week is not used with -frequency monthly, which uses -day-of-month; pass -frequency weekly or fortnightly as well if the plan is weekly

$ go run ./cmd/dca -action create -symbol 700.HK -amount 1000 \
    -frequency daily -day-of-week Monday
error: -day-of-week is not used with -frequency daily, which runs every trading day; drop -day-of-week

$ go run ./cmd/dca -action create -symbol 700.HK -amount 1000 \
    -frequency monthly -day-of-week Monday -day-of-month 15
error: -day-of-week and -day-of-month are mutually exclusive: -frequency monthly takes -day-of-month, so drop -day-of-week

$ go run ./cmd/dca -action create -symbol 700.HK -amount 1000 \
    -frequency daily -day-of-month 15
error: -day-of-month is not used with -frequency daily, which runs every trading day; drop -day-of-month
```

This used to be half true. The mutual-exclusion check existed **only** for
`monthly`, so `weekly` with both flags was accepted, and `daily` was not
cross-validated at all. The API *ignores* the day field that does not match the
frequency rather than rejecting it, so the result of that gap was not an error
but a plan silently running on a schedule the command line never described.

**`update` runs the identical validation.** It used to bypass it completely, so
`-action update -day-of-month 99` reached the request. One consequence is worth
knowing before you use it:

> An update that moves only the weekday must also pass `-frequency weekly`.
> The command cannot read the plan's real frequency without a `List` call, and
> reading before the gate would break the promise this repo keeps everywhere
> else — that a refused write makes **no** network call at all. Naming the
> frequency explicitly keeps the request self-consistent instead of asking the
> API to choose between two contradictory day fields.

```console
$ go run ./cmd/dca -action update -plan-id 1 -amount 2000
[config] mode=simulated … dry_run=true …
[config] dca_dry_run=true (dedicated gate: LONGPORT_DCA_DRY_RUN + --confirm-live-dca + LONGPORT_MODE=live)

[DRY-RUN] request that would be sent:
[DRY-RUN]   allow_margin       -
[DRY-RUN]   day_of_month       -
[DRY-RUN]   day_of_week        -
[DRY-RUN]   endpoint           POST /v1/dailycoins/update
[DRY-RUN]   invest_frequency   -          <-- the schedule is untouched
[DRY-RUN]   per_invest_amount  2000
[DRY-RUN]   plan_id            1
… BLOCKED by the DCA safety gate. Nothing was sent to Longbridge.
$ # exit 3

$ go run ./cmd/dca -action update -plan-id 1 -day-of-week Monday --confirm-live-dca
error: -day-of-week is not used with -frequency monthly, which uses -day-of-month; pass -frequency weekly or fortnightly as well if the plan is weekly

$ go run ./cmd/dca -action update -plan-id 1 -frequency weekly -day-of-week Tuesday --confirm-live-dca
[DRY-RUN]   day_of_week        Tuesday
[DRY-RUN]   invest_frequency   Weekly
… # accepted, and the request now goes out
```

The first block is the whole bug in one screen. `invest_frequency -` means the
sparse `UpdateOptions` left the frequency `nil`, which is what the SDK
documents as "do not send". Before the fix, that same command line printed
`invest_frequency Monthly` and converted the plan.

#### A blank flag is a flag error, not a gate refusal

Every string flag in `cmd/dca` distinguishes three inputs that a bare `s == ""`
test runs together: **absent** (leave the flag out), **padded** (`" 6 "`, a typo
— trimmed and accepted), and **blank** (`"   "`, neither — an error naming the
flag). The third case used to be read as an omission, which meant a blank
`-plan-id` reached the gate and was refused with a message about the three
safety switches. That is the more misleading of the two outcomes in a command
whose exit 3 means "a gate protected you": the user is told the demo declined
rather than that they left an argument empty. It is now exit **1** and it names
the flag. `cmd/trade` does the same for `-symbol`, `-order-id` and `-price`
(`-price "   "` would otherwise become a **zero** limit price).

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
| `-frequency` | `once` | `daily`, `every-time` or `once`. Padded values are trimmed; a whitespace-only one is an error rather than the default. |
| `-id` | — | **Required** for `update` and `delete`. |
| `-enabled` | `true` | For `update`: enable or disable the alert. |
| `-confirm-live-alert` | `false` | **Required** for any write. |

`ValueMap` is `json.RawMessage` in the SDK — the upstream shape is not stable —
so the rendering in `-action list` and the dry-run preview is a pretty-printer,
not a field mapping. `Delete` is irreversible: there is no undelete endpoint,
and recreating the alert yields a new id and loses its trigger state, which the
dry-run `WARNING` line says out loud.

Two small things about the parsers, both of which look like trivia and are not:

- **A whitespace-only `-frequency` is an error, not the default.** It used to
  trim to `""` and fall through to the documented default `once`, which made a
  flag the user got wrong look exactly like one they never passed. Padding a
  real word (`" Once "`) is still normalised — that is a typo, not a value.
- **Both of `cmd/alert`'s enums are safe to return `0` on error, and that is
  checked rather than assumed.** `AlertCondition` and `AlertFrequency` both
  start at **1** in the SDK, so the zero value is not a member of either and
  cannot be a wrong-but-valid trigger. That is not the same property the
  0-based enums in `cmd/dca` and `cmd/fundamentals` have, and it would stop
  being true the moment the SDK renumbered, so a test pins the numbering. The
  error paths in *this* command still return `0`; it does not need a sentinel,
  and the tests say which is which.

`findAlert` also no longer panics on a nil list — it returns not-found, exactly
as it does for an empty one, and `doUpdate` stops before it can send anything.
The input was unreachable from the CLI; the test that used to document the panic
has been inverted.

---

### `screener` — read-only stock screener

All **5** `ScreenerContext` methods, one `-action` per method. Four are `GET`s
and one is a `POST` that queries:

| Action | SDK method | Endpoint | Requests |
| --- | --- | --- | --- |
| `indicators` | `ScreenerIndicators` | `GET /v1/quote/ai/screener/indicators` | 1 |
| `recommend` | `ScreenerRecommendStrategies` | `GET /v1/quote/ai/screener/strategies/recommend` | 1 |
| `mine` | `ScreenerUserStrategies` | `GET /v1/quote/ai/screener/strategies/mine` | 1 |
| `strategy` | `ScreenerStrategy` | `GET /v1/quote/ai/screener/strategy/{id}` | 1 |
| `search` | `ScreenerSearch` | `POST /v1/quote/ai/screener/search` | **1 or 2** — see below |

**There is no write gate here, and that is the correct answer rather than an
oversight.** The `POST` is a query: it takes a market, a filter list and a page
and returns matching securities. It is the same shape as `TopMovers` in
`cmd/market`, which is likewise a `POST` and likewise unguarded. Nothing
server-side changes. As in `cmd/market`, `cmd/fundamentals` and `cmd/warrant`,
the startup banner asserts the **order** gate is still closed, so running the
screener with dry run off and mode live refuses to start rather than
proceeding in a misconfigured environment. Eight binaries carry that assertion
in total; see
[the read-only invariant](#the-read-only-invariant-eight-binaries-assert-it).

```bash
go run ./cmd/screener -action indicators                    # the indicator catalogue
go run ./cmd/screener -action recommend -market HK          # strategies Longbridge suggests
go run ./cmd/screener -action mine -market US               # your own saved strategies
go run ./cmd/screener -action strategy -strategy-id 12345   # one strategy's filters
go run ./cmd/screener -action search -market HK -condition 'pettm::30' -page 0
go run ./cmd/screener -action search -market US -condition 'pettm:0:1' -show capmk,roe
go run ./cmd/screener -action search -strategy-id 12345     # mode A: two requests
```

| Flag | Default | Purpose |
| --- | --- | --- |
| `-action` | `indicators` | `indicators`, `recommend`, `mine`, `strategy` or `search`. |
| `-market` | `HK` | `HK US CN SG UK`. Used by `recommend`, `mine` and `search`; **ignored by `search` when `-strategy-id` is set**. |
| `-strategy-id` | — | **Required for `strategy`**; optional for `search`. `0` is rejected, not treated as unset. |
| `-condition` | — | `search` only: comma-separated `KEY:MIN:MAX[:k=v;k=v]`. Either bound may be empty. |
| `-show` | — | `search` only: extra return columns, e.g. `capmk,roe`. |
| `-page` | `0` | `search` only. **0-indexed.** |
| `-size` | `20` | `search` only. |
| `-timeout` | `15s` | Per-request timeout. |

Three behaviours here look like bugs and are not. All three were read out of
`screener/context.go` in v0.25.2. Each is confirmed to the extent it can be:
all five actions were run with a deliberately invalid token and each returns a
real 401, which proves the dispatch and the request construction but says
nothing about a *successful* response — so the three behaviours below are
**source facts, not observations**.

**1. `-action search -strategy-id N` issues TWO HTTP requests.** The SDK does
not take a strategy and filters it client-side. With a non-nil `strategyID` it
first `GET`s `/v1/quote/ai/screener/strategy/{id}`, reads that strategy's own
`market` and its `filter.filters[]`, and only then `POST`s the search with those
filters. So in this mode:

- `-condition` is **ignored** — the strategy's own filters are used.
- `-market` is **ignored** — the market comes from the strategy, and the SDK
  substitutes `"US"` if the strategy's market is blank or `"-"`.
- The command prints which mode it is in, and why, before making the call, so the
  request count is never a surprise.

**2. `-page` is 0-indexed.** `page 0` is the first page. Every other command in
this repo is 1-based, which makes the screener the odd one out; it is a property
of the screener API, not a choice here. The flag help, the `-h` notes and the
runtime banner all say so, and the banner converts to a 1-based ordinal for
display only (`-page 0` prints "page 0 means the 1st page") so the number you
typed is never quietly rewritten.

**3. `-strategy-id 0` is rejected rather than silently switching modes.** The
SDK signals "no strategy" with a `nil *int64`. Mapping an explicit `0` to `nil`
would quietly switch the user from mode A to mode B and return a *different
result set* from the one they asked for, so the command refuses it and says
what to do instead: omit the flag to search with `-condition`, or pass a real id
from `-action recommend`. The same applies to a negative id. Detecting this
needs `fs.Visit`, because a plain `int64` default of `0` cannot tell an omitted
flag from an explicit `0`.

### Every response is `json.RawMessage`, and the renderer is a pretty-printer

`screener/types.go` defines **no response structs at all**. Each of the five
responses is a wrapper whose only field is `Data json.RawMessage`, because the
payload shape varies by indicator and strategy. There is therefore nothing to
map field-by-field, and this command does not invent one. Instead it:

- unmarshals into `any` and re-marshals with `encoding/json`, which emits object
  keys in **sorted order** — so the same payload always prints identically and
  two runs can be diffed;
- clips each block at **4000 bytes**, dropping a partial trailing rune so the
  marker is never preceded by a broken character, and appending
  `…(truncated, N bytes total)`;
- turns off HTML escaping, because indicator labels and company names can
  legitimately contain `<`, `>` and `&` and escaping them buys nothing in a
  terminal;
- falls back to printing the body verbatim if it is not decodable JSON, rather
  than failing the run.

**This output was written from the SDK's struct definitions alone and has never
been observed from a live API.** All five actions were run with a deliberately
invalid token and each returns a real 401, so the wiring is confirmed — but a
401 says nothing about the shape of a successful payload, which is the one thing
this renderer has to get right. Run `-action indicators` first to discover the
real field names before assuming anything about a search result. The SDK also
strips the `filter_` prefix from every key, both in the filters it sends and in
the `indicators[].key` it returns, so the keys you see are the same short names
you put in `-condition`.

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
auto-detected as `config.yaml` or `config.local.yaml`, in that order, and
`-config PATH` overrides the search. An explicit `-config` path that does not
exist is an error, so a typo cannot silently fall back to env.

Copy `config.example.yaml` to `config.yaml`. The top-level key **must** be
`longbridge:` — the SDK errors with `Longbridge config is not exist in yaml
file` otherwise.

### Config files: YAML only, and why

Only `config.yaml` and `config.local.yaml` are auto-detected, and an explicit
`-config` with any other extension is a hard error (exit 1):

```console
$ go run ./cmd/quote -config ./config.yml
error: config file "./config.yml": this demo reads YAML only, so the file name must end in .yaml (got ".yml"). The Longbridge SDK can also parse TOML and .env, but that reader is not wired up here. Convert the file (see config.example.yaml), rename it to config.yaml, or supply LONGBRIDGE_APP_KEY, LONGBRIDGE_APP_SECRET and LONGBRIDGE_ACCESS_TOKEN in the environment
$ echo $?
1
```

This is a fix, and the two bugs it removes were both real:

- **`config.yml` broke the loader outright.** The SDK infers a config file's
  format from its extension and its `configTypeMap` knows `.env`, `.yaml` and
  `.toml` — but **not** `.yml`. Auto-detecting `config.yml` therefore made the
  SDK fail with `config type:.yml not support` **even when the environment had
  already supplied all three credentials**, because the file was still handed to
  the SDK via `WithFilePath`.
- **`config.toml` supplied no credential at all.** This loader's own
  credential top-up was YAML-only, and the missing-credential check runs *before*
  the SDK is ever handed the file — so a `config.toml` user's credentials were
  unreachable even though the SDK would have parsed them fine a moment later.

The SDK can still parse TOML and `.env`; this demo simply does not wire that
reader up, and offering a filename it cannot honour is worse than refusing the
filename. `TestCandidateFilesAreYamlOnly`, `TestLoad_TomlIsNotACandidate`,
`TestLoad_YmlIsNotACandidate` and
`TestLoad_ExplicitNonYamlPathIsAPlainError` pin the behaviour, including that
the extension is checked **before** the file is stat'ed, so a wrong format is
reported as a wrong format even when the file does not exist.

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
| `LONGPORT_WATCHLIST_DRY_RUN` | `1` | Blocks watchlist writes. Must be `0` to write. See [the six gates](#the-six-safety-gates). |
| `LONGPORT_SHARELIST_DRY_RUN` | `1` | Blocks sharelist writes. Must be `0` **and** `--confirm-live-sharelist` **and** `LONGPORT_MODE=live`. |
| `LONGPORT_CONTENT_DRY_RUN` | `1` | Blocks publishing topics/replies. Must be `0` **and** `--confirm-live-content` **and** `LONGPORT_MODE=live`. |
| `LONGPORT_DCA_DRY_RUN` | `1` | Blocks DCA plan writes. Must be `0` **and** `--confirm-live-dca` **and** `LONGPORT_MODE=live`. |
| `LONGPORT_ALERT_DRY_RUN` | `1` | Blocks price-alert writes. Must be `0` **and** `--confirm-live-alert` **and** `LONGPORT_MODE=live`. |

### Exit codes

| Code | Meaning |
| --- | --- |
| `0` | Success. The command did what it was asked. |
| `1` | A real failure: the API rejected the call, a flag value was unusable, a flag was unknown, or a config file could not be used. |
| `2` | **Missing credentials, and nothing else.** |
| `3` | **BLOCKED.** A safety guard refused a write. Nothing was sent. |

Exit `2` is reached by exactly one condition: a `*config.MissingCredentialError`.
An earlier version of this table said exit 2 also covered "a usage error", which
contradicted the row above it and the code. A bad flag — unknown, or a value
the command cannot use — is a `flag` error, is **not** a
`MissingCredentialError`, and therefore exits **1**:

```console
$ go run ./cmd/quote -period nonsense >/dev/null 2>&1; echo $?
1
$ env -u LONGBRIDGE_APP_KEY -u LONGBRIDGE_APP_SECRET -u LONGBRIDGE_ACCESS_TOKEN \
    go run ./cmd/quote >/dev/null 2>&1; echo $?
2
```

Exit `2` exists so a script can rely on it meaning "go and fix your
environment", and nothing else. The distinction is made once, in `cli.Fail`,
and `TestFail_ExitCodeContract` pins the mapping — including by re-running the
test binary as a subprocess and checking the real exit status, not just
asserting on a mock.

Exit `3` is reserved, and is the one to check when scripting. It is defined
once as `config.ExitBlocked` and reached by every guard refusal, which returns
a `*config.BlockedError`; `cli.Fail` maps that to `os.Exit(3)`. A guard
refusal used to return `nil` and therefore exit `0`, which made a refusal
indistinguishable from a completed order — do not reintroduce that.

Every guarded command uses it: `trade` (submit/replace/cancel),
`executions -action withdraw`, `watchlist` (create/update/pin/delete), `dca`
(create/update/pause/resume/stop/set-reminder), `alert` (add/update/delete),
`sharelist` (create/delete/add/remove/sort) and `content`
(create-topic/reply).

Two things keep landing on the wrong code, so both are now stated as rules:

- **A bad flag value is 1, not 3.** A flag that is present but blank —
  `-symbol "   "` — used to be trimmed to `""` and read as "not supplied", which
  for `cmd/dca` and `cmd/trade` meant it reached a gate and was refused there.
  A reader seeing exit 3 reasonably concludes *"a safety gate protected me"*,
  when in fact they left an argument empty. Blank string flags are now named
  errors and exit **1**, before any gate is consulted. Padded values are still
  trimmed and accepted.
- **A read-only binary with the order gate open is 1, not 3.** Exit 3 is
  reserved for a *guard refusing a write*; a reader that refuses to start is not
  that. See
  [the read-only invariant](#the-read-only-invariant-eight-binaries-assert-it).

### Secret handling

`app_secret` and `access_token` are never printed or logged, not even
redacted — they are pure credentials with no diagnostic value. The startup
banner redacts the App Key only, keeping at most 4 leading characters:

```console
[config] mode=simulated  (expected: credentials from a SIMULATED account) dry_run=true app_key=dumm******78 http=https://openapi.longbridge.com (SDK default) ...
```

`.gitignore` excludes `.env`, `.env.*`, `config.yaml`, `config.local.yaml`,
`config.yml` and `config.toml`, while explicitly keeping `.env.example` and
`config.example.yaml`. The last two are ignored even though they are no longer
candidates, so a stale `config.yml` or `config.toml` left in the tree cannot sit
there looking supported — and if you pass one explicitly you now get a clear
error rather than a confusing SDK one.

---

## SDK coverage

**All 153 exported context methods across all twelve context types are covered:
153 of 153.** `cmd/screener` closed the last gap, `ScreenerContext`.

You can check that claim yourself without trusting this README. **One command
covers every context**, and it is deliberately receiver-agnostic — note the
`[a-z]+` in the pattern, not `[cd]`. The receiver letter is not uniform across
the SDK: `MarketContext`'s methods are declared on `m *MarketContext` while
every other context uses `c *XContext` (and DCA uses `d`). A pattern written as
`\(c \*` silently misses all 12 `MarketContext` methods, which is why the older
version of this section was quietly incomplete.

```console
$ BASE=$(go env GOMODCACHE)/github.com/longbridge/openapi-go@v0.25.2
$ grep -rhoE '^func \([a-z]+ \*[A-Za-z]+Context\) [A-Z][A-Za-z0-9]*' \
    "$BASE" --include=*.go \
  | sed -E 's/^func \([a-z]+ \*([A-Za-z]+)Context\) ([A-Za-z0-9]*)/\1 \2/' | sort -u \
  | tee /tmp/ctx.txt | wc -l
153
$ while read -r ctx m; do grep -rqE "\.$m\(" cmd/ || echo "UNCOVERED: $ctx $m"; done < /tmp/ctx.txt
$ # (no output = fully covered)
$ cut -d' ' -f1 /tmp/ctx.txt | uniq -c
      4 Alert
      2 Asset
      1 Calendar
      7 Content
     11 DCA
     32 Fundamental
     12 Market
      5 Portfolio
     47 Quote
      5 Screener
      8 Sharelist
     19 Trade
```

For reference, here are the per-context greps the older version of this section
used, corrected to match receiver letters. They are subsumed by the loop above
but are kept because they read more clearly when you only care about one area.

```console
# Quote + Trade: 47 + 19 methods, both on receiver c
$ grep -hoE '^func \(c \*(Quote|Trade)Context\) [A-Z][A-Za-z0-9]*' \
    "$BASE"/quote/*.go "$BASE"/trade/*.go | sed -E 's/.*\) //' | sort -u \
  | while read -r m; do grep -rqE "\.$m\(" cmd/ || echo "UNCOVERED: $m"; done
$ # (no output = fully covered)

# Fundamental + Asset + Calendar: 32 + 2 + 1 methods, all in cmd/fundamentals
$ grep -hoE '^func \(c \*(Fundamental|Asset|Calendar)Context\) [A-Z][A-Za-z0-9]*' \
    "$BASE"/fundamental/*.go "$BASE"/asset/*.go "$BASE"/calendar/*.go \
  | sed -E 's/.*\) //' | sort -u \
  | while read -r m; do grep -rqE "\.$m\(" cmd/ || echo "UNCOVERED: $m"; done
$ # (no output = fully covered)

# Market: 12 methods, receiver m — this is the one a \(c \* pattern misses
$ grep -hoE '^func \(m \*MarketContext\) [A-Z][A-Za-z0-9]*' "$BASE"/market/*.go \
  | sed -E 's/.*\) //' | sort -u \
  | while read -r m; do grep -rqE "\.$m\(" cmd/ || echo "UNCOVERED: $m"; done
$ # (no output = fully covered)

# DCA (receiver d), Alert, Sharelist, Content, Screener, Portfolio: 11 + 4 + 8 + 7 + 5 + 5
$ grep -rhoE '^func \([a-z]+ \*(DCA|Alert|Sharelist|Content|Screener|Portfolio)Context\) [A-Z][A-Za-z0-9]*' \
    "$BASE"/dca/*.go "$BASE"/alert/*.go "$BASE"/sharelist/*.go \
    "$BASE"/content/*.go "$BASE"/screener/*.go "$BASE"/portfolio/*.go \
  | sed -E 's/.*\) //' | sort -u \
  | while read -r m; do grep -rqE "\.$m\(" cmd/ || echo "UNCOVERED: $m"; done
$ # (no output = fully covered)
```

This is a **static** check: it proves every method is *referenced*, not that
every method was *exercised against a live account*. No command in this repo
has been run with a working access token, so no successful response has ever
been observed. See [Honest status](#honest-status).

The per-context picture, and where each group lives:

| Area | Context type | Methods | Covered | Notes |
| --- | --- | --- | --- | --- |
| Market data | `QuoteContext` | 47 | 47 | `cmd/quote`, `cmd/watch`, `cmd/market` (2 sections), `cmd/executions`, `cmd/reference`, `cmd/warrant`, `cmd/watchlist -action list`. |
| Fundamentals | `FundamentalContext` | 32 | 32 | `cmd/fundamentals`, one `-action` per method. |
| Orders & account | `TradeContext` | 19 | 19 | `cmd/trade`, plus `cmd/executions` and `cmd/watch -orders`. |
| Market-wide | `MarketContext` | 12 | 12 | `cmd/market`, one `-sections` value per method. |
| DCA | `DCAContext` | 11 | 11 | `cmd/dca`; 6 writes behind the DCA gate. |
| Share lists | `SharelistContext` | 8 | 8 | `cmd/sharelist`; 5 writes behind the sharelist gate. |
| Research content | `ContentContext` | 7 | 7 | `cmd/content`; 2 writes behind the content gate. |
| Screeners | `ScreenerContext` | 5 | 5 | `cmd/screener`, one `-action` per method. All read-only. |
| Portfolios | `PortfolioContext` | 5 | 5 | `cmd/portfolio`, all read-only. |
| Price alerts | `AlertContext` | 4 | 4 | `cmd/alert`; `List` is a read, 3 writes behind the alert gate. |
| Assets | `AssetContext` | 2 | 2 | `cmd/fundamentals`. |
| Calendar | `CalendarContext` | 1 | 1 | `cmd/fundamentals`. `cmd/market` also has `TradingDays` from `QuoteContext`. |
| **Total** | **12 context types** | **153** | **153** | |

There is no longer any omission to explain, so the rule that used to govern
them — "this demo only calls a mutating method when that method sits behind its
own complete gate" — is now a description of *every* write in the repo rather
than a justification for a gap. There are **24** guarded write methods across
the six gates (4 order, 4 watchlist, 5 sharelist, 2 content, 6 DCA, 3 alert) and
**0** uncovered methods. The last omission, `ScreenerContext`, was never a
safety decision: all five screener methods are reads, and one of them
(`Search`) is a `POST` that queries and mutates nothing. It is simply the newest
addition.

The earlier version of this section said `AlertContext` was uncovered because
"all writes … would need its own third guard", and that `ScreenerContext` was
uncovered. Both were wrong. `AlertContext` has a gate — `config.AlertGuard`, a
`config.WriteGuard`, used by `cmd/alert` — and `AlertContext.List` is a plain
`GET /v1/notify/reminders` read, not a write at all. `cmd/screener` now covers
all five screener methods.

---

## Honest status

**This project has never been run against a working Longbridge account.** It
was not built with an access token available, and no order has been placed. The
account the user has is a *simulated* one, but no access token for it was
supplied, so nothing in this repo has ever been validated against a live API.

What *was* verified by execution:

- `gofmt -l .` reports nothing.
- `go build ./...` and `go vet ./...` both exit 0.
- The test suite passes: **435** test functions across **15** packages,
  **2 477** passing cases including subtests, one helper-process test skipped.
  See [Development](#development).
- The suite is green under `-race`, `-count=2` and `-shuffle=on`, under a
  deliberately hostile `env -i` environment, with poisoned
  `LONGBRIDGE_*`/`LONGPORT_*` values set, and with a stray credentialed
  `config.yaml` dropped in both the repository root and `cmd/dca/`.
- Every new test assertion was **mutation-checked**: the production code was
  reverted with the tests kept, and 16 of the 18 mutations tried turn the suite
  red with the specific test that was written for them. The two that do not are
  the new read-only invariant assertions, which live in `main()` and are
  therefore invisible to the suite; that gap is named rather than glossed, under
  [Development](#development).
- All **fifteen** binaries build; `-h` exits 0 with no credentials.
- All fifteen exit **2** with a readable missing-credentials message listing all
  three variables, and no panic.
- A bad flag value exits **1**, not 2: `go run ./cmd/quote -period nonsense`
  → 1, while unsetting the three credentials → 2. Verified for both.
- With dummy credentials, every command reaches the real Longbridge API and
  fails with `httpStatus:401 code:401004 message:token invalid` — proving the
  config, signing and network path are genuinely wired, not stubbed. This was
  checked for all three `warrant` actions, `watchlist -action list`, all seven
  read-only `executions` actions, all **fifteen** `reference` sections, all
  fourteen `market` sections, all three read-only `sharelist` actions, all five
  read-only `content` actions, all five `portfolio` actions, all **thirty-six**
  `fundamentals` actions, all five read-only `dca` actions,
  `alert -action list`, and **all five `screener` actions** — each returns the
  same real 401.
- Every write path is blocked in each of the non-sending configurations, and
  every refusal was confirmed to make **no** network call. The 28 sharelist
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
- An unparseable `LONGPORT_DRY_RUN`, `LONGPORT_WATCHLIST_DRY_RUN`,
  `LONGPORT_DCA_DRY_RUN`, `LONGPORT_ALERT_DRY_RUN`,
  `LONGPORT_SHARELIST_DRY_RUN` or `LONGPORT_CONTENT_DRY_RUN` is rejected at
  startup, not silently treated as "on". The last two are now checked in
  `config.Load` via `declaredGuards()`, so **every** command rejects them, not
  only `cmd/sharelist` and `cmd/content`.
- `-config ./config.yml` and `-config ./config.toml` both fail with the
  explicit "this demo reads YAML only" error and exit **1**, even when the
  environment has already supplied all three credentials.
- The static coverage check above finds no uncovered method — and, unlike the
  previous version of this claim, it now actually examines **all twelve**
  context types. The old snippets used `\(c \*` and so silently skipped all 12
  `MarketContext` methods; the check is receiver-agnostic now.

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

All 20 sharelist rows (5 actions × 4 non-sending switch combinations) and all
8 content rows (2 actions × 4) were checked and every one exited **3** with the
intended request printed and nothing sent. The no-network claim was confirmed
empirically rather than by inspection: with `LONGBRIDGE_HTTP_URL` pointed at
`http://127.0.0.1:1`, a blocked sharelist delete and a blocked content reply
both returned exit 3 in under 10 ms, while a *read* through the same dead
gateway failed with `dial tcp 127.0.0.1:1: connect: connection refused` — so
the override was demonstrably in effect and the refusals demonstrably never
touched it.

`cmd/screener` is the newest command, so it is worth being precise about exactly
how far it has been taken. All five actions were run against the real API with a
deliberately invalid token and each returns the same real 401 as every other
command — so the wiring, the flag validation, the mode banner and the request
construction are confirmed. What is **not** confirmed is anything about a
successful response. **Every one of the five responses is `json.RawMessage`** in
the SDK (`screener/types.go` defines no response structs at all), so the
renderer is a deterministic-key-order pretty-printer with a 4 KB clip, and its
output was **written from the SDK's struct definitions alone and never observed
from a live API**. The three behaviours documented under
[`screener`](#screener--read-only-stock-screener) — the two-request search, the
0-indexed page, the rejected `-strategy-id 0` — were read out of
`screener/context.go`, not observed. In particular the two-request mode A could
not be observed end to end, because the first `GET` is rejected by the dummy
token before the second `POST` is ever issued; the claim rests on the SDK source.

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

A second wrinkle, also now **fixed**: the order gate's mode test was
`Mode == ModeSimulated`, which let an unknown or empty mode through. It is now
`Mode != ModeLive`, fail-closed. See
[the mode test is fail-closed](#the-mode-test-is-fail-closed).

And a third: `cmd/sharelist` and `cmd/content` used to call `os.Exit(3)` from
inside their own gate helpers, duplicating the refusal machinery. Both are now
`config.WriteGuard` values, and their gates `return Blockedf(...)` so the
refusal reaches `cli.Fail` by the same path as every other gate. The exit code
is unchanged at 3; the *mechanism* is now shared, which is what makes
`declaredGuards()` able to validate all four `WriteGuard` switches in
`config.Load` at startup.

And a fourth, and the one worth reading twice because it is a lesson about
testing rather than about DCA. `cmd/dca -action update` registered its
`-frequency` flag with the default `"monthly"`, while `doUpdate` encodes "leave
the plan's schedule alone" as `if frequency != ""`. With a non-empty default
that guard was **always true**, so the example command in this very README —

```bash
go run ./cmd/dca -action update -plan-id <id> -amount 2000 --confirm-live-dca
```

— sent `invest_frequency: monthly` on every single update and silently
**converted a weekly plan to a monthly one**. The unit tests did not catch it,
and could not have: they set the package-level `frequency` variable directly, so
they never went through the flag layer, and they were in fact asserting the
*opposite* of what the shipped binary did. This is the shape of bug a test suite
is structurally blind to — the code under test was not the code that ran.

The default is now `""`, which still means monthly inside the validation, and
the `dca` flag table under [Commands](#commands) distinguishes the two. The
suite was then mutation-checked — production code reverted, new tests kept, each
assertion confirmed to fail — which is what makes this a recorded lesson rather
than a story. Every other fix from this pass is documented in its own command's
section, and the coverage caveats are under [Development](#development).

An earlier version of this README said there was no automated test suite. That
is no longer true, and it was never true of the parts that matter most. See
[Development](#development).

---

## Development

There is a test suite, and it now covers **15 of the 17 packages in the repo** —
`internal/config`, `internal/cli`, and thirteen `cmd/*` binaries. Only
`cmd/portfolio` and `cmd/watch` have no test file.

```bash
go test ./...                                        # everything
go test -race ./...                                  # the same, with the race detector
go test -cover ./...                                 # coverage per package
go test -shuffle=on ./...                            # order independence
go test -count=2 ./...                               # no state leaks between runs
go test -run 'Parser|Enum|Sentinel' ./... -v         # the enum-conversion suites
```

There is **no `make test` target** in this repo's `Makefile` — use `go test`
directly, or `make all` for `fmt + vet + build`. Check with `make help`; the
target list is `build`, `fmt`, `fmt-check`, `vet`, `tidy`, `clean`, `help` and
fourteen `run-*`. (The sibling `tiger-go-demo` does have `make test`; the two
Makefiles are not kept in step.)

### The numbers

| Package | Test functions | Cases executed | Statement coverage |
| --- | --- | --- | --- |
| `internal/config` | 81 | 466 | **100.0%** |
| `cmd/fundamentals` | 62 | 414 | 19.8% |
| `internal/cli` | 39 | 121 (120 pass + 1 skip) | **98.6%** |
| `cmd/warrant` | 36 | 254 | 55.6% |
| `cmd/dca` | 35 | 174 | 48.8% |
| `cmd/market` | 27 | 150 | 21.1% |
| `cmd/sharelist` | 27 | 120 | 36.1% |
| `cmd/alert` | 25 | 121 | 44.2% |
| `cmd/trade` | 23 | 129 | 35.7% |
| `cmd/screener` | 20 | 135 | 41.4% |
| `cmd/content` | 16 | 92 | 31.0% |
| `cmd/executions` | 16 | 115 | 17.2% |
| `cmd/reference` | 16 | 100 | 8.6% |
| `cmd/quote` | 8 | 60 | 11.9% |
| `cmd/watchlist` | 4 | 27 | 3.2% |
| **Total** | **435** | **2 478** (2 477 pass + 1 skip) | — |

Sorted by test functions. `cmd/portfolio` and `cmd/watch` have no test file and
so appear in no row; `go test -cover ./...` reports them at 0.0%.

"Test functions" counts `func TestXxx` declarations; "cases executed" counts
every test case the runner actually entered, subtests included. The one skip is
`TestHelperProcess` in `internal/cli`, a subprocess helper that is only
meaningful when re-executed by its parent. To reproduce the totals:

```console
$ go test -count=1 -v ./... 2>&1 | grep -cE '^\s*--- PASS'
2477
$ go test -count=1 -v ./... 2>&1 | grep -cE '^\s*--- SKIP'
1
$ grep -rhE '^func Test' --include='*_test.go' . | wc -l
435
$ go test -cover ./... | wc -l
17
$ make build && ls bin | wc -l
15
```

`internal/config` is at **100.0% of statements**. `internal/cli` is at **98.6%**,
and the single uncovered statement is `internal/cli/cli.go:141` — the `return
cfg` on `Usage.Load`'s **credentialed success path**. It is unreachable from a
test that has no real credentials, because the only way past the `Load` error
branch is a successful `appcfg.Load`. The failure path on the line above
(`Fail(err)`) *is* covered.

### The `cmd/*` coverage numbers mean less than they look

**Read the per-package percentages in that table as "how much of the flag and
parse layer is tested", not as "how much of the binary is tested".** The range
is wide and the low end is low: 3.2% for `cmd/watchlist`, 8.6% for
`cmd/reference`, 11.9% for `cmd/quote`, 17.2% for `cmd/executions`, 19.8% for
`cmd/fundamentals`. The 55.6% at the top of the range is the most-covered
package, not a typical one.

The reason is structural, and it is not going to change. The `print*` functions
take a **live SDK context** — `printBrokerHolding(ctx, mc *market.MarketContext)`,
`printStatements(ctx, ac *asset.AssetContext)`, and so on, one per SDK method.
The SDK offers **no seam** to fake one: it is a concrete struct over a concrete
HTTP client and websocket, there is no interface to substitute, and there is no
exported constructor that takes a transport. A test that wants to exercise
`printBrokerHolding` therefore has to hold a real `*market.MarketContext`, which
means credentials and a network round trip. **143 functions in this repo are at
0.0% statement coverage**: all 15 `main()` functions, and 86 of the 110 `print*`
functions — the rest of that list is `do*` and `execute` bodies that sit behind
the same wall.

So the `cmd/*` suites deliberately aim at the part that is testable and that
bites: **the conversion of the SDK's bare-`int` enums, and the flag validation
in front of them.** This README's own rule is that an unrecognised value must be
an error "rather than a silent zero", because a wrong cast "would quietly return
the wrong data". Every one of those `switch` statements previously had **zero**
tests. The new suites assert, for each parser:

- the **exact SDK constant** for every accepted value, not merely "no error" —
  so a parser that returns the right *type* and the wrong *member* fails;
- that unrecognised values are **rejected**, including the near-misses that a
  `strings.TrimSpace(strings.ToLower(...))` implementation gets wrong: wrong
  case, whitespace padding, and a value that is perfectly valid for a
  *different* parser in the same file (`-sort-order asc` fed to
  `parseSortBy`, `weekly` fed to `cmd/alert`'s `parseFrequency`, `monthly` fed
  to `parseCondition`, and so on);
- that the **value returned alongside an error is not itself a valid member of
  the enum.** This is the part that is easy to get wrong. Several of these
  parsers used to `return 0, err`, and for a `int` enum whose first member is
  `iota` — `dca.DCAFrequencyDaily`, `calendar.CalendarCategoryReport`,
  `fundamental.FinancialReportKindIncomeStatement` — the zero value *is* a valid
  member and, in two cases, the documented default besides. A caller that
  ignored the error would then have created a daily DCA plan or listed earnings
  reports, silently, under a heading built from the word the user actually typed.
  The error path now returns a sentinel outside the valid range
  (`dcaFrequencyInvalid = -1`, `calendarCategoryInvalid = -1`,
  `financialReportKindInvalid = -1`), and a test asserts the sentinel really is
  outside the enum **and** pins the SDK's own numbering, so an SDK that ever
  added a negative member would fail there rather than quietly making the
  sentinel valid.
- `cmd/alert` is the exception that proves the rule: both its enums are
  **1-based** in the SDK, so `0` cannot be a member and its error returns are
  already safe. That property is not left as a comment — it is pinned, because
  it is the thing that would quietly stop being true if the SDK ever
  renumbered.

### Hermeticity

The suite runs with **no credentials and no network**, and that is enforced
rather than assumed:

- **Green under `-race`, `-count=2` and `-shuffle=on`.** All three were run
  over the whole module, not just `internal/config`.
- **No `t.Parallel()` anywhere that touches process state.** There is no
  `t.Parallel()` call in the repo at all. It could not be used: `applyEnvOverrides`
  mutates the process environment with `os.Setenv`, and `t.Setenv` and `t.Chdir`
  forbid a parallel test outright.
- **Immune to a polluted shell.** The suite was run with
  `LONGBRIDGE_APP_KEY=leak LONGBRIDGE_APP_SECRET=leak LONGBRIDGE_ACCESS_TOKEN=leak
  LONGPORT_MODE=bogus LONGPORT_DRY_RUN=maybe LONGPORT_DCA_DRY_RUN=maybe …` in
  the environment and still passed. `sandbox(t)` unsets everything, and
  `restoreEnv` puts it back, including the variables `applyEnvOverrides` writes
  directly with `os.Setenv` and which would therefore outlive `t.Setenv`.
- **Immune to a hostile environment.** The suite also passes under `env -i`
  with a `HOME` that does not exist, no `LONGPORT_*` variable set at all, and
  every `LONGBRIDGE_*` credential present but **empty** (which is a different
  case from unset, and the one a real misconfiguration produces).
- **Immune to a stray `config.yaml`.** A real `config.yaml` with credentials was
  dropped in the repository root, and again in `cmd/dca/`, and the suite still
  passed, because `sandbox(t)` changes into a `t.TempDir()` working directory
  first. That helper clears all **36** `LONGBRIDGE_*` / `LONGPORT_*` variables
  the package or the SDK reads — the credentials in both spellings, the seven
  demo safety switches, the endpoints, and the protocol tuning knobs.

### What the tests pin, in rough order of value

- **Every SDK enum parser maps each accepted value to the right constant**, and
  refuses everything else, as described above. This is the bulk of the new
  `cmd/*` suites.
- **The exit-code mapping** in `cli.Fail` — 0/1/2/3, and specifically that a
  `flag` parse error is **1** while only a `MissingCredentialError` is **2**.
  The mapping is executed for real, by re-running the test binary as a
  subprocess and checking the actual exit status.
- **Every gate's refusal is fail-closed**: unset and unparseable
  `*_DRY_RUN` both mean "dry run", a missing flag refuses even with the env
  cleared and vice versa, an unknown `Mode` refuses, and `GuardWrite` with an
  empty action is a plain error rather than a refusal.
- **The four `WriteGuard` values are asserted field by field** in
  `TestDeclaredGuards`, plus set-level invariants (no two gates may share a
  switch, and `declaredGuards()` and the test must list the same number of
  gates). So editing `DCAGuard.RequireLive`, or adding a fifth gate in one place
  and not the other, fails the suite immediately. Go offers no constant structs
  and therefore no compile-time freeze, and this is the closest available
  substitute.
- **The DCA schedule cross-validation**, in full: each frequency accepts
  exactly its own day field, and the error names the flag to drop.
- **A blank flag is a flag error, not a gate refusal.** `-symbol "   "` in
  `cmd/dca` and `cmd/trade` must exit **1** with the flag named, never **3**
  with a message about the three switches.
- **The credential precedence rules**: environment beats file, canonical
  `LONGBRIDGE_*` beats deprecated `LONGPORT_*`, and the error message lists
  *every* missing variable rather than the first.
- **The YAML-only config rule**: the candidate list is exactly
  `config.yaml`, `config.local.yaml`; a `.yml` or `.toml` explicit path is
  rejected, and the extension is checked *before* the file is stat'ed.
- **Formatting helpers** in `cli` and in the commands: `Dec`/`Dec4` render `nil`
  as `-` and zero as a real figure, `Truncate` never emits a broken rune or
  slices at a negative index, `Redact` does not leak the length of a long
  secret, and `fmtTimePtr`/`fmtTime` render a zero `time.Time` as `-` rather
  than as `0001-01-01`.
- **Previously-panicking inputs are safe**: `findAlert` on a nil list returns
  not-found, and `truncate` with a non-positive width returns an ellipsis.
  Both inputs were unreachable from the CLI; the tests that used to assert the
  panic have been inverted to assert the new behaviour, so the contract is now
  stated rather than implied.

### What the tests do *not* cover

Stated plainly, because the percentages above invite the wrong conclusion:

- **None of the 8 read-only invariant assertions is test-covered.** All eight
  binaries (`quote`, `watch`, `warrant`, `reference`, `portfolio`,
  `fundamentals`, `market`, `screener`) refuse to start when the order gate is
  open, and no test can see it: the check is in `main()`, and `main()` is
  untestable for the same reason the `print*` functions are. This was checked by
  mutation — deleting each of the eight assertions in turn leaves the suite
  **green**. That is a real gap, and it is the one most likely to be mistaken
  for coverage.
- **No `print*` function, and no `main()` in any `cmd/*` package.** All 15
  `main()` functions and 86 of the 110 `print*` functions are at 0.0%.
- **Nothing about a successful response.** The suite never opens a socket, so
  the "written from the SDK's struct definitions alone" caveat under
  [Honest status](#honest-status) is unchanged by any of this.

### The suite has been mutation-checked

A test that cannot fail proves nothing, so every new assertion was re-checked by
**reverting the production code and keeping the tests**: apply the old
behaviour, confirm the suite goes red, put the fix back.

Sixteen of the eighteen mutations tried go red exactly as predicted, naming the
test that was written for them. The headline case is the `-frequency` default:

```console
$ # put u.FS.StringVar(&frequency, "frequency", "monthly", …) back, keep the tests
$ go test -count=1 ./cmd/dca/
--- FAIL: TestRegisterFlags_TheDefaultsLeaveAnUpdateableScheduleUnspecified
    --- FAIL: …/no_flags_at_all
    --- FAIL: …/the_README's_amount-only_update
    --- FAIL: …/a_create_that_says_nothing_about_the_schedule
FAIL	github.com/shing1211/longbridge-go-demo/cmd/dca
```

The other fifteen that turn red: dropping the `weekly` day-of-month rejection;
dropping the `daily` branch; reading a blank optional flag as an omission;
restoring the zero-value sentinel; restoring `strings.Contains(s, ".")` in
`cmd/sharelist`; restoring `rct_%d` in the broker heading; duplicating the
vocabulary in `validateFlags` again; dropping the startup `-indicator` check;
unvalidating `marketFromSymbol`; comparing the raw `-statement-type` string;
returning the bare number from `parseImportance`; dropping `IsZero` from
`fmtTimePtr`; dropping the `-sort-order` trim; making `findAlert` panic on a nil
list again; and dropping the `truncate` boundary guard. Each of those failures
came from a distinct named test, not from a generic compile error.

**The two that do not turn red are the two new read-only assertions.** Deleting
the `GuardWrite` check from `cmd/market` — or from `cmd/watch`, or from any of
the other six that have one — leaves the suite **green**. That is not a
defect in the mutation check; it is the mutation check reporting a real gap, and
it is listed under
[what the tests do not cover](#what-the-tests-do-not-cover) rather than
smoothed over here.

The reason this matters is in [Honest status](#honest-status): the `-frequency`
default is precisely the class of bug a unit-test suite is **structurally blind
to**, and the mutation check is the only thing in this repo that could see it.

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
top-level `longbridge:` key, or the file extension is not one the SDK's
`configTypeMap` knows (`.env`, `.yaml`, `.toml` — note **not** `.yml`). In
practice you should not be able to hit this through `-config`, because a
non-`.yaml` explicit path is rejected earlier with a clearer message (see
below); you would have to reach the SDK's own parser some other way.

**`config file "./x.yml": this demo reads YAML only …`** — you passed a `.yml`
or `.toml` path to `-config`. That is a hard error, exit **1**, and it is
deliberate: the SDK cannot parse `.yml` at all, and this loader's own
credential top-up is YAML-only, so advertising a filename it cannot honour
would either break startup or silently supply no credential. Convert the file
(see `config.example.yaml`), rename it to `.yaml`, or set the three
`LONGBRIDGE_*` variables in the environment. See
[Config files: YAML only](#config-files-yaml-only-and-why).

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
│   ├── screener/main.go    the 5 screener methods, all read-only
│   └── fundamentals/       the 32 fundamental methods, + asset & calendar
│       ├── main.go         flags, validation, action dispatch
│       └── actions.go      one renderer per SDK method
├── internal/
│   ├── config/             env + YAML loader, mode/dry-run switch, redaction
│   │   ├── config.go       credentials, LONGPORT_MODE, order gate
│   │   ├── guard.go        the separate watchlist gate; exit-3 refusal type
│   │   ├── writeguard.go   the reusable WriteGuard behind the dca/alert/sharelist/content gates
│   │   ├── file.go         YAML-only credential file loader
│   │   └── *_test.go       81 test functions, 466 cases, 100.0% statement coverage
│   └── cli/                shared flag parsing, credential errors, panic guard
│       ├── cli.go
│       ├── cli_test.go     28 test functions
│       └── fail_test.go    11 test functions, incl. real subprocess exit codes
├── .env.example            every LONGPORT_*/LONGBRIDGE_* var, fully commented
├── config.example.yaml     YAML template, `longbridge:` block
├── Makefile                build, fmt, fmt-check, vet, tidy, clean  (no `test` target)
├── go.mod / go.sum
└── README.md
```

`cmd/*/main_test.go` (and `cmd/fundamentals/actions_test.go`) hold the 315
test functions and 1 891 cases for the thirteen binaries that have a suite; see
[Development](#development) for the per-package table and for why the coverage
percentages there are lower than they look. `cmd/portfolio` and `cmd/watch` have
no test file.

Fifteen `cmd/` directories, fifteen `binaries` in the `Makefile`'s `BINARIES`
list, and fifteen `run-*` targets.

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
