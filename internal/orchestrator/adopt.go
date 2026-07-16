// Package orchestrator — external code adoption: scan any directory,
// generate LLM transformation prompt, and commit adopted modules.
package orchestrator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// ── AdoptAnalysis: result of scanning an external directory ──

// AdoptAnalysis describes what was found in an external directory.
type AdoptAnalysis struct {
	SourceDir     string   `json:"source_dir"`
	SuggestedName string   `json:"suggested_name"`
	Language      string   `json:"language"`
	Files         []string `json:"files"`
	ExportCount   int      `json:"export_count"`
	Exports       []string `json:"exports,omitempty"`
	PackageName   string   `json:"package_name,omitempty"`
	HasMain       bool     `json:"has_main"`
}

// AnalyzeExternalDir scans a project-local directory (e.g. pkg/util, internal/helpers)
// and returns an analysis with suggested module name and export summary.
// It does NOT modify any files.
func AnalyzeExternalDir(root, dirPath, lang string) (*AdoptAnalysis, error) {
	absDir := dirPath
	if !filepath.IsAbs(dirPath) {
		absDir = filepath.Join(root, dirPath)
	}
	info, err := os.Stat(absDir)
	if err != nil {
		return nil, fmt.Errorf("directory %q not found: %w", dirPath, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%q is not a directory", dirPath)
	}

	// Resolve language if auto
	if lang == "" || lang == "auto" {
		lang = detectDirLang(absDir)
	}

	analysis := &AdoptAnalysis{
		SourceDir: dirPath,
		Language:  lang,
	}

	// Scan files
	entries, err := os.ReadDir(absDir)
	if err != nil {
		return nil, err
	}
	extSet := map[string]bool{"py": true, "ts": true, "js": true, "go": true}
	var srcFiles []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := filepath.Ext(e.Name())
		if len(ext) > 0 {
			trimExt := ext[1:] // remove dot
			if extSet[trimExt] {
				srcFiles = append(srcFiles, e.Name())
				fullPath := filepath.Join(absDir, e.Name())
				data, err := os.ReadFile(fullPath)
				if err != nil {
					continue
				}
				exports := extractExports(string(data), lang)

				// Check for main()
				if hasMain(string(data), lang) {
					analysis.HasMain = true
				}

				// Package name (first declaration)
				if analysis.PackageName == "" {
					analysis.PackageName = extractPackageName(string(data), lang)
				}

				analysis.Exports = append(analysis.Exports, exports...)
			}
		}
	}
	analysis.Files = srcFiles
	analysis.ExportCount = len(analysis.Exports)

	// Sort and deduplicate exports
	sort.Strings(analysis.Exports)
	deduped := make([]string, 0, len(analysis.Exports))
	seen := map[string]bool{}
	for _, e := range analysis.Exports {
		if !seen[e] {
			seen[e] = true
			deduped = append(deduped, e)
		}
	}
	analysis.Exports = deduped
	analysis.ExportCount = len(deduped)

	// Suggest module name: use the last segment of the directory path
	parts := strings.Split(strings.TrimSuffix(dirPath, string(filepath.Separator)), string(filepath.Separator))
	analysis.SuggestedName = parts[len(parts)-1]

	// Sanitise: replace - with _, lowercase
	analysis.SuggestedName = strings.ToLower(strings.ReplaceAll(analysis.SuggestedName, "-", "_"))

	return analysis, nil
}

// BuildAdoptPrompt returns a prompt for an LLM to transform external code
// into a yanxi-compatible module. The prompt instructs the LLM to:
//   - Keep all internal functions unchanged
//   - Add a Handler(map[string]interface{}) map[string]interface{} entry point
//   - NOT split the module into smaller modules
//   - Return the complete adapted source code + module.json entries spec
func BuildAdoptPrompt(a *AdoptAnalysis) string {
	var b strings.Builder
	b.WriteString("# Adopt External Code into Yanxi Module\n\n")
	b.WriteString(fmt.Sprintf("Transform the code in `%s` into a yanxi-compatible module named `%s`.\n\n", a.SourceDir, a.SuggestedName))
	b.WriteString("## Rules\n\n")
	b.WriteString("1. **Keep all internal functions and logic unchanged.** Do not refactor, rename, or reorganise them.\n")
	b.WriteString("2. **Add exactly one new entry function** that dispatches by `action` field to existing internal functions:\n")
	if a.Language == "go" {
		b.WriteString("   `func Handler(input map[string]interface{}) map[string]interface{}`\n")
	} else if a.Language == "python" {
		b.WriteString("   `def handler(input: dict) -> dict:`\n")
	} else {
		b.WriteString("   `function handler(input: Record<string, any>): Record<string, any>`\n")
	}
	b.WriteString("3. **Do NOT split** the module into multiple smaller modules. Keep it as a single module with multiple entries.\n")
	b.WriteString("4. The handler maps `input[\"action\"]` to the appropriate internal function.\n")
	b.WriteString("5. **Do NOT delete** the original source directory — yanxi will handle file placement and cleanup.\n\n")

	b.WriteString(fmt.Sprintf("## Source directory: %s\n", a.SourceDir))
	b.WriteString(fmt.Sprintf("## Suggested module name: `%s`\n", a.SuggestedName))
	b.WriteString(fmt.Sprintf("## Language: %s\n", a.Language))
	if a.PackageName != "" {
		b.WriteString(fmt.Sprintf("## Current package: `%s`\n", a.PackageName))
	}
	b.WriteString(fmt.Sprintf("## Files: %s\n", strings.Join(a.Files, ", ")))
	if a.ExportCount > 0 {
		b.WriteString(fmt.Sprintf("## Exported functions/types (%d):\n", a.ExportCount))
		for _, exp := range a.Exports {
			b.WriteString(fmt.Sprintf("  - `%s`\n", exp))
		}
	}
	if a.HasMain {
		b.WriteString("\n**Note**: This code contains a `main()` entry. In the adapted module, main() should remain as-is but the module will be called through Handler().\n")
	}

	b.WriteString("\n## Output Format\n\n")
	b.WriteString("Return a JSON object with exactly two keys:\n")
	b.WriteString("```json\n")
	b.WriteString("{\n")
	b.WriteString("  \"adapted_source\": \"<the complete adapted source file contents, with all original functions plus the new Handler>\",\n")
	b.WriteString("  \"entries\": [\n")
	b.WriteString("    {\"name\": \"<entry_name>\", \"description\": \"<what this entry does>\", ")
	b.WriteString("\"input_schema\": {...}, \"output_schema\": {...}}\n")
	b.WriteString("  ]\n")
	b.WriteString("}\n")
	b.WriteString("```\n\n")
	b.WriteString("`input_schema` and `output_schema` are JSON Schema objects describing the expected input and output for each entry.\n")
	b.WriteString("Each entry's `name` is the action value that dispatches to it.\n")
	b.WriteString("Include the main handler entry (e.g. `handler` or `serve`) as one of the entries when applicable.\n")
	return b.String()
}

// AdoptCommitParams describes what commit needs to finalise an adoption.
type AdoptCommitParams struct {
	ModuleName    string `json:"module_name"`
	SourceDir     string `json:"source_dir"`     // original external directory (relative to project root)
	Language      string `json:"language"`
	AdaptedSource string `json:"adapted_source"` // LLM-transformed source code
	// Entries is the parsed entries from LLM output — can also be provided as raw JSON
	EntriesJSON string `json:"entries_json,omitempty"`
}

// AdoptResult describes what was done.
type AdoptResult struct {
	ModuleName string   `json:"module_name"`
	Created    bool     `json:"created"`
	Wired      bool     `json:"wired"`
	Synced     bool     `json:"synced"`
	DeletedSrc bool     `json:"deleted_src"`
	Warnings   []string `json:"warnings,omitempty"`
}

// AdoptCommit completes the adoption process:
// 1. Write adapted source to source/modules/<name>/<name>.<ext>
// 2. Generate module.json
// 3. Delete original source directory
// 4. Wire into main entry
// 5. Regenerate AIexplain
func AdoptCommit(root string, params *AdoptCommitParams) (*AdoptResult, error) {
	result := &AdoptResult{ModuleName: params.ModuleName}
	InvalidateDiscoverCache(root)

	// 1. Parse entries from JSON or build default
	entries := make(map[string]interface{})
	if params.EntriesJSON != "" {
		var parsedEntries []map[string]interface{}
		if err := json.Unmarshal([]byte(params.EntriesJSON), &parsedEntries); err != nil {
			return nil, fmt.Errorf("invalid entries_json: %w", err)
		}
		for _, e := range parsedEntries {
			name, _ := e["name"].(string)
			if name == "" {
				continue
			}
			entryDef := make(map[string]interface{})
			if desc, ok := e["description"].(string); ok {
				entryDef["description"] = desc
			}
			if is, ok := e["input_schema"]; ok {
				entryDef["input_schema"] = is
			} else {
				entryDef["input_schema"] = map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"action": map[string]interface{}{"type": "string"},
					},
				}
			}
			if os, ok := e["output_schema"]; ok {
				entryDef["output_schema"] = os
			} else {
				entryDef["output_schema"] = map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"result": map[string]interface{}{},
					},
				}
			}
			entries[name] = entryDef
		}
	}
	if len(entries) == 0 {
		// Default single entry
		entryName := "handler"
		if params.Language == "go" {
			entryName = "Handler"
		}
		entries[entryName] = map[string]interface{}{
			"description": params.ModuleName + " module entry point",
			"input_schema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{"type": "string"},
				},
			},
			"output_schema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"result": map[string]interface{}{},
				},
			},
		}
	}

	// 2. Write module files
	ext := langExt(params.Language)
	modDir := filepath.Join(root, "source", "modules", params.ModuleName)
	if err := os.MkdirAll(modDir, 0755); err != nil {
		return nil, fmt.Errorf("create module dir: %w", err)
	}

	// Write adapted source
	implPath := filepath.Join(modDir, params.ModuleName+"."+ext)
	if err := os.WriteFile(implPath, []byte(params.AdaptedSource), 0644); err != nil {
		return nil, fmt.Errorf("write source: %w", err)
	}

	// Build module.json
	entryNames := make([]string, 0, len(entries))
	for k := range entries {
		entryNames = append(entryNames, k)
	}
	sort.Strings(entryNames)

	hasNonDefault := false
	for _, n := range entryNames {
		if n != "handler" && n != "Handler" {
			hasNonDefault = true
			break
		}
	}

	// Build interface value — multi-entry or single flat format
	var interfaceVal interface{}
	if len(entries) > 1 || hasNonDefault {
		// Multi-entry: use entries map
		iface := make(map[string]interface{})
		iface["description"] = params.ModuleName + " module (adopted)"
		iface["entries"] = entries
		interfaceVal = iface
	} else {
		// Single entry: use the old flat format
		for name, v := range entries {
			if vv, ok := v.(map[string]interface{}); ok {
				interfaceVal = map[string]interface{}{
					"entry":        name,
					"description":  vv["description"],
					"input_schema": vv["input_schema"],
					"output_schema": vv["output_schema"],
				}
			}
			break
		}
		if interfaceVal == nil {
			interfaceVal = map[string]interface{}{
				"entry":       entryNames[0],
				"description": params.ModuleName + " module",
			}
		}
	}

	contractMap := map[string]interface{}{
		"name":         params.ModuleName,
		"version":      "1.0.0",
		"status":       "active",
		"owner_agent":  "agent",
		"language":     params.Language,
		"dependencies": []string{},
		"interface":    interfaceVal,
	}
	contractJSON, err := json.MarshalIndent(contractMap, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal module.json: %w", err)
	}
	if err := os.WriteFile(filepath.Join(modDir, "module.json"), contractJSON, 0644); err != nil {
		return nil, fmt.Errorf("write module.json: %w", err)
	}
	result.Created = true

	// 3. Delete original source directory
	origDir := params.SourceDir
	if !filepath.IsAbs(origDir) {
		origDir = filepath.Join(root, origDir)
	}
	if err := os.RemoveAll(origDir); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("could not delete original directory %q: %v", params.SourceDir, err))
	} else {
		result.DeletedSrc = true
	}

	// 4. Wire into main entry
	ov := ModuleDiscover(root)
	if ov.ModuleCount > 0 {
		if _, err := WireMain(root, ov.Modules); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("wire: %v", err))
		} else {
			result.Wired = true
		}
	}

	// 5. Regenerate AIexplain
	EnsureAIExplain(root)
	result.Synced = true

	return result, nil
}

// ── Module Deprecation / Archival ──

// DeprecateModule changes a module's status to "deprecated" or "archived",
// records an ADR, and updates the validation state.
func DeprecateModule(root, modName, newStatus, reason string) error {
	if newStatus != "deprecated" && newStatus != "archived" {
		return fmt.Errorf("new_status must be 'deprecated' or 'archived', got %q", newStatus)
	}
	InvalidateDiscoverCache(root)
	modDir := filepath.Join(root, "source", "modules", modName)
	modJSONPath := filepath.Join(modDir, "module.json")
	data, err := os.ReadFile(modJSONPath)
	if err != nil {
		return fmt.Errorf("module %q not found: %w", modName, err)
	}
	var contract map[string]interface{}
	if err := json.Unmarshal(data, &contract); err != nil {
		return fmt.Errorf("invalid module.json: %w", err)
	}
	oldStatus, _ := contract["status"].(string)
	contract["status"] = newStatus
	contractJSON, _ := json.MarshalIndent(contract, "", "  ")
	if err := os.WriteFile(modJSONPath, contractJSON, 0644); err != nil {
		return fmt.Errorf("write module.json: %w", err)
	}

	title := fmt.Sprintf("Deprecate module %s", modName)
	if newStatus == "archived" {
		title = fmt.Sprintf("Archive module %s", modName)
	}
	ctx := fmt.Sprintf("Module %s was %s. Reason: %s", modName, oldStatus, reason)
	decision := fmt.Sprintf("Changed module %s status from %q to %q.", modName, oldStatus, newStatus)
	cons := fmt.Sprintf("Module %s is now %s. Dependents should migrate to alternatives.", modName, newStatus)
	_ = WriteADR(root, ADR{
		Number:      "ADR-AUTO",
		Title:       title,
		Status:      "accepted",
		Context:     ctx,
		Decision:    decision,
		Consequences: cons,
		Module:      modName,
	})

	// Update validation state
	vs := loadValidationState(root)
	vs.Modules[modName] = ModuleValidationState{
		Valid:       true,
		ValidatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	saveValidationState(root, vs)
	return nil
}

// FindDependentsOf returns module names that depend on the given module.
func FindDependentsOf(root, modName string) []string {
	ov := ModuleDiscoverLazy(root)
	var dependents []string
	for _, m := range ov.Modules {
		for _, d := range m.Dependencies {
			if d == modName {
				dependents = append(dependents, m.Name)
			}
		}
	}
	return dependents
}

// extractExports extracts exported function/type/variable names from source code.
func extractExports(src, lang string) []string {
	var exports []string
	switch lang {
	case "go":
		// Exported Go: func Foo, func Foo(...), type Foo, var Foo, const Foo
		re := regexp.MustCompile(`(?m)^(?:func|type|var|const)\s+([A-Z]\w*)`)
		for _, m := range re.FindAllStringSubmatch(src, -1) {
			if len(m) > 1 {
				exports = append(exports, m[1])
			}
		}
	case "python":
		// Top-level def and class (not prefixed with _)
		re := regexp.MustCompile(`(?m)^(?:def|class|async def)\s+([a-zA-Z]\w*)\s*\(`)
		for _, m := range re.FindAllStringSubmatch(src, -1) {
			if len(m) > 1 && !strings.HasPrefix(m[1], "_") {
				exports = append(exports, m[1])
			}
		}
	case "typescript", "javascript":
		// export function, export const, export class, export default
		re := regexp.MustCompile(`(?m)^(?:export\s+)?(?:function|const|class|let|var)\s+(\w+)`)
		for _, m := range re.FindAllStringSubmatch(src, -1) {
			if len(m) > 1 && !strings.HasPrefix(m[1], "_") {
				exports = append(exports, m[1])
			}
		}
	}
	return exports
}

// extractPackageName extracts the package/module name from source.
func extractPackageName(src, lang string) string {
	switch lang {
	case "go":
		re := regexp.MustCompile(`(?m)^package\s+(\w+)`)
		if m := re.FindStringSubmatch(src); len(m) > 1 {
			return m[1]
		}
	case "python":
		return "" // no package declaration
	case "typescript", "javascript":
		return "" // no package declaration
	}
	return ""
}

// hasMain checks if the source has a main entry point.
func hasMain(src, lang string) bool {
	switch lang {
	case "go":
		re := regexp.MustCompile(`(?m)^func\s+main\s*\(`)
		return re.MatchString(src)
	case "python":
		re := regexp.MustCompile(`(?m)^if\s+__name__\s*==\s*['"]__main__['"]\s*:`)
		return re.MatchString(src)
	case "typescript", "javascript":
		return false
	}
	return false
}

// detectDirLang detects the primary language in a directory by file extension count.
func detectDirLang(dir string) string {
	counts := map[string]int{"python": 0, "go": 0, "typescript": 0, "javascript": 0}
	extMap := map[string]string{".py": "python", ".go": "go", ".ts": "typescript", ".js": "javascript"}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "python"
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if lang, ok := extMap[filepath.Ext(e.Name())]; ok {
			counts[lang]++
		}
	}
	best, bestN := "python", 0
	for lang, n := range counts {
		if n > bestN {
			best, bestN = lang, n
		}
	}
	return best
}
