// Package langtmpl — LLM-based language template bootstrapping.
// When a language is not in the built-in set, the external agent uses
// BootstrapPrompt() to get an LLM prompt, then calls save_lang_template
// to persist the generated template to .yanxi/lang-templates/<lang>.json.
package langtmpl

import "fmt"

// BootstrapPrompt returns the LLM prompt for generating a LangTemplate.
// The external agent should send this prompt to an LLM, then pass the
// resulting JSON to save_lang_template().
func BootstrapPrompt(lang string) string {
	return fmt.Sprintf(`You are a code generation expert. Generate a JSON language template for the programming language "%s".

The template defines how to generate module stubs and wire code for a micro-module architecture project.
Each module is a file with a single handler function that takes a dict-like input and returns a dict-like output.

Return ONLY valid JSON (no markdown) with this exact structure:

{
  "language": "%s",
  "version": "1",
  "ext": "FILE_EXTENSION_WITHOUT_DOT",
  "handler": {
    "capitalize_entry": true or false,
    "package_decl": "package template if needed, or empty string",
    "import_syntax": "Go template for import line, use {{.Name}} {{.Alias}} {{.Entry}}",
    "handler_ref": "how to reference handler from another module",
    "comment": "comment prefix for the language",
    "stub_tmpl": "Go template for single-entry handler stub. Use {{.Name}} {{.Entry}} {{.PkgDecl}}",
    "stub_entries_tmpl": "Go template for multi-entry handler stub. Use {{.Name}} {{.Entry}} {{.Entries}}"
  },
  "wire": {
    "import_line": "Go template for wire import line, use {{.Name}} {{.Alias}} {{.Entry}}",
    "map_entry_line": "handler map entry for Go-style, or empty for if/elif style",
    "use_map_pattern": true or false
  },
  "default_entry": "handler or Handler"
}

Examples:

For Python:
{"language":"python","ext":"py","handler":{"capitalize_entry":false,"package_decl":"","import_syntax":"from source.modules.{{.Name}}.{{.Name}} import {{.Entry}} as handler_{{.Name}}","handler_ref":"handler_{{.Name}}","comment":"#","stub_tmpl":"\"\"\"{{.Name}} module\"\"\"\n\ndef {{.Entry}}(d):\n    return {\"result\": f\"{d.get('action','')} not implemented\"}\n","stub_entries_tmpl":"\"\"\"{{.Name}} module\"\"\"\n\n{{range .Entries}}\ndef {{.}}(d):\n    return {\"result\": \"{{.}} not implemented\"}\n\n{{end}}"},"wire":{"import_line":"from source.modules.{{.Name}}.{{.Name}} import {{.Entry}} as handler_{{.Name}}","map_entry_line":"","use_map_pattern":false},"default_entry":"handler"}

For Go:
{"language":"go","ext":"go","handler":{"capitalize_entry":true,"package_decl":"package {{.Name}}","import_syntax":"import {{.Alias}} \"project/source/modules/{{.Name}}\"","handler_ref":"{{.Alias}}.Handler","comment":"//","stub_tmpl":"package {{.Name}}\n\nimport \"fmt\"\n\nfunc {{.Entry}}(d map[string]interface{}) map[string]interface{} {\n    return map[string]interface{}{\"result\": fmt.Sprintf(\"%%v not implemented\", d[\"action\"])}\n}\n","stub_entries_tmpl":"package {{.Name}}\n\nimport \"fmt\"\n{{range .Entries}}\nfunc {{.}}(d map[string]interface{}) map[string]interface{} {\n    return map[string]interface{}{\"result\": \"{{.}} not implemented\"}\n}\n{{end}}"},"wire":{"import_line":"{{.Alias}} \"project/source/modules/{{.Name}}\"","map_entry_line":"\"{{.Name}}\": wrap({{.Alias}}.{{.Entry}}),","use_map_pattern":true},"default_entry":"Handler"}

Return ONLY the JSON object, no explanation, no markdown.`, lang, lang)
}
