# Observability (OpenTelemetry)

Beads intègre [OpenTelemetry](https://opentelemetry.io/) pour la traçabilité et les métriques.
La télémétrie est **désactivée par défaut** — zéro overhead quand elle n't est pas activée.

## Activation

```bash
# Mode dev : spans et métriques affichés sur stdout
BD_OTEL_ENABLED=true BD_OTEL_STDOUT=true bd list

# Mode production : envoi vers un backend OTLP (Jaeger, Grafana Tempo, Honeycomb, Datadog…)
BD_OTEL_ENABLED=true OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4317 bd sync
```

### Variables d'environnement

| Variable | Valeur | Description |
|----------|--------|-------------|
| `BD_OTEL_ENABLED` | `true` | Active la télémétrie (défaut : désactivée) |
| `BD_OTEL_STDOUT` | `true` | Écrit les spans/métriques sur stderr (dev/debug) |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `host:port` | Endpoint OTLP gRPC pour traces **et** métriques |
| `OTEL_EXPORTER_OTLP_METRICS_ENDPOINT` | `host:port` | Endpoint OTLP gRPC pour métriques uniquement (prioritaire sur le précédent) |
| `OTEL_SERVICE_NAME` | string | Surcharge le nom de service (défaut : `bd`) |

Le transport OTLP utilise gRPC sans TLS par défaut. Pour activer TLS, configurer
`OTEL_EXPORTER_OTLP_CERTIFICATE` / `OTEL_EXPORTER_OTLP_HEADERS` selon les
[conventions OTLP](https://opentelemetry.io/docs/specs/otel/protocol/exporter/).

---

## Traces (Spans)

### Commandes CLI

Chaque invocation de `bd` produit un span racine.

| Span | Attributs | Description |
|------|-----------|-------------|
| `bd.command.<name>` | `bd.command`, `bd.version`, `bd.actor` | Durée totale de la commande |

### Base de données Dolt (SQL)

Spans créés pour chaque opération SQL dans le backend Dolt.

| Span | Attributs | Description |
|------|-----------|-------------|
| `dolt.exec` | `db.system=dolt`, `db.operation`, `db.statement`, `db.readonly`, `db.server_mode` | `INSERT`, `UPDATE`, `DELETE`, procédures |
| `dolt.query` | idem | `SELECT` retournant plusieurs lignes |
| `dolt.query_row` | idem | `SELECT` retournant une ligne |

### Opérations de contrôle de version Dolt

Ces opérations SQL (`CALL DOLT_*`) contournent le wrapper standard ; elles ont leurs propres spans.

| Span | Attributs supplémentaires | Description |
|------|--------------------------|-------------|
| `dolt.commit` | — | `CALL DOLT_COMMIT` |
| `dolt.push` | `dolt.remote`, `dolt.branch` | `CALL DOLT_PUSH` |
| `dolt.force_push` | `dolt.remote`, `dolt.branch` | `CALL DOLT_PUSH --force` |
| `dolt.pull` | `dolt.remote`, `dolt.branch` | `CALL DOLT_PULL` |
| `dolt.branch` | `dolt.branch` | `CALL DOLT_BRANCH` |
| `dolt.checkout` | `dolt.branch` | `CALL DOLT_CHECKOUT` |
| `dolt.merge` | `dolt.merge_branch`, `dolt.conflicts` | `CALL DOLT_MERGE` |

### Store éphémère (SQLite)

| Span | Attributs | Description |
|------|-----------|-------------|
| `ephemeral.count` | `db.system=sqlite`, `db.result_count` | Comptage des issues éphémères |
| `ephemeral.nuke` | `db.system=sqlite` | Suppression complète du store |

### Hooks

Les hooks s'exécutent en arrière-plan (fire-and-forget), leurs spans n'ont pas de parent.

| Span | Attributs | Description |
|------|-----------|-------------|
| `hook.exec` | `hook.event`, `hook.path`, `bd.issue_id` | Exécution d'un hook (`on_create`, `on_update`, `on_close`) |

### Synchronisation tracker (Linear, GitLab…)

| Span | Attributs | Description |
|------|-----------|-------------|
| `tracker.sync` | `sync.tracker`, `sync.pull`, `sync.push`, `sync.dry_run`, stats finales | Sync complète (phases 1+2+3) |
| `tracker.pull` | `sync.tracker`, `sync.dry_run`, `sync.created`, `sync.updated`, `sync.skipped` | Import depuis le tracker externe |
| `tracker.detect_conflicts` | `sync.tracker`, `sync.conflicts` | Détection des conflits bidirectionnels |
| `tracker.push` | `sync.tracker`, `sync.dry_run`, `sync.created`, `sync.updated`, `sync.skipped`, `sync.errors` | Export vers le tracker externe |

### Appels Anthropic (IA)

| Span | Attributs | Description |
|------|-----------|-------------|
| `anthropic.messages.new` | `bd.ai.model`, `bd.ai.operation`, `bd.ai.input_tokens`, `bd.ai.output_tokens`, `bd.ai.attempts` | Appels à l'API Claude (compaction, dedup) |

---

## Métriques

### Stockage (`bd.storage.*`)

| Métrique | Type | Attributs | Description |
|----------|------|-----------|-------------|
| `bd.storage.operations` | Counter | `db.operation` | Nombre total d'opérations storage |
| `bd.storage.operation.duration` | Histogram (ms) | `db.operation` | Durée des opérations |
| `bd.storage.errors` | Counter | `db.operation` | Nombre d'erreurs storage |

Ces métriques sont émises par `InstrumentedStorage` — le wrapper OTel utilisé par le SDK beads.
Le backend Dolt embarqué (CLI) est instrumenté directement au niveau SQL.

### Base de données Dolt (`bd.db.*`)

| Métrique | Type | Attributs | Description |
|----------|------|-----------|-------------|
| `bd.db.retry_count` | Counter | — | Retries SQL en mode serveur (erreurs transitoires) |
| `bd.db.lock_wait_ms` | Histogram (ms) | `dolt.lock.exclusive` | Temps d'attente pour acquérir le verrou `dolt-access.lock` |

### Issues (`bd.issue.*`)

| Métrique | Type | Attributs | Description |
|----------|------|-----------|-------------|
| `bd.issue.count` | Gauge | `status` | Nombre d'issues par statut (snapshot à chaque `GetStatistics`) |

Les valeurs de `status` sont : `open`, `in_progress`, `closed`, `deferred`.

### IA (`bd.ai.*`)

| Métrique | Type | Attributs | Description |
|----------|------|-----------|-------------|
| `bd.ai.input_tokens` | Counter | `bd.ai.model` | Tokens d'entrée consommés par l'API Anthropic |
| `bd.ai.output_tokens` | Counter | `bd.ai.model` | Tokens de sortie générés |
| `bd.ai.request.duration` | Histogram (ms) | `bd.ai.model` | Latence des requêtes API |

---

## Backends supportés

N'importe quel backend compatible OTLP/gRPC fonctionne. Exemples :

### Jaeger (dev local)

```bash
docker run -d --name jaeger \
  -p 16686:16686 -p 4317:4317 \
  jaegertracing/all-in-one:latest

BD_OTEL_ENABLED=true OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4317 bd list
# Ouvrir http://localhost:16686
```

### Grafana Tempo + Prometheus

```bash
# Dans grafana-agent.yaml ou otel-collector.yaml :
# receivers.otlp.protocols.grpc.endpoint: 0.0.0.0:4317
BD_OTEL_ENABLED=true OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4317 bd sync
```

### Honeycomb

```bash
BD_OTEL_ENABLED=true \
  OTEL_EXPORTER_OTLP_ENDPOINT=api.honeycomb.io:443 \
  OTEL_EXPORTER_OTLP_HEADERS="x-honeycomb-team=YOUR_API_KEY" \
  bd sync
```

### Datadog

```bash
# Lancer le Datadog Agent avec OTLP activé (otlp_config.receiver.grpc.endpoint)
BD_OTEL_ENABLED=true OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4317 bd sync
```

---

## Architecture

```
cmd/bd/main.go
  └─ telemetry.Init()          → configure TracerProvider + MeterProvider globaux
      ├─ stdout exporter        (BD_OTEL_STDOUT=true)
      └─ OTLP/gRPC exporter     (OTEL_EXPORTER_OTLP_ENDPOINT)

internal/storage/dolt/store.go
  ├─ doltTracer                 → spans SQL (execContext, queryContext, queryRowContext)
  ├─ Commit/Push/Pull/Merge/…   → spans dédiés pour les procédures DOLT_*
  └─ doltMetrics                → bd.db.retry_count, bd.db.lock_wait_ms

internal/storage/dolt/access_lock.go
  └─ AcquireAccessLock()        → enregistre bd.db.lock_wait_ms

internal/storage/ephemeral/store.go
  └─ ephemeralTracer            → spans Count/Nuke

internal/hooks/hooks_unix.go    → span hook.exec (root span, pas de parent)
internal/hooks/hooks_windows.go → idem

internal/tracker/engine.go
  └─ syncTracer                 → spans Sync/doPull/doPush/DetectConflicts

internal/compact/haiku.go       → spans + métriques bd.ai.*

internal/telemetry/storage.go
  └─ InstrumentedStorage        → wrapper pour le SDK beads
      ├─ bd.storage.*           → toutes les opérations storage
      └─ bd.issue.count         → gauge par statut (via GetStatistics)
```

Quand `BD_OTEL_ENABLED` n'est pas `true`, `telemetry.Init()` installe des providers **no-op** :
les chemins chauds (SQL, hooks) n'exécutent que des appels no-op sans allocation mémoire.
