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

{{/*
Create the name of the service account to use
*/}}
{{- define "bd-daemon.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "bd-daemon.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
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
