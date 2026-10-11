# Tasks

## 1. gofmt fix

- [ ] 1.1 Fix `cmd/screener/main.go` alignment (`Total int` field)

## 2. README test counts

- [ ] 2.1 Run actual counts (`go test -count=1 -v ./... | grep -cE '^=== RUN'` etc.)
- [ ] 2.2 Update README.md figures to match

## 3. Smoke test

- [ ] 3.1 Create `test/smoke_test.go` with `chdirTemp` isolation pattern
- [ ] 3.2 Verify `go vet ./...` and `go test ./test/` pass

## 4. Dependency notes

- [ ] 4.1 Create `docs/dependency-notes.md` documenting xerrors transitive chain
- [ ] 4.2 Verify `go mod why` chain matches documented claims

## 5. Verify and PR

- [ ] 5.1 `go vet ./...` clean
- [ ] 5.2 `gofmt -l .` returns nothing
- [ ] 5.3 `go build ./...` passes
- [ ] 5.4 `go test ./...` passes
- [ ] 5.5 Branch, commit, push, open PR for Mary review
- [ ] 5.6 Close issue #14 after merge
