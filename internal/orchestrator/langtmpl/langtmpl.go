// Package langtmpl provides language-specific code generation templates.
// Built-in templates for go, python, typescript. Unknown languages can
// be bootstrapped via LLM and persisted to .yanxi/lang-templates/<lang>.json.
package langtmpl

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
)

// LangTemplate defines all language-specific code generation patterns.
type LangTemplate struct {
	Language string `json:"language"`
	Version  string `json:"version"` // template schema version

	// File extension (without dot)
	Ext string `json:"ext"`

	// Handler rules
	Handler struct {
		// Whether the entry point name must be capitalized (Go: true, Python: false)
		CapitalizeEntry bool `json:"capitalize_entry"`
		// Package/namespace declaration template
		PackageDecl string `json:"package_decl"`
		// Import syntax template: receives {{.Name}} (module name) and {{.Alias}}
		ImportSyntax string `json:"import_syntax"`
		// How to reference a module's handler from another module
		HandlerRef string `json:"handler_ref"`
		// Comment prefix
		Comment string `json:"comment"`
		// Comment wrapping long description
		CommentBlockOpen  string `json:"comment_block_open"`
		CommentBlockClose string `json:"comment_block_close"`

		// Handler stub template (single entry)
		// Receives {{.Name}}, {{.Entry}}, {{.PkgDecl}}
		StubTmpl string `json:"stub_tmpl"`
		// Multi-entry handler stub
		// Receives {{.Name}}, {{.Entries}} ([]string), {{.PkgDecl}}
		StubEntriesTmpl string `json:"stub_entries_tmpl"`
	} `json:"handler"`

	// Wire rules
	Wire struct {
		// Template for each import line in the wire file
		// Receives {{.Name}}, {{.Alias}}
		ImportLine string `json:"import_line"`
		// Template for each handler map entry
		// Receives {{.Name}}, {{.Alias}}, {{.Entry}}, {{.HasEntries}}
		MapEntryLine string `json:"map_entry_line"`
		// Whether the wire file uses a "handlers map" pattern (Go) vs "if/elif" (Python)
		UseMapPattern bool `json:"use_map_pattern"`
	} `json:"wire"`

	// Validate rules (v1.0.0)
	Validate struct {
		// Regex to find entry function definitions in source
		// Receives {{.Entry}} as the entry name to look for
		EntryRegex string `json:"entry_regex,omitempty"`
		// Regex to find lifecycle hook functions
		// Receives {{.Name}} as the function name
		LifecycleRegex string `json:"lifecycle_regex,omitempty"`
		// Regex to extract exportable function/type names from source
		ExportFuncRegex string `json:"export_func_regex,omitempty"`
		// Regex to extract cross-module calls from source
		// Should capture module_name and function_name
		CallExtractRegex string `json:"call_extract_regex,omitempty"`
		// Import extraction regex — captures full import paths
		ImportExtractRegex string `json:"import_extract_regex,omitempty"`
		// Runtime command for running tests, with {{.File}} placeholder
		TestRuntime string `json:"test_runtime,omitempty"`
		// Language standard library packages (comma-separated or regex)
		StdlibPattern string `json:"stdlib_pattern,omitempty"`
	} `json:"validate,omitempty"`

	// Module.json defaults
	DefaultEntry string `json:"default_entry"`
}

// TemplateVars holds the data passed to templates.
type TemplateVars struct {
	Name       string
	Alias      string
	Entry      string
	EntryName  string
	Entries    []string
	PkgDecl    string
	HasEntries bool
}

// built-in templates
func builtin(lang string) *LangTemplate {
	switch lang {
	case "go":
		return goTemplate()
	case "python":
		return pyTemplate()
	case "typescript", "javascript":
		return tsTemplate()
	}
	return nil
}

func goTemplate() *LangTemplate {
	t := &LangTemplate{Language: "go", Version: "1", Ext: "go"}
	t.Handler.CapitalizeEntry = true
	t.Handler.PackageDecl = "package {{.Name}}"
	t.Handler.ImportSyntax = `import {{.Alias}} "yanxipro/source/modules/{{.Name}}"`
	t.Handler.HandlerRef = "{{.Alias}}.Handler"
	t.Handler.Comment = "//"
	t.Handler.CommentBlockOpen = "/*"
	t.Handler.CommentBlockClose = "*/"
	t.Handler.StubTmpl = `package {{.Name}}

import "fmt"

// {{.Entry}} processes input and returns result.
func {{.Entry}}(d map[string]interface{}) map[string]interface{} {
    return map[string]interface{}{"result": fmt.Sprintf("%%v not implemented", d["action"])}
}
`
	t.Handler.StubEntriesTmpl = `package {{.Name}}

import "fmt"
{{range .Entries}}
func {{.}}(d map[string]interface{}) map[string]interface{} {
    return map[string]interface{}{"result": "{{.}} not implemented"}
}
{{end}}`
	t.Wire.ImportLine = `{{.Alias}} "yanxipro/source/modules/{{.Name}}"`
	t.Wire.MapEntryLine = `"{{.Name}}": wrap({{.Alias}}.{{.Entry}}),`
	t.Wire.UseMapPattern = true
	t.Validate.EntryRegex = `func\s+{{.Entry}}\s*\(`
	t.Validate.LifecycleRegex = `(?:func|method)\s+{{.Name}}\s*\(`
	t.Validate.ExportFuncRegex = `(?m)^(?:func|type|var|const)\s+([A-Z]\w*)`
	t.Validate.CallExtractRegex = ``
	t.Validate.ImportExtractRegex = `"([^"]+)"`
	t.Validate.TestRuntime = `go run {{.File}}`
	t.Validate.StdlibPattern = `^(fmt|os|io|strings|strconv|encoding/json|encoding/xml|regexp|net|net/http|sync|time|log|math|sort|path|path/filepath|flag|bytes|bufio|context|errors|reflect)`
	t.DefaultEntry = "Handler"
	return t
}

func pyTemplate() *LangTemplate {
	t := &LangTemplate{Language: "python", Version: "1", Ext: "py"}
	t.Handler.CapitalizeEntry = false
	t.Handler.PackageDecl = ""
	t.Handler.ImportSyntax = `from source.modules.{{.Name}}.{{.Name}} import {{.Entry}} as handler_{{.Name}}`
	t.Handler.HandlerRef = "handler_{{.Name}}"
	t.Handler.Comment = "#"
	t.Handler.CommentBlockOpen = `"""`
	t.Handler.CommentBlockClose = `"""`
	t.Handler.StubTmpl = `"""{{.Name}} module"""

def {{.Entry}}(d):
    return {"result": f"{d.get('action','')} not implemented"}
`
	t.Handler.StubEntriesTmpl = `"""{{.Name}} module"""

{{range .Entries}}
def {{.}}(d):
    return {"result": "{{.}} not implemented"}

{{end}}`
	t.Wire.ImportLine = `from source.modules.{{.Name}}.{{.Name}} import {{.Entry}} as handler_{{.Name}}`
	t.Wire.MapEntryLine = ``
	t.Wire.UseMapPattern = false
	t.Validate.EntryRegex = `def\s+{{.Entry}}\s*\(`
	t.Validate.LifecycleRegex = `(?:def|async def)\s+{{.Name}}\s*\(`
	t.Validate.ExportFuncRegex = `(?m)^(?:def|class|async def)\s+([a-zA-Z]\w*)\s*\(`
	t.Validate.CallExtractRegex = ``
	t.Validate.ImportExtractRegex = `(?m)^\s*(?:import|from)\s+(\S+)`
	t.Validate.TestRuntime = `python -c {{.File}}`
	t.Validate.StdlibPattern = `^(os|sys|json|re|math|time|datetime|collections|functools|itertools|pathlib|typing|io|logging|abc|dataclasses|enum|hashlib|copy|random|uuid|inspect|subprocess|threading|http|urllib|base64|csv|glob|xml|html|unittest|argparse)`
	t.DefaultEntry = "handler"
	return t
}

func tsTemplate() *LangTemplate {
	t := &LangTemplate{Language: "typescript", Version: "1", Ext: "ts"}
	t.Handler.CapitalizeEntry = false
	t.Handler.PackageDecl = ""
	t.Handler.ImportSyntax = `import { {{.Entry}} } from './modules/{{.Name}}/{{.Name}}'`
	t.Handler.HandlerRef = "{{.Entry}}Ref"
	t.Handler.Comment = "//"
	t.Handler.CommentBlockOpen = "/*"
	t.Handler.CommentBlockClose = "*/"
	t.Handler.StubTmpl = `// {{.Name}} module
interface Input { [key: string]: any }
interface Output { result?: any; error?: { code: string; message: string; retryable: boolean } }

export function {{.Entry}}(d: Input): Output {
    return { result: d.action + " not implemented" }
}
`
	t.Handler.StubEntriesTmpl = `// {{.Name}} module
interface Input { [key: string]: any }
interface Output { result?: any; error?: { code: string; message: string; retryable: boolean } }
{{range .Entries}}
export function {{.}}(d: Input): Output {
    return { result: "{{.}} not implemented" }
}
{{end}}`
	t.Wire.ImportLine = `import { {{.Entry}} as {{.Alias}}_{{.Entry}} } from './modules/{{.Name}}/{{.Name}}'`
	t.Wire.MapEntryLine = ``
	t.Wire.UseMapPattern = false
	t.Validate.EntryRegex = `(?:export\s+)?(?:async\s+)?function\s+{{.Entry}}\s*\(|{{.Entry}}\s*[=:]\s*(?:async\s*)?(?:function|\(.*\)\s*=>)`
	t.Validate.LifecycleRegex = `(?:export\s+)?(?:async\s+)?function\s+{{.Name}}\s*\(|{{.Name}}\s*[=:]\s*(?:async\s*)?function`
	t.Validate.ExportFuncRegex = `(?m)^(?:export\s+)?(?:function|const|class|let|var)\s+(\w+)`
	t.Validate.CallExtractRegex = ``
	t.Validate.ImportExtractRegex = `(?:from|require)\s*\(?\s*['"]([^'"]+)['"]\s*\)?`
	t.Validate.TestRuntime = `node -e {{.File}}`
	t.Validate.StdlibPattern = `^(fs|path|os|http|https|url|util|stream|crypto|events|buffer|child_process|assert|querystring|net|dns|cluster|readline|tls|zlib)`
	t.DefaultEntry = "handler"
	return t
}

// Resolve returns the language template for a given language.
// Checks .yanxi/lang-templates/ first, falls back to built-in.
func Resolve(projectDir, lang string) (*LangTemplate, error) {
	// Check project-level override
	tmplPath := filepath.Join(projectDir, ".yanxi", "lang-templates", lang+".json")
	if data, err := os.ReadFile(tmplPath); err == nil {
		var t LangTemplate
		if json.Unmarshal(data, &t) == nil {
			return &t, nil
		}
	}

	// Fall back to built-in
	if t := builtin(lang); t != nil {
		return t, nil
	}

	return nil, fmt.Errorf("no template for language %q (create .yanxi/lang-templates/%s.json)", lang, lang)
}

// Save persists a language template to the project directory for future use.
func Save(projectDir string, t *LangTemplate) error {
	dir := filepath.Join(projectDir, ".yanxi", "lang-templates")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, t.Language+".json"), data, 0644)
}

// EntryName returns the correct entry point name for this language.
// For Go, "handler" becomes "Handler"; for others, stays as-is.
func (t *LangTemplate) EntryName(raw string) string {
	if t.Handler.CapitalizeEntry && len(raw) > 0 {
		return strings.ToUpper(raw[:1]) + raw[1:]
	}
	return raw
}

// RenderImport renders a single import line.
func (t *LangTemplate) RenderImport(name, alias, entry string) string {
	return render(t.Wire.ImportLine, map[string]string{
		"Name": name, "Alias": alias, "Entry": entry,
	})
}

// RenderMapEntry renders a handler map entry line.
func (t *LangTemplate) RenderMapEntry(name, alias, entry string) string {
	if t.Wire.MapEntryLine == "" {
		return ""
	}
	return render(t.Wire.MapEntryLine, map[string]string{
		"Name": name, "Alias": alias, "Entry": entry,
	})
}

// RenderStub renders a handler stub.
func (t *LangTemplate) RenderStub(name, entry string) string {
	pkgDecl := ""
	if strings.Contains(t.Handler.PackageDecl, "{{") {
		pkgDecl = render(t.Handler.PackageDecl, map[string]string{"Name": name})
	}
	return render(t.Handler.StubTmpl, map[string]string{
		"Name": name, "Entry": entry, "PkgDecl": pkgDecl,
	})
}

// RenderStubEntries renders a multi-entry stub.
func (t *LangTemplate) RenderStubEntries(name string, entries []string) string {
	pkgDecl := ""
	if strings.Contains(t.Handler.PackageDecl, "{{") {
		pkgDecl = render(t.Handler.PackageDecl, map[string]string{"Name": name})
	}
	// Build entries block
	var entriesBlock strings.Builder
	for _, e := range entries {
		entriesBlock.WriteString(render(t.Handler.StubEntriesTmpl, map[string]string{
			"Name": name, "Entry": e, "PkgDecl": pkgDecl,
			"Entries": strings.Join(entries, ", "),
		}))
	}
	return entriesBlock.String()
}

// WireImports renders all import lines for a set of modules.
func (t *LangTemplate) WireImports(modules []ModuleInfo) string {
	var b strings.Builder
	for _, m := range modules {
		b.WriteString(t.RenderImport(m.Name, m.Alias, m.Entry))
		b.WriteString("\n")
	}
	return b.String()
}

// WireMap renders handler map entries for Go-like languages.
func (t *LangTemplate) WireMap(modules []ModuleInfo) string {
	if !t.Wire.UseMapPattern {
		return ""
	}
	var b strings.Builder
	for _, m := range modules {
		line := t.RenderMapEntry(m.Name, m.Alias, m.Entry)
		if line != "" {
			b.WriteString("\t" + line + "\n")
		}
	}
	return b.String()
}

// ModuleInfo is a lightweight module descriptor for wire generation.
type ModuleInfo struct {
	Name       string
	Alias      string
	Entry      string
	Entries    []string
	DependsOn  []string
}

// render expands a Go template string with map data.
func render(tmpl string, data map[string]string) string {
	t, err := template.New("").Parse(tmpl)
	if err != nil {
		return tmpl
	}
	var b strings.Builder
	if err := t.Execute(&b, data); err != nil {
		return tmpl
	}
	return b.String()
}

// EntryRegex compiles the entry regex with the given entry name.
func (t *LangTemplate) EntryRegex(name string) string {
	if t.Validate.EntryRegex == "" {
		return `def\s+` + regexp.QuoteMeta(name) + `\s*\(`
	}
	return strings.ReplaceAll(t.Validate.EntryRegex, "{{.Entry}}", regexp.QuoteMeta(name))
}

// LifecycleRegex compiles the lifecycle regex with the given function name.
func (t *LangTemplate) LifecycleRegex(name string) string {
	if t.Validate.LifecycleRegex == "" {
		return `(?:def|func|function)\s+` + regexp.QuoteMeta(name) + `\s*\(`
	}
	return strings.ReplaceAll(t.Validate.LifecycleRegex, "{{.Name}}", regexp.QuoteMeta(name))
}

// ExportFuncRegex returns the compiled export function extraction regex.
func (t *LangTemplate) ExportFuncRegex() string {
	if t.Validate.ExportFuncRegex == "" {
		return `(?m)^(?:def|func|function)\s+(\w+)`
	}
	return t.Validate.ExportFuncRegex
}

// ImportExtractRegex returns the import extraction regex.
func (t *LangTemplate) ImportExtractRegex() string {
	if t.Validate.ImportExtractRegex == "" {
		return `(?:from|import|require)\s+(\S+)`
	}
	return t.Validate.ImportExtractRegex
}

// DefaultEntryName returns the default entry point name for this language.
func (t *LangTemplate) DefaultEntryName() string {
	return t.EntryName(t.DefaultEntry)
}
