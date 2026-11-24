# Architecture & Extending clustercheck

## Data flow

```
kubeconfig ──▶ internal/kube        minimal API client (net/http + TLS)
                    │
                    ▼
           internal/audit           checks → []Result{Status, Lines, Score}
                    │
        ┌───────────┴───────────┐
        ▼                       ▼
   internal/report         internal/score
   terminal | markdown     weighted aggregation → Readiness Score
```

Design rules:

- **Single static binary** (`CGO_ENABLED=0`, no runtime dependencies) —
  if it boots a Linux kernel, it runs. Verified on amd64 desktops and
  arm64 Raspberry Pi nodes.
- **GET-only API access.** The audit never mutates cluster state; a
  cluster-admin-ish _read_ kubeconfig is sufficient.
- **Checks are pure functions** of API responses — no hidden state between
  checks, easy to reason about, trivial to add new ones.

## Package layout

| Package            | Responsibility                                  |
| ------------------ | ----------------------------------------------- |
| `cmd/clustercheck` | entrypoint, flag parsing, orchestration         |
| `internal/kube`    | typed wrappers around the Kubernetes REST API   |
| `internal/audit`   | every check implements `Check`, registry-driven |
| `internal/report`  | terminal (ANSI) and Markdown renderers          |
| `internal/score`   | weighted scoring, readiness grade               |

## Adding a check

Implement the `Check` interface and append your check to the registry.
Renderers and the scoring engine pick it up automatically — no renderer
changes, no scoring changes needed.

```go
type Check struct {
    Name   string   // human-readable, shown in the report
    Weight int      // contribution to the readiness score
    Run    func(ctx context.Context, k kube.Client) ([]Result, error)
}
```

Minimal example:

```go
func init() {
    audit.Register(audit.Check{
        Name:   "Unused ConfigMaps",
        Weight: 1,
        Run: func(ctx context.Context, k kube.Client) ([]Result, error) {
            cms, err := k.ListConfigMaps(ctx)
            if err != nil {
                return nil, err
            }
            // ... build []Result{Status, Lines, Score}
            return results, nil
        },
    })
}
```

Guidelines for new checks:

- **Fail loudly, don't guess.** If an API call fails, return the error —
  the renderer surfaces it; a check that silently returns "OK" on error
  is worse than one that crashes.
- **Separate observation from verdict.** `Result.Lines` carry raw facts;
  `Result.Status` carries the pass/warn/fail call. Keep them decoupled so
  you can change thresholds without losing the underlying data.
- **Weights are promises.** Once a check contributes to the readiness
  score, removing or reweighting it changes every user's baseline —
  document the change in the release notes.

## Building

```bash
make build            # bin/clustercheck, static, UPX-compressed
make build COMPRESS=0 # skip UPX (debugging symbols, faster startup)
make clean            # remove bin/
```

Version stamping comes from `git describe --tags --always --dirty`
injected via `-ldflags "-X main.version=..."` at build time.

## Release process

Releases are cut by pushing an annotated tag (`v*`); GitHub Actions
builds `linux/amd64` and `linux/arm64`, each shipped as a plain stripped
binary plus a UPX-compressed `-upx` variant (~60% smaller, marginally
slower startup).
