{{- define "common.deployment" -}}
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ include "common.fullname" . }}
  namespace: {{ .Release.Namespace }}
  labels: {{- include "common.labels" . | nindent 4 }}
spec:
  {{- if not .Values.hpa.enabled }}
  replicas: {{ .Values.replicaCount }}
  {{- end }}
  selector:
    matchLabels: {{- include "common.selectorLabels" . | nindent 6 }}
  template:
    metadata:
      labels: {{ include "common.selectorLabels" . | nindent 8 }}
      annotations:
        checksum/config: {{ toJson .Values.env | sha256sum }}
    spec:
      securityContext:
        runAsNonRoot: true
        runAsUser: 1000
      containers:
        - name: {{ .Chart.Name }}
          image: "{{ .Values.image.repository }}:{{ .Values.image.tag }}"
          imagePullSecrets:
            - name: ghcr-secret
          imagePullPolicy: {{ .Values.image.pullPolicy | default "IfNotPresent" }}
          ports:
            - containerPort: {{ .Values.service.port }}
              name: http
              protocol: TCP
          {{- with .Values.env }}
          env: {{ toYaml . | nindent 12 }}
          {{- end }}
          {{- with .Values.envFrom }}
          envFrom: {{ toYaml . | nindent 12 }}
          {{- end }}
          resources: {{ toYaml .Values.resources | nindent 12 }}
          livenessProbe:
            httpGet:
              path: {{ .Values.probes.liveness | default "/health" }}
              port: http
            initialDelaySeconds: 10
            periodSeconds: 10
            failureThreshold: 3
          readinessProbe:
            httpGet:
              path: {{ .Values.probes.liveness | default "/health" }}
              port: http
            initialDelaySeconds: 10
            periodSeconds: 10
            failureThreshold: 3
      serviceAccountName: {{ include "common.fullname" . }}
{{- end }}