{{- define "entrypoint_imports" -}}
{{- range plannedImports -}}
{{- if .TypeOnly }}
import type {{ if .Default }}{{ .Default }}{{ else }}{ {{ range $i, $n := .Names }}{{ if $i }}, {{ end }}{{ $n }}{{ end }} }{{ end }} from "{{ .From }}"
{{- else if and .SideEffect (not .Names) (not .Namespace) (not .Default) }}
import "{{ .From }}"
{{- else -}}
{{- if .SideEffect }}
import "{{ .From }}"
{{- end -}}
{{- if .Namespace }}
import {{ .Namespace }} from "{{ .From }}"
{{- end -}}
{{- if or .Default .Names }}
import {{ if .Default }}{{ .Default }}{{ if .Names }}, {{ end }}{{ end }}{{ if .Names }}{ {{ range $i, $n := .Names }}{{ if $i }}, {{ end }}{{ $n }}{{ end }} }{{ end }} from "{{ .From }}"
{{- end -}}
{{- end -}}
{{- end }}
{{- end -}}
