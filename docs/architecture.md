# Architecture & Extending clustercheck

## Data flow

```
kubeconfig ──▶ internal/kube        minimal API client (net/http + TLS)
                    │
                    ▼
           internal/audit           checks → []Result{Status, Lines, Scores}
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
- **Checks produce data, renderers produce output.** Terminal colors and
  the Markdown report are rendered from the same `Result` structures —
  no formatting logic lives in checks.

## Package layout

| Package            | Responsibility                                                                     |
| ------------------ | ---------------------------------------------------------------------------------- |
| `cmd/clustercheck` | entrypoint, flags (`-kubeconfig`, `-md`, `-noscore`), orchestration                |
| `internal/kube`    | minimal hand-rolled REST client + kubeconfig loader (~300 lines, zero `client-go`) |
| `internal/audit`   | `Check` interface, `Result`/`Line`/`ScoreImpact` types, all twelve checks          |
| `internal/report`  | terminal (ANSI, auto-disabled when piped) and Markdown renderers                   |
| `internal/score`   | weighted aggregation of `ScoreImpact` entries, readiness grade                     |

## The core types

```go
// audit.Check — one audit function.
type Check interface {
    Name() string
    Run(ctx context.Context, c *kube.Client) Result
}

// audit.Result — structured outcome of one check.
type Result struct {
    Title  string
    Status Status          // OK | Warn | Fail | Info
    Lines  []Line          // key/value detail rows, each with its own Status
    Err    error
    Scored bool
    Scores []ScoreImpact   // one entry per scored criterion
}

// audit.ScoreImpact — pass/fail is decided HERE, by the check, which owns
// the context. The score package only sums and renders.
type ScoreImpact struct {
    CheckName string
    Passed    bool
    Detail    string
    Weight    int // unset/0 is treated as 1
}
```

Two consequences worth internalizing:

1. **A check decides its own pass/fail** inside `ScoreImpact`. The score
   package is deliberately dumb — it cannot override a check's verdict
   because it lacks the context (e.g. "this restart burst is a boot
   artifact" lives in the crashloop check, not in the scorer).
2. **Every line has its own status.** A single check result can mix
   OK/INFO/WARN lines (e.g. Storage Status: classes OK, a Released PV
   WARN) — which is what keeps the terminal report informative instead
   of collapsing everything into one verdict emoji.

## Checks and their scored criteria

| Check                    | Scored criterion             | Weight |
| ------------------------ | ---------------------------- | ------ |
| Cluster Info             | All Nodes Ready              | 3      |
| Control Plane Health     | API Server Ready (`/readyz`) | 3      |
| Pod Stability (Restarts) | No Active Crashloops         | 3      |
| Problem Pods             | No Problem Pods              | 2      |
| Certificates & TLS       | Certificates Valid           | 2      |
| DaemonSet Completeness   | DaemonSet Completeness       | 2      |
| Backup Tooling           | Backup Tooling (Velero)      | 2      |
| Storage Status           | PV Hygiene                   | 1      |
| Network Policies         | NetworkPolicy Coverage       | 1      |
| Pod Security Admission   | PSA Enforcement              | 1      |
| Pod Disruption Budgets   | Disruption Budgets           | 1      |
| Resource Quotas          | Resource Quotas              | 1      |

Rationale for the weighting: fundamental health (nodes, control plane,
active crashloops) weighs 3, client-facing correctness (problem pods,
certs, DaemonSets, backups) weighs 2, hardening/hygiene items weigh 1.
The weights are printed in every output so the score reads as an
explained opinion, not a magic number.

**Checks are wired explicitly, not via a registry.** `main.go` lists all
twelve checks in an ordered slice — what runs is visible in one place,
execution order is deterministic, and there is no init()-time magic to
trace. Adding a check means adding a constructor call to that slice.

## Adding a check

1. Create `internal/audit/<yourcheck>.go`:

```go
type UnusedConfigMaps struct{}

func NewUnusedConfigMaps() UnusedConfigMaps { return UnusedConfigMaps{} }

// Name implements Check.
func (UnusedConfigMaps) Name() string { return "Unused ConfigMaps" }

// Run implements Check.
func (UnusedConfigMaps) Run(ctx context.Context, c *kube.Client) audit.Result {
    res := audit.Result{Title: "Unused ConfigMaps"}
    // fetch data, fill res.Lines, set res.Status,
    // optionally append audit.ScoreImpact{...}
    return res
}
```

2. Register it in `cmd/clustercheck/main.go`:

```go
checks := []audit.Check{
    // ...existing checks...
    audit.NewUnusedConfigMaps(),
}
```

That is the whole integration — terminal and markdown renderers and the
readiness score consume the new `Result` automatically.

### Guidelines for new checks

- **Fail loudly, don't guess.** A fetch error becomes `res.Err = err`
  (the renderer shows it as a failed check) or a WARN line — never a
  silent OK. Silence is not a pass.
- **Separate observation from verdict.** `Result.Lines` carry the raw
  facts; `Result.Status` and `ScoreImpact.Passed` carry the judgment.
  Keep them decoupled so thresholds can change without losing data.
- **Degradation beats panic.** Sub-queries that fail degrade to WARN
  lines within the result (see `ClusterInfo`), so a half-unreachable
  cluster still produces a usable report.
- **Weights are promises.** Removing or reweighting a scored criterion
  changes every user's score baseline — document it in the release notes.

## Building

```bash
make build            # bin/clustercheck, static, stripped, UPX-compressed
make build COMPRESS=0 # skip UPX (keeps debug symbols, faster startup)
make clean            # remove bin/
```

Version stamping comes from `git describe --tags --always --dirty`,
injected via `-ldflags "-X main.version=..."` at build time.
`-trimpath` keeps builds reproducible.

## Release process

Releases are cut by pushing an annotated tag (`v*`); GitHub Actions
builds `linux/amd64` and `linux/arm64`, each shipped as a plain stripped
binary plus a UPX-compressed `-upx` variant (~60% smaller, marginally
slower startup). `make build` locally runs the same pipeline — including
the UPX step that once broke CI when it packed the same binary twice
(see commit history, lesson learned: verify with `make build`, not
`go build`).
