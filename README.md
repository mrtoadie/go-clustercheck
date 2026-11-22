# go-clustercheck

![CI](https://img.shields.io/github/actions/workflow/status/mrtoadie/go-clustercheck/ci.yml?branch=main&label=CI)
![Release](https://img.shields.io/github/v/tag/mrtoadie/go-clustercheck?label=release)
![Go](https://img.shields.io/badge/go-1.22%2B-00ADD8?logo=go&logoColor=white)
![Platform](https://img.shields.io/badge/platform-linux%20amd64%20%7C%20arm64-blue)
![License](https://img.shields.io/github/license/mrtoadie/go-clustercheck) [![Repo size](https://img.shields.io/github/repo-size/mrtoadie/go-clustercheck)](https://github.com/mrtoadie/go-clustercheck) ![GitHub Repo stars](https://img.shields.io/github/stars/mrtoadie/go-clustercheck?style=flat&label=Stars) ![GitHub Downloads (all assets, all releases)](https://img.shields.io/github/downloads/mrtoadie/go-clustercheck/total)

**Kubernetes production-readiness audit as a single static Go binary.**
Zero `client-go` dependency, structured check results, two renderers from
one data source, and a weighted readiness score.

```text
$ ./clustercheck

| Pod Stability (Restarts > 5)
  ✗ Containers     38 above threshold, 28 active in last 24h (top 10)
  · Details        longhorn-system/longhorn-manager-wkmsg (104.2/day) ACTIVE, ...

Readiness Score: 53% (7/13 weighted points, 7 criteria) [grade: red]
──────────────────────────────
  ✓ All Nodes Ready (w3): 4/4 ready
  ✗ No Active Crashloops (w3): 28 containers restarted within last 24h
  ...
```

## Why

This project started life as a 900-line Bash script that drove `kubectl`,
`jq`, `awk` and `openssl` through a dozen pipelines per run. It worked —
but every check was glued to its output format, field-selector quirks
silently reported "healthy", and storing a report meant scrubbing ANSI
escape codes with `sed`.

The rewrite keeps the intent and changes the foundation:

- **Checks produce data, renderers produce output.** Terminal colors and
  Markdown reports come from the same `Result` structures.
- **No shell-outs.** TLS certificates are parsed with `crypto/x509`,
  JSON with `encoding/json` — no openssl, no jq, no awk.
- **Minimal client.** Talks to the Kubernetes API directly over HTTPS
  with a hand-rolled kubeconfig loader (~300 lines instead of the
  client-go dependency tree). One external dependency total.
- **Static binary.** `CGO_ENABLED=0`, stripped, version-stamped —
  cross-compiled for `amd64` and `arm64` without ever touching a
  Raspberry Pi for compilation.

## Features

Seven checks, each returning structured results:

| Check                  | What it looks at                                                                                                                                                    |
| ---------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Cluster Info           | API version, namespace/node/pod counts, node readiness                                                                                                              |
| Storage Status         | StorageClasses, default SC, PV phases, orphaned `Released` PVs, pending PVCs                                                                                        |
| Problem Pods           | pods that are neither `Running` nor `Succeeded` (job pods are healthy, not errors)                                                                                  |
| Pod Stability          | containers with >5 restarts, classified by **rate and recency** — an active crashloop (`<24h`) is a FAIL, a pod that accumulated restarts over months is statistics |
| Certificates & TLS     | every `kubernetes.io/tls` secret, leaf certificate expiry via `crypto/x509`, expired / <30d / healthy buckets                                                       |
| Network Policies       | policy count and namespaces without any policy                                                                                                                      |
| Pod Security Admission | namespaces with `pod-security.kubernetes.io/enforce` labels                                                                                                         |

Plus a **weighted readiness score** across all checks: fundamental health
(nodes, crashloops) weighs 3, client-facing correctness weighs 2,
hardening items weigh 1. The weights are visible in every output, so the
score is an explained opinion, not a magic number.

### Crashloop discrimination

Raw restart counts lie on clusters that get powered down regularly —
a nightly power cycle restarts every pod once per day, and naive
recency checks misread that as an active crashloop. `clustercheck`
requires **both** signals before flagging a container as ACTIVE:

- **FAIL** — restarts are _dense_ (rate ≥ 1/day) **and** _recent_
  (last restart < 6h): active crashloop, look now
- **WARN** — sustained restart rate ≥ 1/day over the pod's lifetime:
  chronic instability
- **INFO** — high historic counts on long-lived pods: statistics, not
  incidents

Boot artifacts don't fail the check — real crashloops repeat quickly,
boot restarts do not.

## Usage

```bash
# colored terminal report (colors auto-disable when piped)
./clustercheck

# markdown report, directly usable in Obsidian/GitHub wikis
./clustercheck -md > report.md

# skip the readiness score footer
./clustercheck -noscore

# explicit kubeconfig
./clustercheck -kubeconfig ~/.kube/config-homelab
```

Exit code is `1` if any check reported a FAIL (e.g. active crashloop,
expired certificate) — safe for cron jobs and pipelines.

## Tested Platforms

The binary is statically linked (`CGO_ENABLED=0`) and intentionally has no
runtime dependencies — if it boots a Linux kernel, it should run. Verified
in the wild on:

| Platform                       | Arch  | Runs on                    |
| ------------------------------ | ----- | -------------------------- |
| Arch Linux                     | amd64 | daily driver / workstation |
| Raspberry Pi OS (Debian-based) | arm64 | k3s cluster nodes          |

## Installation

Grab a binary from the [releases](../../releases) page:

```bash
curl -LO https://github.com/mrtoadie/go-clustercheck/releases/latest/download/clustercheck-linux-arm64
chmod +x clustercheck-linux-arm64
```

Both release variants (`-upx` suffix: ~60% smaller, marginally slower
startup) are exercised on x86_64 desktops and on the ARM64 Raspberry Pi
nodes that make up the cluster being audited.

Requires a valid kubeconfig with cluster-admin-ish read permissions
(the tool only performs GET requests).

Or build from source:

```bash
make
./bin/clustercheck
```

## Architecture

```text
kubeconfig ──▶ internal/kube      minimal API client (net/http + TLS)
                    │
                    ▼
              internal/audit     checks → []Result{Status, Lines, Scores}
                    │
        ┌───────────┴───────────┐
        ▼                       ▼
  internal/report          internal/score
  terminal | markdown      weighted aggregation
```

Adding a check means implementing the `Check` interface and appending it
to the registry — renderers and scoring pick it up automatically.

<details>
<summary><strong>Example markdown output</strong> (from a real 4-node k3s cluster)</summary>

```markdown
# Cluster Check Report

**go-clustercheck** v0.2.0 · 2026-11-21 18:30

**Summary:** 7 checks · ✅ 4 ok · ⚠️ 2 warnings · ❌ 1 failed

## ❌ Pod Stability (Restarts > 5)

| Status | Item       | Value                                                |
| ------ | ---------- | ---------------------------------------------------- |
| ❌     | Containers | `38 above threshold, 28 active in last 24h (top 10)` |

## 🎯 Production Readiness Score

**Score: 53%** (7/13 weighted points across 7 criteria, grade: red)

| Criterion                 | Weight | Passed | Detail                                    |
| ------------------------- | ------ | ------ | ----------------------------------------- |
| ✅ All Nodes Ready        | 3      | ✅     | `4/4 ready`                               |
| ❌ No Active Crashloops   | 3      | ❌     | `28 containers restarted within last 24h` |
| ✅ Certificates Valid     | 2      | ✅     | `0 expired, 0 expiring <30d`              |
| ❌ NetworkPolicy Coverage | 1      | ❌     | `8 workload namespaces without NP`        |
| ❌ PSA Enforcement        | 1      | ❌     | `5/11 namespaces with enforce label`      |
```

_(Yes, the example cluster ships a live Longhorn crashloop — real
findings make better documentation than pristine green.)_

</details>

## Roadmap

- [ ] Additional checks for full bash-parity: PDBs, ResourceQuotas, backup tooling (Velero), GitOps presence, control-plane `/readyz`
- [ ] `-o json` output for dashboards and monitoring pipelines
- [ ] Unit tests for scoring aggregation and certificate parsing
- [ ] Optional `--thresholds` file to customize weights and warning windows

## License

MIT — [LICENSE](LICENSE).
