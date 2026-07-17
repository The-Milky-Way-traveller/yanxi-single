package orchestrator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"yanxi-single/internal/orchestrator/langtmpl"
	"yanxi-single/internal/validate"
)

type ModuleContract struct {
	Name         string                 `json:"name"`
	Version      string                 `json:"version"`
	Status       string                 `json:"status"`
	OwnerAgent   string                 `json:"owner_agent"`
	Dependencies []string               `json:"dependencies"`
	Interface    map[string]interface{} `json:"interface"`
	Language     string                 `json:"language"`
	Layer        string                 `json:"layer,omitempty"`   // "leaf" or "group"
	Children     []string               `json:"children,omitempty"` // child module names (groups only)
}

func (m *ModuleContract) Description() string {
	if m.Interface != nil {
		if d, ok := m.Interface["description"].(string); ok { return d }
	}
	return ""
}
func (m *ModuleContract) ToDict() map[string]interface{} {
	return map[string]interface{}{
		"name": m.Name, "version": m.Version, "status": m.Status,
		"owner_agent": m.OwnerAgent, "dependencies": m.Dependencies,
		"interface": m.Interface, "language": m.Language,
	}
}

// handlerName returns the entry point name for a module's interface.
// If a language template exists, the name is capitalized as needed.
func handlerName(iface map[string]interface{}) string {
	if iface != nil {
		if e, ok := iface["entry"].(string); ok && e != "" { return e }
	}
	return "handler"
}

// entryNames returns all entry point names. For new-style "entries" format,
// returns the keys; for old-style "entry" format, returns a single-element slice.
func entryNames(iface map[string]interface{}) []string {
	if iface == nil {
		return []string{"handler"}
	}
	if entries, ok := iface["entries"].(map[string]interface{}); ok && len(entries) > 0 {
		names := make([]string, 0, len(entries))
		for k := range entries {
			names = append(names, k)
		}
		sort.Strings(names)
		return names
	}
	return []string{handlerName(iface)}
}

// hasEntries checks if the module uses the new multi-entry format.
func hasEntries(iface map[string]interface{}) bool {
	if iface == nil { return false }
	entries, ok := iface["entries"].(map[string]interface{})
	return ok && len(entries) > 0
}

// entrySchema returns input_schema and output_schema for a specific entry.
// For old-style, returns the top-level schemas regardless of entry name.
func entrySchema(iface map[string]interface{}, name string) (input, output map[string]interface{}) {
	if !hasEntries(iface) {
		input, _ = iface["input_schema"].(map[string]interface{})
		output, _ = iface["output_schema"].(map[string]interface{})
		return
	}
	entries := iface["entries"].(map[string]interface{})
	if entry, ok := entries[name].(map[string]interface{}); ok {
		input, _ = entry["input_schema"].(map[string]interface{})
		output, _ = entry["output_schema"].(map[string]interface{})
	}
	return
}

// entryDescription returns the description for an entry.
func entryDescription(iface map[string]interface{}, name string) string {
	if !hasEntries(iface) {
		if d, ok := iface["description"].(string); ok { return d }
		return ""
	}
	entries := iface["entries"].(map[string]interface{})
	if entry, ok := entries[name].(map[string]interface{}); ok {
		if d, ok := entry["description"].(string); ok { return d }
	}
	return ""
}

// ── Language helpers ──

var extMap = map[string]string{"python": "py", "typescript": "ts", "javascript": "js", "go": "go"}

func langExt(lang string) string {
	if e, ok := extMap[lang]; ok { return e }
	return "py"
}

func langStub(root, lang, name string) string {
	t, err := langtmpl.Resolve(root, lang)
	if err != nil {
		// Fallback: Python-like stub
		return fmt.Sprintf(`"""%s module"""
def handler(d):
    return {"result": f"{d.get('action','')} not implemented"}
`, name)
	}
	return t.RenderStub(name, t.DefaultEntryName())
}

// langStubEntries generates multi-entry stubs (one function per entry name).
func langStubEntries(root, lang, name string, entries []string) string {
	t, err := langtmpl.Resolve(root, lang)
	if err != nil {
		var b strings.Builder
		b.WriteString(fmt.Sprintf(`"""%s module"""
`, name))
		for _, e := range entries {
			b.WriteString(fmt.Sprintf("def %s(d):\n    return {\"result\": \"%s not implemented\"}\n\n", e, e))
		}
		return b.String()
	}
	return t.RenderStubEntries(name, entries)
}

func detectPrimary(root string) string {
	modulesDir := filepath.Join(root, "source", "modules")
	entries, _ := os.ReadDir(modulesDir)
	counts := map[string]int{}
	for _, e := range entries {
		if !e.IsDir() { continue }
		modJSON := filepath.Join(modulesDir, e.Name(), "module.json")
		if data, err := os.ReadFile(modJSON); err == nil {
			var m ModuleContract
			if json.Unmarshal(data, &m) == nil && m.Language != "" && m.Layer != "group" {
				counts[m.Language]++
			}
		}
	}
	best, bestN := "python", 0
	for lang, n := range counts {
		if n > bestN { best, bestN = lang, n }
	}
	return best
}

// ── Types ──

type ModuleDiscoverResult struct {
	Name, Version, Status, OwnerAgent, Language string
	Dependencies []string
	Interface    map[string]interface{}
	ImplPath, ContractPath string
	AiCard, AiInterface     string
	DependedBy              []string
	Layer string   // "leaf" (default) or "group"
	Children                []string // child module names (groups only)
	ChildCount              int      // number of leaf children (for groups)
}

type DepNode struct {
	Name    string   `json:"name"`
	Depends []string `json:"depends"`
	DepBy   []string `json:"dep_by"`
}

type ProjectOverview struct {
	ProjectDir, IndexContent, ArchitectureMD, ContractsJSON string
	ModuleCount                                             int
	HasAIExplain, HasINDEX, HasProjectMemory                bool
	ProjectMemory    map[string]string
	Config          *ProjectConfig    `json:"config,omitempty"`
	ProjectSummary  string            `json:"project_summary,omitempty"`
	PrimaryLanguage  string
	Warnings         []string
	DependencyGraph  []DepNode
	CallGraph        CallGraph         `json:"call_graph,omitempty"`
	CircularDeps     [][]string
	BuildOrder       []string
	Modules          []ModuleDiscoverResult
	LazyModules      []LazyModuleSummary   `json:"lazy_modules,omitempty"`
}

type LazyModuleSummary struct {
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	Status       string   `json:"status"`
	Language     string   `json:"language"`
	Description  string   `json:"description"`
	Dependencies []string `json:"dependencies"`
	Dependents   []string `json:"dependents"`
	EntryCount   int      `json:"entry_count"`
}

func ModuleDiscover(root string) ProjectOverview {
	// Check cache
	if cached := loadDiscoverCache(root); cached != nil && discoverCacheValid(root, cached) {
		// Return a copy so callers can't mutate the cache
		ov := *cached.Data
		// Refresh dynamic fields (project memory content may have changed independently)
		pm := LoadProjectMemory(root)
		if pm != nil {
			ov.HasProjectMemory = true
			ov.ProjectMemory = make(map[string]string)
			if pm.ADRs != "" { ov.ProjectMemory["architecture-decisions.md"] = pm.ADRs }
			if pm.Lessons != "" { ov.ProjectMemory["lessons-learned.md"] = pm.Lessons }
			if pm.Conventions != "" { ov.ProjectMemory["conventions.md"] = pm.Conventions }
		}
		// Refresh AIexplain check
		if info, err := os.Stat(filepath.Join(root, "AIexplain")); err == nil && info.IsDir() {
			ov.HasAIExplain = true
		}
		return ov
	}

	ov := ProjectOverview{ProjectDir: root, Modules: []ModuleDiscoverResult{}}

	// Level 1: project config + memory
	ov.Config = LoadProjectConfig(root)
	pm := LoadProjectMemory(root)
	if pm != nil {
		ov.HasProjectMemory = true
		ov.ProjectMemory = make(map[string]string)
		if pm.ADRs != "" { ov.ProjectMemory["architecture-decisions.md"] = pm.ADRs }
		if pm.Lessons != "" { ov.ProjectMemory["lessons-learned.md"] = pm.Lessons }
		if pm.Conventions != "" { ov.ProjectMemory["conventions.md"] = pm.Conventions }
	}
	// Structured memory: parse ADRs and filter by status
	sm := LoadStructuredMemory(root)
	if sm != nil && len(sm.ADRs) > 0 {
		var activeADRs []ADR
		var expiredOrSuperseded int
		for _, a := range sm.ADRs {
			if a.Status == "accepted" {
				activeADRs = append(activeADRs, a)
			} else if a.Status == "expired" || a.Status == "superseded" {
				expiredOrSuperseded++
			}
		}
		if len(activeADRs) > 0 {
			ov.ProjectMemory["adr_active"] = fmt.Sprintf("%d active ADRs", len(activeADRs))
		}
		if expiredOrSuperseded > 0 {
			ov.ProjectMemory["adr_expired"] = fmt.Sprintf("%d expired/superseded ADRs (use module_discover(include_expired=true) to view)", expiredOrSuperseded)
		}
	}

	if data, err := os.ReadFile(filepath.Join(root, "INDEX.md")); err == nil {
		ov.HasINDEX = true; ov.IndexContent = string(data)
	}
	aiexp := filepath.Join(root, "AIexplain")
	if info, err := os.Stat(aiexp); err == nil && info.IsDir() { ov.HasAIExplain = true }
	if data, err := os.ReadFile(filepath.Join(aiexp, "project-architecture.md")); err == nil { ov.ArchitectureMD = string(data) }
	if data, err := os.ReadFile(filepath.Join(aiexp, "module-contracts.json")); err == nil { ov.ContractsJSON = string(data) }

	ov.ProjectSummary = ConfigSummary(ov.Config, 0, "", pm)

	modulesDir := filepath.Join(root, "source", "modules")
	entries, err := os.ReadDir(modulesDir)
	if err != nil { return ov }
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	modNameSet := map[string]bool{}
	for _, entry := range entries {
		if !entry.IsDir() { continue }
		modName := entry.Name()
		modNameSet[modName] = true
		modJSONPath := filepath.Join(modulesDir, modName, "module.json")
		data, err := os.ReadFile(modJSONPath)
		if err != nil { continue }
		var m ModuleContract
		if json.Unmarshal(data, &m) != nil { continue }
		if m.Name == "" { m.Name = modName }

		var implPath string
		for _, ext := range []string{"py", "ts", "js", "go"} {
			p := filepath.Join(modulesDir, modName, modName+"."+ext)
			if _, err := os.Stat(p); err == nil { implPath = p; break }
		}
		var aiCard, aiIface string
		if data, err := os.ReadFile(filepath.Join(aiexp, "modules", modName, modName+".md")); err == nil { aiCard = string(data) }
		if data, err := os.ReadFile(filepath.Join(aiexp, "modules", modName, "interface.md")); err == nil { aiIface = string(data) }

		ov.Modules = append(ov.Modules, ModuleDiscoverResult{
			Name: m.Name, Version: m.Version, Status: m.Status,
			OwnerAgent: m.OwnerAgent, Dependencies: m.Dependencies,
			Interface: m.Interface, Language: m.Language,
			ImplPath: implPath, ContractPath: modJSONPath,
			AiCard: aiCard, AiInterface: aiIface,
			Layer: m.Layer, Children: m.Children,
		})
	}
	ov.ModuleCount = len(ov.Modules)
	ov.PrimaryLanguage = detectPrimary(root)

	depGraph, cycles, order, applyDepBy := buildDepGraph(ov.Modules)
	ov.DependencyGraph = depGraph; ov.CircularDeps = cycles; ov.BuildOrder = order
	applyDepBy(ov.Modules)

	// Build call graph (v5.3.0)
	ov.CallGraph = BuildCallGraph(ov.Modules)

	for _, m := range ov.Modules {
		if m.Status == "wip" && m.AiCard == "" { ov.Warnings = append(ov.Warnings, "Module '"+m.Name+"' is wip, no AIexplain card") }
		if m.AiCard == "" && m.AiInterface == "" { ov.Warnings = append(ov.Warnings, "Module '"+m.Name+"' has no AIexplain — run aiexplain_generate()") }
		for _, dep := range m.Dependencies {
			if !modNameSet[dep] { ov.Warnings = append(ov.Warnings, "Module '"+m.Name+"' depends on '"+dep+"' which does not exist") }
		}
	}
	if !ov.HasProjectMemory { ov.Warnings = append(ov.Warnings, "No project-memory/ directory found. Run memory_init() to create one.") }
	for _, m := range ov.Modules {
		if m.AiCard != "" && m.ImplPath != "" {
			cardPath := filepath.Join(root, "AIexplain", "modules", m.Name, m.Name+".md")
			ci, _ := os.Stat(cardPath); si, _ := os.Stat(m.ImplPath)
			if ci != nil && si != nil && si.ModTime().After(ci.ModTime()) {
				ov.Warnings = append(ov.Warnings, fmt.Sprintf("Module '%s': AIexplain card older than source — run aiexplain_generate()", m.Name))
			}
		}
	}
	for _, cycle := range cycles { ov.Warnings = append(ov.Warnings, "Circular dependency: "+strings.Join(cycle, " → ")) }
	return ov
}

// ModuleDiscoverLazy returns a lightweight ProjectOverview with module summaries only.
// No AIexplain card content or skill.md content — just names, versions, statuses, descriptions.
// Agent then calls module_skill_read(module) for detailed instructions on specific modules.
func ModuleDiscoverLazy(root string) ProjectOverview {
	ov := ProjectOverview{ProjectDir: root, Modules: []ModuleDiscoverResult{}}

	if data, err := os.ReadFile(filepath.Join(root, "INDEX.md")); err == nil {
		ov.HasINDEX = true; ov.IndexContent = string(data)
	}
	ov.ProjectMemory = make(map[string]string)
	if info, err := os.Stat(filepath.Join(root, "project-memory")); err == nil && info.IsDir() {
		ov.HasProjectMemory = true
		for _, fn := range []string{"architecture-decisions.md", "lessons-learned.md", "conventions.md"} {
			if data, err := os.ReadFile(filepath.Join(root, "project-memory", fn)); err == nil {
				ov.ProjectMemory[fn] = string(data)
			}
		}
	}
	aiexp := filepath.Join(root, "AIexplain")
	if info, err := os.Stat(aiexp); err == nil && info.IsDir() { ov.HasAIExplain = true }

	modulesDir := filepath.Join(root, "source", "modules")
	entries, err := os.ReadDir(modulesDir)
	if err != nil { return ov }
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	modNameSet := map[string]bool{}
	for _, entry := range entries {
		if !entry.IsDir() { continue }
		modName := entry.Name()
		modNameSet[modName] = true
		modJSONPath := filepath.Join(modulesDir, modName, "module.json")
		data, err := os.ReadFile(modJSONPath)
		if err != nil { continue }
		var m ModuleContract
		if json.Unmarshal(data, &m) != nil { continue }
		if m.Name == "" { m.Name = modName }

		var implPath string
		for _, ext := range []string{"py", "ts", "js", "go"} {
			p := filepath.Join(modulesDir, modName, modName+"."+ext)
			if _, err := os.Stat(p); err == nil { implPath = p; break }
		}

		entryCount := 1
		if m.Interface != nil {
			if ents, ok := m.Interface["entries"].(map[string]interface{}); ok {
				entryCount = len(ents)
			}
		}

		ov.Modules = append(ov.Modules, ModuleDiscoverResult{
			Name: m.Name, Version: m.Version, Status: m.Status,
			OwnerAgent: m.OwnerAgent, Dependencies: m.Dependencies,
			Interface: m.Interface, Language: m.Language,
			ImplPath: implPath, ContractPath: modJSONPath,
		})
		ov.LazyModules = append(ov.LazyModules, LazyModuleSummary{
			Name: m.Name, Version: m.Version, Status: m.Status,
			Language: m.Language, Description: m.Description(),
			Dependencies: m.Dependencies,
			EntryCount: entryCount,
		})
	}
	ov.ModuleCount = len(ov.Modules)
	ov.PrimaryLanguage = detectPrimary(root)

	depGraph, cycles, order, applyDepBy := buildDepGraph(ov.Modules)
	ov.DependencyGraph = depGraph; ov.CircularDeps = cycles; ov.BuildOrder = order
	applyDepBy(ov.Modules)

	// Fill dependents into lazy summaries
	for i := range ov.Modules {
		if i < len(ov.LazyModules) {
			ov.LazyModules[i].Dependents = ov.Modules[i].DependedBy
		}
	}

	for _, m := range ov.Modules {
		for _, dep := range m.Dependencies {
			if !modNameSet[dep] { ov.Warnings = append(ov.Warnings, "Module '"+m.Name+"' depends on '"+dep+"' which does not exist") }
		}
	}
	if !ov.HasProjectMemory { ov.Warnings = append(ov.Warnings, "No project-memory/ directory found. Run memory_init() to create one.") }
	for _, cycle := range cycles { ov.Warnings = append(ov.Warnings, "Circular dependency: "+strings.Join(cycle, " → ")) }
	saveDiscoverCache(root, &ov)
	return ov
}

func buildDepGraph(mods []ModuleDiscoverResult) ([]DepNode, [][]string, []string, func([]ModuleDiscoverResult) []ModuleDiscoverResult) {
	depMap, revMap := map[string][]string{}, map[string][]string{}
	for _, m := range mods { depMap[m.Name] = m.Dependencies }
	for _, m := range mods { for _, d := range m.Dependencies { revMap[d] = append(revMap[d], m.Name) } }
	var nodes []DepNode
	for _, m := range mods { nodes = append(nodes, DepNode{m.Name, depMap[m.Name], revMap[m.Name]}) }
	visited, inStack := map[string]bool{}, map[string]bool{}
	var cycles [][]string
	var order []string
	var dfs func(string, []string) bool
	dfs = func(name string, path []string) bool {
		visited[name] = true; inStack[name] = true
		for _, d := range depMap[name] {
			// Skip dependencies that don't exist (e.g. deleted modules)
			if _, exists := depMap[d]; !exists { continue }
			if !visited[d] { if dfs(d, append(path, d)) { return true } } else if inStack[d] {
				si := -1
				for i, p := range path { if p == d { si = i; break } }
				if si >= 0 { cycles = append(cycles, append(path[si:], d)) }
				return true
			}
		}
		inStack[name] = false; order = append(order, name); return false
	}
	for _, m := range mods { if !visited[m.Name] { dfs(m.Name, []string{m.Name}) } }
	for i, j := 0, len(order)-1; i < j; i, j = i+1, j-1 { order[i], order[j] = order[j], order[i] }
	apply := func(mods []ModuleDiscoverResult) []ModuleDiscoverResult {
		for i := range mods { mods[i].DependedBy = revMap[mods[i].Name] }
		return mods
	}
	return nodes, cycles, order, apply
}

// ValidationState tracks per-module validation results.
type ValidationState struct {
	Modules map[string]ModuleValidationState `json:"modules"`
}

type ModuleValidationState struct {
	Valid       bool     `json:"valid"`
	ValidatedAt string   `json:"validated_at"`
	Errors      []string `json:"errors"`
	Warnings    []string `json:"warnings"`
}

// ── Discover Cache ──

type discoverCache struct {
	Mtimes map[string]string `json:"mtimes"` // module → mtime of module.json
	Data   *ProjectOverview  `json:"data"`
}

func discoverCachePath(root string) string {
	return filepath.Join(root, ".yanxi", "discover_cache.json")
}

func loadDiscoverCache(root string) *discoverCache {
	data, err := os.ReadFile(discoverCachePath(root))
	if err != nil {
		return nil
	}
	var c discoverCache
	if json.Unmarshal(data, &c) != nil || c.Data == nil {
		return nil
	}
	// Check the serialized data was for the same project dir
	if c.Data.ProjectDir != root {
		return nil
	}
	return &c
}

func saveDiscoverCache(root string, ov *ProjectOverview) {
	c := discoverCache{
		Mtimes: collectModuleMtimes(root),
		Data:   ov,
	}
	data, _ := json.MarshalIndent(c, "", "  ")
	os.WriteFile(discoverCachePath(root), data, 0644)
}

func discoverCacheValid(root string, c *discoverCache) bool {
	if c == nil || c.Data == nil {
		return false
	}
	current := collectModuleMtimes(root)
	if len(current) != len(c.Mtimes) {
		return false // module added or removed
	}
	for mod, mtime := range current {
		prev, ok := c.Mtimes[mod]
		if !ok || prev != mtime {
			return false // new or changed
		}
	}
	return true
}

func collectModuleMtimes(root string) map[string]string {
	mtimes := map[string]string{}
	modDir := filepath.Join(root, "source", "modules")
	entries, err := os.ReadDir(modDir)
	if err != nil {
		return mtimes
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		modJSON := filepath.Join(modDir, e.Name(), "module.json")
		if info, err := os.Stat(modJSON); err == nil {
			mtimes[e.Name()] = info.ModTime().UTC().Format(time.RFC3339Nano)
		}
	}
	return mtimes
}

// InvalidateDiscoverCache removes the discover cache so the next call rebuilds.
func InvalidateDiscoverCache(root string) {
	p := discoverCachePath(root)
	if _, err := os.Stat(p); err == nil {
		os.Remove(p)
	}
}

func validationStatePath(root string) string {
	return filepath.Join(root, ".yanxi", "validation_state.json")
}

func loadValidationState(root string) *ValidationState {
	data, err := os.ReadFile(validationStatePath(root))
	if err != nil {
		return &ValidationState{Modules: make(map[string]ModuleValidationState)}
	}
	var vs ValidationState
	if json.Unmarshal(data, &vs) != nil {
		return &ValidationState{Modules: make(map[string]ModuleValidationState)}
	}
	if vs.Modules == nil {
		vs.Modules = make(map[string]ModuleValidationState)
	}
	return &vs
}

func saveValidationState(root string, vs *ValidationState) error {
	data, _ := json.MarshalIndent(vs, "", "  ")
	return os.WriteFile(validationStatePath(root), data, 0644)
}

func MarkValidated(root string, result validate.Result) {
	vs := loadValidationState(root)
	vs.Modules[result.Module] = ModuleValidationState{
		Valid:       result.Valid,
		ValidatedAt: time.Now().UTC().Format(time.RFC3339),
		Errors:      result.Errors,
		Warnings:    result.Warnings,
	}
	saveValidationState(root, vs)

	// Propagate breaking changes to dependent modules' skill.md
	if len(result.BreakingChanges) > 0 {
		HandleBreakingChanges(root, result.Module, result.BreakingChanges)
	}
}

// HandleBreakingChanges propagates breaking schema changes to dependent modules' skill.md
// so that Agents working on those modules know an upstream interface changed.
func HandleBreakingChanges(root, changedModule string, changes []validate.BreakingChange) {
	// Build a human-readable summary
	var summary strings.Builder
	summary.WriteString(fmt.Sprintf("⚠ 上游模块 %s 发生不兼容合约变更:\n", changedModule))
	for _, bc := range changes {
		summary.WriteString(fmt.Sprintf("  - 入口 %s (%s):\n", bc.Entry, bc.Side))
		for _, ch := range bc.Changes {
			if !ch.Compatible {
				summary.WriteString(fmt.Sprintf("    · %s\n", ch.Message))
			}
		}
	}

	// Find dependent modules via module_discover
	ov := ModuleDiscover(root)
	for _, m := range ov.Modules {
		for _, dep := range m.Dependencies {
			if dep == changedModule {
				note := fmt.Sprintf("上游模块 %s 变更: %s", changedModule, strings.TrimSpace(summary.String()))
				_ = MemoryAppendLesson(root, note)
			}
		}
	}
}

// ── Create / Wire / Bootstrap ──

func CreateModule(root, name, agent, language string) error {
	InvalidateDiscoverCache(root)
	modDir := filepath.Join(root, "source", "modules", name)
	if err := os.MkdirAll(modDir, 0755); err != nil { return err }
	ext := langExt(language)
	m := ModuleContract{
		Name: name, Version: "0.1.0", Status: "wip", OwnerAgent: agent, Language: language,
		Interface: map[string]interface{}{
			"entry": "handler", "description": name + " module",
			"input_schema":  map[string]interface{}{"type": "object", "properties": map[string]interface{}{"action": map[string]interface{}{"type": "string"}}},
			"output_schema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"result": map[string]interface{}{}, "error": map[string]interface{}{"type": "string"}}},
		},
	}
	data, _ := json.MarshalIndent(m.ToDict(), "", "  ")
	if err := os.WriteFile(filepath.Join(modDir, "module.json"), data, 0644); err != nil { return err }
	impl := langStub(root, language, name)
	if err := os.WriteFile(filepath.Join(modDir, name+"."+ext), []byte(impl), 0644); err != nil { return err }
	// Validate after creation
	result := validate.ValidateModule(root, name)
	MarkValidated(root, result)
	// Auto-record ADR for new module
	_ = WriteADR(root, ADR{
		Number:      "ADR-NEW", // placeholder; next ADR function would increment
		Title:       fmt.Sprintf("Create module %s", name),
		Status:      "accepted",
		Context:     fmt.Sprintf("New %s module for %s project", language, filepath.Base(root)),
		Decision:    fmt.Sprintf("Created module %s in %s. Interface: handler(input: dict) -> dict.", name, language),
		Consequences: "Module is in wip status. Run aiexplain_generate() to sync AIexplain.",
		Module:      name,
	})
	_ = result
	return nil
}

func WireMain(root string, mods []ModuleDiscoverResult) (string, error) {
	if len(mods) == 0 { return "", fmt.Errorf("no modules") }
	lang := detectPrimary(root)
	ext := langExt(lang)

	t, err := langtmpl.Resolve(root, lang)
	if err != nil {
		return "", fmt.Errorf("unsupported language %q: %w", lang, err)
	}

	// Check validation state: warn for unvalidated modules
	vs := loadValidationState(root)
	unvalidated := 0
	failed := 0
	for _, m := range mods {
		state, ok := vs.Modules[m.Name]
		if !ok {
			unvalidated++
		} else if !state.Valid {
			failed++
		}
	}
	if unvalidated > 0 || failed > 0 {
		var msg string
		if unvalidated > 0 { msg = fmt.Sprintf("%d unvalidated, ", unvalidated) }
		if failed > 0 { msg += fmt.Sprintf("%d failed validation", failed) }
		return "", fmt.Errorf("wire blocked: %s. Run module_validate() first", msg)
	}

	var imports, routes, middlewareLines, transportLines strings.Builder
	middlewareDeclared := false
	hasMultiEntry := false
	for _, m := range mods {
		// Skip group modules - they don't get wired
		isGroup := m.Layer == "group" || len(m.Children) > 0
		if isGroup { continue }
		hn := handlerName(m.Interface)
		if t.Handler.CapitalizeEntry && len(hn) > 0 {
			hn = strings.ToUpper(hn[:1]) + hn[1:]
		}
		alias := m.Name + "mod"
		imports.WriteString(t.RenderImport(m.Name, alias, hn) + "\n")

		// Collect transport entries for registry (v0.8)
		enList := entryNames(m.Interface)
		for _, en := range enList {
			entryHn := en
			if t.Handler.CapitalizeEntry && len(entryHn) > 0 {
				entryHn = strings.ToUpper(entryHn[:1]) + entryHn[1:]
			}
			if t.Wire.UseMapPattern {
				transportLines.WriteString(fmt.Sprintf("\t\"%s.%s\": nil, // %s\n", m.Name, en, entryHn))
			} else {
				transportLines.WriteString(fmt.Sprintf("    \"%s.%s\": %s,\n", m.Name, en, entryHn))
			}
		}
		if len(enList) > 1 { hasMultiEntry = true }

		// Parse middleware declarations (v5.3.0)
		if mwRaw, ok := m.Interface["middleware"].(map[string]interface{}); ok {
			if before, _ := mwRaw["before"].([]interface{}); len(before) > 0 {
				middlewareDeclared = true
				if !t.Wire.UseMapPattern {
					middlewareLines.WriteString(fmt.Sprintf("    # before middleware for %s:\n", m.Name))
					for _, mw := range before {
						mwStr, _ := mw.(string)
						parts := strings.SplitN(mwStr, ".", 2)
						if len(parts) == 2 {
							mwMod, mwEntry := parts[0], parts[1]
							// Auto-import the middleware module
							imports.WriteString(t.RenderImport(mwMod, mwMod+"mod", mwEntry) + "\n")
							// Generate real middleware call with short-circuit
							middlewareLines.WriteString(fmt.Sprintf("    _mw = %s(request)\n", mwEntry))
							middlewareLines.WriteString("    if _mw.get(\"error\") and _mw[\"error\"].get(\"severity\") == \"fatal\":\n")
							middlewareLines.WriteString("        return _mw\n")
						}
					}
				}
			}
			if after, _ := mwRaw["after"].([]interface{}); len(after) > 0 {
				middlewareDeclared = true
				if !t.Wire.UseMapPattern {
					middlewareLines.WriteString(fmt.Sprintf("    # after middleware for %s:\n", m.Name))
					for _, mw := range after {
						mwStr, _ := mw.(string)
						parts := strings.SplitN(mwStr, ".", 2)
						if len(parts) == 2 {
							mwMod, mwEntry := parts[0], parts[1]
							imports.WriteString(t.RenderImport(mwMod, mwMod+"mod", mwEntry) + "\n")
							middlewareLines.WriteString(fmt.Sprintf("    _mw = %s(result)\n", mwEntry))
						}
					}
				}
			}
		}

		if t.Wire.UseMapPattern {
			// Go-style: handlers map
			line := strings.ReplaceAll(t.Wire.MapEntryLine, "{{.Name}}", m.Name)
			line = strings.ReplaceAll(line, "{{.Alias}}", alias)
			line = strings.ReplaceAll(line, "{{.Entry}}", hn)
			routes.WriteString("\t" + line + "\n")
		} else {
			// Python/TS-style: if/elif chain
			if hn != "handler" && hn != "Handler" {
				fmt.Fprintf(&routes, "    # custom entry: %s\n", hn)
			}
			fmt.Fprintf(&routes, "    if module == \"%s\":\n        return %s(request)\n", m.Name, hn)
		}
	}

	var entry string
	if t.Wire.UseMapPattern {
		// Go: map-based dispatch with HTTP server
		entry = fmt.Sprintf(`package main

import (
    "encoding/json"
    "fmt"
    "net/http"
    "os"
%s
)

func wrap(fn func(map[string]interface{}) map[string]interface{}) func(map[string]interface{}) map[string]interface{} {
    return fn
}

var handlers = map[string]func(map[string]interface{}) map[string]interface{}{
%s}

func run(request map[string]interface{}) map[string]interface{} {
    module, _ := request["module"].(string)
    if h, ok := handlers[module]; ok {
        return h(request)
    }
    return map[string]interface{}{"result": nil, "error": map[string]interface{}{"code": "GATEWAY_ROUTE_NOT_FOUND", "message": fmt.Sprintf("Unknown module: %%s", module), "retryable": false, "source_module": "main"}}
}

func httpHandler(w http.ResponseWriter, r *http.Request) {
    var request map[string]interface{}
    if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
        http.Error(w, "invalid JSON body", 400)
        return
    }
    result := run(request)
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(result)
}

func main() {
    if len(os.Args) >= 2 && os.Args[1] == "-http" {
        port := "8080"
        if len(os.Args) >= 3 { port = os.Args[2] }
        http.HandleFunc("/api/", httpHandler)
        fmt.Println("Listening on :" + port)
        http.ListenAndServe(":"+port, nil)
        return
    }
    if len(os.Args) < 2 {
        data, _ := json.MarshalIndent(map[string]interface{}{"result": nil, "error": map[string]interface{}{"code": "GATEWAY_ROUTE_NOT_FOUND", "message": "Usage: main <module> or main -http [port]"}}, "", "  ")
        fmt.Println(string(data))
        os.Exit(1)
    }
    result := run(map[string]interface{}{"module": os.Args[1]})
    data, _ := json.MarshalIndent(result, "", "  ")
    fmt.Println(string(data))
}
`, imports.String(), routes.String())
	} else {
		// Python/TS: linear dispatch with middleware scaffolding (v5.3.0)
		var pyEntry strings.Builder
		pyEntry.WriteString(fmt.Sprintf("# Main entry point — auto-generated for %s\n", lang))
		pyEntry.WriteString(imports.String())

		// Transport registry (v0.8)
		pyEntry.WriteString("\n# Transport registry — maps \"module.entry\" to handler\n")
		if hasMultiEntry {
			pyEntry.WriteString("_transport = {\n")
			pyEntry.WriteString(transportLines.String())
			pyEntry.WriteString("}\n\n")
		} else {
			pyEntry.WriteString("# (single-entry modules, no registry needed)\n\n")
		}

		pyEntry.WriteString("def run(request):\n")
		pyEntry.WriteString("    module = request.get(\"module\", \"\")\n")
		if middlewareDeclared {
			pyEntry.WriteString(middlewareLines.String())
			pyEntry.WriteString("\n")
		}
		pyEntry.WriteString(routes.String())
		pyEntry.WriteString("    return {\"result\": None, \"error\": {\"code\": \"GATEWAY_ROUTE_NOT_FOUND\", \"message\": f\"Unknown module: {module}\", \"retryable\": false, \"source_module\": \"main\"}}\n")
		entry = pyEntry.String()
	}

	mainDir := filepath.Join(root, "source", "main")
	os.MkdirAll(mainDir, 0755)
	os.WriteFile(filepath.Join(mainDir, "main."+ext), []byte(entry), 0644)
	updateIndex(root, mods)
	return entry, nil
}

func updateIndex(root string, mods []ModuleDiscoverResult) {
	var b strings.Builder
	b.WriteString("# Project INDEX\n\n## Module Registry\n\n| Module | Version | Status | Language | Interface | Dependencies |\n")
	b.WriteString("|--------|---------|--------|----------|-----------|-------------|\n")
	for _, m := range mods {
		deps := strings.Join(m.Dependencies, ", ")
		if deps == "" { deps = "—" }
		iface := handlerName(m.Interface)
		if hasEntries(m.Interface) {
			enList := entryNames(m.Interface)
			iface = strings.Join(enList, ", ")
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | `%s(input) -> dict` | %s |\n", m.Name, m.Version, m.Status, m.Language, iface, deps)
	}
	b.WriteString("\n## Interface Convention\n\nAll modules expose `handler(input: dict) -> dict`.\n")
	os.WriteFile(filepath.Join(root, "INDEX.md"), []byte(b.String()), 0644)
}

func BuildOverview(ov ProjectOverview) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("## Project: %s\n\n", ov.ProjectDir))
	if ov.HasINDEX { b.WriteString("INDEX.md: ✓\n") }
	if ov.ProjectSummary != "" {
		b.WriteString("\n### Project Summary\n\n" + ov.ProjectSummary + "\n\n")
	}
	b.WriteString(fmt.Sprintf("Primary language: %s\nAIexplain: %v\nProject Memory: %v", ov.PrimaryLanguage, ov.HasAIExplain, ov.HasProjectMemory))
	if ov.HasProjectMemory { b.WriteString(fmt.Sprintf(" (%d files)", len(ov.ProjectMemory))) }
	b.WriteString(fmt.Sprintf("\nModules: %d\n", ov.ModuleCount))
	if len(ov.BuildOrder) > 0 { b.WriteString(fmt.Sprintf("Build order: %s\n", strings.Join(ov.BuildOrder, " → "))) }
	b.WriteString("\n")
	if len(ov.Warnings) > 0 {
		b.WriteString("### Warnings\n\n")
		for _, w := range ov.Warnings { b.WriteString(fmt.Sprintf("- ⚠ %s\n", w)) }
		b.WriteString("\n")
	}
	if ov.ModuleCount == 0 { b.WriteString("(empty)\n"); return b.String() }

	// Call graph display
	if len(ov.CallGraph) > 0 {
		b.WriteString("\n### Call Graph\n\n| Caller | → | Target.Entry |\n|--------|---|-------------|\n")
		for _, mod := range ov.Modules {
			if calls, ok := ov.CallGraph[mod.Name]; ok {
				for _, c := range calls {
					target := c.Module + "." + c.Entry
					if c.Input != "" {
						target += " (" + c.Input + "→" + c.Output + ")"
					}
					fmt.Fprintf(&b, "| %s | → | %s |\n", mod.Name, target)
				}
			}
		}
		b.WriteString("\n")
	}

	vs := loadValidationState(ov.ProjectDir)
	unvalidatedCount := 0
	failedCount := 0

	b.WriteString("| Module | Version | Status | Lang | Type | Deps | Valid |\n|--------|---------|--------|------|------|------|-------|\n")
	for _, m := range ov.Modules {
		deps := strings.Join(m.Dependencies, ", "); if deps == "" { deps = "—" }
		isGroup := m.Layer == "group" || len(m.Children) > 0
		isDeprecated := m.Status == "deprecated" || m.Status == "archived"

		moduleType := "leaf"
		moduleDisplay := m.Name
		if isGroup {
			moduleType = fmt.Sprintf("group (%d)", len(m.Children))
			moduleDisplay = "📦 " + m.Name
		}
		if isDeprecated {
			moduleDisplay = "⚠ " + m.Name
		}

		validStr := "—"
		if state, ok := vs.Modules[m.Name]; ok {
			if state.Valid {
				validStr = "✓"
			} else {
				validStr = "✗"
				failedCount++
			}
		} else {
			unvalidatedCount++
			validStr = ""
		}

		// For groups, skip normal handler/deps display
		if isGroup {
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s |\n",
				moduleDisplay, m.Version, m.Status, m.Language, moduleType, deps, validStr)
		} else {
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s |\n",
				m.Name, m.Version, m.Status, m.Language, moduleType, deps, validStr)
		}
	}
	if unvalidatedCount > 0 {
		ov.Warnings = append(ov.Warnings, fmt.Sprintf("%d modules not yet validated — run module_validate()", unvalidatedCount))
	}

	// Warning about deprecated module dependents
	for _, m := range ov.Modules {
		if m.Status == "deprecated" || m.Status == "archived" {
			dependents := FindDependentsOf(ov.ProjectDir, m.Name)
			if len(dependents) > 0 {
				ov.Warnings = append(ov.Warnings,
					fmt.Sprintf("Module '%s' is %s but still depended on by: %v",
						m.Name, m.Status, strings.Join(dependents, ", ")))
			}
		}
	}

	return b.String()
}

// ── Module Read & Memory (replaces skill.md) ──

// ModuleRead returns Level 3 details for a single module.
func ModuleRead(projectDir, moduleName string) (map[string]interface{}, error) {
	modPath := filepath.Join(projectDir, "source", "modules", moduleName, "module.json")
	modData, err := os.ReadFile(modPath)
	if err != nil {
		return nil, fmt.Errorf("module %q not found: %w", moduleName, err)
	}
	var contract map[string]interface{}
	json.Unmarshal(modData, &contract)

	aiCard := ""
	if data, err := os.ReadFile(filepath.Join(projectDir, "AIexplain", "modules", moduleName, moduleName+".md")); err == nil {
		aiCard = string(data)
	}
	aiIface := ""
	if data, err := os.ReadFile(filepath.Join(projectDir, "AIexplain", "modules", moduleName, "interface.md")); err == nil {
		aiIface = string(data)
	}

	srcPreview := ""
	for _, ext := range []string{".py", ".ts", ".js", ".go"} {
		p := filepath.Join(projectDir, "source", "modules", moduleName, moduleName+ext)
		if data, err := os.ReadFile(p); err == nil {
			s := string(data)
			if len(s) > 2000 { s = s[:2000] + "\n... (truncated)" }
			srcPreview = s
			break
		}
	}

	return map[string]interface{}{
		"module":      moduleName,
		"contract":    contract,
		"aiexplain":   aiCard,
		"interface":   aiIface,
		"source_preview": srcPreview,
	}, nil
}

func DiscoverModules(root string) []*ModuleContract {
	ov := ModuleDiscover(root)
	mods := make([]*ModuleContract, len(ov.Modules))
	for i, m := range ov.Modules {
		mods[i] = &ModuleContract{Name: m.Name, Version: m.Version, Status: m.Status, OwnerAgent: m.OwnerAgent, Dependencies: m.Dependencies, Interface: m.Interface, Language: m.Language}
	}
	return mods
}

type BootstrapResult struct {
	Created, Wired, Validated, Synced, ImportOk bool
	BuildOrder                                  []string
	ModuleCount                                 int
	Errors, Warnings                            []string
}

func BootstrapModule(root, name, language, description string) *BootstrapResult {
	r := &BootstrapResult{}
	modDir := filepath.Join(root, "source", "modules", name)
	if err := CreateModule(root, name, "agent", language); err != nil { r.Errors = append(r.Errors, "create: "+err.Error()); return r }
	r.Created = true
	ov := ModuleDiscover(root)
	if _, err := WireMain(root, ov.Modules); err != nil {
		r.Warnings = append(r.Warnings, "wire: "+err.Error())
		// Rollback: remove created module files
		if rmErr := os.RemoveAll(modDir); rmErr == nil {
			r.Warnings = append(r.Warnings, "rolled back: removed "+modDir)
		}
	} else {
		r.Wired = true
		r.BuildOrder = ov.BuildOrder
		r.ModuleCount = ov.ModuleCount
		EnsureAIExplain(root)
		r.Synced = true
	}
	return r
}

// BuildReport generates a project-level health report.
func BuildReport(root string) map[string]interface{} {
	ov := ModuleDiscover(root)
	vs := loadValidationState(root)

	report := map[string]interface{}{
		"project":       filepath.Base(root),
		"module_count":  ov.ModuleCount,
		"language":      ov.PrimaryLanguage,
		"has_memory":    ov.HasProjectMemory,
		"has_aiexplain": ov.HasAIExplain,
		"circular_deps": len(ov.CircularDeps),
	}

	type moduleInfo struct {
		Name       string   `json:"name"`
		Status     string   `json:"status"`
		Dependents int      `json:"dependents"`
		Valid      string   `json:"valid"`
		EntryCount int      `json:"entries"`
		Warnings   []string `json:"warnings,omitempty"`
	}
	var modules []moduleInfo
	unvalidated := 0
	failed := 0
	deprecated := 0
	for _, m := range ov.Modules {
		info := moduleInfo{
			Name:       m.Name,
			Status:     m.Status,
			Dependents: len(m.DependedBy),
			EntryCount: len(entryNames(m.Interface)),
		}
		if state, ok := vs.Modules[m.Name]; ok {
			if state.Valid {
				info.Valid = "valid"
			} else {
				info.Valid = "failed"
				failed++
			}
		} else {
			info.Valid = "unvalidated"
			unvalidated++
		}
		if m.Status == "deprecated" || m.Status == "archived" {
			deprecated++
		}
		if len(m.DependedBy) == 0 && m.Status != "deprecated" && m.Status != "archived" {
			info.Warnings = append(info.Warnings, "no dependents — may be dead code")
		}
		if len(entryNames(m.Interface)) > 7 {
			info.Warnings = append(info.Warnings, fmt.Sprintf("%d entries, consider splitting", len(entryNames(m.Interface))))
		}
		modules = append(modules, info)
	}
	report["modules"] = modules
	report["unvalidated"] = unvalidated
	report["failed"] = failed
	report["deprecated"] = deprecated

	// Risk score
	risk := 0
	if ov.ModuleCount > 0 {
		risk = int(float64(unvalidated)/float64(ov.ModuleCount)*30 +
			float64(failed)/float64(ov.ModuleCount)*50 +
			float64(deprecated)*5)
		if risk > 100 {
			risk = 100
		}
	}
	report["risk_score"] = risk

	// Core modules
	type coreInfo struct {
		Name       string `json:"name"`
		Dependents int    `json:"dependents"`
	}
	var coreModules []coreInfo
	for _, m := range modules {
		if m.Dependents >= 2 {
			coreModules = append(coreModules, coreInfo{Name: m.Name, Dependents: m.Dependents})
		}
	}
	report["core_modules"] = coreModules

	return report
}
