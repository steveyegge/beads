# Observability (OpenTelemetry)

Beads envoie ses métriques via OTLP HTTP. La télémétrie est **inactive par défaut** — aucun overhead quand aucune variable n'est définie.

## Stack locale recommandée

| Service | Port | Rôle |
|---------|------|------|
| VictoriaMetrics | 8428 | Métriques OTLP |
| VictoriaLogs | 9428 | Logs OTLP |
| Grafana | 9429 | Visualisation |

```bash
# Depuis le dossier opentelemetry/ de ton stack perso
docker compose up -d
```

## Configuration

Deux variables suffisent. Les ajouter au shell ou au `.env` du workspace :

```bash
export BD_OTEL_METRICS_URL=http://localhost:8428/opentelemetry/api/v1/push
export BD_OTEL_LOGS_URL=http://localhost:9428/insert/opentelemetry/v1/logs
```

À partir de là, chaque commande `bd` envoie automatiquement ses métriques.

### Profil shell (recommandé)

```bash
# ~/.zshrc ou ~/.bashrc
export BD_OTEL_METRICS_URL=http://localhost:8428/opentelemetry/api/v1/push
export BD_OTEL_LOGS_URL=http://localhost:9428/insert/opentelemetry/v1/logs
```

### Variables d'environnement

| Variable | Exemple | Description |
|----------|---------|-------------|
| `BD_OTEL_METRICS_URL` | `http://localhost:8428/opentelemetry/api/v1/push` | Push métriques vers VictoriaMetrics. Active la télémétrie. |
| `BD_OTEL_LOGS_URL` | `http://localhost:9428/insert/opentelemetry/v1/logs` | Push logs vers VictoriaLogs (réservé). |
| `BD_OTEL_STDOUT` | `true` | Écrit spans et métriques sur stderr (dev/debug). Active aussi la télémétrie. |

### Mode debug local

```bash
BD_OTEL_STDOUT=true bd list
```

## Vérification

```bash
bd list   # déclenche des métriques → visible dans VictoriaMetrics
```

Requête de vérification dans Grafana (datasource VictoriaMetrics) :

```promql
bd_storage_operations_total
```

---

## Métriques

### Commandes CLI

| Métrique | Type | Description |
|----------|------|-------------|
| *(via spans stdout uniquement)* | — | Les spans de commande ne sont émis que si `BD_OTEL_STDOUT=true` |

### Stockage (`bd_storage_*`)

| Métrique | Type | Attributs | Description |
|----------|------|-----------|-------------|
| `bd_storage_operations_total` | Counter | `db.operation` | Opérations storage exécutées |
| `bd_storage_operation_duration_ms` | Histogram | `db.operation` | Durée des opérations (ms) |
| `bd_storage_errors_total` | Counter | `db.operation` | Erreurs storage |

> Ces métriques sont émises par `InstrumentedStorage`, le wrapper SDK beads.

### Base de données Dolt (`bd_db_*`)

| Métrique | Type | Attributs | Description |
|----------|------|-----------|-------------|
| `bd_db_retry_count_total` | Counter | — | Retries SQL en mode serveur |
| `bd_db_lock_wait_ms` | Histogram | `dolt_lock_exclusive` | Attente pour acquérir `dolt-access.lock` |

### Issues (`bd_issue_*`)

| Métrique | Type | Attributs | Description |
|----------|------|-----------|-------------|
| `bd_issue_count` | Gauge | `status` | Nombre d'issues par statut |

Valeurs de `status` : `open`, `in_progress`, `closed`, `deferred`.

### IA (`bd_ai_*`)

| Métrique | Type | Attributs | Description |
|----------|------|-----------|-------------|
| `bd_ai_input_tokens_total` | Counter | `bd_ai_model` | Tokens d'entrée Anthropic |
| `bd_ai_output_tokens_total` | Counter | `bd_ai_model` | Tokens de sortie Anthropic |
| `bd_ai_request_duration_ms` | Histogram | `bd_ai_model` | Latence des appels API |

---

## Traces (spans)

Les spans ne sont exportés que si `BD_OTEL_STDOUT=true` — il n'y a pas de backend trace dans la stack locale recommandée.

| Span | Source | Description |
|------|--------|-------------|
| `bd.command.<name>` | CLI | Durée totale de la commande |
| `dolt.exec` / `dolt.query` / `dolt.query_row` | SQL | Chaque opération SQL |
| `dolt.commit` / `dolt.push` / `dolt.pull` / `dolt.merge` | Dolt VC | Procédures de contrôle de version |
| `ephemeral.count` / `ephemeral.nuke` | SQLite | Opérations sur le store éphémère |
| `hook.exec` | Hooks | Exécution d'un hook (span racine, fire-and-forget) |
| `tracker.sync` / `tracker.pull` / `tracker.push` | Sync | Phases de synchronisation tracker |
| `anthropic.messages.new` | IA | Appels API Claude |

---

## Architecture

```
cmd/bd/main.go
  └─ telemetry.Init()
      ├─ BD_OTEL_STDOUT=true  → TracerProvider stdout + MeterProvider stdout
      └─ BD_OTEL_METRICS_URL  → MeterProvider HTTP → VictoriaMetrics

internal/storage/dolt/        → bd_db_* métriques + spans dolt.*
internal/storage/ephemeral/   → spans ephemeral.*
internal/hooks/               → spans hook.exec
internal/tracker/             → spans tracker.*
internal/compact/             → bd_ai_* métriques + spans anthropic.*
internal/telemetry/storage.go → bd_storage_* métriques (SDK wrapper)
```

Quand aucune variable n'est définie, `telemetry.Init()` installe des providers **no-op** :
les chemins chauds n'exécutent que des appels no-op sans allocation mémoire.
