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
      "@ip": ${NODE_IP}
      {{- range $key, $value := .Tags }}
      {{ $key }}: "{{ $value }}"
      {{- end }}
  tail_files: false
  # Harvester closing options
  close_eof: false
  close_inactive: 5m
  close_removed: false
  close_renamed: false
  ignore_older: 48h  
  # State options
  clean_removed: true
  clean_inactive: 72h
{{- end}}

