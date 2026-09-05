# low-latency-observability

> Real-time market data ingestion pipeline (Binance `aggTrade`), instrumented to measure p99 latency, with SLO, error budget and alerting on a GitOps-managed Kubernetes cluster.

[![CI](https://github.com/Rysekk/low-latency-observability/actions/workflows/ci.yml/badge.svg)](https://github.com/Rysekk/low-latency-observability/actions)

An SRE learning project built end to end, from instrumented Go code to a persistent Kubernetes cluster reconciled by ArgoCD, documenting every trade-off along the way. The goal is not business functionality but the observability chain itself and the compromises it forces in a low-latency environment.

---

## Architecture

### Runtime

```
Binance WebSocket Stream (aggTrade / BTCUSDT)
         │  push, persistent connection
         ▼
  ┌──────────────────────────────────────────┐
  │  Go application (cmd/ingestor)           │
  │                                          │
  │  readStream goroutine                    │
  │         │                                │
  │         ▼  buffered channel (128)        │
  │      [ ■ ■ ■ □ □ ]  full -> DROP         │
  │         │           + ingest_message_    │
  │         │             dropped_total      │
  │         ▼                                │
  │  parse -> processing                     │
  │  (json)   (decimal.Decimal, time.Time)   │
  │    │         │                           │
  │    └────┬────┘                           │
  │         ▼  HistogramVec{stage}           │
  │      /metrics  (:8080)                   │
  └─────────┬────────────────────────────────┘
            │ scrape (10s)
            ▼
  ┌──────────────────────┐      ┌──────────────────────┐
  │  Prometheus          │<-----|  Grafana             │
  │  recording rules     │      │  provisioned         │
  │  alerting rules      │      │  dashboards (Git)    │
  │  TSDB PVC, 30d       │      │  PVC (SQLite)        │
  └──────────┬───────────┘      └──────────┬───────────┘
             │                             │
             └──────────┬──────────────────┘
                        ▼
                 Traefik (Ingress)
             prometheus.local / grafana.local
```

**Cluster**: k3d, application namespace `trading-app`. One Deployment and one Service per component, persistent volumes through the `local-path` StorageClass.

### Delivery

```
  git push (cmd/**, build/**, go.*)
            │
            ▼
  ┌──────────────────────────────┐
  │  GitHub Actions              │
  │  vet -> test  (gate)         │
  │        │                     │
  │        ▼                     │
  │  build + push ghcr.io:<SHA>  │
  │        │                     │
  │        ▼  yq                 │
  │  bump image in               │
  │  deploy/k8s/app/deployment   │
  │  commit "[skip ci]"          │
  └────────┬─────────────────────┘
           │ git
           ▼
  ┌──────────────────────────────┐
  │  ArgoCD  (app-of-apps)       │
  │                              │
  │  root ──> traefik            │
  │       ──> trading-app        │
  │       ──> prometheus         │
  │       ──> grafana            │
  │       ──> ingress-monitoring │
  │                              │
  │  prune: true, selfHeal: true │
  └────────┬─────────────────────┘
           │ reconcile
           ▼
        Cluster
```

Git is the only write path to the cluster. `make up` is a bootstrap, not a deploy: it installs ArgoCD and hands over.

---

## SLO and error budget

| | |
|---|---|
| **SLI** | Latency of the `pipeline` stage (receive to end of processing), measured with a histogram |
| **SLO** | p99 below **2 ms** over a rolling **30 day** window |
| **Error budget** | Roughly 43 min per month of allowed breach |
| **Alerts** | `page_high_latency_pipeline_p99` above 2 ms, `warning_high_latency_pipeline_p99` above 0.5 ms. Both `for: 10m`, `keep_firing_for: 5m` |
| **Actual window** | Started on 2026-08-11, when the TSDB became persistent. Before that, any 30 day SLO was fictional |

SLIs are materialised as **versioned recording rules** (`ingest:processed_ratio`, `ingest:pipeline_latency_p99`) rather than computed on the fly in Grafana, so the SLI definition travels with the code and survives infrastructure migrations.

The 0.5 ms warning tier is an early signal, not part of the SLO. The provisioned dashboard (`dash-slo`) derives error budget consumption from the p99 recording rule over a 30 day subquery. Downtime does not yet consume budget there, since the SLI series is simply absent when the app is down: the no-data alert on the backlog closes that gap.

### Exposed metrics

| Metric | Type | Purpose |
|---|---|---|
| `ingest_duration_seconds{stage}` | Histogram | Per-stage latency, `parse` / `processing` / `pipeline`. Exponential buckets from 10 µs, factor 1.5, 15 buckets. The top finite bucket is about 2.9 ms, so a breach is measurable but its magnitude saturates there |
| `ingest_message_receive_total` | Counter | Messages read off the socket, SLI denominator |
| `ingest_message_dropped_total` | Counter | Messages discarded on a full channel, SLI numerator |
| `ingest_json_parse_errors_total` | Counter | Malformed frames |
| `ingest_trade_parse_errors_total` | Counter | Frames that parse as JSON but not as a trade |

![SLO dashboard](docs/img/dashboard-slo.png)


---

## Notable trade-offs

Every technical decision is recorded in [`docs/decisions.md`](docs/decisions.md). The four most structural ones:

**[Backpressure strategy: drop rather than block](docs/decisions.md#6-backpressure-drop-rather-than-block)**
When the channel is full, the incoming message is discarded and a counter is incremented. Blocking guarantees zero loss but leaves latency unbounded. In a low-latency context, bounded latency outweighs completeness.

**[`strategy: Recreate` instead of a rolling update](docs/decisions.md#11-strategy-recreate)**
With a single replica and `maxSurge: 1`, a rolling update opens two Binance WebSockets at once during the switchover. Both feeds are aggregated into the recording rules, so the SLIs are corrupted with no visible symptom. `Recreate` causes a gap of a few seconds instead. Between losing data and holding wrong data, the gap wins: a gap is detectable (`absent_over_time`), silent contamination is not. Budgeted cost: about 1 min per deployment.

**[Burstable QoS with no CPU limit](docs/decisions.md#12-burstable-qos)**
`requests = limits` on memory (incompressible, exceeding it means OOMKill), `requests` only on CPU. A CPU limit triggers CFS throttling, which freezes the container until the end of the 100 ms period, a stall of up to **50 times the p99 SLO**. Accepted downside: without a limit, the pod can starve its neighbours on the node.

**[Traefik managed by ArgoCD rather than bootstrapped](docs/decisions.md#29-treafik-under-argocd)**
The ingress controller is an ArgoCD Application using a multi-source Helm setup (upstream chart, `$values` from this repo), so no part of the cluster sits outside GitOps. The accepted cost is an ordering dependency: nothing is reachable through ingress until the root Application has synced. This supersedes the manual Helm install described in [ADR 24](docs/decisions.md#24-ingress-through-a-manually-installed-traefik).

---

## Timeline

| | |
|---|---|
| **Jul 11** | First real-time feed received (`aggTrade` WebSocket) |
| **Jul 19** | Three-stage instrumentation, full Go to Prometheus to Grafana chain working |
| **Jul 25** | Multi-stage `scratch` image (~8.5 MB), docker-compose stack |
| **Jul 26** | SLO and SLIs formalised: recording rules plus alerting rule, Pending to Firing cycle validated |
| **Aug 02** | Kubernetes migration (k3d) for the Go app and Prometheus, graceful SIGTERM shutdown |
| **Aug 10** | Grafana provisioned as code, read-only root filesystem |
| **Aug 11** | Persistent volumes (dynamic PVCs), start of the real SLO window |
| **Aug 15** | Exposure through Ingress and Traefik, port-forwards retired |
| **Aug 28** | CI on GitHub Actions, tests green on push to `main` |
| **Aug 29** | Trades converted to domain types (`decimal.Decimal`, `time.Time`), real processing stage and its tests. CI completed: lint, test, build, push to GHCR at the commit SHA, build gated on the test job |
| **Aug 30** | CI to GitOps closed: image tagged with the commit SHA, manifest bumped by the pipeline, deployed by ArgoCD |
| **Sep 05** | ArgoCD app-of-apps, automated sync (`prune` and `selfHeal`) across the whole stack. Traefik moved under ArgoCD, `namespace.yaml` removed in favour of `CreateNamespace=true`. CI path filter so only application changes trigger a build. Makefile reduced to a bootstrap role with `kubectl wait`, validated from scratch on a fresh cluster |

---

## Running locally

### Prerequisites

- Docker
- [k3d](https://k3d.io/), kubectl, Helm
- Local host entries so the ingress domains resolve:

```
127.0.0.1 grafana.local
127.0.0.1 prometheus.local
```

### 1. Create the cluster

```bash
k3d cluster create local-dev \
  --agents 2 \
  --port "80:80@loadbalancer" \
  --k3s-arg "--disable=traefik@server:*"
```

Port 80 is mapped onto the k3d load balancer so host traffic reaches the Traefik Service. Without that mapping, a `LoadBalancer` Service inside the cluster stays unreachable from the host regardless of how Traefik was installed. The mapping can also be added later with `k3d cluster edit local-dev --port-add "80:80@loadbalancer"`, which recreates the load balancer container.

Only HTTP is exposed at this stage, since the stack runs without TLS (see [known limitations](#known-limitations)). Adding 443 later follows the same pattern.

`--disable=traefik` turns off the Traefik bundled with k3s. Traefik is installed instead as an ArgoCD Application pinned to chart `41.2.0`, so its version and values live in Git ([ADR 29](docs/decisions.md#29-treafik-under-argocd)).

The two agent nodes matter: Prometheus and Grafana volumes are provisioned by `local-path` with a node affinity pinned to whichever node first scheduled the pod (see [ADR 22](docs/decisions.md#22-dynamic-pvc-instead-of-a-static-hostpath-pv)). Recreating the cluster discards those volumes and resets the SLO window.

### 2. Bootstrap

```bash
make up
```

The target does three things and then gets out of the way: installs ArgoCD via Helm (chart `10.4.2`, server as `ClusterIP`), waits for `argocd-server` to become available, and applies `deploy/argocd/root.yaml`.

Everything else is reconciled from Git. The root Application watches `deploy/argocd/apps/` and creates the five child Applications; each one syncs with `prune: true` and `selfHeal: true`, and creates its own namespace through `CreateNamespace=true`. Nothing is built locally: the application image is pulled from `ghcr.io/rysekk/low-latency-observability` at the SHA tag the CI wrote into the manifest.

Watch it converge:

```bash
kubectl get applications -n argocd -w
kubectl get pods -n trading-app
```

Services are then available at http://grafana.local and http://prometheus.local

### 3. ArgoCD UI (optional)

The ArgoCD server is `ClusterIP` and has no Ingress, so reach it through a port-forward:

```bash
kubectl port-forward -n argocd svc/argocd-server 8081:443
kubectl -n argocd get secret argocd-initial-admin-secret \
  -o jsonpath="{.data.password}" | base64 -d
```

Then open https://localhost:8081 and log in as `admin`.

### Teardown

```bash
k3d cluster delete local-dev
```

This discards the PVCs and resets the SLO window.

---

## Continuous integration

`.github/workflows/ci.yml`, triggered on push to `main` restricted to `cmd/**`, `build/**` and `go.*`, so documentation and manifest changes do not rebuild an image.

1. `lintAndTest`: `go vet ./...` then `go test ./...` on Go 1.26.5
2. `build-push`, gated on the previous job: builds `build/Dockerfile` with Buildx and pushes `ghcr.io/rysekk/low-latency-observability:<commit SHA>`
3. Same job: `yq` rewrites the image reference in `deploy/k8s/app/deployment.yaml` and commits it back with `[skip ci]`

The write-back uses the default `GITHUB_TOKEN` on purpose. A PAT would let the resulting commit retrigger the workflow and loop ([ADR 27](docs/decisions.md#27-sha-tag-ci-over-argo-image-updater)).

---

## Repository layout

```
cmd/ingestor/            Go application and its tests
build/Dockerfile         multi-stage build down to scratch
deploy/
  argocd/
    root.yaml            app-of-apps root Application
    apps/                one Application per component
  k8s/
    app/                 ingest-app Deployment and Service
    monitoring/          prometheus, grafana, ingress
  helm/                  Traefik values consumed by ArgoCD via $values
  compose/               docker-compose stack, kept for local iteration
observability/           Prometheus config and rules, Grafana provisioning
docs/                    decisions.md, backlog.md
```

---

## Stack

**Application**: Go 1.26.5, `coder/websocket`, `prometheus/client_golang`, `shopspring/decimal`

**Observability**: Prometheus 3.13 (recording and alerting rules, 30d retention), Grafana 13 (provisioned through ConfigMaps)

**Infrastructure**: Kubernetes (k3d), ArgoCD (Helm chart `argo-cd` 10.4.2), Traefik (Helm chart 41.2.0), `local-path` PVCs

**Distribution**: multi-stage `scratch` image (~8.5 MB), published to `ghcr.io/rysekk/low-latency-observability`

**Hardening**: `runAsNonRoot` (65534), `readOnlyRootFilesystem`, all capabilities dropped, `GOMEMLIMIT` set below the memory limit so the GC reacts before the OOM killer does

---

## Known limitations

Deliberately out of scope at this stage, listed so there is no ambiguity about what is and is not done:

- Prometheus exposed without access control, Grafana in anonymous-admin mode, HTTP without TLS
- ArgoCD reachable only through a port-forward, no Ingress, default admin credentials
- Ingress depends on the ArgoCD sync completing, so nothing is reachable until the root Application has synced
- Single replica: scaling a WebSocket horizontally would duplicate the feed and require sharding, which is not warranted at a few hundred msg/s
- Local cluster (k3d), no EKS deployment yet
- Prometheus rules and the Grafana dashboard each exist in two places, `observability/` for compose and the ConfigMaps for Kubernetes, and nothing enforces that they stay identical

Planned work: [`docs/backlog.md`](docs/backlog.md)

---

## Documentation

- [`docs/decisions.md`](docs/decisions.md): technical decision records
- [`docs/backlog.md`](docs/backlog.md): planned work and accepted debt