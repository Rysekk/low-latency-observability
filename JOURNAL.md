# Journal de progression — low-latency-observability
---

## 🧱 Architecture cible (briques à intégrer)

Vue d'ensemble des briques prévues. Coche au fur et à mesure.

- [ ] Appli Go low-latency, ingestion flux de marché réel + instrumentation latence
- [ ] Observabilité complète — Prometheus / Grafana / Loki / Tempo / OpenTelemetry / alerting
- [ ] SLO/SLI + error budgets + dashboards SLO
- [ ] Cluster Kubernetes (k3s/kind local → EKS ?)
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

---

## ✅ Étapes franchies

- [x] 11/07/2026 — Go installé (go1.26.5) après upgrade depuis la version apt périmée (1.18). Repo `low-latency-observability` initialisé. Lib WebSocket choisie (coder/websocket).

---

## 🔨 En cours

**Étape actuelle :** Mise en place du squelette de l'appli Go (connexion WebSocket)
**Objectif :** Se connecter au stream aggTrade BTCUSDT de Binance et afficher les messages bruts. Rien d'autre (pas encore de métriques ni de parsing élaboré).
**Où j'en suis :** Cadrage terminé, Go à jour, lib choisie. Reste à créer le go.mod et décrire le flux de connexion avant de coder.
**Prochain sous-pas :** (1) `go mod init github.com/Rysekk/low-latency-observability` et montrer le go.mod. (2) Décrire en français les 3-4 étapes du flux de connexion WebSocket. (3) Écrire le squelette.

---

## 🔜 Prochaines étapes identifiées

- [ ] Créer le go.mod + décrire le flux de connexion WebSocket (dans mes mots)
- [ ] Écrire le squelette : connexion + lecture en boucle + affichage messages bruts
- [ ] Structurer autour d'une goroutine de lecture + un channel (pattern backpressure)
- [ ] Bufferiser les messages reçus avec drop quand plein + compteur → métrique SLI (messages_dropped_total)
- [ ] Parser le JSON aggTrade en struct Go
- [ ] Instrumenter parse_latency / processing_latency / pipeline_latency (histogrammes)

---

## ❓ Points à revoir / questions ouvertes

- [x] Reformuler dans mes mots : pourquoi histogramme et pas gauge/counter
- [x] Corriger ma compréhension : ce sont les percentiles HAUTS (p99.9) qu'on traque, pas les bas
- [ ] Vérifier la version min de Go exigée par coder/websocket
- [x] Créer le repo git distant sur GitHub (Rysekk) et pousser
---

## 📚 Concepts appris (mémo perso)

- **Push vs Pull (WebSocket Stream vs REST)** : Le modèle Pull est utilisé avec les API REST. Ici, nous avons besoin d'un flux continu de données ; celles-ci sont donc poussées (Push) via un WebSocket Stream de Binance.
- **Désynchro d'horloge / pourquoi mesurer la latence en local** : L'horloge de Binance et celle du serveur ne sont pas forcément synchronisées, ce qui fausse la mesure du temps de récupération des données (et peut même parfois produire des valeurs négatives). C'est pourquoi nous mesurons uniquement la latence de notre pipeline local.
- **Histogramme + percentiles (p99.9) en low-latency** : En environnement low latency, on cherche à surveiller les pires cas. On s'intéresse donc aux percentiles les plus élevés (comme le p99.9), qui représentent les cas les plus rares, mais aussi les plus impactants.
- **Backpressure + arbitrage block vs drop** : Comme nous allons ingérer un grand volume de données, une forte backpressure peut apparaître. Si le buffer est plein, la goroutine se bloque et aucune nouvelle donnée n'est ingérée tant qu'il ne se vide pas. Dans notre cas, nous privilégions des données fraîches : il est donc préférable de supprimer (drop) certaines données plutôt que de bloquer entièrement le pipeline..
- **Channel Go = file d'attente + synchronisation (bloque plein/vide)** : [à écrire]