# Journal de progression : low-latency-observability
---

## 🧱 Architecture cible (briques à intégrer)

Vue d'ensemble des briques prévues. Coche au fur et à mesure.

- [ ] Appli Go low-latency, ingestion flux de marché réel + instrumentation latence
- [ ] Observabilité complète : Prometheus / Grafana / Loki / Tempo / OpenTelemetry / alerting
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
| 26/07/2026 | k3d pour le dev | Outils déjà connus avec la possibilité d'avoir plusieur noeuds, facile a mettre ne place distribution Kubernetes complète et certifiée, allégée sur certains composants, kubeadm reporté à une session CKA dédiée |
| 26/07/2026 | ghcr.io et versioning par tag | Le repos du projet est déjà sur GitHub, donc utiliser le Container Registery de GitHub était le choix logique. Pour le versionning, un tag versionné est immuable et identifiable contrairement a latest, lors de mise ne place de la CI/CD le tag sera remplacer par le SHA du commit en plus, pour lier image ↔ code exact. |
| 29/07/2026 | `spec.strategy: Recreate` (au lieu du rolling update par défaut) | Avec 1 replica et `maxSurge: 1`, un rolling update démarre le nouveau pod avant de tuer l'ancien → deux WebSockets Binance ouvertes simultanément → double ingestion agrégée dans les recording rules, donc SLI faussés de façon invisible. Recreate coupe franchement (quelques secondes sans pod, trou de données) : entre perdre de la donnée et avoir de la donnée fausse, on choisit le trou, parce qu'un trou se voit et se détecte (`absent_over_time`). Coût budgété : ~1 min par déploiement sur 43 min/mois de budget d'erreur. |
| 29/07/2026 | QoS **Burstable** : `requests = limits` sur la mémoire, `requests` seule sur le CPU | La mémoire est incompressible (dépassement = OOMKill), donc on la borne strictement. Le CPU est compressible : une limite CPU déclenche le throttling CFS, qui gèle le conteneur jusqu'à la fin de la période de 100 ms, soit un stall de 50× le SLO p99 de 2 ms. Contrepartie assumée : sans limite CPU, le pod peut affamer ses voisins sur le nœud ; on privilégie la latence sur l'équité. |
| 02/08/2026 | `replicas: 1` (pas de scaling horizontal) | L'utilisatiion d'un Websocket ne permet pas d'utiliser plusieurs replicas, la duplication du flux est un probléme lorsque l'on fait du scaling horizontale sauf si on met en plce du sharding, dans notre cas avec une volumetrie réel de quelque centaines de msg/s ça ne le justifie pas |
| 02/08/2026 | Namespace unique `trading-app` (pas de séparation dev/prod par namespace) | Pas de separation pas environnement (dev/prod) ça se fait sur deux cluster different, on garde le noms des ressources identiques partout pour permettre les overlays GitOps |
| 02/08/2026 | Un Deployment + un Service **par composant** (app, Prometheus, Grafana) | Chaque composant (app, prometheus, grafana..etc) a sont propre deploiement, cela permet un cycles de vie indépendants des uns et des autres, le multi-conteneur dans une deploiement ne se justifie que si le couplage est fort (localhost / volume partagé) |
| 02/08/2026 | Nommage des ressources sans préfixe ni suffixe de version | Le nom d'une ressource est une adresse DNS stable, la version vit dans le tag d'image. |
| 02/08/2026 | Arrêt gracieux via `signal.NotifyContext` + `defer close(channel)` | Si on utilise `log.Fatal` = `os.Exit(1)` -> Aucun defer exécuté et exit code d'erreur, la bonne methode c'est le producteur qui ferme le channel |
| 10/08/2026 | Datasource Grafana avec **`uid` fixe** (`prometheus`) + URL DNS explicite `http://prometheus.trading-app:9090` | Fixé l'UID plutôt que le laisser générer, pour eviter qu'il change a chaque re deployement, utilisation du FQDN plutôt que le nom court pour que cela fonctionne même dans une namspace different |
| 10/08/2026 | Provisioning Grafana = **3 ConfigMaps** (datasource / provider / dashboards JSON), une par répertoire de montage | Le critère = chemin de montage, le `path` du provider doit matcher le mountPath de la ConfigMap dashboards |
| 10/08/2026 | Grafana : `runAsUser: 472`, `readOnlyRootFilesystem: true` + emptyDir sur `/var/lib/grafana` et `/tmp` | L'UID 472 imposé par l'image, le chemins d'écriture découverts empiriquement via les logs sous `readOnlyRootFilesystem: true` (read-only) |
| 10/08/2026 | Dashboards récupérés **exclusivement via l'API classique** (`/api/dashboards/uid/<UID>` + `jq '.dashboard'`), jamais via l'export UI Grafana 13 | L'export UI de Grafana 13 (« classic » comme V2) produit le schéma V2 (`elements`/`layout`), qui ne se charge pas quand on le met dans une ConfigMap ; seul l'export API produit un JSON provisionnable. Workflow d'itération : copie bac-à-sable non-provisionnée → export API → réalignement de l'UID → ConfigMap |
| 11/08/2026 | PVC dynamique `local-path` pour le TSDB Prometheus (abandon du PV statique `hostPath`/`manual`) | PV statique met le pod en crashloop lorsque qu'il n'est pas placer sur le même noeud que le pv. Le PVC Dynamique régle ce soucis, le pods est scheduler d'abord, le PV est ensuite créé sur le bon noeud avec une node affinity, c'est cette affinity du PV qui contraint le scheduler à ramener le pod vers le volume. |
| 11/08/2026 | PVC `local-path` sur `/var/lib/grafana`, bascule assumée vers un modèle **hybride** : dashboards provisionnés (ConfigMap, lecture seule, autorité = Git) + dashboards UI (SQLite, autorité = base) | Pour les besoins de construction des dashboards, on laisse la possibilité de créer des dashboards directement depuis l’UI et de les sauvegarder via un PVC dynamique. Une fois qu’un dashboard est finalisé, il peut être transformé en code. |

---

## ✅ Étapes franchies

- [x] 11/07/2026 : Go installé (go1.26.5) après upgrade depuis la version apt périmée (1.18). Repo `low-latency-observability` initialisé. Lib WebSocket choisie (coder/websocket).
- [x] 12/07/2026 : Squelette v1 fonctionnel : connexion au stream aggTrade BTCUSDT + boucle Read + affichage des messages JSON bruts. Premier flux de marché temps réel reçu.
- [x] 18/07/2026 : Refactoring goroutine de lecture (readStream) + channel bufferisé []byte avec drop via select/default. Parsing JSON en struct AggTrade (champs typés, erreur vérifiée).
- [x] 19/07/2026 : Instrumentation latence : trois segments (parse, processing, pipeline) via HistogramVec + label stage. Counter messages_dropped_total. Endpoint /metrics + Prometheus scrape + Grafana heatmap fonctionnels. Chaîne complète Go → Prometheus → Grafana opérationnelle.
- [x] 25/07/2026 : Dockerisation multi-stage (scratch, ~8.5 MB). Stack complète en docker-compose (app + Prometheus + Grafana) via un seul `docker compose up --build`.
- [x] 26/07/2026 : SLO/SLI : deux recording rules (ingest:processed_ratio, ingest:pipeline_latency_p99) + une alerting rule (high_latency_pipeline_p99 à 2ms, for 10m). Cycle Inactive→Pending→Firing testé et validé. Image poussée sur ghcr.io/rysekk/low-latency-app:0.1, Grafana provisionné as code (datasource + provider OK, dashboard).
- [x] 02/08/2026 : **Migration Kubernetes (k3d)** : Namespace `trading-app`, Deployment + Service de l'appli Go (`replicas: 1`, `strategy: Recreate`, QoS Burstable, `securityContext` pod + conteneur, `GOMEMLIMIT`). Pod `Running`, résolution DNS interne validée par curl depuis un pod jetable.
- [x] 02/08/2026 : **Prometheus migré dans le cluster** : ConfigMap (prometheus.yml + rules.yml) montée en volume, Deployment + Service ClusterIP, volume `emptyDir` pour le TSDB. Target `ingest-app` UP, 2 recording rules + 2 alerting rules chargées et visibles dans l'UI via port-forward.
- [x] 02/08/2026 : **Arrêt gracieux SIGTERM** implémenté dans l'appli Go (`signal.NotifyContext`, propagation du ctx dans `conn.Read`, `defer close(channel)`, struct déclarée dans la boucle). Image `0.2` buildée et poussée sur `ghcr.io/rysekk/low-latency-observability`.
- [x] 10/08/2026 : **Grafana migré dans le cluster** : 3 ConfigMaps (datasource `uid: prometheus` + URL FQDN, dashboard provider `path: /var/lib/grafana/dashboards`, dashboards JSON), Deployment (`runAsUser: 472`, `readOnlyRootFilesystem: true`, emptyDir sur `/var/lib/grafana` et `/tmp`), Service ClusterIP. Datasource testée verte, dashboard `Pipeline Latency / SLO` chargé par provisioning et affichant la donnée via port-forward. Débogage : `ContainerCreating` (nom de ConfigMap erroné) diagnostiqué via `kubectl describe` ; chemins d'écriture sous rootfs read-only découverts via les logs. Piège format dashboard V1/V2 tranché empiriquement : seul l'export API est provisionnable.
- [x] 11/08/2026 : **TSDB Prometheus persistant.** PVC dynamique (`local-path`, `WaitForFirstConsumer`), PV généré automatiquement (200Mi, node affinity `agent-1` injectée), `emptyDir` remplacé, `readOnlyRootFilesystem: true` conservé, rétention `30d` active. Détour pédagogique par un PV statique manuel → crashloop (pod `agent-1` / PV `server-0`) → diagnostic : `hostPath` sans node affinity cloue le volume à un nœud. Test destructif validé (`delete pod` → `WAL replay completed`). **Démarrage réel de la fenêtre SLO 30 jours.**
- [x] 11/08/2026 : **PVC Grafana + modèle hybride de dashboards.** PVC `local-path` (200Mi) sur `/var/lib/grafana`, `emptyDir` conservé sur `/tmp`. Motivation : ouvrir la création de dashboards à l'UI (édition JSON à la main impraticable) → la SQLite devient autoritative pour ces dashboards, donc persistée. Deux sources de vérité assumées : provisionné = Git (lecture seule), UI = SQLite. Test de persistance UI validé (création dashboard UI → `delete pod` → dashboard survivant).

---

## 🔨 En cours

## 🔨 En cours

**Étape actuelle :** Migration Kubernetes (k3d) — **close**. Ouverture de l'étape « Exposition des services ».
**Objectif de la nouvelle étape :** Remplacer les `kubectl port-forward` (tunnel de debug jetable, mono-utilisateur) par une exposition stable des UI (app, Prometheus, Grafana).
**Où j'en suis :** Cluster fonctionnel, trois composants dans `trading-app`, état autoritatif persistant (TSDB + SQLite Grafana sur PVC). Tout l'accès externe passe encore par port-forward. Traefik désactivé sur le cluster → pas d'Ingress Controller actif à ce stade.
**Décisions à trancher avant de coder :**
- NodePort jetable (sentir le mécanisme) puis Ingress, ou Ingress directement.
- Contrôleur d'Ingress : réactiver Traefik (rapide) ou installer ingress-nginx (plus formateur CKA).
- Périmètre d'exposition : quelles UI ouvrir, lesquelles garder internes (Prometheus en accès libre ?).
**Dettes de durcissement renvoyées au backlog (hors périmètre de la nouvelle étape) :** recalibration mémoire Prometheus, mesure du trou de déploiement SIGTERM, self-scrape Prometheus, réconciliation dashboard UI → Git, replicas/HPA.
---

## 🔜 Prochaines étapes identifiées (Backlog)

### Court terme (clôture de l'étape K8s)
- [ ] Recalibrer le `requests.memory` de Prometheus sur une conso réelle une fois le TSDB persistant
- [ ] Mesurer le trou de déploiement (`rate(ingest_message_receive_total[1m])`), avant/après SIGTERM
- [ ] surveiller `container_cpu_cfs_throttled_seconds_total`
- [ ] Nettoyer les manifests : noms de ports, cohérence des labels
- [ ] Bloc `global` explicite dans prometheus.yml

### Observabilité à durcir
- [ ] Job Prometheus qui se scrape lui-même (`prometheus_tsdb_head_series`, RSS)
- [ ] Alerte d'absence de données : `absent_over_time(ingest_message_receive_total[5m])`
- [ ] Compteur `ingest_parse_errors_total` + `continue` après erreur de parsing
- [ ] Gauge d'état de connexion WS + alerte associée
- [ ] Probes : liveness sur la santé WS (⚠️ /metrics doit rester scrapable en permanence)
- [ ] Alertmanager pour router les alertes (email/Slack)
- [ ] Dashboard SLO + error budget (une fois l'appli en H24), enrichir via le workflow copie bac-à-sable → export API → ConfigMap
- [ ] Logging structuré (JSON) pour Loki

### Code Go (v0.3)
- [ ] Retry + backoff sur la WebSocket
- [ ] `http.Server` explicite avec `Shutdown(ctx)`
- [~] Séparer en packages (ingestion, metrics, config)

### Briques suivantes
- [ ] Exercice `imagePullSecret` (repasser le package en privé)
- [ ] `kubernetes_sd_config` + RBAC au lieu de `static_configs`
- [ ] CI/CD : GitHub Actions (build, test, lint, push image)
- [ ] IaC : Terraform
- [ ] GitOps : ArgoCD ou Flux, les ConfigMaps Grafana (datasource/provider/dashboards) rentreront dans le périmètre synchronisé, pas de chantier dashboard séparé
- [ ] Chaos / résilience : injection de pannes + post-mortems

### ✔️ Fait
- [x] Nettoyer le code : supprimer le log.Println du processing, préfixe `ingest_`
- [x] Dockeriser l'appli Go
- [x] Définir les SLO/SLI formels sur la latence pipeline
- [x] Créer le namespace `trading-app`
- [x] Migration K8s : appli Go + Prometheus
- [x] Build + push image `0.2` (fix SIGTERM)
- [x] Recalibrer les resources Prometheus (memory-bound, bursty → pas de limite CPU)
- [x] Migrer Grafana sur le cluster (provisioning as code via ConfigMaps)
- [x] Remplacer `emptyDir` par un PVC (StorageClass `local-path`), Prometheus en priorité, sans ça SLO 30j fictif


---

## ❓ Points à revoir / questions ouvertes

- [x] Reformuler dans mes mots : pourquoi histogramme et pas gauge/counter
- [x] Corriger ma compréhension : ce sont les percentiles HAUTS (p99.9) qu'on traque, pas les bas
- [X] Vérifier la version min de Go exigée par coder/websocket
- [x] Créer le repo git distant sur GitHub (Rysekk) et pousser
- [x] Réfléchir : un SLO qu'on ne risque jamais de violer est-il utile ? Envisager seuil warning + critical
- [~] Apprendre PromQL, en cours (rate, histogram_quantile, recording/alerting rules acquis)
- [ ] Gérer le warmup Go (p99 plus élevé au démarrage), chauffer l'appli avant exposition ?
- [ ] Reconnaître un JSON de dashboard provisionnable : quels marqueurs distinguent le V1 (`panels`, `schemaVersion`) du V2 (`elements`, `layout`, `vizConfig`, ou enveloppe `apiVersion`/`kind`/`metadata`) ?

---

## 📚 Concepts appris (mémo perso)

- **Push vs Pull (WebSocket Stream vs REST)** : Le modèle Pull est utilisé avec les API REST. Ici, nous avons besoin d'un flux continu de données ; celles-ci sont donc poussées (Push) via un WebSocket Stream de Binance.
- **Désynchro d'horloge / pourquoi mesurer la latence en local** : L'horloge de Binance et celle du serveur ne sont pas forcément synchronisées, ce qui fausse la mesure du temps de récupération des données (et peut même parfois produire des valeurs négatives). C'est pourquoi nous mesurons uniquement la latence de notre pipeline local.
- **Histogramme + percentiles (p99.9) en low-latency** : En environnement low latency, on cherche à surveiller les pires cas. On s'intéresse donc aux percentiles les plus élevés (comme le p99.9), qui représentent les cas les plus rares, mais aussi les plus impactants.
- **Backpressure + arbitrage block vs drop** : Comme nous allons ingérer un grand volume de données, une forte backpressure peut apparaître. Si le buffer est plein, la goroutine se bloque et aucune nouvelle donnée n'est ingérée tant qu'il ne se vide pas. Dans notre cas, nous privilégions des données fraîches : il est donc préférable de supprimer (drop) certaines données plutôt que de bloquer entièrement le pipeline.
- **Channel Go**: Un channel go est un tuyaux qui sert de buffer pour un nombre données de valeur (ici 128) et qui traite les données en mode FIFO et qui bloque l'ecriture dedant quand le channel est plein et la lecture quand c'est vide.
- **Channel Go = file d'attente + synchronisation (bloque plein/vide)** : Mise en place d'un channel pour faire transitionné les données reçus de la websocket vers la pipeline de traitement. Nous utilisons un `select` avec `default` : si le buffer est plein, le message est drop plutôt que de bloquer la lecture et incrementé un compteur que l'on pourras utiliser comme metrique pour savoir si notre buffer est congestionné.
- **Provisioning Grafana as code** : Découpage des ConfigMaps par chemin de montage. Dans la ConfigMap de la datasource Prometheus, nous avons utilisé le DNS interne k8s et son FQDN (`prometheus.trading-app:9090`), ce qui permet de contacter le pod de façon stable, y compris depuis un autre namespace. Mise en place d'un UID de datasource fixe pour le versionning (le dashboard référence la datasource par cet UID ; sans UID fixe, chaque instance Grafana en génère un aléatoire et le dashboard ne retrouve plus sa datasource).
- **Déploiement de Grafana** : l'option `readOnlyRootFilesystem: true` impose de couvrir tous les chemins d'écriture (`/var/lib/grafana`, `/tmp`) par des volumes inscriptibles. Les chemins ont été découverts empiriquement via les logs sous rootfs read-only. Pour les montages imbriqués, k8s applique toujours le parent avant l'enfant (tri par profondeur de chemin, pas par ordre de déclaration YAML), ce qui garantit que la ConfigMap dashboards montée sur `/var/lib/grafana/dashboards` n'est pas écrasée par l'emptyDir parent. UID d'exécution propre à chaque image (Grafana = 472, ≠ nobody 65534).
- **Versionning des dashboards** : seul l'export via l'API classique (`/api/dashboards/uid/<UID>` + `jq '.dashboard'`) produit un JSON relisible par le classic-file-provisioning. Les exports de l'UI Grafana 13 (« classic » comme V2) produisent le schéma V2 (`elements`/`layout`) que le provisioning par fichier ne charge pas.