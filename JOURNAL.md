# Journal de progression — low-latency-observability
---

## 🧱 Architecture cible (briques à intégrer)

Vue d'ensemble des briques prévues. Coche au fur et à mesure.

- [ ] Appli Go low-latency, ingestion flux de marché réel + instrumentation latence
- [ ] Observabilité complète — Prometheus / Grafana / Loki / Tempo / OpenTelemetry / alerting
- [ ] SLO/SLI + error budgets + dashboards SLO
- [ ] Cluster Kubernetes (EKS)
- [ ] GitOps,  ArgoCD ou Flux
- [ ] IaC, Terraform (modules réutilisables)
- [ ] CI/CD, GitHub Actions
- [ ] Chaos / résilience, injection de pannes + post-mortems
- [ ] (Futur) Order book via stream `depth`
- [ ] (Futur) Replay pour benchmarks reproductibles
- [ ] (Futur) Kafka si besoin de découpler l'ingestion de plusieurs consommateurs downstream

---

## 🧭 Décisions techniques prises

| Date | Décision | Raison |
|------|----------|--------|
| 11/07/2026 | Source = WebSocket **Stream** Binance (pas REST, pas WS API) | Flux poussé (push) en temps réel, public/sans auth, connexion permanente. REST = pull/polling qu'on veut éviter ; WS API = requête/réponse (ordres), pas notre besoin |
| 11/07/2026 | Stream = `aggTrade` sur **BTCUSDT** | aggTrade = exécutions groupées par prix/côté sur un court instant en un message (moins de messages, chacun autonome avec event time). BTCUSDT = plus gros volume |
| 11/07/2026 | Latence mesurée en **local** (horloge unique) | Éviter la désynchro d'horloge PC vs Binance : on ne mesure que des segments internes à la machine |
| 11/07/2026 | `E` (event time Binance) = signal de **fraîcheur**, pas SLO | Donne un timestamp d'événement pour évaluer la fraîcheur de la donnée, mais inutilisable en SLO strict (désynchro horloge) |
| 11/07/2026 | Métriques latence = **histogrammes** | Pour sortir des percentiles. En low-latency on surveille les percentiles HAUTS (p99, p99.9) = les pires cas / queues de distribution |
| 11/07/2026 | lib WebSocket = **coder/websocket** | Plus récent, API moderne basée sur context.Context, minimaliste. (Note : gorilla n'est plus archivé, a repris sa maintenance) |
| 11/07/2026 | Stratégie backpressure = **drop** (pas block) | Latence bornée > exhaustivité. Channel plein → drop du message entrant + incrément compteur. Bloquer = zéro perte mais latence non bornée |
| 25/07/2026 | Dockerisation multi-stage (builder golang:1.26-alpine → scratch, CGO_ENABLED=0) | Image finale ~8.5 MB, binaire statique, surface d'attaque minimale. Copie manuelle des CA certs pour le TLS (wss://) car scratch est vide |
| 26/07/2026 | SLI matérialisés via recording rules Prometheus (fichiers versionnés) | Config as code : pérenne, voyage avec le repo, survivra à la migration K8s. Pré-calcul = moins de charge que recalcul à la volée dans Grafana |

---

## ✅ Étapes franchies

- [x] 11/07/2026 - Go installé (go1.26.5) après upgrade depuis la version apt périmée (1.18). Repo `low-latency-observability` initialisé. Lib WebSocket choisie (coder/websocket).
- [x] 12/07/2026 - Squelette v1 fonctionnel : connexion au stream aggTrade BTCUSDT + boucle Read + affichage des messages JSON bruts. Premier flux de marché temps réel reçu.
- [x] 18/07/2026 - Refactoring goroutine de lecture (readStream) + channel bufferisé []byte avec drop via select/default. Parsing JSON en struct AggTrade (champs typés, erreur vérifiée).
- [x] 19/07/2026 - Instrumentation latence : trois segments (parse, processing, pipeline) via HistogramVec + label stage. Counter messages_dropped_total. Endpoint /metrics + Prometheus scrape + Grafana heatmap fonctionnels. Chaîne complète Go → Prometheus → Grafana opérationnelle.
- [x] 25/07/2026 - Dockerisation multi-stage (scratch, ~8.5 MB). Stack complète en docker-compose (app + Prometheus + Grafana) via un seul `docker compose up --build`.
- [x] 26/07/2026 - SLO/SLI : deux recording rules (ingest:processed_ratio, ingest:pipeline_latency_p99) + une alerting rule (high_latency_pipeline_p99 à 2ms, for 10m). Cycle Inactive→Pending→Firing testé et validé.

---

## 🔨 En cours

**Étape actuelle :** SLO/SLI recording + alerting rules — complété ✅
**Prochain sujet :** Migration Kubernetes (k3s/kind local, pertinent CKA)

---

## 🔜 Prochaines étapes identifiées

- [x] Nettoyer le code : supprimer le log.Println(aggTrade) du processing (pollue la mesure), renommer le counter avec préfixe `ingest_`
- [x] Dockeriser l'appli Go elle-même
- [x] Définir les SLO/SLI formels sur la latence pipeline (ex. p99 < X µs)
- [ ] Migration Kubernetes : déployer la stack sur k3s ou kind (manifests, services, configmaps)
- [ ] Ajouter Alertmanager pour router les alertes (email/Slack), pertinent une fois sur K8s
- [ ] Dashboard SLO + error budget (une fois l'appli tournant en continu H24)
- [ ] Structurer le code : séparer en packages (ingestion, metrics, config)
- [ ] Logging structuré (JSON logs) pour intégration Loki future
- [ ] CI/CD : GitHub Actions (build, test, lint, push image)
- [ ] IaC : Terraform pour l'infra
- [ ] GitOps : ArgoCD ou Flux pour le déploiement
- [ ] Chaos / résilience : injection de pannes + post-mortems

---

## ❓ Points à revoir / questions ouvertes

- [x] Reformuler dans mes mots : pourquoi histogramme et pas gauge/counter
- [x] Corriger ma compréhension : ce sont les percentiles HAUTS (p99.9) qu'on traque, pas les bas
- [X] Vérifier la version min de Go exigée par coder/websocket
- [x] Créer le repo git distant sur GitHub (Rysekk) et pousser
- [x] Réfléchir : un SLO qu'on ne risque jamais de violer est-il utile ? Envisager seuil warning + critical
- [~] Apprendre PromQL — en cours (rate, histogram_quantile, recording/alerting rules acquis)
- [ ] Gérer le warmup Go (p99 plus élevé au démarrage), chauffer l'appli avant exposition ?

---

## 📚 Concepts appris (mémo perso)

- **Push vs Pull (WebSocket Stream vs REST)** : Le modèle Pull est utilisé avec les API REST. Ici, nous avons besoin d'un flux continu de données ; celles-ci sont donc poussées (Push) via un WebSocket Stream de Binance.
- **Désynchro d'horloge / pourquoi mesurer la latence en local** : L'horloge de Binance et celle du serveur ne sont pas forcément synchronisées, ce qui fausse la mesure du temps de récupération des données (et peut même parfois produire des valeurs négatives). C'est pourquoi nous mesurons uniquement la latence de notre pipeline local.
- **Histogramme + percentiles (p99.9) en low-latency** : En environnement low latency, on cherche à surveiller les pires cas. On s'intéresse donc aux percentiles les plus élevés (comme le p99.9), qui représentent les cas les plus rares, mais aussi les plus impactants.
- **Backpressure + arbitrage block vs drop** : Comme nous allons ingérer un grand volume de données, une forte backpressure peut apparaître. Si le buffer est plein, la goroutine se bloque et aucune nouvelle donnée n'est ingérée tant qu'il ne se vide pas. Dans notre cas, nous privilégions des données fraîches : il est donc préférable de supprimer (drop) certaines données plutôt que de bloquer entièrement le pipeline.
- **Channel Go**: Un channel go est un tuyaux qui sert de buffer pour un nombre données de valeur (ici 128) et qui traite les données en mode FIFO et qui bloque l'ecriture dedant quand le channel est plein et la lecture quand c'est vide.
- **Channel Go = file d'attente + synchronisation (bloque plein/vide)** : Mise en place d'un channel pour faire transitionné les données reçus de la websocket vers la pipeline de traitement. Nous utilisons un `select` avec `default` : si le buffer est plein, le message est drop plutôt que de bloquer la lecture et incrementé un compteur que l'on pourras utiliser comme metrique pour savoir si notre buffer est congestionné.