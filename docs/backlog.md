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

- [ ] **Reserved public IP for Oracle A1**: the ephemeral IP changes on stop/start and breaks the tunnel endpoint; a reserved IP fixes it
- [ ] **Oracle load balancer**: replace the Traefik NodePort ([ADR 32](decisions.md#32-traefik-exposed-as-a-nodeport)). Triggered by the first service that has to be public, not before
- [ ] **Ingresses stay `Progressing` in ArgoCD**: with a NodePort, Traefik publishes no `ADDRESS`, so Ingress health never settles. Resolved by the load balancer, or earlier by a health override (`resource.customizations` in `argocd-cm`) or Traefik's `ingressEndpoint`
- [ ] **OCI Cloud Controller Manager**: prerequisite for the load balancer, lets `type: LoadBalancer` Services provision an Oracle LB instead of staying `pending`
- [ ] **OCI Vault for the WireGuard key**: move the server private key out of the Standard bucket for encryption, rotation and audit ([ADR 31](decisions.md#31-private-access-through-wireguard-server-key-in-object-storage))
- [ ] **Persistent iptables**: the UDP 51820 rule is inserted by `wg-key.sh`, which cloud-init runs once per instance, so a reboot of the same VM loses it. Save it with `netfilter-persistent` (or a systemd unit)
- [ ] **A1 capacity resilience**: `terraform apply` fails with "Out of host capacity" on Always Free A1 and is currently retried in a loop. Options: try several availability domains, fall back to a smaller shape, or keep the instance alive (no destroy on change)
- [ ] **Pin cloud-init to a Git ref**: currently clones `main` at HEAD, non-reproducible
- [ ] **Hybrid multi-provider cluster over WG**: remote node via tunnel, latency-critical pods pinned to Oracle (see ADR 30)
- [ ] **NATS/JetStream between ingest and processing**: gated on measured `processing` saturation. Does not lift single-WebSocket constraint
- [ ] **Terraform state** remote encrypted Terraform state
- [ ] **Dev environment**: prod-only for now. Rebuild a local dev cluster on home-lab hardware when available; `deploy/compose/` remains for app-level iteration
- [ ] **Deployment**: Kustomize dev/prod overlays, once a dev environment exists again
- [ ] **Security**: TLS for Grafana, Prometheus and ArgoCD. Low priority: private exposure is done and transport is already encrypted by the WireGuard tunnel
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
| Plain HTTP, no TLS | Reachable through the WireGuard tunnel only. ArgoCD runs with `server.insecure: true` |
| Grafana in anonymous-admin mode | No authentication |

## Open questions

- [ ] **Go warmup**: p99 is higher at startup (cold caches, GC ramp-up). Should the application be warmed before being exposed to scraping, or excluded from the SLO over an initial window?
- [~] **PromQL**: `rate`, `histogram_quantile`, recording and alerting rules are covered. Still to explore: long-window aggregations for error budget computation

---

*Upkeep rule: a finished step gets ticked here and is removed. A non-obvious decision gets an entry in [`decisions.md`](decisions.md). Nothing else needs updating.*