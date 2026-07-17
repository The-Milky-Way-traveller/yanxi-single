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
  "validate": {
    "entry_regex": "regex to find an entry function in source code. Use {{.Entry}} as placeholder for the entry name. Example for Python: 'def\\s+{{.Entry}}\\s*\\('",
    "lifecycle_regex": "regex to find a lifecycle function (setup/teardown/health). Use {{.Name}} placeholder.",
    "export_func_regex": "regex to extract all exported function/type/variable names from source. Must capture the name in group 1.",
    "import_extract_regex": "regex to extract all import paths/statements from source code.",
    "test_runtime": "command template to run a test file. Use {{.File}} placeholder.",
    "stdlib_pattern": "regex pattern matching standard library package prefixes. Example: '^(os|sys|json|re)'"
  },
  "default_entry": "handler or Handler"
}

Provide realistic, working regex patterns for %s source code analysis.`, lang, lang, lang)
}
