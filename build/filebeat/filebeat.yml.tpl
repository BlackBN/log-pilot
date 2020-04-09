filebeat.config:
  inputs:
    enabled: true
    path: ${path.home}/inputs.d/*.yml
    reload.enabled: true
    reload.period: 10s

filebeat.registry_file: registry_{{ .registryIndex }}

filebeat.inputs: 
- type: log
  enabled: true
  fields_under_root: true
  paths:
  - /var/log/dockerd.log
  fields:
    cluster: ${CLUSTER_ID}
    node_name: ${NODE_NAME}
    component: system.docker
    kafka_topic: compass_docker
    "@ip": ${NODE_IP}
- type: log
  enabled: true
  fields_under_root: true
  paths:
  - /var/log/kubelet.log
  fields:
    cluster: ${CLUSTER_ID}
    node_name: ${NODE_NAME}
    component: system.kubelet
    kafka_topic: compass_kubelet
    "@ip": ${NODE_IP}
# TODO: etcd, apiserver and more..

processors:
- rename:
    fields:
    {{ if eq .type "kafka" -}}
    - from: message
      to: "@message"
    - from: source
      to: "@path"
    - from: node_name
      to: "@hostname"
    - from: kubernetes.namespace_name
      to: kafka_topic
    ignore_missing: true
    {{ else -}}
    - from: message
      to: log
    ignore_missing: true
    {{- end }}
- drop_fields:
    {{ if eq .type "kafka" -}}
    fields: ["beat", "host.name", "input.type", "prospector.type", "offset", "source","@metadata","stream","node_name","prospector","input","cluster","kubernetes","host",]
    {{ else -}}
    fields: ["beat", "host.name", "input.type", "prospector.type", "offset", "source",]
    {{- end }}

{{- if eq .type "elasticsearch" }}
setup.template.enabled: true
setup.template.overwrite: false
setup.template.name: k8s-log-template
setup.template.pattern: logstash-*
setup.template.json.name: k8s-log-template
setup.template.json.path: /etc/filebeat/k8s-log-template.json
setup.template.json.enabled: true

output.elasticsearch:
    hosts:
    {{- range .hosts }}
    - {{ . }}
    {{- end }}
    index: logstash-%{+yyyy.MM.dd}
{{- end }}

{{- if eq .type "kafka" }}
output.kafka:
    enabled: true
    hosts:
    {{- range .brokers }}
    - {{ . }}
    {{- end }}
    topic: {{ .topic }}
    version: {{ .version }}
{{- end -}}
