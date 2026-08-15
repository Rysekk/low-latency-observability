# Technical decisions (ADR)

> **Why things were decided this way.** One entry equals one trade-off between defensible options, naming the discarded option and the accepted downside. Obvious or imposed choices are grouped at the end under "Conventions".
>
> Entries are dated and immutable: a reversed decision gets a new entry marked `Superseded by #N`, never a rewrite of the old one.
>
> Original numbering is kept. Missing numbers correspond to choices reclassified as conventions.

| # | Date | Decision |
|---|---|---|
| [1](#1-source-binance-websocket-stream) | Jul 11 | Source: Binance WebSocket Stream |
| [3](#3-e-binance-event-time-as-a-freshness-signal-not-an-slo-basis) | Jul 11 | `E` as freshness signal, not SLO basis |
| [6](#6-backpressure-drop-rather-than-block) | Jul 11 | Backpressure: drop rather than block |
| [7](#7-multi-stage-image-down-to-scratch) | Jul 25 | Multi-stage image down to `scratch` |
| [8](#8-slis-materialised-as-versioned-recording-rules) | Jul 26 | SLIs as versioned recording rules |
| [11](#11-strategy-recreate) | Jul 29 | `strategy: Recreate` |
| [12](#12-burstable-qos) | Jul 29 | Burstable QoS, no CPU limit |
| [17](#17-graceful-shutdown-through-signalnotifycontext) | Aug 02 | Graceful shutdown via `signal.NotifyContext` |
| [21](#21-dashboard-export-through-the-classic-api-only) | Aug 10 | Dashboard export through the API only |
| [22](#22-dynamic-pvc-instead-of-a-static-hostpath-pv) | Aug 11 | Dynamic PVC instead of static PV |
| [23](#23-grafana-pvc-and-hybrid-dashboard-model) | Aug 11 | Grafana PVC and hybrid model |
| [24](#24-ingress-through-a-manually-installed-traefik) | Aug 15 | Ingress through a manually installed Traefik |
| [-](#conventions) | - | Conventions |

---

## 1. Source: Binance WebSocket Stream

**Options**: REST API, WebSocket API, WebSocket Stream.

**Decision**: WebSocket **Stream**.

**Rationale**: REST forces polling, which means picking an arbitrary interval, accepting a latency floor equal to that interval, and generating load even when nothing moves. The WebSocket *API* is a request/response channel meant for order placement, not for broadcasting. Only the Stream provides a pushed feed over a persistent connection, public and unauthenticated.

**Follow-on**: the `aggTrade` stream on BTCUSDT was selected. Executions are grouped by price and side over a short interval, which cuts message count while keeping each message self-contained (event time included). BTCUSDT for volume.

---

## 3. `E` (Binance event time) as a freshness signal, not an SLO basis

**Context**: every message carries an event timestamp `E` issued by Binance. Computing end-to-end latency as `now() - E` is tempting.

**Decision**: `E` is used as a qualitative freshness indicator. All SLIs are measured on **machine-local** stages, against a single clock.

**Rationale**: the local clock and the Binance clock are not synchronised. The resulting measurement mixes real latency with clock skew in unknown and varying proportions, sometimes producing negative values. An SLI built on that would be indefensible.

**Downside**: network latency and publisher-side latency are not measured. The SLO scope stops at what is under our control, which is the right boundary for an error budget anyway.

---

## 6. Backpressure: drop rather than block

**Context**: the WebSocket reader goroutine writes into a buffered channel (128) consumed by the processing stage. What should happen when the consumer falls behind and the buffer saturates?

**Options**:
- **Block**: blocking write. Zero loss, but WebSocket reads stall, the TCP queue builds up, and the latency of processed messages grows without bound. The system stays "correct" while serving arbitrarily stale data.
- **Drop**: `select` with `default`. Buffer full means the message is discarded and `ingest_messages_dropped_total` is incremented.

**Decision**: drop.

**Rationale**: in a low-latency context, late data is wrong data. Bounding latency matters more than completeness.

**Downside**: message loss under load. It is **made observable** by the counter, which doubles as a congestion signal usable in alerting. A counted drop beats an invisible delay.

---

## 7. Multi-stage image down to `scratch`

**Decision**: `golang:1.26-alpine` builder, final image on `scratch`, `CGO_ENABLED=0`.

**Result**: around 8.5 MB, static binary, near-zero attack surface (no shell, no package manager, no libc).

**Gotcha hit**: `scratch` is literally empty, so TLS for the `wss://` connection fails for lack of a certificate store. CA certs have to be copied explicitly from the build stage.

**Downside**: no diagnostic tooling inside the container. Accepted, since debugging goes through metrics and logs, which is the whole point of the project.

---

## 8. SLIs materialised as versioned recording rules

**Options**: compute SLIs on the fly inside Grafana queries, or precompute them as Prometheus recording rules defined in versioned files.

**Decision**: recording rules (`ingest:processed_ratio`, `ingest:pipeline_latency_p99`).

**Rationale**: an SLI definition is a contract, not a presentation detail. Inside a Grafana panel it can be edited by anyone, stays invisible to code review, and disappears with the instance. As a versioned file it travels with the repository, which was confirmed during the Docker Compose to Kubernetes migration where the rules carried over untouched. Secondary benefit: precomputation takes load off dashboard queries.

---

## 11. `strategy: Recreate`

**Context**: a single replica, running the default strategy (`RollingUpdate`, `maxSurge: 1`).

**Problem**: a rolling update starts the new pod **before** terminating the old one. During the overlap, two Binance WebSockets are open and ingesting the same feed. Both metric sets are aggregated into the recording rules, so the SLIs are corrupted and **nothing signals it**. The pipeline looks healthy.

**Decision**: `strategy: Recreate`, so the old pod terminates before the new one starts.

**Trade-off**: `Recreate` opens a gap of a few seconds with no ingestion. Between losing data and producing wrong data, loss wins: a gap is detectable (`absent_over_time`), silent contamination is not.

**Budgeted cost**: roughly 1 min of downtime per deployment against 43 min per month of error budget. Sustainable while deployment frequency stays low, to be revisited once CI/CD raises the cadence.

**Discarded alternative**: sharding or a distributed lock would allow multiple replicas without duplication, but the complexity is not justified at this volume (see conventions).

---

## 12. Burstable QoS

**Decision**: `requests = limits` on memory, `requests` only on CPU with **no limit**.

**Rationale, memory**: an incompressible resource. Exceeding it does not degrade, it kills the container (OOMKill). It is therefore bounded strictly, with `GOMEMLIMIT` aligned so the Go GC reacts before the kernel does.

**Rationale, CPU**: a compressible resource, but the compression mechanism is blunt. A CPU limit enables CFS throttling: once the quota for the 100 ms period is consumed, the container is **frozen until the period ends**. That is a stall of up to 100 ms against a p99 SLO of 2 ms, a factor of 50. A CPU limit would guarantee breaching the very SLO it is supposed to protect.

**Accepted downside**: with no limit, the pod can starve its neighbours on the node. Latency is explicitly favoured over fair sharing.

**To watch**: `container_cpu_cfs_throttled_seconds_total` must stay at zero. If it rises, a limit has been reintroduced somewhere.

---

## 17. Graceful shutdown through `signal.NotifyContext`

**Context**: on SIGTERM (deployment, eviction), the process must stop cleanly, closing the WebSocket and draining the channel rather than being killed cold.

**Decision**: `signal.NotifyContext` at the root, context propagated down to `conn.Read`, `defer close(channel)` on the **producer** side.

**Rationale**: `log.Fatal` is `os.Exit(1)`, so no `defer` runs, nothing closes cleanly, and the exit code reports an error even though the shutdown was requested. Closing the channel belongs to the producer, which alone knows it will not write again. Closing from the consumer side panics on the next write.

**Measurable effect**: the cut becomes deterministic, which makes the deployment gap from [ADR 11](#11-strategy-recreate) short and predictable rather than incidental.

---

## 21. Dashboard export through the classic API only

**Problem**: dashboards exported from the Grafana 13 UI, including through the "classic" option, produce the V2 schema (`elements`, `layout`, `vizConfig`). Placed in a ConfigMap, they are **not loaded** by file provisioning, and no explicit error is raised.

**Decision**: export exclusively through `GET /api/dashboards/uid/<UID>` followed by `jq '.dashboard'`.

**Iteration workflow**: non-provisioned sandbox copy, free editing in the UI, API export, UID realignment, ConfigMap.

**Diagnosis**: telling a provisionable JSON apart comes down to markers. V1 exposes `panels` and `schemaVersion`, V2 exposes `elements`/`layout` or an `apiVersion`/`kind`/`metadata` envelope.

---

## 22. Dynamic PVC instead of a static `hostPath` PV

**First attempt**: a static `hostPath` PV with the `manual` StorageClass for the Prometheus TSDB. Result: crashloop, with the pod scheduled on `agent-1` while the volume physically sat on `server-0`.

**Cause**: a `hostPath` without node affinity carries no location information. The scheduler places the pod freely and the volume is simply not there.

**Decision**: a dynamic PVC on the `local-path` StorageClass in `WaitForFirstConsumer` mode. The order inverts: the pod is scheduled first, the PV is created on the chosen node with an injected node affinity, and that affinity is what pulls the pod back to the right place on later restarts.

**Validation**: destructive test (`kubectl delete pod`) produced `WAL replay completed` in the logs with data intact. Retention raised to 30d.

**Accepted consequence**: node affinity solves the scheduling conflict but pins each pod to one node for good. On the current two-agent cluster the Grafana and Prometheus volumes landed on different nodes, so losing an agent leaves its component `Pending` with a `volume node affinity conflict` event, rather than rescheduling elsewhere. Acceptable on a local cluster where data locality is the point of `local-path`. On a real cluster this is precisely the argument for network-attached storage, where the volume follows the pod instead of the reverse.

**Major consequence**: this date marks the **start of the real SLO window**. Before persistence, any 30 day SLO was fictional since a restart reset the history.

---

## 23. Grafana PVC and hybrid dashboard model

**Context**: ConfigMap provisioning makes dashboards read-only in the UI. Editing dashboard JSON by hand is impractical, which blocks any design iteration.

**Decision**: a `local-path` PVC on `/var/lib/grafana`, making the SQLite database persistent and therefore authoritative for dashboards created through the UI.

**Two sources of truth, explicitly accepted**:
- **provisioned** dashboards: authority is Git, read-only, reproducible
- **UI** dashboards: authority is SQLite, editable, not reproducible

**Exit rule**: a dashboard considered final is exported through the API ([ADR 21](#21-dashboard-export-through-the-classic-api-only)) and moves to the Git side. The PVC is a workspace, not permanent storage.

**Named risk**: without discipline on the exit rule, SQLite becomes unversioned state that nobody knows how to rebuild.

## 24. Ingress through a manually installed Traefik

**Context**: services were reachable through `kubectl port-forward` only, which is a per-developer, per-session workaround rather than an exposure model.

**Decision**: expose Prometheus and Grafana through Ingress resources, served by a Traefik installed from the upstream Helm chart with a versioned values file, and disable the Traefik bundled with k3s.

**Rationale**: nothing here is a technical necessity. The k3s-bundled Traefik serves Ingress correctly and would have required no installation step at all. Installing it manually buys two things instead: the chart version is pinned in the repository rather than tied to whatever k3s ships, and the values file becomes a reviewable artifact that falls inside the GitOps scope alongside the Grafana ConfigMaps. Setting up an ingress controller by hand is also the practice this project exists for.

**Primary driver**: <!-- pick one and delete the other
  version control and GitOps readiness; hands-on practice is a welcome side effect
  hands-on practice; version pinning is a welcome side effect
-->

**Discarded alternative, NodePort as an intermediate step**: exposing a NodePort first would have shown the raw mechanism, a port opened on every node and forwarded to the Service, before layering host-based routing on top. It was skipped deliberately. NodePort is not the target model, it would have been thrown away within the same session, and the concept is simple enough to be understood without being run. What is given up is seeing the plain Service-to-node-port mapping in isolation, before Traefik's routing hides it.

**Accepted downside**: one more component to install, upgrade and keep aligned with the cluster, where k3s offered a working default for free. The bundled Traefik remains a valid fallback and is documented as such in the README.

**Constraint carried over**: the k3d load balancer must map host port 80 at cluster creation, or the Traefik `LoadBalancer` Service stays unreachable regardless of how it was installed. This is a property of the k3d topology, not of the ingress controller.


---

## Conventions

Choices applied without a real trade-off, recorded for reference.

| Topic | Convention | Short rationale |
|---|---|---|
| Dev cluster | **k3d** | Certified Kubernetes, multi-node, quick to set up. kubeadm deferred to a dedicated CKA session |
| Registry | **ghcr.io**, versioned tag | The repository already lives on GitHub. A versioned tag is immutable, unlike `latest`, and will be extended with the commit SHA in CI/CD |
| Namespace | Single `trading-app` | Dev and prod are separated by cluster, not by namespace. Identical names everywhere keep GitOps overlays possible |
| Component split | One Deployment and one Service per component | Independent lifecycles. Multi-container pods are only justified under tight coupling (localhost or shared volume) |
| Naming | No version prefix or suffix | A resource name is a stable DNS address, the version lives in the image tag |
| Replication | `replicas: 1` | A single WebSocket cannot scale out without duplicating the feed, and sharding is unwarranted at a few hundred msg/s |
| WebSocket library | `coder/websocket` | Minimal API built on `context.Context`, consistent with the context propagation of [ADR 17](#17-graceful-shutdown-through-signalnotifycontext) |
| Latency metrics | Histograms | The only type from which percentiles can be derived. Low-latency work watches the high percentiles (p99, p99.9), not the average |
| Grafana runtime | `runAsUser: 472`, `readOnlyRootFilesystem: true` | UID imposed by the image. A read-only root filesystem requires covering every write path (`/var/lib/grafana`, `/tmp`) with volumes. Kubernetes applies nested mounts by path depth, so the dashboards ConfigMap is not masked by the parent emptyDir |
| Grafana datasource | Fixed `uid: prometheus`, FQDN URL | A generated UID changes on every redeploy and breaks dashboard references. The FQDN `prometheus.trading-app:9090` stays reachable from another namespace |