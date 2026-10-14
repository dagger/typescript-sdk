{{- define "entrypoint_state_helpers" -}}
{{- $module := . -}}
// An interface value crosses the boundary as the engine ID of the object
// implementing it, so the only values that can be returned are ones carrying
// such an ID: an object from a generated client, or an interface value received
// as an argument. A plain object implementing the interface structurally has no
// engine identity, and nothing here can give it one — so it fails saying that,
// rather than inside the dispatcher on a .id() that is not there.
async function __interfaceID(value: any, ifaceName: string): Promise<string | null> {
  if (value === null || value === undefined) {
    return null
  }
  if (typeof value.id !== "function") {
    throw new Error(
      `cannot return a plain object as interface ${ifaceName}: an interface value is the engine ID of the object implementing it, so it has to come from a generated client or arrive as an argument`,
    )
  }
  return await value.id()
}

{{ range $name := sortedKeysObjects $module.Objects -}}
{{- $obj := index $module.Objects $name }}
{{- if and (eq $obj.Kind "class") (not $obj.IsExported) }}
const __cls_{{ $obj.Name }}: any = (() => { const c = getRegisteredClass({{ jsString $obj.Name }}); if (!c) throw new Error("class {{ $obj.Name }} is not registered (missing @object() decorator?)"); return c })()

{{ end -}}
{{ template "entrypoint_class_rebuild" (dict "Obj" $obj "Module" $module) }}

{{ template "entrypoint_class_serialize" (dict "Obj" $obj "Module" $module) }}

{{ end -}}
{{- end -}}

{{- define "entrypoint_class_rebuild" -}}
{{- $obj := .Obj -}}
function rebuild{{ $obj.Name }}(state: any): {{ classTypeRef $obj }} {
{{- if eq $obj.Kind "class" }}
  const __obj = Object.assign(Object.create({{ classRuntimeRef $obj }}.prototype), state ?? {})
{{- else }}
  const __obj: any = { ...(state ?? {}) }
{{- end }}
  if (state) {
{{- range $name := sortedKeysProps $obj.Properties }}
{{- $prop := index $obj.Properties $name }}
{{- if $prop.Type }}
{{- $field := propFieldName $prop }}
{{- $xform := needsTransform $prop.Type }}
{{- $renamed := ne $field $prop.Name }}
{{- if or $xform $renamed }}
    if (state[{{ jsString $field }}] !== undefined && state[{{ jsString $field }}] !== null) {
      __obj[{{ jsString $prop.Name }}] = {{ if $xform }}{{ coerceExpr (printf "state[%s]" (jsString $field)) $prop.Type }}{{ else }}state[{{ jsString $field }}]{{ end }}
    }
{{- if $renamed }}
    delete __obj[{{ jsString $field }}]
{{- end }}
{{- end }}
{{- end }}
{{- end }}
  }
  return __obj
}
{{- end -}}

{{- define "entrypoint_class_serialize" -}}
{{- $obj := .Obj -}}
async function serialize{{ $obj.Name }}(__obj: {{ classTypeRef $obj }}): Promise<any> {
  if (__obj === null || __obj === undefined) return __obj
  const __state: any = { ...__obj }
{{- range $name := sortedKeysProps $obj.Properties }}
{{- $prop := index $obj.Properties $name }}
{{- if $prop.Type }}
{{- $field := propFieldName $prop }}
{{- $xform := needsTransform $prop.Type }}
{{- $renamed := ne $field $prop.Name }}
{{- if or $xform $renamed }}
  if ((__obj as any)[{{ jsString $prop.Name }}] !== undefined && (__obj as any)[{{ jsString $prop.Name }}] !== null) {
    __state[{{ jsString $field }}] = {{ if $xform }}{{ serializeExpr (printf "(__obj as any)[%s]" (jsString $prop.Name)) $prop.Type }}{{ else }}(__obj as any)[{{ jsString $prop.Name }}]{{ end }}
  }
{{- if $renamed }}
  delete __state[{{ jsString $prop.Name }}]
{{- end }}
{{- end }}
{{- end }}
{{- end }}
  return __state
}
{{- end -}}
