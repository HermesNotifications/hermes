{{/*
TLS plumbing for the bundled datastores.

Nine Deployments, three Jobs, a CronJob and two test pods all need the same client-side CA
mounts, so they live here as two paired helpers rather than as twenty lines repeated fifteen
times. The pairing is the point: a volume without its mount, or a mount without its volume, is
a pod that fails to start or a file that is silently absent.
*/}}

{{/* Secret holding the bundled Postgres server certificate. */}}
{{- define "hermes.postgresqlTLSSecret" -}}
{{- .Values.postgresql.tls.existingSecret | default (printf "%s-postgresql-tls" (include "hermes.fullname" .)) -}}
{{- end }}

{{/* Secret holding the bundled Redis server certificate. */}}
{{- define "hermes.redisTLSSecret" -}}
{{- .Values.redis.tls.existingSecret | default (printf "%s-redis-tls" (include "hermes.fullname" .)) -}}
{{- end }}

{{/* Secret holding the bundled NATS server certificate. */}}
{{- define "hermes.natsTLSSecret" -}}
{{- printf "%s-nats-tls" .Release.Name -}}
{{- end }}

{{/*
Whether a given bundled store's connection is TLS. Call with (dict "root" . "store" "postgresql").
*/}}
{{- define "hermes.storeTLS" -}}
{{- $store := index .root.Values .store -}}
{{- if and .root.Values.tls.enabled $store.enabled -}}true{{- end -}}
{{- end }}

{{/*
Client-side CA volumes for every Hermes workload.

Only the ca.crt entry is projected from each Secret, never the server's private key: `items`
is what keeps the key out of nine service containers that have no business holding it.

Paths are stable and referenced from the ConfigMap, so changing one here means changing
HERMES_REDIS_CA_BUNDLE and HERMES_NATS_CA_BUNDLE with it.
*/}}
{{- define "hermes.datastoreCAVolumes" -}}
{{- if include "hermes.storeTLS" (dict "root" . "store" "postgresql") }}
- name: postgresql-ca
  secret:
    secretName: {{ include "hermes.postgresqlTLSSecret" . }}
    items:
      - key: ca.crt
        path: postgresql-ca.crt
{{- end }}
{{- if include "hermes.storeTLS" (dict "root" . "store" "redis") }}
- name: redis-ca
  secret:
    secretName: {{ include "hermes.redisTLSSecret" . }}
    items:
      - key: ca.crt
        path: redis-ca.crt
{{- end }}
{{- if include "hermes.storeTLS" (dict "root" . "store" "nats") }}
- name: nats-ca
  secret:
    secretName: {{ include "hermes.natsTLSSecret" . }}
    items:
      - key: ca.crt
        path: nats-ca.crt
{{- end }}
{{- end }}

{{/* The mounts matching hermes.datastoreCAVolumes. Always emit both or neither. */}}
{{- define "hermes.datastoreCAMounts" -}}
{{- if include "hermes.storeTLS" (dict "root" . "store" "postgresql") }}
- name: postgresql-ca
  mountPath: /etc/hermes/tls/postgresql
  readOnly: true
{{- end }}
{{- if include "hermes.storeTLS" (dict "root" . "store" "redis") }}
- name: redis-ca
  mountPath: /etc/hermes/tls/redis
  readOnly: true
{{- end }}
{{- if include "hermes.storeTLS" (dict "root" . "store" "nats") }}
- name: nats-ca
  mountPath: /etc/hermes/tls/nats
  readOnly: true
{{- end }}
{{- end }}
