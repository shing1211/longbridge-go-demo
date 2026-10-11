# Dependency Notes

## golang.org/x/xerrors — transitive test-only dependency

`golang.org/x/xerrors` is an unavoidable transitive test-only dependency. It is
not imported by any production code in this repository.

### Dependency chain

```
cmd/executions
  → github.com/longbridge/openapi-go/trade
    → github.com/longbridge/openapi-protobufs/gen/go/trade
      → github.com/golang/protobuf/proto
        → google.golang.org/protobuf/reflect/protoregistry
          → protoregistry.test (test-only)
            → github.com/google/go-cmp/cmp/cmpopts
              → golang.org/x/xerrors
```

Verified with:

```console
$ go mod why -m golang.org/x/xerrors
# golang.org/x/xerrors
github.com/shing1211/longbridge-go-demo/cmd/executions
github.com/longbridge/openapi-go/trade
github.com/longbridge/openapi-protobufs/gen/go/trade
github.com/golang/protobuf/proto
google.golang.org/protobuf/reflect/protoregistry
google.golang.org/protobuf/reflect/protoregistry.test
github.com/google/go-cmp/cmp/cmpopts
golang.org/x/xerrors
```

### Why it cannot be removed

`go mod tidy` does not remove it because the chain runs through a *test*
dependency of `google.golang.org/protobuf` (`protoregistry.test`), and Go's
module graph includes test dependencies of direct and indirect requirements.

Removing it would require an upstream change in `longbridge/openapi-go` or
`longbridge/openapi-protobufs` — neither of which we control.

### Risk assessment

**Low.** The dependency is test-only: no production binary imports it. It
appears in `go.sum` but is never compiled into any `cmd/` binary. The only
effect is a slightly larger module download during `go build` and a pinned
version in `go.mod` that cannot be upgraded without an upstream SDK release.

### Resolution path

When `longbridge/openapi-go` or `longbridge/openapi-protobufs` releases a
version that drops the `golang/protobuf` transitive dependency (or upgrades to
a `google.golang.org/protobuf` release whose test chain no longer reaches
`xerrors`), run `go mod tidy` and this dependency will disappear automatically.

---

## github.com/golang/protobuf — transitive dependency

`github.com/golang/protobuf` v1.5.2 is a transitive dependency of the Longbridge
SDK. It is the legacy protobuf API that wraps `google.golang.org/protobuf`.

### Dependency chain

```
cmd/executions
  → github.com/longbridge/openapi-go/trade
    → github.com/longbridge/openapi-protobufs/gen/go/trade
      → github.com/golang/protobuf/proto
```

### Why it cannot be removed

Same as above: `longbridge/openapi-protobufs` imports it directly. We do not
control that module.

### Risk assessment

**Low.** `golang/protobuf` v1.5.2 is a thin wrapper around
`google.golang.org/protobuf` v1.28.1 (which is also present). The wrapper
delegates all real work to the modern API. No known CVEs affect this version.

### Resolution path

Same as above: upgrade `longbridge/openapi-go` when the upstream SDK drops
the legacy protobuf dependency.
