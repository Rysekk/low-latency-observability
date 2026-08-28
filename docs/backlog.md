# Backlog

> Planned work and accepted debt. The current state of the project is described in the [README](../README.md).

## Kubernetes finishing touches

- [ ] Recalibrate Prometheus `requests.memory` against actual consumption observed since the PVC was introduced
- [ ] Measure the deployment gap with `rate(ingest_message_receive_total[1m])` before and after SIGTERM, to quantify the real cost of `Recreate` against the 43 min per month budget
- [ ] Watch `container_cpu_cfs_throttled_seconds_total`, which must stay at zero (see [ADR 12](decisions.md#12-burstable-qos))
- [ ] Clean up manifests: port naming, label consistency
- [ ] Make the `global` block explicit in `prometheus.yml`

## Observability hardening

- [ ] **Have Prometheus scrape itself**: `prometheus_tsdb_head_series`, RSS. An unobserved observability system is a blind spot
- [ ] **No-data alert**: `absent_over_time(ingest_message_receive_total[5m])`. This is the safety net that makes the trade-off in [ADR 11](decisions.md#11-strategy-recreate) defensible, since the gap is only acceptable if it is detected
- [x] `ingest_parse_errors_total` counter plus `continue` after a parse error
- [ ] WebSocket connection state gauge and matching alert
- [ ] Liveness probe on WebSocket health. Note that `/metrics` must stay scrapable at all times, including during degradation
- [ ] Alert on pods stuck in `Pending`, which is how a node loss surfaces once volumes are pinned by node affinity. A component that never schedules produces no metrics at all, so the no-data alert catches the symptom without naming the cause
- [ ] Alertmanager for alert routing (email or Slack)
- [ ] SLO and error budget burn dashboard, which requires the application running continuously
- [ ] Structured JSON logging, in preparation for Loki

## Go application, v0.3

- [ ] Retry with exponential backoff on WebSocket reconnection
- [ ] Explicit `http.Server` with `Shutdown(ctx)`, aligning metrics server shutdown with ingestion shutdown
- [ ] Version Go v0.3 with processing : conversion string→number of Price and Quantity for the first step of the processing stage, implies a choice between 2 type : float64 or number with exact decimal (ADR).Adds a second parse-error path to count.
- [~] Split into packages: `ingestion`, `metrics`, `config`

## Platform

- [ ] **CI/CD with GitHub Actions**: build, test, lint, push image. Replace the manual tag with the commit SHA to tie image to exact code
- [ ] **IaC with Terraform**: reusable modules
- [ ] **GitOps** with ArgoCD or Flux. The Grafana ConfigMaps (datasource, provider, dashboards) fall inside the synced scope
- [ ] **Chaos and resilience**: fault injection and post-mortems. The node affinity crashloop from [ADR 22](decisions.md#22-dynamic-pvc-instead-of-a-static-hostpath-pv) is a first case worth writing up
- [ ] `kubernetes_sd_config` with RBAC, replacing `static_configs`
- [ ] `imagePullSecret` exercise, by switching the package back to private
- [ ] EKS migration

## Accepted debt

Out of scope for now, listed to remove any ambiguity.

| Debt | Scope |
|---|---|
| Prometheus without access control | Routed by the Ingress, but not protected |
| Plain HTTP, no TLS | Local exposure only |
| Grafana in anonymous-admin mode | No authentication |
| Local k3d cluster | No cloud deployment |

## Open questions

- [ ] **Go warmup**: p99 is higher at startup (cold caches, GC ramp-up). Should the application be warmed before being exposed to scraping, or excluded from the SLO over an initial window?
- [~] **PromQL**: `rate`, `histogram_quantile`, recording and alerting rules are covered. Still to explore: long-window aggregations for error budget computation

---

*Upkeep rule: a finished step gets ticked here. A non-obvious decision gets an entry in [`decisions.md`](decisions.md). Nothing else needs updating.*