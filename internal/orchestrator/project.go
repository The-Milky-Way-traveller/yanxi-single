// Package orchestrator — project-level configuration, memory, and ADR.
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

// ── Project Config ──

// ProjectConfig describes project-level metadata read from .yanxi/project.json.
type ProjectConfig struct {
	Name         string   `json:"name"`
	Summary      string   `json:"summary"`
	TechStack    []string `json:"tech_stack,omitempty"`
	Architecture string   `json:"architecture,omitempty"`
	Status       string   `json:"status,omitempty"`
	StartHere    []string `json:"start_here,omitempty"`
}

// ProjectMemory holds the three project-memory markdown files (raw).
type ProjectMemory struct {
	ADRs        string   `json:"adrs,omitempty"`
	Lessons     string   `json:"lessons,omitempty"`
	Conventions string   `json:"conventions,omitempty"`
}

// ── Structured Memory (v5.3.0) ──

// ADR represents a single Architecture Decision Record.
type ADR struct {
	Number      string `json:"number"`
	Title       string `json:"title"`
	Status      string `json:"status"` // proposed | accepted | superseded | expired
	Date        string `json:"date"`
	Decisioner  string `json:"decisioner,omitempty"`
	Module      string `json:"module,omitempty"`
	Context     string `json:"context"`
	Decision    string `json:"decision"`
	Consequences string `json:"consequences,omitempty"`
	ReplacedBy string `json:"replaced_by,omitempty"`
}

// StructuredMemory holds parsed project-memory content.
type StructuredMemory struct {
	ADRs        []ADR
	Lessons     []string
	Conventions string
}

// ── Project Config Load/Save ──

func LoadProjectConfig(root string) *ProjectConfig {
	path := filepath.Join(root, ".yanxi", "project.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var cfg ProjectConfig
	if json.Unmarshal(data, &cfg) != nil {
		return nil
	}
	return &cfg
}

func SaveProjectConfig(root string, cfg *ProjectConfig) error {
	dir := filepath.Join(root, ".yanxi")
	os.MkdirAll(dir, 0755)
	data, _ := json.MarshalIndent(cfg, "", "  ")
	return os.WriteFile(filepath.Join(dir, "project.json"), data, 0644)
}

// ── Project Memory Load ──

// LoadProjectMemory reads the three project-memory files (raw markdown).
func LoadProjectMemory(root string) *ProjectMemory {
	dir := filepath.Join(root, "project-memory")
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return nil
	}
	pm := &ProjectMemory{}
	if data, err := os.ReadFile(filepath.Join(dir, "architecture-decisions.md")); err == nil {
		pm.ADRs = string(data)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "lessons-learned.md")); err == nil {
		pm.Lessons = string(data)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "conventions.md")); err == nil {
		pm.Conventions = string(data)
	}
	return pm
}

// LoadStructuredMemory loads and parses project-memory into structured form.
func LoadStructuredMemory(root string) *StructuredMemory {
	pm := LoadProjectMemory(root)
	if pm == nil {
		return nil
	}
	sm := &StructuredMemory{}
	if pm.ADRs != "" {
		sm.ADRs = ParseADRs(pm.ADRs)
	}
	if pm.Lessons != "" {
		sm.Lessons = ParseLessons(pm.Lessons)
	}
	if pm.Conventions != "" {
		sm.Conventions = pm.Conventions
	}
	return sm
}

// ── Ensure Project Memory ──

func EnsureProjectMemory(root string) {
	dir := filepath.Join(root, "project-memory")
	os.MkdirAll(dir, 0755)

	templates := map[string]string{
		"architecture-decisions.md": `# Architecture Decision Records

## ADR Template

| Field | Content |
|-------|---------|
| Number | ADR-001 |
| Status | proposed / accepted / superseded / expired |
| Date | YYYY-MM-DD |
| Context | What problem needed a decision? |
| Decision | What was decided? |
| Consequences | What trade-offs were accepted? |
| Replaced by | ADR-XXX (when superseded) |

*No ADRs yet. Use memory_write("adr", ...) to add one.*
`,
		"lessons-learned.md": `# Lessons Learned

Lessons are automatically recorded when module_validate fails or when
schema-breaking changes are detected. Agents can also add lessons manually.

*No lessons recorded yet.*
`,
		"conventions.md": `# Project Conventions

## Code Style
- handler entry: 'handler(input: dict) -> dict' (Python) / 'Handler(input map[string]interface{}) map[string]interface{}' (Go)
- Error code format: 'MODULE_ERROR_TYPE'
- Module names: lowercase_with_underscores

## Versioning
- Fix: patch bump (1.0.0 → 1.0.1)
- New feature (backward-compatible): minor bump (1.0.0 → 1.1.0)
- Breaking change: major bump (1.0.0 → 2.0.0)

## Workflow
1. Read module AIexplain before editing
2. Edit source code + bump version in module.json
3. Run module_validate("<name>")
4. Run aiexplain_generate() to sync

*Edit this file to add project-specific conventions.*
`,
	}

	for name, tmpl := range templates {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			os.WriteFile(path, []byte(tmpl), 0644)
		}
	}
}

// ── Config Summary ──

func ConfigSummary(cfg *ProjectConfig, moduleCount int, primaryLang string, pm *ProjectMemory) string {
	if cfg == nil || cfg.Summary == "" {
		s := fmt.Sprintf("Go project with %d modules. Primary language: %s.", moduleCount, primaryLang)
		if pm != nil {
			s += " Has project-memory."
		} else {
			s += " No project-memory — run memory_init() to create one."
		}
		return s
	}
	s := cfg.Summary
	if cfg.Architecture != "" {
		s += "\nArchitecture: " + cfg.Architecture
	}
	if cfg.Status != "" {
		s += "\nStatus: " + cfg.Status
	}
	if len(cfg.StartHere) > 0 {
		s += "\nStart here: " + fmt.Sprintf("%v", cfg.StartHere)
	}
	return s
}

// ── Memory Write ──

func MemoryWrite(root, kind, content string) error {
	var filename string
	switch kind {
	case "adr":
		filename = "architecture-decisions.md"
	case "lesson", "lessons":
		filename = "lessons-learned.md"
	case "convention", "conventions":
		filename = "conventions.md"
	default:
		return fmt.Errorf("unknown memory kind: %q (use adr/lesson/convention)", kind)
	}
	dir := filepath.Join(root, "project-memory")
	os.MkdirAll(dir, 0755)
	f, err := os.OpenFile(filepath.Join(dir, filename), os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(fmt.Sprintf("\n- %s [%s]\n", content, time.Now().UTC().Format("2006-01-02")))
	return err
}

func MemoryAppendLesson(root, note string) error {
	// Dedup: check if a similar lesson already exists
	pm := LoadProjectMemory(root)
	if pm != nil && pm.Lessons != "" {
		existing := ParseLessons(pm.Lessons)
		for _, e := range existing {
			if strings.Contains(e, note) || strings.Contains(note, e) {
				return nil // already recorded
			}
		}
	}
	return MemoryWrite(root, "lesson", note)
}

// ── Memory Init ──

// InitProjectMemory creates .yanxi/project.json (if missing) and project-memory/
// with all three template files. Returns what was created.
func InitProjectMemory(root string) map[string]bool {
	result := map[string]bool{}

	// .yanxi/project.json
	cfgPath := filepath.Join(root, ".yanxi", "project.json")
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		cfg := &ProjectConfig{
			Name:    filepath.Base(root),
			Summary: "Auto-detected project. Update .yanxi/project.json with a real summary.",
			Status:  "initialized",
		}
		if err := SaveProjectConfig(root, cfg); err == nil {
			result["project.json"] = true
		}
	}

	// project-memory/ (idempotent — only creates missing files)
	EnsureProjectMemory(root)

	// Check which files were created
	for _, name := range []string{"architecture-decisions.md", "lessons-learned.md", "conventions.md"} {
		path := filepath.Join(root, "project-memory", name)
		if info, err := os.Stat(path); err == nil && info.Size() > 80 {
			result[name] = true
		}
	}

	return result
}

// ── Structured ADR Parsing ──

// ParseADRs extracts ADR records from Markdown content.
// Expects format: "## ADR-001: Title" followed by a table.
func ParseADRs(text string) []ADR {
	var adrs []ADR
	re := regexp.MustCompile(`(?m)^##\s+(ADR-\d+):\s*(.*?)$`)
	matches := re.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return nil
	}
	for i, match := range matches {
		start := match[1]
		end := len(text)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		number := text[match[2]:match[3]]
		title := text[match[4]:match[5]]
		content := text[start:end]

		adr := ParseADRows(number, title, content)
		if adr != nil {
			adrs = append(adrs, *adr)
		}
	}
	return adrs
}

// ParseADRows parses an ADR's table rows into a struct.
func ParseADRows(number, title, content string) *ADR {
	adr := &ADR{Number: number, Title: title, Status: "proposed"}
	rowRe := regexp.MustCompile(`(?m)^\|\s*([^|]+)\s*\|\s*([^|]+)\s*\|`)
	for _, m := range rowRe.FindAllStringSubmatch(content, -1) {
		if len(m) < 3 {
			continue
		}
		key := strings.TrimSpace(m[1])
		val := strings.TrimSpace(m[2])
		switch key {
		case "编号", "Number":
			if val != "" {
				adr.Number = val
			}
		case "状态", "Status":
			adr.Status = val
		case "日期", "Date":
			adr.Date = val
		case "决策者", "Decisioner":
			adr.Decisioner = val
		case "关联模块", "Module":
			adr.Module = val
		case "问题", "Context":
			adr.Context = val
		case "决策", "Decision", "选择理由":
			adr.Decision = val
		case "代价", "Consequences":
			adr.Consequences = val
		case "替代", "Replaced by":
			adr.ReplacedBy = val
		}
	}
	return adr
}

// ParseLessons extracts lesson entries from lessons-learned.md.
func ParseLessons(text string) []string {
	var lessons []string
	re := regexp.MustCompile(`(?m)^-\s*(.+?)\s*\[\d{4}-\d{2}-\d{2}\]$`)
	for _, m := range re.FindAllStringSubmatch(text, -1) {
		if len(m) > 1 {
			lessons = append(lessons, strings.TrimSpace(m[1]))
		}
	}
	return lessons
}

// WriteADR appends a structured ADR to architecture-decisions.md.
func WriteADR(root string, adr ADR) error {
	dir := filepath.Join(root, "project-memory")
	os.MkdirAll(dir, 0755)
	path := filepath.Join(dir, "architecture-decisions.md")

	if adr.Date == "" {
		adr.Date = time.Now().UTC().Format("2006-01-02")
	}
	if adr.Status == "" {
		adr.Status = "accepted"
	}

	entry := fmt.Sprintf("\n\n## %s: %s\n\n| Field | Content |\n|-------|---------|\n| Number | %s |\n| Status | %s |\n| Date | %s |\n| Context | %s |\n| Decision | %s |\n| Consequences | %s |\n",
		adr.Number, adr.Title,
		adr.Number, adr.Status, adr.Date,
		adr.Context, adr.Decision, adr.Consequences)

	if adr.Module != "" {
		entry += fmt.Sprintf("| Module | %s |\n", adr.Module)
	}
	if adr.ReplacedBy != "" {
		entry += fmt.Sprintf("| Replaced by | %s |\n", adr.ReplacedBy)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(entry)
	return err
}
