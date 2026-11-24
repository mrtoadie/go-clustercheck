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
  ⚠ Containers     18 above threshold, 0 crashlooping (dense + recent, top 10 shown)

Readiness Score: 81% (18/22 weighted points, 12 criteria) [grade: yellow]
──────────────────────────────
  ✓ All Nodes Ready (w3): 4/4 ready
  ✓ API Server Ready (w3): readyz check passed
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

Twelve checks, each returning structured results:

| Check                  | What it looks at                                                                                                                                                                                    |
| ---------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Cluster Info           | API version, namespace/node/pod counts, node readiness                                                                                                                                              |
| Storage Status         | StorageClasses, default SC, PV phases, orphaned `Released` PVs, pending PVCs                                                                                                                        |
| Problem Pods           | pods with a non-healthy phase AND containers stuck in waiting states like `CrashLoopBackOff`/`ImagePullBackOff` — the pod phase alone says "Running" while a container burns                        |
| Pod Stability          | containers with >5 restarts, classified by **density and recency** — dense (≥1/day) AND recent (<6h) is an active crashloop; boot artifacts and long-term accumulation are reported without failing |
| Certificates & TLS     | every `kubernetes.io/tls` secret, leaf certificate expiry via `crypto/x509`, expired / <30d / healthy buckets                                                                                       |
| Network Policies       | policy count and namespaces without any policy                                                                                                                                                      |
| Pod Security Admission | namespaces with `pod-security.kubernetes.io/enforce` labels                                                                                                                                         |
| Control Plane          | `/readyz?verbose` — missing entries are informational, real `[-]` failures are not                                                                                                                  |
| DaemonSet Completeness | desired vs current vs available — the silent "PSA blocked recreation" failure class                                                                                                                 |
| Disruption Budgets     | PDB coverage + which budgets currently allow zero disruptions                                                                                                                                       |
| Resource Quotas        | quota coverage across namespaces                                                                                                                                                                    |
| Backup Tooling         | Velero deployment detection                                                                                                                                                                         |

Plus a **weighted readiness score** across all checks: fundamental health
(nodes, crashloops) weighs 3, client-facing correctness weighs 2,
hardening items weigh 1. The weights are visible in every output, so the
score is an explained opinion, not a magic number.

## Works on any conformant cluster

The hand-rolled kubeconfig client follows the kubeconfig spec, not a
vendor convention: `current-context` selects the context (merged
multi-cluster configs are resolved correctly), and all four credential
forms are supported — CA and client certificate both as inline base64
(k3s style) and as file path (minikube/kubeadm style). Auth precedence:
client certificate > bearer token > basic auth.

Zero external client dependencies — tested against **k3s v1.36** and
**minikube v1.35**.

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

| Platform                       | Arch  | Runs on                                                             |
| ------------------------------ | ----- | ------------------------------------------------------------------- |
| Arch Linux                     | amd64 | daily driver / workstation                                          |
| Raspberry Pi OS (Debian-based) | arm64 | k3s cluster nodes                                                   |
| minikube (driver VM)           | amd64 | local test clusters — k3s _and_ minikube audited by the same binary |

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

**go-clustercheck** dev · 2026-11-24 22:29

**Summary:** 12 checks · ✅ 8 ok · ⚠️ 3 warnings · ❌ 0 failed

---

## ✅ Cluster Info

| Status | Item        | Value               |
| ------ | ----------- | ------------------- |
| ✅     | K8s Version | `v1.36.3+k3s1`      |
| ✅     | Namespaces  | `10`                |
| ✅     | Nodes       | `4 total / 4 ready` |
| ✅     | Pods        | `23`                |

## ✅ Control Plane Health (/readyz)

| Status | Item   | Value                 |
| ------ | ------ | --------------------- |
| ✅     | Result | `readyz check passed` |

## ✅ Storage Status

| Status | Item           | Value                                                                |
| ------ | -------------- | -------------------------------------------------------------------- |
| ✅     | StorageClasses | `2`                                                                  |
| ✅     | Default SC     | `local-path`                                                         |
| ✅     | PVs            | `8 total \| Bound=7 \| Released=1`                                   |
| ⚠️     | Released PVs   | `1 - pvc-54d04e1f-662a-4008-a640-53abdd3ffbc2 (ex dev/forgejo-data)` |
| ✅     | PVCs           | `7 total \| 0 pending`                                               |

## ✅ Problem Pods (phase or container state)

| Status | Item   | Value                                                         |
| ------ | ------ | ------------------------------------------------------------- |
| ✅     | Result | `all pods Running or Succeeded, no blocking container states` |

## ⚠️ Pod Stability (Restarts > 5)

| Status | Item       | Value                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| ------ | ---------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| ⚠️     | Containers | `18 above threshold, 0 crashlooping (dense + recent, top 10 shown)`                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| ℹ️     | Details    | `cert-manager/cert-manager-cainjector-6ffc5b65d9-kj2kt (cert-manager-cainjector: 336, 9.3/day) (last 23h0m0s ago), kube-system/svclb-traefik-8d4930a1-zz5ll (lb-tcp-80: 123, 0.8/day) (last 23h0m0s ago), kube-system/svclb-traefik-8d4930a1-zz5ll (lb-tcp-443: 123, 0.8/day) (last 23h0m0s ago), kube-system/local-path-provisioner-58d557dc48-j565k (local-path-provisioner: 108, 0.9/day) (last 23h0m0s ago), kube-system/svclb-traefik-8d4930a1-x2zg5 (lb-tcp-443: 104, 0.8/day) (last 6h0m0s ago), kube-system/svclb-traefik-8d4930a1-x2zg5 (lb-tcp-80: 104, 0.8/day) (last 6h0m0s ago), dev/forgejo-6f554f4886-mc9wt (forgejo: 92, 0.9/day) (last 23h0m0s ago), kube-system/svclb-traefik-8d4930a1-78w5z (lb-tcp-80: 73, 0.8/day) (last 6h0m0s ago), kube-system/svclb-traefik-8d4930a1-78w5z (lb-tcp-443: 73, 0.8/day) (last 6h0m0s ago), kube-system/svclb-traefik-8d4930a1-pslts (lb-tcp-443: 65, 0.7/day) (last 6h0m0s ago)` |

## ✅ Certificates & TLS

| Status | Item        | Value                                                       |
| ------ | ----------- | ----------------------------------------------------------- |
| ✅     | TLS Secrets | `5 total \| 0 expired \| 0 expiring <30d \| 5 healthy`      |
| ✅     | Next Expiry | `audiobookshelf/audiobookshelf-tls in 53 days (2027-01-17)` |

## ⚠️ Network Policies

| Status | Item       | Value                                                                                              |
| ------ | ---------- | -------------------------------------------------------------------------------------------------- |
| ℹ️     | Policies   | `0 across 10 namespaces`                                                                           |
| ⚠️     | Without NP | `audiobookshelf, cert-manager, dev, kube-node-lease, kube-public, monitoring, nfs-storage, velero` |

## ⚠️ Pod Security Admission (PSA)

| Status | Item    | Value                                                                                      |
| ------ | ------- | ------------------------------------------------------------------------------------------ |
| ℹ️     | Enforce | `4/10 namespaces labeled`                                                                  |
| ℹ️     | Labeled | `audiobookshelf=baseline, cert-manager=baseline, monitoring=privileged, velero=privileged` |

## ✅ Pod Disruption Budgets

| Status | Item | Value          |
| ------ | ---- | -------------- |
| ℹ️     | PDBs | `0 configured` |

## ℹ️ Resource Quotas

| Status | Item    | Value                   |
| ------ | ------- | ----------------------- |
| ℹ️     | Quotas  | `2 across 2 namespaces` |
| ℹ️     | Covered | `audiobookshelf, dev`   |

## ✅ Backup Tooling (Velero)

| Status | Item   | Value                                  |
| ------ | ------ | -------------------------------------- |
| ✅     | Velero | `deployment found in namespace velero` |

## ✅ DaemonSet Completeness

| Status | Item         | Value                                                     |
| ------ | ------------ | --------------------------------------------------------- |
| ℹ️     | DaemonSets   | `2 total`                                                 |
| ✅     | Completeness | `all DaemonSets complete (current = desired = available)` |

## 🎯 Production Readiness Score

**Score: 81%** (18/22 weighted points across 12 criteria, grade: yellow)

| Criterion                 | Weight | Passed | Detail                                       |
| ------------------------- | ------ | ------ | -------------------------------------------- |
| ✅ All Nodes Ready        | 3      | ✅     | `4/4 ready`                                  |
| ✅ API Server Ready       | 3      | ✅     | `readyz check passed`                        |
| ❌ PV Hygiene             | 1      | ❌     | `failed=0 released=1`                        |
| ✅ No Problem Pods        | 2      | ✅     | `0 pods broken by phase or container state`  |
| ✅ No Active Crashloops   | 3      | ✅     | `0 crashlooping containers (dense + recent)` |
| ✅ Certificates Valid     | 2      | ✅     | `0 expired, 0 expiring <30d`                 |
| ❌ NetworkPolicy Coverage | 1      | ❌     | `8 workload namespaces without NP`           |
| ❌ PSA Enforcement        | 1      | ❌     | `4/10 namespaces with enforce label`         |
| ❌ Disruption Budgets     | 1      | ❌     | `0 PDBs, 0 currently blocking`               |
| ✅ Resource Quotas        | 1      | ✅     | `2 quotas in 2 namespaces`                   |
| ✅ Backup Tooling         | 2      | ✅     | `velero deployment present: true`            |
| ✅ DaemonSet Completeness | 2      | ✅     | `0 incomplete of 2 DaemonSets`               |

---

_Generated by go-clustercheck dev_
```

_(Example from the 4-node k3s cluster that taught this tool most of
its checks — including a PSA incident that silently blinded a node
for nine days.)_

</details>

## Roadmap

- [x] Additional checks for full bash-parity: PDBs, ResourceQuotas, backup tooling (Velero), GitOps presence, control-plane `/readyz`
- [x] Full kubeconfig context resolution (merged configs, client-cert auth — k3s + minikube verified)
- [ ] `-o json` output for dashboards and monitoring pipelines
- [ ] Unit tests for scoring aggregation and certificate parsing
- [ ] Optional `--thresholds` file to customize weights and warning windows

## License

MIT — [LICENSE](LICENSE).
