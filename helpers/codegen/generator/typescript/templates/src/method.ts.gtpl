{{- /* Write method. */ -}}
{{ define "method" }}
	{{- $parentName := .ParentObject.Name }}
	{{- $required := GetRequiredArgs .Args }}
	{{- $optionals := GetOptionalArgs .Args }}

	{{- if and ($optionals) (eq $parentName "Query") }}
		{{- $parentName = "Client" }}
	{{- end }}

	{{- /* Write method comment. */ -}}
	{{- template "method_comment" . }}

	{{- /* Write method name. */ -}}
	{{- "" }}  {{ .Name | FormatMemberName }} = (

	{{- /* Write required arguments. */ -}}
	{{- if $required }}
		{{- template "args" . }}
	{{- end }}

	{{- /* Write optional arguments */ -}}
	{{- if $optionals }}
		{{- /* Insert a comma if there was previous required arguments. */ -}}
		{{- if $required }}, {{ end }}
		{{- "" }}opts?: {{ OptsTypeName $parentName .Name }} {{- with .Directives.SourceMap }} // {{ .Module }} ({{ .Filelink | ModuleRelPath }}) 
		{{ "" }} 
		{{- end }}
	{{- end }}

	{{- /* Write return type. */ -}}
	{{- "" }}){{- "" }}: {{ .TypeRef | FormatOutputType }} => { {{- with .Directives.SourceMap }} // {{ .Module }} ({{ .Filelink | ModuleRelPath }}) {{- end }}
	{{- /* Body is shared between the core and per-module client classes. */ -}}
	{{- template "method_body" . }}
  }
	{{- template "method_alias" (MethodAlias . true) }}
{{- end }}

{{- /* Write a deprecated alias under the name a method had before the engine's
naming rules, when it differs. The dot is a methodAlias, nil for none. */ -}}
{{ define "method_alias" }}
	{{- with . }}

  /**
   * @deprecated use {{ .Name }} instead.
   */
  {{ .Legacy }}: {{ .Class }}["{{ .Name }}"] = (...args) =>
    this.{{ .Name }}(...args)
	{{- end }}
{{- end }}
