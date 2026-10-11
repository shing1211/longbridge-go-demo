# Production Hardening

## Why

longbridge-go-demo has minor formatting drift and stale README test counts, and lacks the same production-hardening artifacts already landed on tiger-go-demo (smoke test, dependency notes). Bringing both repos to parity reduces maintenance friction and documents transitive dependency risks.

## What Changes

- Fix `gofmt` alignment in `cmd/screener/main.go` (1 line)
- Update README test-count figures to match actual run (550 functions / 2893 cases)
- Add `test/smoke_test.go` — credentials-free wiring test
- Add `docs/dependency-notes.md` — document `golang/protobuf` and `golang.org/x/xerrors` as transitive test-only deps

## Capabilities

None — this is a pure tooling change with no spec-level behaviour changes. `skip_specs: true`.

## Impact

- Affected code: `cmd/screener/main.go` (formatting), `README.md` (counts)
- New files: `test/smoke_test.go`, `docs/dependency-notes.md`
- New dependencies: none
- Affected specs: none
- Affected workflows: none
