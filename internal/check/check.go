package check

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ── ImportResult (existing) ──

// ImportResult holds the comparison between declared and actual imports.
type ImportResult struct {
	Module     string   `json:"module"`
	Declared   []string `json:"declared"`
	Imported   []string `json:"imported"`
	Undeclared []string `json:"undeclared"`
	Unused     []string `json:"unused"`
	Ok         bool     `json:"ok"`
	Error      string   `json:"error,omitempty"`
}

// Imports checks a module's declared dependencies against actual source imports.
func Imports(root, modName string) ImportResult {
	r := ImportResult{Module: modName, Ok: true}

	// 1. Load module.json to get declared dependencies
	modJSONPath := filepath.Join(root, "source", "modules", modName, "module.json")
	data, err := os.ReadFile(modJSONPath)
	if err != nil {
		r.Ok = false
		r.Error = "module.json not found: " + modJSONPath
		return r
	}
	var contract map[string]interface{}
	if err := json.Unmarshal(data, &contract); err != nil {
		r.Ok = false
		r.Error = "invalid module.json"
		return r
	}

	depsRaw, _ := contract["dependencies"].([]interface{})
	declaredSet := map[string]bool{}
	for _, d := range depsRaw {
		if s, ok := d.(string); ok {
			r.Declared = append(r.Declared, s)
			declaredSet[s] = true
		}
	}

	// 2. Find implementation file
	lang, _ := contract["language"].(string)
	if lang == "" {
		lang = "python"
	}
	extMap := map[string]string{"python": "py", "typescript": "ts", "javascript": "js", "go": "go"}
	ext := extMap[lang]
	if ext == "" {
		ext = "py"
	}
	implPath := filepath.Join(root, "source", "modules", modName, modName+"."+ext)
	src, err := os.ReadFile(implPath)
	if err != nil {
		r.Ok = false
		r.Error = "implementation file not found: " + implPath
		return r
	}

	// 3. Extract imports — language-specific patterns
	var importRe *regexp.Regexp
	switch lang {
	case "go":
		importRe = regexp.MustCompile(`"(?:\w+/)?source/modules/(\w+)(?:/\w+)?[""]`)
	case "typescript", "javascript":
		importRe = regexp.MustCompile(`(?:from|require)\s*\(\s*['"].*source/modules/(\w+)/\w+['"]\s*\)`)
	default:
		importRe = regexp.MustCompile(`from\s+source\.modules\.(\w+)\.\w+\s+import\s+\w+`)
	}
	matches := importRe.FindAllStringSubmatch(string(src), -1)

	importedSet := map[string]bool{}
	for _, m := range matches {
		if len(m) > 1 {
			mod := m[1]
			if mod != modName && !importedSet[mod] {
				r.Imported = append(r.Imported, mod)
				importedSet[mod] = true
			}
		}
	}

	// 4. Compare
	for _, imp := range r.Imported {
		if !declaredSet[imp] {
			r.Undeclared = append(r.Undeclared, imp)
			r.Ok = false
		}
	}
	for _, dec := range r.Declared {
		if !importedSet[dec] {
			r.Unused = append(r.Unused, dec)
		}
	}

	if len(r.Undeclared) > 0 {
		r.Error = fmt.Sprintf("Undeclared imports: %v must be added to module.json dependencies", r.Undeclared)
	}
	if len(r.Unused) > 0 {
		if r.Error != "" {
			r.Error += "; "
		}
		r.Error += fmt.Sprintf("Unused declarations: %v declared in module.json but not imported", r.Unused)
	}

	return r
}

// ── Comprehensive Import Scan (v5.4.0) ──

// ImportCategory categorises an import target.
type ImportCategory string

const (
	ImportKnown     ImportCategory = "known"       // source/modules/<name> — registered yanxi module
	ImportLocal     ImportCategory = "local"       // internal/ pkg/ util/ — project-local non-module package
	ImportThirdParty ImportCategory = "third_party" // external dependency (go.mod, pip, npm)
	ImportStdlib    ImportCategory = "stdlib"      // language standard library
	ImportUnknown   ImportCategory = "unknown"     // can't determine
)

// ImportItem describes one discovered import line.
type ImportItem struct {
	Raw      string         `json:"raw"`      // the raw import string (path expression)
	Category ImportCategory `json:"category"` // classification
	Package  string         `json:"package"`  // last segment for display
	Suggestion string       `json:"suggestion,omitempty"` // what to do
}

// ImportScanResult is the comprehensive output of scanning a module's source.
type ImportScanResult struct {
	Module     string       `json:"module"`
	AllImports []ImportItem `json:"all_imports"`
	Known      []ImportItem `json:"known"`
	Local      []ImportItem `json:"local"`
	ThirdParty []ImportItem `json:"third_party"`
	Stdlib     []ImportItem `json:"stdlib"`
	Unknown    []ImportItem `json:"unknown,omitempty"`
}

// ScanImports scans ALL imports in a module's source file and classifies them.
// It returns every known, local, third-party, and stdlib import.
func ScanImports(root, modName, lang string, src []byte) *ImportScanResult {
	r := &ImportScanResult{Module: modName}
	text := string(src)

	// Step 1: Extract all import strings (language-agnostic form)
	var rawImports []string
	switch lang {
	case "go":
		// Single: import "path"
		// Group: import ( "path1"\n"path2" )
		singleRe := regexp.MustCompile(`(?m)^\s*import\s+"([^"]+)"`)
		groupRe := regexp.MustCompile(`(?m)^\s*import\s+\(([^)]+)\)`)
		for _, m := range singleRe.FindAllStringSubmatch(text, -1) {
			if len(m) > 1 {
				rawImports = append(rawImports, m[1])
			}
		}
		for _, m := range groupRe.FindAllStringSubmatch(text, -1) {
			if len(m) > 1 {
				inner := m[1]
				lineRe := regexp.MustCompile(`"([^"]+)"`)
				for _, lm := range lineRe.FindAllStringSubmatch(inner, -1) {
					if len(lm) > 1 {
						rawImports = append(rawImports, lm[1])
					}
				}
			}
		}
	case "typescript", "javascript":
		// import ... from '...'
		// require('...')
		impRe := regexp.MustCompile(`(?:from|require)\s*\(?\s*['"]([^'"]+)['"]\s*\)?`)
		for _, m := range impRe.FindAllStringSubmatch(text, -1) {
			if len(m) > 1 {
				rawImports = append(rawImports, m[1])
			}
		}
	default: // python
		// import X; from X import Y
		impRe := regexp.MustCompile(`(?m)^\s*(?:import|from)\s+(\S+)`)
		for _, m := range impRe.FindAllStringSubmatch(text, -1) {
			if len(m) > 1 {
				rawImports = append(rawImports, strings.SplitN(m[1], " ", 2)[0])
			}
		}
	}

	// Step 2: Load known yanxi modules
	knownSet := knownModuleSet(root)

	// Step 3: Classify each import
	seen := map[string]bool{}
	for _, imp := range rawImports {
		if seen[imp] {
			continue
		}
		seen[imp] = true
		item := ImportItem{Raw: imp, Package: lastSegment(imp)}
		item.Category = classifyImport(root, imp, knownSet, lang)
		item.Suggestion = importSuggestion(item.Category, imp)
		r.AllImports = append(r.AllImports, item)
		switch item.Category {
		case ImportKnown:
			r.Known = append(r.Known, item)
		case ImportLocal:
			r.Local = append(r.Local, item)
		case ImportThirdParty:
			r.ThirdParty = append(r.ThirdParty, item)
		case ImportStdlib:
			r.Stdlib = append(r.Stdlib, item)
		default:
			r.Unknown = append(r.Unknown, item)
		}
	}
	return r
}

// knownModuleSet returns the set of registered yanxi module names.
func knownModuleSet(root string) map[string]bool {
	set := map[string]bool{}
	modDir := filepath.Join(root, "source", "modules")
	entries, err := os.ReadDir(modDir)
	if err != nil {
		return set
	}
	for _, e := range entries {
		if e.IsDir() {
			set[e.Name()] = true
		}
	}
	return set
}

// classifyImport determines the category of an import path.
func classifyImport(root, imp string, known map[string]bool, lang string) ImportCategory {
	// 1. Check known yanxi modules
	// Go: "yanxipro/source/modules/auth" or "source/modules/auth/auth"
	// Python: "source.modules.auth"
	// TS: "./modules/auth/auth"
	if strings.Contains(imp, "source/modules/") || strings.Contains(imp, "source.modules.") || strings.Contains(imp, "./modules/") {
		return ImportKnown
	}
	// Check if the import path or its last segment matches a known module name
	pkg := lastSegment(imp)
	if known[pkg] {
		return ImportKnown
	}

	// 2. Check project-local (non-module) directories
	localPrefixes := []string{"internal/", "pkg/", "util/", "common/", "shared/"}
	for _, p := range localPrefixes {
		if strings.HasPrefix(imp, p) || strings.Contains(imp, "/"+p) {
			return ImportLocal
		}
		// Also check on-disk existence
		candidate := filepath.Join(root, imp)
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return ImportLocal
		}
	}

	// 3. Language standard library reference
	if isStdlib(imp, lang) {
		return ImportStdlib
	}

	// 4. Anything else is third-party or unknown
	if strings.Contains(imp, ".") || strings.Contains(imp, "/") {
		return ImportThirdParty
	}
	return ImportUnknown
}

// isStdlib returns true if the import is a known standard library path.
func isStdlib(imp string, lang string) bool {
	switch lang {
	case "go":
		goStdlib := map[string]bool{
			"fmt": true, "os": true, "io": true, "strings": true, "strconv": true,
			"encoding/json": true, "encoding/xml": true, "regexp": true,
			"net": true, "net/http": true, "net/url": true,
			"sync": true, "time": true, "log": true, "math": true, "sort": true,
			"path": true, "path/filepath": true, "flag": true,
			"bytes": true, "bufio": true, "context": true, "crypto": true,
			"errors": true, "reflect": true, "testing": true,
		}
		if goStdlib[imp] {
			return true
		}
		// Standard library packages with subpackages
		stdPrefixes := []string{"net/", "os/", "crypto/", "encoding/", "io/", "math/", "sync/", "time/", "go/"}
		for _, p := range stdPrefixes {
			if strings.HasPrefix(imp, p) {
				return true
			}
		}
	case "python":
		pyStdlib := []string{"os", "sys", "json", "re", "math", "time", "datetime",
			"collections", "functools", "itertools", "pathlib", "typing",
			"io", "logging", "abc", "dataclasses", "enum", "hashlib",
			"copy", "random", "statistics", "string", "textwrap", "types",
			"uuid", "warnings", "weakref", "inspect", "tempfile", "shutil",
			"subprocess", "threading", "multiprocessing", "concurrent",
			"http", "urllib", "email", "base64", "binascii", "zlib",
			"argparse", "configparser", "csv", "glob", "json", "pickle",
			"xml", "html", "unittest", "doctest", "pdb", "traceback",
			"dataclasses", "decimal", "fractions", "numbers", "calendar",
		}
		for _, s := range pyStdlib {
			if imp == s || strings.HasPrefix(imp, s+".") {
				return true
			}
		}
	case "typescript", "javascript":
		tsStdlib := []string{"fs", "path", "os", "http", "https", "url",
			"util", "stream", "crypto", "events", "buffer", "child_process",
			"assert", "querystring", "net", "dns", "dgram", "cluster",
			"readline", "repl", "tls", "zlib", "string_decoder", "timers",
			"console", "process", "buffer", "module", "worker_threads",
		}
		for _, s := range tsStdlib {
			if imp == s {
				return true
			}
		}
	}
	return false
}

// lastSegment returns the last path segment of an import path.
func lastSegment(imp string) string {
	if idx := strings.LastIndex(imp, "/"); idx >= 0 {
		return imp[idx+1:]
	}
	if idx := strings.LastIndex(imp, "."); idx >= 0 {
		return imp[idx+1:]
	}
	return imp
}

// importSuggestion returns a human-readable suggestion for an import category.
func importSuggestion(cat ImportCategory, imp string) string {
	switch cat {
	case ImportKnown:
		return "registered yanxi module — normal check applies"
	case ImportLocal:
		return fmt.Sprintf("project-local package not under source/modules/ — consider module_adopt(%q)", lastSegment(imp))
	case ImportThirdParty:
		return fmt.Sprintf("third-party dependency — ensure version is declared in go.mod/package.json/requirements.txt")
	case ImportStdlib:
		return "language standard library — no action needed"
	default:
		return "unrecognized import — may need manual review"
	}
}
