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
| [22](#22-dynamic-pvc-instead-of-a-static-hostpath-pv) | Aug 11 | Dynamic PVC instead of static PV |
| [23](#23-grafana-pvc-and-hybrid-dashboard-model) | Aug 11 | Grafana PVC and hybrid model |
| [24](#24-ingress-through-a-manually-installed-traefik) | Aug 15 | Ingress through a manually installed Traefik |
| [25](#25-convert-string-to-decimaldecimal-instead-of-float64) | Aug 29 | `decimal.Decimal` instead of `float64` |
| [26](#26-argocd-over-flux) | Sep 05 | ArgoCD over Flux |
| [27](#27-sha-tag-ci-over-argo-image-updater) | Sep 05 | SHA tag: CI over Argo Image Updater |
| [28](#28-app-of-apps-vs-manual-application) | Sep 05 | App-of-apps |
| [29](#29-treafik-under-argocd) | Sep 05 | Traefik under ArgoCD |
| [30](#30-single-node-k3s-in-production) | Sep 19 | Single-node k3s in production |
| [31](#31-private-access-through-wireguard-server-key-in-object-storage) | Oct 10 | Private access through WireGuard |
| [32](#32-traefik-exposed-as-a-nodeport) | Oct 18 | Traefik as NodePort |
| [33](#33-argocd-manages-itself) | Oct 18 | ArgoCD manages itself |
| [34](#34-observability-architecture-prometheus-owns-metrics-opentelemetry-owns-logs-and-traces) | Oct 24 | Prometheus owns metrics, OpenTelemetry owns logs |
| [35](#35-observability-stack-co-located-with-the-ingestor) | Oct 24 | Observability stack co-located with the ingestor |
| [36](#36-the-slo-measures-pipeline-latency-including-queue-wait) | Oct 24 | The SLO measures pipeline latency, including queue wait |
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

**Exit rule**: a dashboard considered final is exported through the classic API (see *Dashboard export* in [Conventions](#conventions)) and moves to the Git side. The PVC is a workspace, not permanent storage.

**Named risk**: without discipline on the exit rule, SQLite becomes unversioned state that nobody knows how to rebuild.

## 24. Ingress through a manually installed Traefik

> Superseded by [ADR 29](#29-treafik-under-argocd) (installation) and [ADR 32](#32-traefik-exposed-as-a-nodeport) (exposure).

**Context**: services were reachable through `kubectl port-forward` only, which is a per-developer, per-session workaround rather than an exposure model.

**Decision**: expose Prometheus and Grafana through Ingress resources, served by a Traefik installed from the upstream Helm chart with a versioned values file, and disable the Traefik bundled with k3s.

**Rationale**: nothing here is a technical necessity. The k3s-bundled Traefik serves Ingress correctly and would have required no installation step at all. Installing it manually buys two things instead: the chart version is pinned in the repository rather than tied to whatever k3s ships, and the values file becomes a reviewable artifact that falls inside the GitOps scope alongside the Grafana ConfigMaps. Setting up an ingress controller by hand is also the practice this project exists for.


**Discarded alternative, NodePort as an intermediate step**: exposing a NodePort first would have shown the raw mechanism, a port opened on every node and forwarded to the Service, before layering host-based routing on top. It was skipped deliberately. NodePort is not the target model, it would have been thrown away within the same session, and the concept is simple enough to be understood without being run. What is given up is seeing the plain Service-to-node-port mapping in isolation, before Traefik's routing hides it.

**Accepted downside**: one more component to install, upgrade and keep aligned with the cluster, where k3s offered a working default for free. The bundled Traefik remains a valid fallback and is documented as such in the README.

**Constraint carried over**: the k3d load balancer must map host port 80 at cluster creation, or the Traefik `LoadBalancer` Service stays unreachable regardless of how it was installed. This is a property of the k3d topology, not of the ingress controller.

## 25. Convert string to decimal.Decimal instead of float64.

**Context**: We need to preserve maximum precision for Price and Quantity values since this is a market data application.

**Decision**: Since this is a market data application, we decided to preserve maximum precision for Price and Quantity values. To achieve this, we use shopspring/decimal to convert strings to decimal.Decimal.

**Rationale**: Even though we know this may introduce some additional latency because decimal.Decimal uses big.Int, which is slower and consumes more resources than float64 (not yet measured), this project is focused on monitoring latency. However, we also want to reflect what is used in real-world market data applications. We are conscious that in a real HFT environment, nanosecond-level performance gains may be considered more important than decimal precision.

**Discarded alternatives**: We did not choose float64 with strconv.ParseFloat because we want to avoid losing precision during the conversion. float64 cannot represent all decimal values exactly (e.g. 0.1 + 0.2 != 0.3), and rounding errors can accumulate over multiple trading operations. We also considered using scaled integers as a fixed-point representation. While this approach could provide exact decimal representation with better performance than decimal.Decimal, it would require us to implement and carefully manage the arithmetic operations ourselves. For example, multiplying two scaled values requires correctly handling the resulting scale and rescaling the result. We preferred to use a library that already manages these operations and the associated edge cases rather than implementing and maintaining our own decimal arithmetic.

**Accepted downside**: decimal.Decimal is expected to be slower and consume more resources than float64, so we expect it to have an impact on latency. The actual impact has not yet been measured. We also introduce an additional dependency by using shopspring/decimal, which needs to be maintained and kept up to date, whereas using the native float64 and strconv packages would introduce no external dependencies.

## 26. ArgoCD over Flux.

**Context**: The cluster needs a GitOps controller so that Git is the only write path. The two CNCF graduated options are ArgoCD and Flux.

**Decision**: We decided to use ArgoCD instead of Flux, even though Flux is more lightweight.

**Rationale**: We chose ArgoCD because it is more widely used in enterprise environments, which better aligns with the goal of this project.

**Discarded alternatives**: We considered Flux even if it is lightweight and closer to the Unix spirit but less visible in enterprise, but decided not to use it because ArgoCD is more widely adopted in enterprise environments and makes more sense in a portfolio application.

**Accepted downside**: ArgoCD is heavier than Flux, consumes more resources, and introduces additional components such as a UI and CRDs.

## 27. SHA tag: CI over Argo Image Updater

**Context**: When building the Docker image and publishing it to GHCR, we need to tag the image with the SHA of the commit.

**Decision**: We decided to use the CI pipeline to tag the Docker image with the commit SHA.

**Rationale**: We chose the CI because it is more lightweight than Argo Image Updater. The image is built and published in the CI pipeline when a commit is made, so there is no asynchronous process that could cause conflicts or inconsistencies in the repository.

**Discarded alternatives**: Argo Image Updater is useful because it separates image publishing from the deployment process. However, for a small project like this, it adds unnecessary complexity and resource consumption. Using the CI pipeline is sufficient for our needs.

**Accepted downside**: Using the CI pipeline couples the image build and tagging process to the CI workflow using the GITHUB_TOKEN do not trigger the CI in a infinit loop. A more advanced setup could separate these responsibilities using Argo Image Updater, but this is not necessary for the current scope of the project.

## 28. App-of-apps vs manual Application

**Context**: We initially discarded the App-of-Apps pattern because of its impact on application visibility. As the GitOps setup evolved, we decided to prioritize having the whole ArgoCD configuration managed through Git.

**Decision**: We decided to use the App-of-Apps pattern to manage ArgoCD Applications.

**Rationale**: App-of-Apps allows all Applications to be declared and managed from Git, which is more consistent with our GitOps approach and avoids manually creating Applications in ArgoCD

**Discarded alternatives**: Manually managing each Application would provide better visibility and would be simpler for a small number of applications, but it would introduce manual configuration outside Git.

**Accepted downside**: App-of-Apps adds a level of indirection and cascading deletion (pruning the root can uninstall a component) instead to managing Applications individually. If the number of Applications grows, we may consider using ApplicationSet instead.

## 29. Treafik under ArgoCD

**Context**: We need to deploy Traefik as the ingress controller for the Kubernetes cluster. We considered deploying it through an imperative bootstrap process or managing it through ArgoCD.

**Decision**: We decided to manage Traefik through ArgoCD as an Application, using a multi-source configuration with $values.

**Rationale**: We chose to manage Traefik through ArgoCD to keep the cluster configuration declarative and managed through GitOps. This avoids having part of the cluster infrastructure managed outside ArgoCD and keeps the deployment configuration versioned in Git.

**Discarded alternatives**: We considered bootstrapping Traefik imperatively before ArgoCD. This would make Traefik available earlier in the cluster bootstrap process, but would introduce a manually managed component outside the GitOps workflow.

**Accepted downside**: Traefik is not available until the ArgoCD root Application has been synchronized and the Traefik Application has been deployed. This introduces an ordering dependency during the initial bootstrap: ArgoCD must be available and synchronized before Traefik can provide ingress.

## 30. Single-node K3s in production

**Context**: K3s can be installed on a single VM or on multiple VMs to provide high availability. We need to choose between these two architectures.

**Decision**: We decided to go with a single-node installation.

**Rationale**: We chose this architecture because the project is primarily focused on application latency and observability. Our SLOs and SLI are focused on latency rather than availability see [ADR 11](#11-strategy-recreate).

**Discarded alternatives**: We considered splitting the available resources (2 CPU / 12 GB RAM) across two separate VMs. However, since both VMs would run on the same hypervisor, this would not provide meaningful additional availability. If the hypervisor goes down, both VMs would be unavailable.

A multi-provider setup could provide better fault isolation, but it would require additional networking configuration, such as WireGuard, and would introduce additional network latency. This would be counterproductive for a project focused on measuring application latency. Adding remote worker nodes later is deferred, and tracked in the backlog; it depends on WireGuard and must not place latency-critical pods off the Oracle node.

**Accepted downside**: The single-node architecture does not provide high availability for our application and Kubernetes workloads. A failure of the VM or the underlying hypervisor would make the entire cluster unavailable.

## 31. Private access through WireGuard, server key in Object Storage

**Context**: The prod cluster runs on a public Oracle VM. Grafana, Prometheus, the ArgoCD UI and the Kubernetes API have no authentication worth exposing ([Accepted debt](backlog.md#accepted-debt)), and the VM is rebuilt with `terraform apply` whenever cloud-init changes. Admin access must stay private and survive a rebuild without touching the laptop config.

**Decision**: A WireGuard tunnel (`10.8.0.1` server, `10.8.0.2` laptop) is the only path to the services. The OCI security list opens UDP 51820 to `my_ip` only, and SSH goes through the tunnel like everything else. Server side, the laptop peer is pinned to `AllowedIPs = 10.8.0.2/32`, so the tunnel accepts a single source address. On first boot, `wg-key.sh` fetches the server private key from the `bucket-wireguard` bucket, or generates and uploads it if absent. The VM authenticates with **instance principals**: a dynamic group matching the instances of the compartment, and a policy limited to `OBJECT_CREATE`/`READ`/`INSPECT` on `wg/*` in that bucket.

**Rationale**: The key outliving the VM is what makes rebuilds cheap: the server public key is stable, so the laptop peer never changes. Instance principals avoid shipping an API key or a secret in cloud-init, and the policy scope means a compromised VM can only read and write the WireGuard objects. One UDP port is a smaller surface than one port per service, and WireGuard does not answer unauthenticated packets.

**Discarded alternatives**:
- *Exposing the services publicly behind basic auth or TLS*: one exposed port and one credential per service, and the auth debt has to be paid before anything is reachable.
- *SSH port-forwards*: work, but one forward per service and per session, nothing persistent.
- *Generating a new key on every boot*: no storage needed, but every rebuild breaks the laptop config.
- *OCI Vault for the key*: the right home for a secret (encryption, rotation, audit), but more IAM and API surface than a single key justifies today. Tracked in the backlog.
- *Tailscale or another mesh*: less to configure, but a third-party control plane, and the point is to learn the mechanism.

**Accepted downside**: The key sits in a Standard bucket protected only by IAM, without Vault-level encryption or rotation. The tunnel is a single point of access: if `my_ip` changes or the public IP changes on stop/start, access is lost until Terraform is re-applied. The `iptables` rule opening UDP 51820 is inserted by `wg-key.sh`, which cloud-init runs once per instance (`runcmd`), not on every boot. A fresh instance always gets it, but a reboot of the same instance loses it and cuts the tunnel.

## 32. Traefik exposed as a NodePort

> Supersedes the `LoadBalancer` exposure model of [ADR 24](#24-ingress-through-a-manually-installed-traefik).

**Context**: In k3d, the Traefik `LoadBalancer` Service was served by k3d's load balancer. On the Oracle VM, k3s runs with `servicelb` disabled and there is no Oracle Cloud Controller Manager, so nothing provisions an external IP: the Service stayed `<pending>` forever and nothing listened on port 80.

**Decision**: Traefik runs as a `NodePort` Service, `web` on **30080** and `websecure` on **30443** (`deploy/helm/traefik-values.yaml`). Services are reached through the tunnel at `http://<host>.local:30080`.

**Rationale**: NodePort needs nothing outside the cluster. All access already goes through WireGuard ([ADR 31](#31-private-access-through-wireguard-server-key-in-object-storage)), so a public load balancer has nothing to serve yet, and fixed node ports keep the URLs stable across redeploys.

**Discarded alternatives**:
- *Keeping `LoadBalancer` without a CCM*: stays `pending`, never reachable.
- *Re-enabling k3s `servicelb` (Klipper)*: binds host ports 80/443 and works on a single node, but it is a k3s-only mechanism that does not transfer to other clusters, and it would have to be undone the day a real load balancer arrives.
- *An Oracle load balancer with the OCI CCM*: the production model, but it costs a public IP and a listener for a service with no public user yet. Deferred until a service has to be public.
- *`hostNetwork` / `hostPort` on 80*: removes the `:30080` from URLs, but ties the Traefik pod to the node's network namespace.

**Accepted downside**: URLs carry a non-standard port, which is easy to forget (a wrong port looks like an unreachable cluster). Moving to a load balancer later means changing the Service type and the access documentation. Without an external IP on the Traefik Service, Ingresses never get an `ADDRESS`, so ArgoCD reports every Ingress as `Progressing` permanently, which hides a real health change. Publishing a fixed address through Traefik's `ingressEndpoint` would mask it, and is not applied.

## 33. ArgoCD manages itself

**Context**: `make up` installs ArgoCD with Helm, then hands over to the app-of-apps. After that, any change to ArgoCD itself (values, chart version, Ingress) meant a manual `helm upgrade`, outside Git and outside the "Git is the only write path" rule.

**Decision**: An `argocd` Application in `deploy/argocd/apps/` points at the same `argo-cd` chart (`10.4.2`) and release name, with `deploy/helm/argocd-values.yaml` through `$values`. ArgoCD adopts the objects created by the bootstrap release and reconciles them with `prune` and `selfHeal`. The UI is exposed through an Ingress on `argocd.local`, with `server.insecure: true` because TLS is not terminated anywhere yet.

**Rationale**: It closes the last component managed outside Git. Upgrading ArgoCD becomes a commit, like everything else, and the bootstrap stays a minimal one-off. It is the same multi-source pattern as Traefik ([ADR 29](#29-treafik-under-argocd)).

**Discarded alternatives**:
- *Helm upgrades from the Makefile*: simple and explicit, but imperative and invisible in the ArgoCD view.
- *A separate management cluster running ArgoCD*: the clean answer at scale, unjustified for one cluster.

**Accepted downside**: ArgoCD can break itself: a bad value or chart bump can take down the controller that would revert it, and recovery goes back through `make up` or a manual `helm`. The bootstrap command and the Application must stay on the same chart version, or the first sync triggers an upgrade. The UI runs over plain HTTP, acceptable only because it is reachable through the tunnel alone.

## 34. Observability architecture: Prometheus owns metrics, OpenTelemetry owns logs and traces

**Context**: Prometheus only scraped the ingestor. Cluster signals were missing although the backlog depends on them (pods stuck in `Pending`, CFS throttling). Logs lived only in `kubectl logs`, which keeps one previous container and loses everything on a rebuild. Alerts fired into the Prometheus UI with nobody notified. The open question was whether OpenTelemetry should become the single collection layer.

**Decision**:
- *Metrics*: Prometheus pulls everything: the ingestor, kube-state-metrics and node-exporter (Helm charts as ArgoCD Applications), kubelet, cAdvisor and CoreDNS through `kubernetes_sd_configs`, annotated pods, and the observability stack itself. The ingestor keeps the Prometheus Go client, no OTel SDK.
- *Logs*: an OpenTelemetry Collector (contrib) DaemonSet reads the `trading-app` pod logs (`filelog`) and Kubernetes events (`k8sobjects`), and exports them to Loki over OTLP. Loki runs as a single binary on a `local-path` PVC, 7 days retention. The ingestor logs JSON through `log/slog`, errors and lifecycle events only, never one line per message.
- *Traces*: none for now. The same Collector receives OTLP traces when they exist.
- *Alerting*: Prometheus rules only, routed by Alertmanager to a single receiver. Loki serves investigation, no ruler.

**Rationale**: Pull gives `up` for every target, which the no-data alert relies on; with push, a silent target looks the same as absent data. The SLO path stays app → Prometheus, so a Collector failure costs logs, not the SLO. The Prometheus client costs a few atomic adds per observation in the measured path; the OTel metrics SDK does more work per record and would need its own benchmark to earn its place. The exporters already speak the Prometheus format, so a Collector in front of them is a hop for nothing. Logs need an agent anyway, and the Collector is the vendor-neutral one that will also carry traces: one agent for both. Anything worth alerting on in the logs becomes a counter in the app, as `ingest_json_parse_errors_total` already is.

**Discarded alternatives**:
- *Every signal through OpenTelemetry* (Collector scrapes, remote-writes to Prometheus): one pipeline and shared resource attributes, but an extra hop in the SLO path and pull semantics rebuilt with `absent()`. To revisit with a second metrics backend, a second cluster or a push-only source.
- *kube-prometheus-stack*: the Operator and ServiceMonitors fit many apps owning their own scrape config. Here it would replace a documented hand-written Prometheus, cost memory on a 12 GB node and hide the scrape config being learned.
- *Grafana Alloy*: simpler with Loki, but Grafana-specific. *Promtail*: end of life. *Fluent Bit*: light, no path to traces.
- *Loki on Object Storage*: survives rebuilds, at the cost of a bucket, a credential and more config, for logs that are only lost on planned rebuilds.
- *Tracing now*: one process, no network hop, per-stage latency already in the histogram. A span per trade adds work in the measured path. To revisit at the first hop between two processes.

**Accepted downside**: Two collection mechanisms to run instead of one. Logs and metrics are correlated through aligned labels (`namespace`, `pod`, `container`), not shared resource attributes. A rebuild (`preserve_boot_volume = false`) wipes Loki the same way it wipes Prometheus. Prometheus needs cluster-wide read RBAC to discover nodes and pods.

## 35. Observability stack co-located with the ingestor

**Context**: The Oracle Always Free A1 allowance was cut in 2026 to 1,500 OCPU-hours and 9,000 GB-hours per month, i.e. 2 OCPU and 12 GB, which the single VM already uses ([ADR 30](#30-single-node-k3s-in-production)). [ADR 34](#34-observability-architecture-prometheus-owns-metrics-opentelemetry-owns-logs-and-traces) adds kube-state-metrics, node-exporter, the Collector, Loki and Alertmanager on the node that runs the ingestor, whose CPU request is 50m with no limit ([ADR 12](#12-burstable-qos)).

**Decision**: The observability stack shares the node, with guardrails: every new component declares CPU and memory requests and a memory limit; the ingestor gets a higher `PriorityClass`, so monitoring pods are evicted first under pressure; ingestor p99 and `container_cpu_cfs_throttled_seconds_total` are measured before and after the rollout.

**Rationale**: No second node exists within the free tier, and exceeding the allowance disables then deletes the A1 instances. Splitting the same 2 OCPU / 12 GB into two VMs gives isolation on paper but smaller nodes on the same hypervisor (ADR 30). Measuring the delta turns co-location from an assumption into a number tied to the SLO.

**Discarded alternatives**:
- *A dedicated monitoring node*: real isolation, outside the free tier, or remote over WireGuard (backlog), where monitoring would observe through a tunnel.
- *Two 1 OCPU / 6 GB VMs*: same total, same hypervisor, weaker nodes.
- *No Loki*: drops the heaviest component, and the logs decided in ADR 34 with it.

**Accepted downside**: Monitoring competes with the ingestor for CPU and memory bandwidth, so a Loki or Prometheus compaction can show up in the very p99 it measures. Monitoring is evicted first, so under memory pressure the stack goes blind exactly when something is wrong.

**To watch**: p99 delta before and after, `container_cpu_cfs_throttled_seconds_total` at zero, node memory headroom.

## 36. The SLO measures pipeline latency, including queue wait

**Context**: The `pipeline` stage starts its timer when a message leaves the buffer. Time spent waiting in the buffer is not measured, so under backpressure the SLI stays low while messages queue up, and the buffer can fill and drop messages with the SLO still green. The histogram's last finite bucket is 2.92 ms (`ExponentialBuckets(10µs, 1.5, 15)`): enough for processing time, which stays well under 2 ms, but not for queue wait.

**Decision**:
- The SLO is defined on pipeline latency: from the moment `conn.Read` returns to the end of trade conversion. The read timestamp (`time.Time`) travels with the message through the buffer.
- Stages: `queue` (read → buffer exit), `parse`, `processing` (conversion), and `pipeline` = queue + parse + processing. The SLO is computed on `pipeline`.
- Buckets are explicit: 10µs, 25µs, 50µs, 100µs, 250µs, 500µs, 1ms, 2ms, 5ms, 10ms, 25ms, 50ms, 100ms. The alert thresholds (0.5 ms and 2 ms) are bucket bounds, so counting slow trades needs no interpolation, and the range up to 100 ms shows the real size of a backlog.
- The 2 ms page and 0.5 ms warning thresholds are kept, and re-evaluated once the baseline is recorded.

**Rationale**: The SLO must degrade when the service falls behind, which is the failure it exists to catch. Both timestamps come from the process's monotonic clock (`time.Since` on a `time.Time` from `time.Now()`), so an NTP adjustment cannot distort a measurement, consistent with [ADR 3](#3-e-binance-event-time-as-a-freshness-signal-not-an-slo-basis).

**Out of scope**: Time spent in the kernel TCP buffer before `conn.Read` returns, and network time from Binance, are not covered. `E` watches them as a freshness signal (ADR 3). Dropped messages never reach `pipeline`, so latency describes only the trades that survived; the admission and processed ratios cover the loss ([ADR 6](#6-backpressure-drop-rather-than-block)).

**Discarded alternatives**:
- *Keeping processing time as the SLI*: measures how fast the code runs, not how late the trade is, and is blind to backpressure.
- *A queue depth gauge* (`len(channel)`): cheaper, but it counts waiting messages instead of measuring how long they wait. A latency SLO needs a time.
- *`now() - E`*: mixes latency with clock skew, rejected in ADR 3.

**Accepted downside**: The SLI history before the change is not comparable with the history after it. The channel element becomes a struct (slice header + `time.Time`, about 48 bytes) passed by value: a larger copy, 0 allocs/op, about +30 ns per message on the micro-benchmark, mostly time.Now(), against ~100 µs of queue wait.

**Validation**: Deployed Oct 24. Production p99 pipeline ≈ 0.25 ms, mostly queue wait. Keeping the metric name while changing the bucket layout mixed both layouts in `rate[5m]` for 5 minutes per image change, which produced a false p99 of 100 ms (the last bucket). A rollback added two more windows. Cost: 12 bad minutes recorded, ~28% of the 30-day budget.

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
| WebSocket library | `coder/websocket` | Minimal API built on `context.Context`, consistent with the context propagation of the *Graceful shutdown* convention |
| Latency metrics | Histograms | The only type from which percentiles can be derived. Low-latency work watches the high percentiles (p99, p99.9), not the average |
| Grafana runtime | `runAsUser: 472`, `readOnlyRootFilesystem: true` | UID imposed by the image. A read-only root filesystem requires covering every write path (`/var/lib/grafana`, `/tmp`) with volumes. Kubernetes applies nested mounts by path depth, so the dashboards ConfigMap is not masked by the parent emptyDir |
| Grafana datasource | Fixed `uid: prometheus`, FQDN URL | A generated UID changes on every redeploy and breaks dashboard references. The FQDN `prometheus.trading-app:9090` stays reachable from another namespace |
| Graceful shutdown | `signal.NotifyContext` on SIGINT/SIGTERM, the producer closes the channel | `log.Fatal` skips `defer`. Only the producer knows it will not write again, so closing from the consumer panics |
| Dashboard export | Classic API only: `GET /api/dashboards/uid/<UID>` then `jq '.dashboard'`. Never "Export for sharing externally" | UI exports produce the V2 schema, and external exports leave `${DS_*}` inputs. File provisioning resolves neither, silently |
| Metric naming | New meaning or new buckets → new metric name | Old and new series never mix in one query, and the history break is visible |