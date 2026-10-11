# Design

## Context

longbridge-go-demo is a CLI demo repo with 16 binaries, 17 test packages, and an existing CI pipeline (`ci.yml` — vet/build/test on ubuntu). The repo already has a Makefile `verify` target. The gaps versus tiger-go-demo are: no smoke test, no dependency notes, a gofmt drift, and stale README counts.

## Goals / Non-Goals

**Goals:**
- Fix gofmt alignment in `cmd/screener/main.go`
- Correct README test-count figures to match actual run
- Add a credentials-free smoke test proving SDK wiring
- Document transitive test-only dependency chain

**Non-Goals:**
- No multi-OS CI matrix (ubuntu-only is sufficient for a demo repo)
- No protobuf migration (already on `google.golang.org/protobuf` via SDK)
- No xerrors removal (transitive test-only, same as tiger-go-demo)

## Decisions

### 1. Document xerrors, do not remove it

`golang.org/x/xerrors` is a transitive test-only dependency. `go mod why` chain:
```
cmd/executions → openapi-go/trade → openapi-protobufs/gen/go/trade
  → github.com/golang/protobuf/proto → google.golang.org/protobuf/reflect/protoregistry
  → protoregistry.test → github.com/google/go-cmp/cmp/cmpopts → golang.org/x/xerrors
```
`go mod tidy` does not remove it. Documented in `docs/dependency-notes.md`.

### 2. Smoke test follows tiger-go-demo pattern

`test/smoke_test.go` uses the `chdirTemp` isolation pattern (clear `LONGPORT_*`/`LONGBRIDGE_*` env vars, point `$HOME` at temp dir), constructs config with dummy creds, calls `NewClient`, asserts accessors are non-nil. No live API call — the SDK requires credentials for every endpoint.

### 3. README counts updated from actual run

`go test -count=1 -v ./...` yields 2893 `=== RUN` lines and 550 `--- PASS:` lines across 17 packages. README previously said 551/2894 — off by one from a stale count.

## Risks / Trade-offs

- **Smoke test only proves wiring**: No live API call means we cannot verify the SDK still works against Longbridge. Accepted — the SDK is a third-party dependency; a live test would require real credentials and network access in CI.
- **gofmt fix is cosmetic**: One-line alignment change in `cmd/screener/main.go`. No behavioural impact.

## Migration Plan

No migration needed — all changes are additive or cosmetic.

## Open Questions

None.
