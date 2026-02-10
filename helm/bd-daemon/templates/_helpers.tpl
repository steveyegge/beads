{{/*
Expand the name of the chart.
*/}}
{{- define "bd-daemon.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "bd-daemon.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "bd-daemon.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "bd-daemon.labels" -}}
helm.sh/chart: {{ include "bd-daemon.chart" . }}
{{ include "bd-daemon.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "bd-daemon.selectorLabels" -}}
app.kubernetes.io/name: {{ include "bd-daemon.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/* ===== Daemon component helpers ===== */}}

{{/*
Daemon fully qualified name
*/}}
{{- define "bd-daemon.daemon.fullname" -}}
{{- printf "%s-daemon" (include "bd-daemon.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Daemon labels
*/}}
{{- define "bd-daemon.daemon.labels" -}}
{{ include "bd-daemon.labels" . }}
app.kubernetes.io/component: daemon
{{- end }}

{{/*
Daemon selector labels
*/}}
{{- define "bd-daemon.daemon.selectorLabels" -}}
{{ include "bd-daemon.selectorLabels" . }}
app.kubernetes.io/component: daemon
{{- end }}

{{/*
Daemon service account name
*/}}
{{- define "bd-daemon.daemon.serviceAccountName" -}}
{{- if .Values.daemon.serviceAccount.create }}
{{- default (include "bd-daemon.daemon.fullname" .) .Values.daemon.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.daemon.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Daemon token secret name
*/}}
{{- define "bd-daemon.daemon.tokenSecretName" -}}
{{- printf "%s-token" (include "bd-daemon.daemon.fullname" .) }}
{{- end }}

{{/*
TLS secret name
*/}}
{{- define "bd-daemon.daemon.tlsSecretName" -}}
{{- if .Values.daemon.tls.existingSecret }}
{{- .Values.daemon.tls.existingSecret }}
{{- else }}
{{- printf "%s-tls" (include "bd-daemon.daemon.fullname" .) }}
{{- end }}
{{- end }}

{{/* ===== Dolt component helpers ===== */}}

{{/*
Dolt fully qualified name
*/}}
{{- define "bd-daemon.dolt.fullname" -}}
{{- printf "%s-dolt" (include "bd-daemon.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Dolt labels
*/}}
{{- define "bd-daemon.dolt.labels" -}}
{{ include "bd-daemon.labels" . }}
app.kubernetes.io/component: dolt
{{- end }}

{{/*
Dolt selector labels
*/}}
{{- define "bd-daemon.dolt.selectorLabels" -}}
{{ include "bd-daemon.selectorLabels" . }}
app.kubernetes.io/component: dolt
{{- end }}

{{/*
Dolt service name (used for auto-wiring daemon -> dolt)
*/}}
{{- define "bd-daemon.dolt.serviceName" -}}
{{- include "bd-daemon.dolt.fullname" . }}
{{- end }}

{{/*
Dolt service account name
*/}}
{{- define "bd-daemon.dolt.serviceAccountName" -}}
{{- if .Values.dolt.serviceAccount.create }}
{{- default (include "bd-daemon.dolt.fullname" .) .Values.dolt.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.dolt.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
S3 remote URL for Dolt
Format: aws://[dynamo-table:bucket]/prefix
*/}}
{{- define "bd-daemon.dolt.s3RemoteUrl" -}}
{{- if and .Values.dolt.s3.dynamoTable .Values.dolt.s3.prefix }}
{{- printf "aws://[%s:%s]/%s" .Values.dolt.s3.dynamoTable .Values.dolt.s3.bucket .Values.dolt.s3.prefix }}
{{- else if .Values.dolt.s3.dynamoTable }}
{{- printf "aws://[%s:%s]/%s" .Values.dolt.s3.dynamoTable .Values.dolt.s3.bucket .Values.dolt.database }}
{{- else if .Values.dolt.s3.prefix }}
{{- printf "aws://[%s]/%s" .Values.dolt.s3.bucket .Values.dolt.s3.prefix }}
{{- else }}
{{- printf "aws://[%s]/%s" .Values.dolt.s3.bucket .Values.dolt.database }}
{{- end }}
{{- end }}

{{/*
Dolt data directory path
*/}}
{{- define "bd-daemon.dolt.dataDir" -}}
/var/lib/dolt
{{- end }}

{{/*
Dolt database directory path
*/}}
{{- define "bd-daemon.dolt.databaseDir" -}}
{{ include "bd-daemon.dolt.dataDir" . }}/{{ .Values.dolt.database }}
{{- end }}

{{/*
NATS URL for the event bus.
When NATS is enabled, constructs nats://<service>:4222.
*/}}
{{- define "bd-daemon.natsURL" -}}
{{- if .Values.nats.enabled -}}
nats://{{ include "bd-daemon.fullname" . }}-nats:4222
{{- end -}}
{{- end }}

{{/*
Redis URL for the wisp store.
When the Redis subchart is enabled, constructs the URL from the subchart service name.
*/}}
{{- define "bd-daemon.redisURL" -}}
{{- if .Values.redis.enabled -}}
{{- $redisHost := printf "%s-redis-master" .Release.Name -}}
{{- if .Values.redis.auth.enabled -}}
redis://default:$(REDIS_PASSWORD)@{{ $redisHost }}:6379/0
{{- else -}}
redis://{{ $redisHost }}:6379/0
{{- end -}}
{{- end -}}
{{- end }}
