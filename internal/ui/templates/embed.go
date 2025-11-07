package templates

import (
	"embed"
	"encoding/json"
	"html/template"
)

//go:embed *.tmpl
var files embed.FS

// Parse parses the named template from the embedded filesystem.
func Parse(name string) (*template.Template, error) {
	return template.New(name).Funcs(template.FuncMap{
		"safeHTML": func(s string) template.HTML { return template.HTML(s) },
		"toJSON": func(v any) template.JS {
			data, err := json.Marshal(v)
			if err != nil {
				return template.JS("null")
			}
			return template.JS(data)
		},
	}).ParseFS(files, name)
}
