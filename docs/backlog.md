# Backlog

> Planned work and accepted debt. The current state of the project is described in the [README](../README.md).

## Finishing touches & Housekeeping

- [ ] Recalibrate Prometheus `requests.memory` against actual consumption observed since the PVC was introduced
- [ ] Measure the deployment gap with `rate(ingest_message_receive_total[1m])` before and after SIGTERM, to quantify the real cost of `Recreate` against the 43 min per month budget
- [ ] Watch `container_cpu_cfs_throttled_seconds_total`, which must stay at zero (see [ADR 12](decisions.md#12-burstable-qos))
- [ ] Clean up manifests: port naming, label consistency
- [ ] Make the `global` block explicit in `prometheus.yml`
- [ ] Fix typos and redundancies in ADRs 26, 27, and 28

## Observability hardening

- [ ] **SLI defect**: Capture the time spent in the channel buffer. Currently, `pipelineStart` begins upon channel exit, leaving the queue wait time unmeasured
- [ ] **Have Prometheus scrape itself**: `prometheus_tsdb_head_series`, RSS. An unobserved observability system is a blind spot
- [ ] **No-data alert**: `absent_over_time(ingest_message_receive_total[5m])`. This is the safety net that makes the trade-off in [ADR 11](decisions.md#11-strategy-recreate) defensible, since the gap is only acceptable if it is detected
- [ ] WebSocket connection state gauge and matching alert
- [ ] Liveness probe on WebSocket health. Note that `/metrics` must stay scrapable at all times, including during degradation
- [ ] Alert on pods stuck in `Pending`, which is how a node loss surfaces once volumes are pinned by node affinity. A component that never schedules produces no metrics at all, so the no-data alert catches the symptom without naming the cause
- [ ] Alertmanager for alert routing (email or Slack)
- [ ] SLO and error budget burn dashboard, which requires the application running continuously
- [ ] Structured JSON logging, in preparation for Loki

## Go application, v0.3

- [ ] Retry with exponential backoff on WebSocket reconnection
- [ ] Explicit `http.Server` with `Shutdown(ctx)`, aligning metrics server shutdown with ingestion shutdown
- [~] Split into packages: `ingestion`, `metrics`, `config`

## Platform

- [~] **Infrastructure & Provisioning**: Oracle VM via Terraform — network + A1 node provisioned, SSH reachable. Remaining: k3s via cloud-init, refactor into reusable modules
- [ ] **Deployment**: Kustomize strategy with dev/prod overlays
- [ ] **CI/CD**: Multi-arch image builds (amd64 / arm64) — the current amd64 scratch image will fail on arm64 nodes
- [ ] **Security**: TLS + private exposure for Grafana and Prometheus
- [ ] **Chaos and resilience**: Fault injection and post-mortems. The node affinity crashloop from [ADR 22](decisions.md#22-dynamic-pvc-instead-of-a-static-hostpath-pv) is a first case worth writing up

## Direction

- [ ] **Living substrate**: extend ingestion to multiple venues and add feed quality SLIs (gaps, out-of-order sequences, clock skew). The value comes from accumulation — months of continuous data and real post-mortems, not from novelty
- [ ] **Binary exchange protocols**: implement an SBE decoder in Go, run it head-to-head with the JSON parser on the same feed, and publish the p99.9 parse latency delta. Check Binance's SBE market data availability in their API docs first
- [ ] **Host tail latency engineering**: quantify the effect of `GOGC`, `GOMAXPROCS`, CPU pinning, `isolcpus`, network IRQ affinity and busy polling on the measured p99.9. Inject network jitter with `tc netem` and CPU contention, and tie the impact back to the SLO with a written post-mortem. Extends [ADR 12](decisions.md#12-burstable-qos)


## Accepted debt

Out of scope for now, listed to remove any ambiguity.

| Debt | Scope |
|---|---|
| Use GITHUB_TOKEN for no infinite loop | Do not replace the GITHUB_TOKEN in the pipeline with a PAT Token otherwise everything break |
| Prometheus without access control | Routed by the Ingress, but not protected |
| Plain HTTP, no TLS | Local exposure only |
| Grafana in anonymous-admin mode | No authentication |
| Local k3d cluster | No cloud deployment |

## Open questions

- [ ] **Go warmup**: p99 is higher at startup (cold caches, GC ramp-up). Should the application be warmed before being exposed to scraping, or excluded from the SLO over an initial window?
- [~] **PromQL**: `rate`, `histogram_quantile`, recording and alerting rules are covered. Still to explore: long-window aggregations for error budget computation

---

*Upkeep rule: a finished step gets ticked here and is removed. A non-obvious decision gets an entry in [`decisions.md`](decisions.md). Nothing else needs updating.*