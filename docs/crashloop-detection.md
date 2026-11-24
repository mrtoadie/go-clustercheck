# Deep Dive: Crashloop Detection that Survives Nightly Power Cycles

Most restart-count alarms are naive: they look at `restartCount > N` and call it
a day. On a homelab cluster that gets powered off every night, that logic
produces noise — every single pod "restarts" once per day, not because anything
is broken, but because the nodes themselves boot fresh each morning.

`clustercheck` refuses to confuse boot artifacts with real crashloops. It
requires **two independent signals** before flagging a container:

1. **Restart count above threshold** (default: 5) — the coarse filter.
2. **Restart recency** — the container actually restarted _recently_, within
   a configurable activity window (default: last 24h).

A pod that was restarted once by last night's power cycle sits far outside
the activity window and passes quietly. A pod that died eleven times since
breakfast is **ACTIVE** and gets flagged.

```
restartCount: 6, last restart 3h ago   →  ACTIVE crashloop  ✗
restartCount: 6, last restart 5d ago   →  stale boot relic  ✓
restartCount: 90, last restart 5d ago  →  stale boot relic  ✓
```

## Why the window matters more than the count

On our own 4-node k3s cluster, the nightly power cycle gives every pod a
guaranteed minimum of ~365 restarts per year. Any check built purely on
counts would render the whole metric useless. Pairing count with recency
turns a broken statistic into a usable signal: high count + recent activity
means _ongoing_ trouble, high count + no recent activity means _history_.

## Reading the output

Flagged containers show up in the **Pod Stability** section:

```
| Pod Stability (Restarts > 5)
  ✗ Containers     21 above threshold, 21 active in last 24h (top 10)
  · Details        monitoring/monitoring-prometheus-node-exporter-4jh4l
                  (node-exporter: 778, 8.4/day) ACTIVE, ...
```

The `ACTIVE` marker is the discriminator: it means the restart happened
inside the activity window. If you only see the raw count listed without
ACTIVE, the container is being reported for context, not alarmed on.

## Tuning

- `--restart-threshold` (env: `CLUSTERCHECK_RESTART_THRESHOLD`) — raise for
  clusters with chatty workloads, lower for stricter SRE posture.
- Activity window — adjustable if your cluster rarely reboots and you want
  longer memory.

## The PSA incident that motivated this check

This check exists because of a real failure: a Pod Security Admission
enforcement change silently landed a node outside the allowed security
level, and kubelet kept rejecting its workloads **for nine days** before
anyone noticed. Restart counts alone had normalized that signal away —
every day looked like "just the usual nightly restarts". Recency-based
discrimination makes that class of silent failure visible again.
