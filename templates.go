package main

import (
	"bytes"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gofiber/fiber/v2"
)

var templates *template.Template

var (
	reIfStatement = regexp.MustCompile(`\{\%\s*if\s+([^%]+?)\s*==\s*("?[^"%]+?"?)\s*\%\}`)
	reURLStatic   = regexp.MustCompile(`\{\{\s*url_for\('static',\s*filename='([^']+)'\)\s*\}\}`)
	reURLIndex    = regexp.MustCompile(`\{\{\s*url_for\('index'\)\s*\}\}`)
	reBareVar     = regexp.MustCompile(`\{\{\s*([A-Za-z_][A-Za-z0-9_]*)\s*\}\}`)

	// Go template keywords that must never be turned into field accesses by reBareVar.
	bareVarKeywords = map[string]bool{
		"if": true, "else": true, "end": true, "range": true,
		"with": true, "block": true, "define": true, "template": true,
	}
)

// transformJinja converts the small subset of Jinja2 used by the templates into
// Go html/template syntax so the original templates stay drop-in.
func transformJinja(src string) string {
	src = reIfStatement.ReplaceAllString(src, `{{ if eq .$1 $2 }}`)
	src = strings.ReplaceAll(src, `{% else %}`, `{{ else }}`)
	src = strings.ReplaceAll(src, `{% endif %}`, `{{ end }}`)
	src = reURLStatic.ReplaceAllString(src, `{{ url_for "static" (dict "filename" "$1") }}`)
	src = reURLIndex.ReplaceAllString(src, `{{ url_for "index" }}`)
	src = reBareVar.ReplaceAllStringFunc(src, func(m string) string {
		name := reBareVar.FindStringSubmatch(m)[1]
		if bareVarKeywords[name] {
			return m
		}
		return "{{ ." + name + " }}"
	})
	return src
}

func initTemplates() {
	funcs := template.FuncMap{
		"dict": func(values ...any) map[string]any {
			m := map[string]any{}
			for i := 0; i+1 < len(values); i += 2 {
				m[fmt.Sprintf("%v", values[i])] = values[i+1]
			}
			return m
		},
		"url_for": func(args ...any) string {
			if len(args) == 0 {
				return ""
			}
			endpoint, ok := args[0].(string)
			if !ok {
				return ""
			}
			switch endpoint {
			case "static":
				if len(args) > 1 {
					if m, ok := args[1].(map[string]any); ok {
						if f, ok := m["filename"].(string); ok {
							return "/static/" + f
						}
					}
				}
			case "index":
				return "/student-portal"
			}
			return ""
		},
	}
	t := template.New("").Funcs(funcs)
	for _, glob := range []string{"templates/*.html", "templates/admin/*.html", "templates/tests/*.html"} {
		matches, err := filepath.Glob(glob)
		if err != nil {
			panic(err)
		}
		for _, path := range matches {
			b, err := os.ReadFile(path)
			if err != nil {
				panic(err)
			}
			name := strings.TrimPrefix(filepath.ToSlash(path), "templates/")
			if _, err := t.New(name).Parse(transformJinja(string(b))); err != nil {
				panic(fmt.Sprintf("template %s: %v", name, err))
			}
		}
	}
	templates = t
}

func render(c *fiber.Ctx, name string, data map[string]any) error {
	if data == nil {
		data = map[string]any{}
	}
	var buf bytes.Buffer
	if err := templates.ExecuteTemplate(&buf, name, data); err != nil {
		fmt.Printf("Template render error (%s): %v\n", name, err)
		return c.Status(500).SendString("Internal Server Error")
	}
	c.Set(fiber.HeaderContentType, fiber.MIMETextHTMLCharsetUTF8)
	return c.Send(buf.Bytes())
}

func subtitleURL(fid, filename string) string {
	return filepath.ToSlash(fmt.Sprintf("/subtitles/%s/%s", fid, filename))
}
