package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"yanxi-single/internal/check"
	"yanxi-single/internal/mcp"
	"yanxi-single/internal/orchestrator"
	"yanxi-single/internal/orchestrator/langtmpl"
	"yanxi-single/internal/search"
	"yanxi-single/internal/validate"
)

func main() {
	srv := mcp.NewServer("yanxi-single", "1.0.0")

	srv.RegisterTool("module_discover", "Discover all modules. Returns project overview (Level 1) + module summaries (Level 2). Use module_read() for Level 3 details.",
		mcp.ToolSchema{Type: "object", Properties: map[string]mcp.PropertySpec{
			"project_dir": {Type: "string", Description: "Project root"},
			"lazy":        {Type: "boolean", Description: "If true, return summaries only. Use module_read() for details."},
		}},
		func(args map[string]interface{}) (interface{}, error) {
			projectDir, _ := args["project_dir"].(string)
			if projectDir == "" { projectDir = "." }
			lazy := false
			if l, ok := args["lazy"].(bool); ok { lazy = l }
			var ov orchestrator.ProjectOverview
			var ns string
			if lazy {
				ov = orchestrator.ModuleDiscoverLazy(projectDir)
				ns = fmt.Sprintf("Lazy mode: %d modules. Use module_read(\"<name>\") for details.", ov.ModuleCount)
			} else {
				ov = orchestrator.ModuleDiscover(projectDir)
				ns = "Level 1: project summary. Level 2: module summaries. Use module_read(name) for Level 3."
			}
			return map[string]interface{}{"overview_text": orchestrator.BuildOverview(ov), "project": ov, "next_step": ns}, nil
		},
	)

	srv.RegisterTool("module_create", "Create a new module skeleton.",
		mcp.ToolSchema{Type: "object", Properties: map[string]mcp.PropertySpec{
			"name": {Type: "string", Description: "Module name", Required: true},
			"language": {Type: "string", Description: "python/typescript/javascript/go"},
			"project_dir": {Type: "string", Description: "Project root"},
			"description": {Type: "string", Description: "Short description"},
		}, Required: []string{"name"}},
		func(args map[string]interface{}) (interface{}, error) {
			name, _ := args["name"].(string)
			if name == "" { return nil, fmt.Errorf("name required") }
			lang, _ := args["language"].(string)
			if lang == "" { lang = "python" }
			projectDir, _ := args["project_dir"].(string)
			if projectDir == "" { projectDir = "." }
			desc, _ := args["description"].(string)

			// If language not built-in, return bootstrap prompt for external agent
			if _, err := langtmpl.Resolve(projectDir, lang); err != nil {
				return map[string]interface{}{
					"status": "need_lang_template",
					"language": lang,
					"prompt": langtmpl.BootstrapPrompt(lang),
					"next_step": fmt.Sprintf("Send the prompt to an LLM, then call save_lang_template(%q, <json>)", lang),
				}, nil
			}

			if err := orchestrator.CreateModule(projectDir, name, "agent", lang); err != nil { return nil, err }
			if desc != "" {
				mp := filepath.Join(projectDir, "source", "modules", name, "module.json")
				if data, err := os.ReadFile(mp); err == nil {
					var mod map[string]interface{}
					json.Unmarshal(data, &mod)
					if iface, ok := mod["interface"].(map[string]interface{}); ok { iface["description"] = desc }
					d, _ := json.MarshalIndent(mod, "", "  ")
					os.WriteFile(mp, d, 0644)
				}
			}
			// Re-validate after description update
			vr := validate.ValidateModule(projectDir, name)
			orchestrator.MarkValidated(projectDir, vr)
			return map[string]interface{}{"status": "created", "module": name, "language": lang, "next_step": fmt.Sprintf("Write handler, then module_validate(\"%s\").", name)}, nil
		},
	)

	srv.RegisterTool("aiexplain_generate", "Regenerate AIexplain + rebuild search index. Incremental.",
		mcp.ToolSchema{Type: "object", Properties: map[string]mcp.PropertySpec{"project_dir": {Type: "string", Description: "Project root"}}},
		func(args map[string]interface{}) (interface{}, error) {
			projectDir, _ := args["project_dir"].(string)
			if projectDir == "" { projectDir = "." }
			orchestrator.EnsureAIExplain(projectDir)
			var idxInfo string
			idx, err := search.BuildIndex(projectDir)
			if err == nil { idx.Save(projectDir); idxInfo = fmt.Sprintf("%d docs", idx.N) }
			return map[string]interface{}{"status": "regenerated", "index": idxInfo, "next_step": "Use module_search(query) or module_discover()."}, nil
		},
	)

	srv.RegisterTool("module_validate", "Validate module: handler exists, deps consistent, multi-language tests.",
		mcp.ToolSchema{Type: "object", Properties: map[string]mcp.PropertySpec{
			"module": {Type: "string", Description: "Module name", Required: true},
			"project_dir": {Type: "string", Description: "Project root"},
		}, Required: []string{"module"}},
		func(args map[string]interface{}) (interface{}, error) {
			modName, _ := args["module"].(string)
			if modName == "" { return nil, fmt.Errorf("module required") }
			projectDir, _ := args["project_dir"].(string)
			if projectDir == "" { projectDir = "." }
			r := validate.ValidateModule(projectDir, modName)
			ns := "Run module_wire() then aiexplain_generate()."
			if !r.Valid {
				ns = "Fix errors above, then re-run."
				for _, e := range r.Errors {
					orchestrator.MemoryAppendLesson(projectDir, "validate "+modName+" failed: "+e)
				}
			}

			result := map[string]interface{}{
				"module": r.Module, "valid": r.Valid,
				"errors": r.Errors, "warnings": r.Warnings,
				"tests": r.Tests, "imports": r.Imports,
				"breaking_changes": r.BreakingChanges, "strict_mode": r.StrictMode,
				"side_effects": r.SideEffects, "benchmarks": r.Benchmarks, "coverage": r.Coverage,
				"call_issues": r.CallIssues, "deprecated_deps": r.DeprecatedDeps,
				"middleware_issues": r.MiddlewareIssues,
				"transport_issues": r.TransportIssues,
				"convention_issues": r.ConventionIssues,
				"next_step": ns,
			}
			return result, nil
		},
	)

	srv.RegisterTool("module_wire", "Generate main routing + update INDEX.md.",
		mcp.ToolSchema{Type: "object", Properties: map[string]mcp.PropertySpec{"project_dir": {Type: "string", Description: "Project root"}}},
		func(args map[string]interface{}) (interface{}, error) {
			projectDir, _ := args["project_dir"].(string)
			if projectDir == "" { projectDir = "." }
			ov := orchestrator.ModuleDiscover(projectDir)
			if ov.ModuleCount == 0 { return nil, fmt.Errorf("no modules") }
			mp, err := orchestrator.WireMain(projectDir, ov.Modules)
			if err != nil { return nil, err }
			return map[string]interface{}{"status": "wired", "wired": ov.ModuleCount, "order": ov.BuildOrder, "preview": mp[:min(len(mp), 500)], "next_step": "Run aiexplain_generate() to sync."}, nil
		},
	)

	srv.RegisterTool("module_search", "Search AIexplain+source. BM25 default, mode=vector/hybrid (needs -tags vector).",
		mcp.ToolSchema{Type: "object", Properties: map[string]mcp.PropertySpec{
			"query": {Type: "string", Description: "Search query", Required: true},
			"top_k": {Type: "integer", Description: "Results (default 10)"},
			"kind":  {Type: "string", Description: "aiexplain/source/all"},
			"mode":  {Type: "string", Description: "bm25/vector/hybrid (default bm25)"},
			"project_dir": {Type: "string", Description: "Project root"},
		}, Required: []string{"query"}},
		func(args map[string]interface{}) (interface{}, error) {
			q, _ := args["query"].(string)
			if q == "" { return nil, fmt.Errorf("query required") }
			projectDir, _ := args["project_dir"].(string)
			if projectDir == "" { projectDir = "." }
			k := 10
			if v, ok := args["top_k"].(float64); ok && v > 0 { k = int(v) }
			mode, _ := args["mode"].(string)
			if mode == "" { mode = "bm25" }
			idx, err := search.Load(projectDir)
			if err != nil { idx, err = search.BuildIndex(projectDir); if err != nil { return nil, fmt.Errorf("index: %w", err) }; idx.Save(projectDir) }
			vr := idx.SearchVector(q, k, mode)
			if kf, _ := args["kind"].(string); kf != "" && kf != "all" {
				var f []search.VectorResult
				for _, r := range vr { if string(r.Doc.Kind) == kf { f = append(f, r) } }
				vr = f
			}
			return map[string]interface{}{"query": q, "results": vr, "total": len(vr), "docs": idx.N, "next_step": "Read the top result document."}, nil
		},
	)

	srv.RegisterTool("module_check_imports", "Verify declared vs actual imports.",
		mcp.ToolSchema{Type: "object", Properties: map[string]mcp.PropertySpec{
			"module": {Type: "string", Description: "Module name", Required: true},
			"project_dir": {Type: "string", Description: "Project root"},
		}, Required: []string{"module"}},
		func(args map[string]interface{}) (interface{}, error) {
			modName, _ := args["module"].(string)
			if modName == "" { return nil, fmt.Errorf("module required") }
			projectDir, _ := args["project_dir"].(string)
			if projectDir == "" { projectDir = "." }
			ir := check.Imports(projectDir, modName)
			ns := "Imports consistent."
			if !ir.Ok { ns = fmt.Sprintf("Fix: %s", ir.Error) }
			return map[string]interface{}{"module": ir.Module, "ok": ir.Ok, "declared": ir.Declared, "imported": ir.Imported, "undeclared": ir.Undeclared, "unused": ir.Unused, "error": ir.Error, "next_step": ns}, nil
		},
	)

	srv.RegisterTool("module_bootstrap", "One-shot: create+wire+sync.",
		mcp.ToolSchema{Type: "object", Properties: map[string]mcp.PropertySpec{
			"name": {Type: "string", Description: "Module name", Required: true},
			"language": {Type: "string", Description: "python/typescript/javascript/go"},
			"project_dir": {Type: "string", Description: "Project root"},
		}, Required: []string{"name"}},
		func(args map[string]interface{}) (interface{}, error) {
			name, _ := args["name"].(string)
			if name == "" { return nil, fmt.Errorf("name required") }
			lang, _ := args["language"].(string)
			if lang == "" { lang = "python" }
			projectDir, _ := args["project_dir"].(string)
			if projectDir == "" { projectDir = "." }
			r := orchestrator.BootstrapModule(projectDir, name, lang, "")
			return map[string]interface{}{"result": r, "next_step": fmt.Sprintf("Write handler then module_validate(\"%s\").", name)}, nil
		},
	)

	srv.RegisterTool("save_lang_template", "Save a language template generated by an LLM to .yanxi/lang-templates/<lang>.json",
		mcp.ToolSchema{Type: "object", Properties: map[string]mcp.PropertySpec{
			"language": {Type: "string", Description: "Language name (e.g. rust)", Required: true},
			"template_json": {Type: "string", Description: "LangTemplate JSON from LLM", Required: true},
			"project_dir": {Type: "string", Description: "Project root"},
		}, Required: []string{"language", "template_json"}},
		func(args map[string]interface{}) (interface{}, error) {
			lang, _ := args["language"].(string)
			tmplStr, _ := args["template_json"].(string)
			if lang == "" || tmplStr == "" { return nil, fmt.Errorf("language and template_json required") }
			projectDir, _ := args["project_dir"].(string)
			if projectDir == "" { projectDir = "." }

			var tmpl langtmpl.LangTemplate
			if err := json.Unmarshal([]byte(tmplStr), &tmpl); err != nil {
				return nil, fmt.Errorf("invalid template JSON: %w", err)
			}
			tmpl.Language = lang
			if err := langtmpl.Save(projectDir, &tmpl); err != nil {
				return nil, fmt.Errorf("save failed: %w", err)
			}
			return map[string]interface{}{"status": "saved", "language": lang, "next_step": fmt.Sprintf("Now use module_create with language=%q", lang)}, nil
		},
	)

	srv.RegisterTool("module_read", "Read a module's full details: AIexplain card + interface + module.json + source preview.",
		mcp.ToolSchema{Type: "object", Properties: map[string]mcp.PropertySpec{
			"module": {Type: "string", Description: "Module name", Required: true},
			"project_dir": {Type: "string", Description: "Project root"},
		}, Required: []string{"module"}},
		func(args map[string]interface{}) (interface{}, error) {
			modName, _ := args["module"].(string)
			if modName == "" { return nil, fmt.Errorf("module required") }
			projectDir, _ := args["project_dir"].(string)
			if projectDir == "" { projectDir = "." }
			result, err := orchestrator.ModuleRead(projectDir, modName)
			if err != nil {
				return nil, err
			}
			return result, nil
		},
	)

	srv.RegisterTool("memory_init", "Create .yanxi/project.json + project-memory/ with template files. Idempotent — safe to re-run.",
		mcp.ToolSchema{Type: "object", Properties: map[string]mcp.PropertySpec{
			"project_dir": {Type: "string", Description: "Project root"},
		}},
		func(args map[string]interface{}) (interface{}, error) {
			projectDir, _ := args["project_dir"].(string)
			if projectDir == "" { projectDir = "." }
			created := orchestrator.InitProjectMemory(projectDir)
			ns := "Project memory ready."
			if !created["project.json"] {
				ns = "project-memory already exists. Use memory_write() to add entries."
			}
			for _, created := range []bool{created["architecture-decisions.md"], created["lessons-learned.md"], created["conventions.md"]} {
				if created { ns += " Created templates."; break }
			}
			return map[string]interface{}{
				"created": created,
				"next_step": ns,
			}, nil
		},
	)

	srv.RegisterTool("memory_write", "Write an entry to project-memory (adr/lesson/convention).",
		mcp.ToolSchema{Type: "object", Properties: map[string]mcp.PropertySpec{
			"kind": {Type: "string", Description: "adr | lesson | convention", Required: true},
			"content": {Type: "string", Description: "Content to append", Required: true},
			"project_dir": {Type: "string", Description: "Project root"},
		}, Required: []string{"kind", "content"}},
		func(args map[string]interface{}) (interface{}, error) {
			kind, _ := args["kind"].(string)
			content, _ := args["content"].(string)
			projectDir, _ := args["project_dir"].(string)
			if projectDir == "" { projectDir = "." }
			if err := orchestrator.MemoryWrite(projectDir, kind, content); err != nil {
				return nil, err
			}
			return map[string]interface{}{"status": "written", "kind": kind, "next_step": "module_discover() to see updated memory."}, nil
		},
	)

	srv.RegisterTool("module_search_loose", "Search any directory without micro-architecture. Indexes all code files.",
		mcp.ToolSchema{Type: "object", Properties: map[string]mcp.PropertySpec{
			"query": {Type: "string", Description: "Search query", Required: true},
			"top_k": {Type: "integer", Description: "Results (default 10)"},
			"project_dir": {Type: "string", Description: "Directory"},
		}, Required: []string{"query"}},
		func(args map[string]interface{}) (interface{}, error) {
			q, _ := args["query"].(string)
			if q == "" { return nil, fmt.Errorf("query required") }
			projectDir, _ := args["project_dir"].(string)
			if projectDir == "" { projectDir = "." }
			k := 10
			if v, ok := args["top_k"].(float64); ok && v > 0 { k = int(v) }
			idx, err := search.BuildLooseIndex(projectDir)
			if err != nil { return nil, fmt.Errorf("index: %w", err) }
			idx.Save(projectDir)
			return map[string]interface{}{"query": q, "results": idx.Search(q, k), "total": idx.N, "next_step": "Read the matching source file."}, nil
		},
	)

	srv.RegisterTool("module_sync", "Apply pending changes to a module: sync entries, calls, and version from source code.",
		mcp.ToolSchema{Type: "object", Properties: map[string]mcp.PropertySpec{
			"module":     {Type: "string", Description: "Module name", Required: true},
			"project_dir": {Type: "string", Description: "Project root"},
		}, Required: []string{"module"}},
		func(args map[string]interface{}) (interface{}, error) {
			modName, _ := args["module"].(string)
			if modName == "" { return nil, fmt.Errorf("module required") }
			projectDir, _ := args["project_dir"].(string)
			if projectDir == "" { projectDir = "." }
			result := validate.ModuleSync(projectDir, modName)
			ns := "Sync complete."
			if err, ok := result["error"].(string); ok && err != "" {
				ns = "Sync failed: " + err
			}
			result["next_step"] = ns
			return result, nil
		},
	)

	srv.RegisterTool("module_deprecate", "Mark a module as deprecated or archived. Records an ADR and warns dependents.",
		mcp.ToolSchema{Type: "object", Properties: map[string]mcp.PropertySpec{
			"module":     {Type: "string", Description: "Module name", Required: true},
			"new_status": {Type: "string", Description: "deprecated or archived", Required: true},
			"reason":     {Type: "string", Description: "Why this module is being deprecated", Required: true},
			"project_dir": {Type: "string", Description: "Project root"},
		}, Required: []string{"module", "new_status", "reason"}},
		func(args map[string]interface{}) (interface{}, error) {
			modName, _ := args["module"].(string)
			newStatus, _ := args["new_status"].(string)
			reason, _ := args["reason"].(string)
			projectDir, _ := args["project_dir"].(string)
			if projectDir == "" { projectDir = "." }
			if modName == "" || newStatus == "" || reason == "" {
				return nil, fmt.Errorf("module, new_status, and reason are required")
			}
			if err := orchestrator.DeprecateModule(projectDir, modName, newStatus, reason); err != nil {
				return nil, err
			}
			dependents := orchestrator.FindDependentsOf(projectDir, modName)
			ns := fmt.Sprintf("Module %q is now %s.", modName, newStatus)
			if len(dependents) > 0 {
				ns += fmt.Sprintf(" Warning: still depended on by %v", dependents)
			}
			return map[string]interface{}{
				"status":      newStatus,
				"module":      modName,
				"dependents":  dependents,
				"next_step":   ns,
			}, nil
		},
	)

	srv.RegisterTool("module_adopt", "Analyse an external directory for adoption as a yanxi module. Returns analysis + LLM prompt for transformation.",
		mcp.ToolSchema{Type: "object", Properties: map[string]mcp.PropertySpec{
			"dir":    {Type: "string", Description: "External directory path (relative to project root)", Required: true},
			"language": {Type: "string", Description: "python/go/typescript/javascript (auto-detect if empty)"},
			"project_dir": {Type: "string", Description: "Project root"},
		}, Required: []string{"dir"}},
		func(args map[string]interface{}) (interface{}, error) {
			dir, _ := args["dir"].(string)
			if dir == "" { return nil, fmt.Errorf("dir required") }
			projectDir, _ := args["project_dir"].(string)
			if projectDir == "" { projectDir = "." }
			lang, _ := args["language"].(string)
			analysis, err := orchestrator.AnalyzeExternalDir(projectDir, dir, lang)
			if err != nil { return nil, err }
			prompt := orchestrator.BuildAdoptPrompt(analysis)
			return map[string]interface{}{
				"analysis":  analysis,
				"prompt":    prompt,
				"next_step": "Send the prompt to an LLM, then call module_adopt_commit() with the result.",
			}, nil
		},
	)

	srv.RegisterTool("module_adopt_commit", "Finalise an adoption: write adapted source, generate module.json, delete original, wire + aiexplain.",
		mcp.ToolSchema{Type: "object", Properties: map[string]mcp.PropertySpec{
			"module_name":    {Type: "string", Description: "New module name", Required: true},
			"source_dir":    {Type: "string", Description: "Original external directory (relative to project root)", Required: true},
			"language":      {Type: "string", Description: "python/go/typescript/javascript", Required: true},
			"adapted_source": {Type: "string", Description: "LLM-transformed source code", Required: true},
			"entries_json":  {Type: "string", Description: "JSON array of entry definitions [{name, description, input_schema, output_schema}]"},
			"project_dir":   {Type: "string", Description: "Project root"},
		}, Required: []string{"module_name", "source_dir", "language", "adapted_source"}},
		func(args map[string]interface{}) (interface{}, error) {
			name, _ := args["module_name"].(string)
			srcDir, _ := args["source_dir"].(string)
			lang, _ := args["language"].(string)
			adapted, _ := args["adapted_source"].(string)
			entriesJSON, _ := args["entries_json"].(string)
			projectDir, _ := args["project_dir"].(string)
			if projectDir == "" { projectDir = "." }
			if name == "" || srcDir == "" || lang == "" || adapted == "" {
				return nil, fmt.Errorf("module_name, source_dir, language, and adapted_source are required")
			}
			params := &orchestrator.AdoptCommitParams{
				ModuleName:    name,
				SourceDir:     srcDir,
				Language:      lang,
				AdaptedSource: adapted,
				EntriesJSON:   entriesJSON,
			}
			result, err := orchestrator.AdoptCommit(projectDir, params)
			if err != nil { return nil, err }
			ns := fmt.Sprintf("Module %q adopted. Run module_validate() to verify.", name)
			return map[string]interface{}{"result": result, "next_step": ns}, nil
		},
	)

	srv.RegisterTool("module_report", "Generate a project-level health report: heatmap, risk score, core modules.",
		mcp.ToolSchema{Type: "object", Properties: map[string]mcp.PropertySpec{
			"project_dir": {Type: "string", Description: "Project root"},
		}},
		func(args map[string]interface{}) (interface{}, error) {
			projectDir, _ := args["project_dir"].(string)
			if projectDir == "" { projectDir = "." }
			report := orchestrator.BuildReport(projectDir)
			report["next_step"] = "Review the report and address warnings."
			return report, nil
		},
	)

	log.Println("Yanxi MCP v1.1.0 (18 tools: discover/create/aiexplain/validate/wire/search/loose_search/check_imports/bootstrap/save_lang_template/module_read/memory_init/memory_write/adopt/adopt_commit/deprecate/sync/report + schema diff/strict/deep/streaming/middleware/cross-module/external-import-scan/deprecation-warning/lesson-dedup/discover-cache/granularity/conventions/http-server)")
	srv.ListenStdio()
}

func min(a, b int) int { if a < b { return a }; return b }
