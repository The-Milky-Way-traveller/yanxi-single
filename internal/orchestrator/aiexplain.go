package orchestrator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// EnsureAIExplain generates agent-readable skill-card .md documentation.
// Incremental: only regenerates modules whose source files have changed since last sync.
func EnsureAIExplain(root string) {
	lastSync := loadLastSync(root)
	newSync := make(map[string]time.Time)

	modulesDir := filepath.Join(root, "source", "modules")
	if isDirExist(modulesDir) {
		// Pre-scan all modules to build dependency map (for "Depended by" links)
		depMap := make(map[string][]string) // module → list of modules that depend on it
		allModData := make(map[string]map[string]interface{})
		if preEntries, err := os.ReadDir(modulesDir); err == nil {
			for _, e := range preEntries {
				if !e.IsDir() { continue }
				mn := e.Name()
				if d, err2 := os.ReadFile(filepath.Join(modulesDir, mn, "module.json")); err2 == nil {
					var mData map[string]interface{}
					if json.Unmarshal(d, &mData) == nil {
						allModData[mn] = mData
						if deps, ok := mData["dependencies"].([]interface{}); ok {
							for _, dep := range deps {
								if depStr, ok := dep.(string); ok {
									depMap[depStr] = append(depMap[depStr], mn)
								}
							}
						}
					}
				}
			}
		}

		entries, _ := os.ReadDir(modulesDir)
		for _, entry := range entries {
			if !entry.IsDir() { continue }
			modName := entry.Name()
			modDir := filepath.Join(modulesDir, modName)
			aiModDir := filepath.Join(root, "AIexplain", "modules", modName)
			os.MkdirAll(aiModDir, 0755)

			var modData map[string]interface{}
			modJSON := filepath.Join(modDir, "module.json")
			if data, err := os.ReadFile(modJSON); err == nil {
				json.Unmarshal(data, &modData)
			}

			exts := []string{".py", ".ts", ".js", ".go"}
			var implPath, implExt string
			for _, e := range exts {
				p := filepath.Join(modDir, modName+e)
				if fileExists(p) { implPath, implExt = p, e; break }
			}

			// Incremental check
			key := "module:" + modName
			newestMtime := getFileMtime(modJSON)
			if implPath != "" {
				if m := getFileMtime(implPath); m.After(newestMtime) { newestMtime = m }
			}
			newSync[key] = newestMtime
			if prev, ok := lastSync[key]; ok && !prev.Before(newestMtime) {
				continue // skip: no changes
			}

			handlerSig := "handler(input: dict) -> dict"
			docstring := ""
			lang := "python"
			if modData != nil { if l, ok := modData["language"].(string); ok && l != "" { lang = l } }
			var src string
			if implPath != "" {
				if data, err := os.ReadFile(implPath); err == nil {
					src = string(data)
					docstring = extractModuleDocstring(src, lang)
					handlerSig = extractHandlerSignature(src, lang)
				}
			}

			md := fmt.Sprintf("# %s Module\n\n", modName)

			// Status + Version header
			modStatus := "active"
			modVersion := "0.1.0"
			modLang := lang
			if modData != nil {
				if s, _ := modData["status"].(string); s != "" { modStatus = s }
				if v, _ := modData["version"].(string); v != "" { modVersion = v }
				if l, _ := modData["language"].(string); l != "" { modLang = l }
			}
			md += fmt.Sprintf("**Status**: %s | **Version**: %s | **Language**: %s\n\n", modStatus, modVersion, modLang)

			// Purpose
			md += "## Purpose\n\n"
			if modData != nil {
				if desc, ok := modData["description"].(string); ok && desc != "" && !strings.Contains(desc, modName+" module") {
					md += desc + "\n\n"
				} else if docstring != "" {
					md += docstring + "\n\n"
				} else {
					md += modName + " module." + "\n\n"
				}
			} else {
				md += modName + " module." + "\n\n"
			}

			// Dependencies (what this module depends ON)
			if modData != nil {
				if deps, ok := modData["dependencies"].([]interface{}); ok && len(deps) > 0 {
					var depNames []string
					for _, d := range deps { depNames = append(depNames, fmt.Sprintf("%v", d)) }
					md += "**Depends on**: " + strings.Join(depNames, ", ") + "\n\n"
				}
			}

			// Dependents (who depends on this)
			if dependents, ok := depMap[modName]; ok && len(dependents) > 0 {
				md += "**Depended by**: " + strings.Join(dependents, ", ") + "\n\n"
			} else {
				md += "**Depended by**: none\n\n"
			}

			md += fmt.Sprintf("## Interface\n\n`%s`\n\n", handlerSig)
			if modData != nil {
				if iface, ok := modData["interface"].(map[string]interface{}); ok {
					if entries, hasE := iface["entries"].(map[string]interface{}); hasE {
						md += "### Entries\n\n"
						for entryName, entryRaw := range entries {
							entryDef, _ := entryRaw.(map[string]interface{})
							desc := ""
							if d, ok := entryDef["description"].(string); ok && d != "" && !strings.Contains(d, "auto-synced") {
								desc = d
							} else {
								// Try to extract per-entry docstring from source
								entryDoc := extractEntryDocstring(src, entryName, lang)
								if entryDoc != "" {
									desc = entryDoc
								}
							}
							md += fmt.Sprintf("- **`%s`**", entryName)
							if desc != "" {
								md += ": " + desc
							}
							md += "\n"
						}
						md += "\n"
					} else {
						if is, ok := iface["input_schema"]; ok {
							ij, _ := json.MarshalIndent(is, "", "  ")
							md += fmt.Sprintf("### Input Schema\n\n```json\n%s\n```\n\n", string(ij))
						}
						if os, ok := iface["output_schema"]; ok {
							oj, _ := json.MarshalIndent(os, "", "  ")
							md += fmt.Sprintf("### Output Schema\n\n```json\n%s\n```\n\n", string(oj))
						}
					}
				}
			}
			// Usage example: show the real handler signature
			md += "## Usage Example\n\n"
			if modData != nil {
				if mIface, ok := modData["interface"].(map[string]interface{}); ok {
					if ents, hasE := mIface["entries"].(map[string]interface{}); hasE && len(ents) > 0 {
						var firstEntry string
						for k := range ents { firstEntry = k; break }
						md += fmt.Sprintf("```go\n%s.%s(input)\n```\n\n", modName, firstEntry)
					} else {
						md += fmt.Sprintf("```\n%s(input) -> dict\n```\n\n", handlerSig)
					}
				}
			} else {
				md += fmt.Sprintf("```\n%s(input) -> dict\n```\n\n", handlerSig)
			}

			// Error codes
			if modData != nil {
				if errDecls, ok := modData["errors"].(map[string]interface{}); ok && len(errDecls) > 0 {
					md += "## Error Codes\n\n"
					for code, descRaw := range errDecls {
						if desc, _ := descRaw.(string); desc != "" {
							md += fmt.Sprintf("- `%s`: %s\n", code, desc)
						} else {
							md += fmt.Sprintf("- `%s`\n", code)
						}
					}
					md += "\n"
				}
			}

			md += "## Dependencies\n\n"
			if modData != nil {
				if deps, ok := modData["dependencies"].([]interface{}); ok && len(deps) > 0 {
					for _, d := range deps { md += fmt.Sprintf("- `%v`\n", d) }
				} else { md += "None\n" }
			} else { md += "None\n" }
			// Depended by (cross-module links, v5.3.0)
			if dependents, ok := depMap[modName]; ok && len(dependents) > 0 {
				md += "\n## Depended by\n\n"
				for _, dep := range dependents {
					md += fmt.Sprintf("- [`%s`](../%s/%s.md)\n", dep, dep, dep)
				}
				md += "\n"
			}
			md += fmt.Sprintf("\n_Source: `source/modules/%s/%s%s`_\n", modName, modName, implExt)
			os.WriteFile(filepath.Join(aiModDir, modName+".md"), []byte(md), 0644)

			ifaceContent := fmt.Sprintf("# %s Interface\n\n", modName)
			if modData != nil {
				if v, ok := modData["version"].(string); ok { ifaceContent += fmt.Sprintf("**Version**: %s  \n", v) }
				if s, ok := modData["status"].(string); ok { ifaceContent += fmt.Sprintf("**Status**: %s  \n", s) }
				if o, ok := modData["owner_agent"].(string); ok { ifaceContent += fmt.Sprintf("**Owner**: `%s`  \n", o) }
				if l, ok := modData["language"].(string); ok { ifaceContent += fmt.Sprintf("**Language**: %s  \n", l) }
				if iface, ok := modData["interface"].(map[string]interface{}); ok {
					if entries, hasE := iface["entries"].(map[string]interface{}); hasE {
						ifaceContent += fmt.Sprintf("\n## Entry Points (%d)\n\n", len(entries))
						for entryName, entryRaw := range entries {
							entryDef, _ := entryRaw.(map[string]interface{})
							ifaceContent += fmt.Sprintf("### `%s`\n\n", entryName)
							if desc, ok := entryDef["description"].(string); ok && desc != "" {
								ifaceContent += desc + "\n\n"
							}
							if is, ok := entryDef["input_schema"]; ok {
								ij, _ := json.MarshalIndent(is, "", "  ")
								ifaceContent += fmt.Sprintf("**Input**:\n```json\n%s\n```\n\n", string(ij))
							}
							if os, ok := entryDef["output_schema"]; ok {
								oj, _ := json.MarshalIndent(os, "", "  ")
								ifaceContent += fmt.Sprintf("**Output**:\n```json\n%s\n```\n\n", string(oj))
							}
						}
					} else {
						ifaceContent += fmt.Sprintf("## Entry Point\n\n`%s`\n\n", handlerSig)
						if entry, ok := iface["entry"].(string); ok && entry != "" { ifaceContent += fmt.Sprintf("**Entry**: `%s`  \n", entry) }
					}
				}
			} else {
				ifaceContent += fmt.Sprintf("## Entry Point\n\n`%s`\n\n", handlerSig)
			}
			os.WriteFile(filepath.Join(aiModDir, "interface.md"), []byte(ifaceContent), 0644)
		}
	}

	// main/ entry point docs (always regenerated — cheap)
	sourceMain := filepath.Join(root, "source", "main")
	aiMain := filepath.Join(root, "AIexplain", "main")
	os.MkdirAll(aiMain, 0755)
	if isDirExist(sourceMain) {
		entries, _ := os.ReadDir(sourceMain)
		for _, entry := range entries {
			if entry.IsDir() || strings.HasSuffix(entry.Name(), ".pyc") || strings.HasSuffix(entry.Name(), ".pyo") { continue }
			name := entry.Name()
			ext := filepath.Ext(name)
			if ext == ".py" || ext == ".ts" || ext == ".js" || ext == ".go" {
				srcPath := filepath.Join(sourceMain, name)
				aiName := strings.TrimSuffix(name, ext) + ".md"
				if data, err := os.ReadFile(srcPath); err == nil {
					src := string(data)
					sig := extractHandlerSignature(src, extToLang(ext))
					doc := extractModuleDocstring(src, extToLang(ext))
					md := fmt.Sprintf("# %s\n\n_Source: `source/main/%s`_\n\n## Purpose\n\nMain application entry point.\n\n", strings.TrimSuffix(name, ext), name)
					if doc != "" { md += doc + "\n\n" }
					if sig != "handler(input: dict) -> dict" { md += fmt.Sprintf("## Functions\n\n`%s`\n\n", sig) }
					funcRe := regexp.MustCompile(`(?m)^(async\s+)?def\s+(\w+)\s*\(`)
					funcs := funcRe.FindAllStringSubmatch(src, -1)
					if len(funcs) > 1 {
						md += "## Defined Functions\n\n"
						for _, f := range funcs { md += fmt.Sprintf("- `%s()`\n", f[2]) }
						md += "\n"
					}
					os.WriteFile(filepath.Join(aiMain, aiName), []byte(md), 0644)
				}
			}
		}
	}

	// shared-functions-guide.md
	aiDir := filepath.Join(root, "AIexplain")
	sharedPath := filepath.Join(aiDir, "shared-functions-guide.md")
	sharedFuncPath := filepath.Join(root, "source", "main", "shared_functions.py")
	var sharedFuncList string
	if data, err := os.ReadFile(sharedFuncPath); err == nil {
		funcRe := regexp.MustCompile(`(?m)^(async\s+)?def\s+(\w+)\s*\(`)
		funcs := funcRe.FindAllStringSubmatch(string(data), -1)
		if len(funcs) > 0 {
			var lines []string
			for _, f := range funcs { lines = append(lines, fmt.Sprintf("- `%s()`", f[2])) }
			sharedFuncList = strings.Join(lines, "\n") + "\n"
		}
	}
	md := "# Shared Functions Guide\n\nBase functions available to all modules in `source/main/shared_functions.py`.\n\n"
	if sharedFuncList != "" { md += "## Available Functions\n\n" + sharedFuncList + "\n" }
	md += "## Convention\n\n- All modules expose `handler(input: dict) -> dict`\n"
	os.WriteFile(sharedPath, []byte(md), 0644)

	// module-contracts.json + project-architecture.md (always regenerated — cheap)
	mods := DiscoverModules(root)
	contractsPath := filepath.Join(aiDir, "module-contracts.json")
	var contracts []map[string]interface{}
	for _, m := range mods { contracts = append(contracts, m.ToDict()) }
	data, _ := json.MarshalIndent(contracts, "", "  ")
	os.WriteFile(contractsPath, data, 0644)

	archPath := filepath.Join(aiDir, "project-architecture.md")
	var b strings.Builder
	b.WriteString("# Project Architecture\n\n## Module Registry\n\n| Module | Version | Status | Language | Interface |\n")
	b.WriteString("|--------|---------|--------|----------|-----------|\n")
	for _, m := range mods {
		entry := "handler"
		if m.Interface != nil {
			if e, ok := m.Interface["entry"].(string); ok && e != "" { entry = e }
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | `%s(input: dict) -> dict` |\n", m.Name, m.Version, m.Status, m.Language, entry)
	}
	b.WriteString("\n## How to Read\n- `AIexplain/<module>/<module>.md` — understand each module\n- `AIexplain/modules/<name>/interface.md` — API reference\n\n## Convention\nAll modules expose `handler(input: dict) -> dict`.\n")
	os.WriteFile(archPath, []byte(b.String()), 0644)

	// Save sync state
	saveLastSync(root, newSync)
}

// ── Incremental sync helpers ──

func loadLastSync(root string) map[string]time.Time {
	result := make(map[string]time.Time)
	path := filepath.Join(root, ".yanxi", "last_sync.json")
	data, err := os.ReadFile(path)
	if err != nil { return result }
	var raw map[string]string
	if json.Unmarshal(data, &raw) != nil { return result }
	for k, v := range raw {
		if t, err := time.Parse(time.RFC3339, v); err == nil { result[k] = t }
	}
	return result
}

func saveLastSync(root string, state map[string]time.Time) {
	raw := make(map[string]string)
	for k, v := range state { raw[k] = v.Format(time.RFC3339) }
	dir := filepath.Join(root, ".yanxi")
	os.MkdirAll(dir, 0755)
	data, _ := json.MarshalIndent(raw, "", "  ")
	os.WriteFile(filepath.Join(dir, "last_sync.json"), data, 0644)
}

func getFileMtime(path string) time.Time {
	if info, err := os.Stat(path); err == nil { return info.ModTime() }
	return time.Time{}
}

func extractHandlerSignature(src string, lang string) string {
	var re *regexp.Regexp
	switch lang {
	case "go":
		re = regexp.MustCompile(`(?m)^func\s+(Handler|\w+)\s*\([^)]*\)\s*[^(]*\{`)
	case "typescript", "javascript":
		re = regexp.MustCompile(`(?m)^(?:export\s+)?(?:async\s+)?function\s+(handler|\w+)\s*\(`)
	case "python":
		re = regexp.MustCompile(`(?m)^(async\s+)?def\s+(handler|\w+)\s*\([^)]*\)`)
	default:
		re = regexp.MustCompile(`(?m)^(async\s+)?def\s+(\w+)\s*\([^)]*\)`)
	}
	if m := re.FindString(src); m != "" { return strings.TrimSpace(m) }
	return "handler(input: dict) -> dict"
}

func extractModuleDocstring(src string, lang string) string {
	var re *regexp.Regexp
	switch lang {
	case "go":
		// Go: package comment ("// PackageName") or leading block comments
		re = regexp.MustCompile(`//\s*(.+)`)
	case "typescript", "javascript":
		// JSDoc: /** ... */
		re = regexp.MustCompile(`/\*\*([\s\S]*?)\*/`)
	case "python":
		re = regexp.MustCompile(`"""([\\s\\S]*?)"""`)
	default:
		re = regexp.MustCompile(`"""([\\s\\S]*?)"""`)
	}
	if m := re.FindStringSubmatch(src); len(m) > 1 { return strings.TrimSpace(m[1]) }
	return ""
}

// extractEntryDocstring extracts the comment/docstring immediately before a function definition.
func extractEntryDocstring(src, entryName, lang string) string {
	var commentRe *regexp.Regexp

	switch lang {
	case "go":
		// Go: single-line // Comment immediately before func
		commentRe = regexp.MustCompile(`//\s*(.+)\nfunc\s+` + regexp.QuoteMeta(entryName) + `\s*\(`)
		if m := commentRe.FindStringSubmatch(src); len(m) > 1 {
			return strings.TrimSpace(m[1])
		}
	case "python":
		// Python: """docstring""" above def entry_name
		commentRe = regexp.MustCompile(`(?s)"""([^"]*)"""\s*\n(?:async\s+)?def\s+` + regexp.QuoteMeta(entryName) + `\s*\(`)
		if m := commentRe.FindStringSubmatch(src); len(m) > 1 {
			return strings.TrimSpace(m[1])
		}
		// Also try # comments
		commentRe = regexp.MustCompile(`(?m)#\s*(.+)\n(?:async\s+)?def\s+` + regexp.QuoteMeta(entryName) + `\s*\(`)
		if m := commentRe.FindStringSubmatch(src); len(m) > 1 {
			return strings.TrimSpace(m[1])
		}
	case "typescript", "javascript":
		// TS/JS: /** JSDoc */ above function entryName
		commentRe = regexp.MustCompile(`(?s)/\*\*([^*]*)\*/\s*\n(?:export\s+)?(?:async\s+)?function\s+` + regexp.QuoteMeta(entryName))
		if m := commentRe.FindStringSubmatch(src); len(m) > 1 {
			return strings.TrimSpace(m[1])
		}
	}
	return ""
}

func extToLang(ext string) string {
	switch ext {
	case ".py":
		return "python"
	case ".ts":
		return "typescript"
	case ".js":
		return "javascript"
	case ".go":
		return "go"
	default:
		return "python"
	}
}

func isDirExist(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
