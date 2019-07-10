{{range .configList}}
- type: log
  enabled: true
  paths:
      - {{ .LogFile }}
  scan_frequency: 10s
  fields_under_root: true
  {{if .Stdout}}
  docker-json:
    stream: all
    partial: true 
    cri_flags: true
  {{end}}
  fields:
      cluster: ${CLUSTER_ID}
      {{- range $key, $value := .Tags }}
      {{ $key }}: "{{ $value }}"
      {{- end }}
  {{ if .MultilinePattern -}}
  {{- if ne .MultilinePattern ""}}
  multiline:
    pattern: {{ .MultilinePattern }}
    negate: false
    match: after
  {{- end -}}
  {{- end }}
  tail_files: false
  # Harvester closing options
  close_eof: false
  close_inactive: 5m
  close_removed: false
  close_renamed: false
  ignore_older: {{ if .IgnoreOlder }}{{ .IgnoreOlder }}{{ else }}48h{{ end }}
  # State options
  clean_removed: true
  clean_inactive: 72h
{{- end}}

