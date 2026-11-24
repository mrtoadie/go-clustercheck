# Deep Dive: Crashloop Detection that Survives Nightly Power Cycles

Most restart-count alarms are naive: they look at `restartCount > N` and call it
a day. On a homelab cluster that gets powered off every night, that logic
produces noise — every single pod "restarts" once per day, not because anything
is broken, but because the nodes themselves boot fresh each morning. A count
built on that statistic is worse than useless: it cries wolf daily.

`clustercheck` separates three situations before it alarms:

| Situation                                          | Classification              | Report status |
| -------------------------------------------------- | --------------------------- | ------------- |
| Restarts are **dense AND recent**                  | active crashloop — look NOW | FAIL          |
| Restart rate ≥ 1/day over lifetime, but not recent | chronic instability         | WARN          |
| High historic count, low rate, long ago            | statistics, not incidents   | INFO          |

## The two signals: density and recency

A container is only flagged as an **active crashloop** when **both** hold:

1. **Density:** the restart rate over the pod's _entire lifetime_ is
   ≥ 1 restart per day (`count / ageDays >= 1.0`).
2. **Recency:** the last restart happened less than **6 hours ago**
   (`lastState.terminated.finishedAt` within a 6h window).

Real crashloops repeat quickly — every few minutes — so they satisfy both
signals almost by definition. Boot artifacts do not: one restart per power
cycle over a 100-day-old pod yields a rate around 1/day at best, and if the
pod booted this morning, the last restart is typically hours past the 6h
window by the time you look.

```
778 restarts, 8.4/day,  last restart  3h ago  →  ACTIVE  ✗ FAIL
123 restarts, 0.8/day,  last restart 23h ago  →  reported, not alarmed
 90 restarts, 0.9/day,  last restart  5d ago  →  historic noise, INFO
```

Note the second line: even a _recent_ restart is not enough without density.
That is deliberate — it is exactly the nightly-boot signature.

## Reading the output

The Pod Stability section shows the aggregate first, then the top 10
containers by restart count:

```
| Pod Stability (Restarts > 5)
  ⚠ Containers   18 above threshold, 0 crashlooping (dense + recent, top 10 shown)
  · Details      cert-manager/cert-manager-cainjector-xxx (cert-manager-cainjector: 336,
                 9.3/day) (last 23h0m0s ago), kube-system/svclb-traefik-... (lb-tcp-80: 123,
                 0.8/day) (last 23h0m0s ago), ...
```

- `0 crashlooping (dense + recent)` is the headline verdict — that number feeds
  the readiness score criterion **No Active Crashloops** (weight 3).
- The `(last 23h0m0s ago)` annotations are context so you can eyeball whether
  an entry is _today's_ problem or yesterday's history.
- An `ACTIVE` marker on an entry means it failed both filters — that is the
  only thing that turns the section red.

## Scoring

Only active crashloops fail the score criterion. Chronic-instability entries
cost nothing in points — they are a WARN to a human, not a gate for a cron
job. Exit code `1` (pipeline mode) is likewise tied to real failures, not to
the natural restart arithmetic of a power-cycled cluster.

## Tuning

The thresholds are exported struct fields on the check, tunable by callers
and tests without touching the logic:

| Field              | Default | Meaning                               |
| ------------------ | ------- | ------------------------------------- |
| `RestartThreshold` | 5       | minimum restart count to be inspected |
| `RecentWindow`     | 6h      | how recent the last restart must be   |
| `DenseRate`        | 1.0     | restarts/day over lifetime required   |

CLI flags for these are on the roadmap; today you adjust them by
constructing the check with different values before running it.

## Why this check exists

This cluster is powered off every night by design. Under that regime, the
restart-count metric accumulates guaranteed noise (~1 per pod per day, plus
real incidents on top), and any alarm keyed on raw counts trains you to
ignore it. Density × recency turns the metric back into a signal: it stays
quiet across months of nightly boots and speaks up only when something is
actually burning. It is the difference between a monitoring check you mute
and one you read.
