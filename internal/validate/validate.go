package validate

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"yanxi-single/internal/check"
	"yanxi-single/internal/orchestrator/langtmpl"
)

type Result struct {
	Module    string       `json:"module"`
	Valid     bool         `json:"valid"`
	Errors    []string     `json:"errors"`
	Warnings  []string     `json:"warnings"`
	Tests     []TestResult `json:"tests"`
	Imports   interface{}  `json:"imports,omitempty"`
	Lifecycle *LifecycleResult `json:"lifecycle,omitempty"`
	BreakingChanges []BreakingChange `json:"breaking_changes,omitempty"`
	StrictMode      bool             `json:"strict_mode,omitempty"`
	SideEffects     *SideEffectResult    `json:"side_effects,omitempty"`
	Benchmarks      []PerformanceResult  `json:"benchmarks,omitempty"`
	Coverage        *CoverageResult      `json:"coverage,omitempty"`
	ErrorDecls      []string             `json:"error_declarations,omitempty"`
	StreamingEntries []string             `json:"streaming_entries,omitempty"`
	ImportScan      *check.ImportScanResult `json:"import_scan,omitempty"`
	CallIssues      []string             `json:"call_issues,omitempty"`
	DeprecatedDeps  []string             `json:"deprecated_deps,omitempty"`
	MiddlewareIssues []string            `json:"middleware_issues,omitempty"`
	TransportIssues []string             `json:"transport_issues,omitempty"`
	ConventionIssues []string            `json:"convention_issues,omitempty"`
}

type LifecycleResult struct {
	Setup    *TestResult `json:"setup,omitempty"`
	Teardown *TestResult `json:"teardown,omitempty"`
	Health   *TestResult `json:"health,omitempty"`
}

type TestResult struct {
	Input    map[string]interface{} `json:"input"`
	Expected string                 `json:"expected"`
	Actual   string                 `json:"actual"`
	Passed   bool                   `json:"passed"`
	Error    string                 `json:"error,omitempty"`
	Note     string                 `json:"note,omitempty"`
}

func ValidateModule(root, modName string) Result {
	r := Result{Module: modName, Valid: true}
	modJSONPath := filepath.Join(root, "source", "modules", modName, "module.json")
	modData, err := os.ReadFile(modJSONPath)
	if err != nil { r.Valid = false; r.Errors = append(r.Errors, "module.json not found"); return r }
	var contract map[string]interface{}
	if json.Unmarshal(modData, &contract) != nil { r.Valid = false; r.Errors = append(r.Errors, "invalid JSON"); return r }
	for _, f := range []string{"name", "version", "status", "interface"} {
		if _, ok := contract[f]; !ok { r.Valid = false; r.Errors = append(r.Errors, "Missing '"+f+"'") }
	}

	// Check if this is a group module
	if layer, _ := contract["layer"].(string); layer == "group" {
		children, _ := contract["children"].([]interface{})
		if len(children) == 0 {
			r.Valid = false
			r.Errors = append(r.Errors, "group module has no children")
			return r
		}
		// Recursively validate all children
		for _, childRaw := range children {
			childName, _ := childRaw.(string)
			if childName == "" { continue }
			childResult := ValidateModule(root, childName)
			if !childResult.Valid {
				r.Valid = false
				r.Errors = append(r.Errors, fmt.Sprintf("child '%s' failed: %v", childName, childResult.Errors))
			}
		}
		return r
	}

	ifaceRaw, ok := contract["interface"].(map[string]interface{})
	if !ok { r.Valid = false; r.Errors = append(r.Errors, "interface missing"); return r }

	// Detect multi-entry vs single-entry format
	var entryList []string
	if entriesRaw, hasEntries := ifaceRaw["entries"].(map[string]interface{}); hasEntries && len(entriesRaw) > 0 {
		for k := range entriesRaw {
			entryList = append(entryList, k)
		}
	} else {
		entry, _ := ifaceRaw["entry"].(string)
		if entry == "" { entry = "handler" }
		entryList = []string{entry}
	}

	lang, _ := contract["language"].(string)
	if lang == "" { lang = "python" }
	extMap := map[string]string{"python": "py", "typescript": "ts", "javascript": "js", "go": "go"}
	ext := extMap[lang]
	if ext == "" { ext = "py" }
	implPath := filepath.Join(root, "source", "modules", modName, modName+"."+ext)
	if _, err := os.Stat(implPath); os.IsNotExist(err) { r.Valid = false; r.Errors = append(r.Errors, "impl not found: "+implPath); return r }
	implData, err := os.ReadFile(implPath)
	if err != nil { r.Valid = false; r.Errors = append(r.Errors, "cannot read "+implPath); return r }

	src := string(implData)

	// ── Lifecycle hooks (v5.2.0) ──
	var lcSetup, lcTeardown, lcHealth string
	if lcRaw, ok := contract["lifecycle"].(map[string]interface{}); ok {
		if s, ok := lcRaw["setup"].(string); ok { lcSetup = s }
		if t, ok := lcRaw["teardown"].(string); ok { lcTeardown = t }
		if h, ok := lcRaw["health"].(string); ok { lcHealth = h }
	}
	// Resolve language template for validate patterns
	langTmpl, tmplErr := langtmpl.Resolve(root, lang)

	for _, fn := range []string{lcSetup, lcTeardown, lcHealth} {
		if fn == "" { continue }
		pat := `(?:def|func|function)\s+` + regexp.QuoteMeta(fn) + `\s*\(`
		if tmplErr == nil {
			pat = langTmpl.LifecycleRegex(fn)
		}
		fnRe := regexp.MustCompile(pat)
		if !fnRe.MatchString(src) {
			r.Valid = false
			r.Errors = append(r.Errors, "Lifecycle function '"+fn+"' not found in source")
		}
	}
	var setupTr *TestResult
	if lcSetup != "" && r.Valid {
		tr := runLifecycleTest(root, modName, lcSetup, lang)
		setupTr = &tr
		if !tr.Passed { r.Valid = false; r.Errors = append(r.Errors, "setup failed: "+tr.Error) }
	}
	r.Lifecycle = &LifecycleResult{Setup: setupTr}

	// ── State Management: requires validation (v1.0.0) ──
	if requiresRaw, ok := contract["requires"].(map[string]interface{}); ok && len(requiresRaw) > 0 {
		if lcSetup == "" {
			r.Errors = append(r.Errors, "Module declares 'requires' but no 'lifecycle.setup' function")
			r.Valid = false
		} else {
			// Check each required resource has setup declaring it
			for resName := range requiresRaw {
				// For now: check that setup function name contains the resource name
				// Future: could parse setup() return type
				resRe := regexp.MustCompile(`(?i)` + regexp.QuoteMeta(resName))
				if !resRe.MatchString(src) {
					r.Warnings = append(r.Warnings,
						fmt.Sprintf("Required resource %q declared but not found in setup function", resName))
				}
			}
		}
	}

	for _, entry := range entryList {
		pat := `def\s+` + regexp.QuoteMeta(entry) + `\s*\(`
		if tmplErr == nil {
			pat = langTmpl.EntryRegex(entry)
		}
		handlerRe := regexp.MustCompile(pat)
		if !handlerRe.MatchString(src) { r.Valid = false; r.Errors = append(r.Errors, "Entry '"+entry+"' not found") }
	}

	// ── Entry Auto-Sync (v1.0.0) ──
	// Scan source for exportable functions not declared in entries, auto-add them
	exportPat := `(?m)^(?:def|func|function)\s+(\w+)`
	if tmplErr == nil && langTmpl.Validate.ExportFuncRegex != "" {
		exportPat = langTmpl.ExportFuncRegex()
	}
	exportRe := regexp.MustCompile(exportPat)
	declaredSet := make(map[string]bool)
	for _, e := range entryList {
		declaredSet[e] = true
	}
	var newEntries []string
	for _, m := range exportRe.FindAllStringSubmatch(src, -1) {
		if len(m) > 1 {
			fn := m[1]
			// Skip private/internal and common Go/Python specials
			if strings.HasPrefix(fn, "_") || fn == "init" || fn == "main" || fn == "Handler" {
				continue
			}
			if !declaredSet[fn] {
				newEntries = append(newEntries, fn)
				declaredSet[fn] = true
			}
		}
	}
	if len(newEntries) > 0 {
		// Auto-write into module.json
		if ifaceRaw != nil {
			autoSyncEntries(root, modName, ifaceRaw, newEntries, lang)
		}
		r.Warnings = append(r.Warnings, fmt.Sprintf("Auto-synced %d entries from source: %v", len(newEntries), newEntries))
	}

	// ── Module Granularity Checks (v1.0.0) ──
	if len(entryList) > 7 {
		r.Warnings = append(r.Warnings,
			fmt.Sprintf("Module %q has %d entries (>7), consider splitting into smaller modules", modName, len(entryList)))
	}
	vagueNames := map[string]bool{"utils": true, "stuff": true, "common": true, "helpers": true, "shared": true}
	if vagueNames[modName] {
		r.Warnings = append(r.Warnings,
			fmt.Sprintf("Module name %q is vague. Use a domain-specific name like 'auth' or 'storage' instead.", modName))
	}

	// ── Error declarations (v5.3.0) ──
	if errDecls, ok := contract["errors"].(map[string]interface{}); ok {
		for errCode := range errDecls {
			r.ErrorDecls = append(r.ErrorDecls, errCode)
		}
	}

	if !r.Valid { return r } // stop early if handlers missing

	deps, _ := contract["dependencies"].([]interface{})
	for _, d := range deps {
		depName, ok := d.(string)
		if !ok { continue }
		if _, err := os.Stat(filepath.Join(root, "source", "modules", depName, "module.json")); os.IsNotExist(err) {
			r.Valid = false; r.Errors = append(r.Errors, "Dependency '"+depName+"' not found")
		}
	}

	importResult := checkImports(root, modName)
	if !importResult["ok"].(bool) { r.Valid = false; r.Errors = append(r.Errors, fmt.Sprintf("Import mismatch: %s", importResult["error"])) }
	r.Imports = importResult

	// ── Comprehensive Import Scan (v5.4.0) ──
	langScan := lang
	if langScan == "" { langScan = "python" }
	scanResult := check.ScanImports(root, modName, langScan, implData)
	// Add warnings for local (non-module) imports that could be adopted
	for _, loc := range scanResult.Local {
		r.Warnings = append(r.Warnings, fmt.Sprintf("Local import %q — consider module_adopt(\"%s\") to bring under yanxi management", loc.Raw, loc.Package))
	}
	// Track unknown imports as warnings
	for _, unk := range scanResult.Unknown {
		r.Warnings = append(r.Warnings, fmt.Sprintf("Unknown import %q — may need manual review", unk.Raw))
	}
	// Embed full scan in structured result (already JSON-safe via ImportScanResult type)
	if scanResult != nil {
		r.ImportScan = scanResult
	}

	// ── Cross-Module Checks (v5.4.0) ──

	// 1. Calls: every declared call target must exist (upstream check)
	r.CallIssues = checkModuleCalls(root, modName, contract)

	// 1b. Calls auto-infer: scan source for cross-module call patterns not yet declared
	inferredCalls := inferModuleCalls(src, modName, lang, root)
	if len(inferredCalls) > 0 {
		autoSyncCalls(root, modName, contract, inferredCalls)
		r.Warnings = append(r.Warnings, fmt.Sprintf("Auto-synced %d calls from source", len(inferredCalls)))
	}

	// 2. Middleware: every declared middleware function must exist as an entry in target module
	r.MiddlewareIssues = checkMiddlewareTargets(root, modName, contract)

	// 3. Deprecated dependencies: warn if any upstream module is deprecated
	r.DeprecatedDeps = checkDeprecatedDeps(root, modName, contract)

	// 4. Transport validation: check HTTP route declarations
	r.TransportIssues = checkTransportHTTP(root, modName, contract)

	// ── Schema Diff (v5.3.0) ──
	breakingChanges := CompareModuleSchemas(root, modName, contract)
	if len(breakingChanges) > 0 {
		r.BreakingChanges = breakingChanges
		hasBreaking := false
		for _, bc := range breakingChanges {
			for _, ch := range bc.Changes {
				if !ch.Compatible {
					hasBreaking = true
					break
				}
			}
		}
		if hasBreaking {
			r.Errors = append(r.Errors, "Breaking schema changes detected — see breaking_changes")
			r.Valid = false
		}
	}
	saveSchemaCache(root, modName, contract)

	// ── Version Auto-Bump (v1.0.0) ──
	if hasBreakingVersion(r.BreakingChanges) {
		// Re-read to preserve entry/calls auto-sync changes
		vdata, verr := os.ReadFile(modJSONPath)
		if verr == nil {
			var vcontract map[string]interface{}
			if json.Unmarshal(vdata, &vcontract) == nil {
				bumpVersion(vcontract, "major")
				if nd, e := json.MarshalIndent(vcontract, "", "  "); e == nil {
					os.WriteFile(modJSONPath, nd, 0644)
				}
			}
		}
		r.Warnings = append(r.Warnings, "Version auto-bumped to major (breaking changes)")
	}

	// ── Downstream Compatibility (v5.4.0) ──
	// When schema changed, check if dependent modules' calls still target valid entries
	if len(r.BreakingChanges) > 0 {
		downstreamIssues := checkDownstreamCompatibility(root, modName, contract)
		if len(downstreamIssues) > 0 {
			r.Warnings = append(r.Warnings, "Downstream compatibility: "+strings.Join(downstreamIssues, "; "))
		}
	}

	// ── Side Effects Detection (v5.3.0) ──
	if constraints, ok := contract["constraints"].(map[string]interface{}); ok {
		if noSideEffects, _ := constraints["side_effects"].(bool); !noSideEffects {
			// side_effects is false or missing — check for them
			ok, patterns := detectSideEffects(src, lang)
			r.SideEffects = &SideEffectResult{OK: ok, Patterns: patterns}
			if !ok {
				r.Warnings = append(r.Warnings, fmt.Sprintf("Side effects detected: %v. If intentional, set constraints.side_effects=true", patterns))
			}
		}
	}

	// ── Performance constraints (v5.3.0) ──
	maxLatencyMs := float64(0)
	if constraints, ok := contract["constraints"].(map[string]interface{}); ok {
		if ml, _ := constraints["max_latency_ms"].(float64); ml > 0 { maxLatencyMs = ml }
		// max_memory_mb parsed but measurement not yet implemented
		_ = constraints["max_memory_mb"]
	}

	// ── Strict mode check (v5.3.0) ──
	hasStrict := false
	for _, entry := range entryList {
		if entriesRaw, hasE := ifaceRaw["entries"].(map[string]interface{}); hasE {
			if entryDef, ok := entriesRaw[entry].(map[string]interface{}); ok {
				if strict, _ := entryDef["strict"].(bool); strict {
					hasStrict = true
					break
				}
			}
		}
	}
	if hasStrict {
		r.StrictMode = true
	}

	// ── Streaming detection (v5.3.0) ──
	for _, entry := range entryList {
		if entriesRaw, hasE := ifaceRaw["entries"].(map[string]interface{}); hasE {
			if entryDef, ok := entriesRaw[entry].(map[string]interface{}); ok {
				if streaming, _ := entryDef["streaming"].(bool); streaming {
					r.StreamingEntries = append(r.StreamingEntries, entry)
					// Check for yield/generator pattern in source
					yieldRe := regexp.MustCompile(`(yield|yield from|generator|async\s+function\s*\*|function\s*\*|\bgen\b|\bstream\b)`)
					if !yieldRe.MatchString(src) {
						r.Warnings = append(r.Warnings, fmt.Sprintf("Entry '%s' declared streaming but no yield/generator found", entry))
					}
				}
			}
		}
	}

	// Run tests for each entry
	for _, entry := range entryList {
		var inputSchema, outputSchema map[string]interface{}
		if entriesRaw, hasE := ifaceRaw["entries"].(map[string]interface{}); hasE {
			if entryDef, ok := entriesRaw[entry].(map[string]interface{}); ok {
				inputSchema, _ = entryDef["input_schema"].(map[string]interface{})
				outputSchema, _ = entryDef["output_schema"].(map[string]interface{})
			}
		} else {
			inputSchema, _ = ifaceRaw["input_schema"].(map[string]interface{})
			outputSchema, _ = ifaceRaw["output_schema"].(map[string]interface{})
		}
		if inputSchema == nil || outputSchema == nil {
			r.Warnings = append(r.Warnings, "No schema for entry '"+entry+"'")
			continue
		}
		// Check per-entry strict mode
		entryStrict := false
		if entriesRaw, hasE := ifaceRaw["entries"].(map[string]interface{}); hasE {
			if entryDef, ok := entriesRaw[entry].(map[string]interface{}); ok {
				if strict, _ := entryDef["strict"].(bool); strict {
					entryStrict = true
				}
			}
		}

		for _, tc := range generateTestCases(inputSchema, outputSchema, entry) {
			var tr TestResult
			var latencyMs float64
			switch lang {
			case "python":
				tr, latencyMs = measureTestTime(func() TestResult {
					return runPyTest(root, modName, entry, tc.Input, tc.Expected)
				})
			case "typescript", "javascript":
				tr, latencyMs = measureTestTime(func() TestResult {
					return runTSTest(root, modName, entry, tc.Input, tc.Expected, lang)
				})
			case "go":
				tr, latencyMs = measureTestTime(func() TestResult {
					return runGoTest(root, modName, entry, tc.Input, tc.Expected)
				})
			default: continue
			}
			// Latency benchmark check
			if maxLatencyMs > 0 && tr.Passed && latencyMs > maxLatencyMs {
				tr.Passed = false
				tr.Error = fmt.Sprintf("latency %.1fms exceeds max %.0fms", latencyMs, maxLatencyMs)
				r.Warnings = append(r.Warnings, fmt.Sprintf("Entry '%s': latency %.1fms > %.0fms max", entry, latencyMs, maxLatencyMs))
			}
			// Benchmark tracking
			if tr.Passed {
				r.Benchmarks = append(r.Benchmarks, PerformanceResult{
					OK: latencyMs <= maxLatencyMs || maxLatencyMs == 0,
					LatencyMs: latencyMs,
					MaxAllowed: maxLatencyMs,
				})
			}
			// Strict mode: validate input before test
			if entryStrict && tr.Passed && len(tc.Input) > 0 {
				if inErrs := ValidateStrict(inputSchema, tc.Input, entry+".input"); len(inErrs) > 0 {
					tr.Passed = false
					tr.Error = "strict input: " + strings.Join(inErrs, "; ")
				}
			}
			// Strict mode: validate output against output_schema
			if entryStrict && tr.Passed && tr.Actual != "" {
				var actualMap map[string]interface{}
				if json.Unmarshal([]byte(tr.Actual), &actualMap) == nil {
					if resultVal, ok := actualMap["result"]; ok {
						if resultMap, ok := resultVal.(map[string]interface{}); ok {
							if outErrs := ValidateStrict(outputSchema, resultMap, entry+".output"); len(outErrs) > 0 {
								tr.Passed = false
								tr.Error = "strict output: " + strings.Join(outErrs, "; ")
							}
						}
					}
				}
			}
			r.Tests = append(r.Tests, tr)
			if !tr.Passed { r.Valid = false }
		}

		// Coverage report per entry
		if inputSchema != nil && len(r.Tests) > 0 {
			testedCount := 0
			for _, t := range r.Tests { if t.Passed { testedCount++ } }
			cov := computeCoverage(inputSchema, len(r.Tests))
			cov.Tested = testedCount
			r.Coverage = &cov
		}
	}
	// Run teardown (always, even after failures)
	if lcTeardown != "" {
		tr := runLifecycleTest(root, modName, lcTeardown, lang)
		r.Lifecycle.Teardown = &tr
		if !tr.Passed { r.Valid = false; r.Errors = append(r.Errors, "teardown failed: "+tr.Error) }
	}
	// Run health check
	if lcHealth != "" {
		tr := runLifecycleTest(root, modName, lcHealth, lang)
		r.Lifecycle.Health = &tr
		if !tr.Passed { r.Warnings = append(r.Warnings, "health check failed: "+tr.Error) }
	}

	// Cross-module issues → warnings and invalid
	if len(r.CallIssues) > 0 {
		r.Warnings = append(r.Warnings, "Call issues: "+strings.Join(r.CallIssues, "; "))
	}
	if len(r.MiddlewareIssues) > 0 {
		r.Warnings = append(r.Warnings, "Middleware issues: "+strings.Join(r.MiddlewareIssues, "; "))
	}
	if len(r.DeprecatedDeps) > 0 {
		r.Warnings = append(r.Warnings, "Deprecated dependencies: "+strings.Join(r.DeprecatedDeps, "; "))
	}
	if len(r.TransportIssues) > 0 {
		r.Warnings = append(r.Warnings, "Transport issues: "+strings.Join(r.TransportIssues, "; "))
	}

	// ── Convention Checks (v1.0.0) ──
	r.ConventionIssues = checkConventions(root, modName, src, lang, entryList)
	if len(r.ConventionIssues) > 0 {
		r.Warnings = append(r.Warnings, "Convention issues: "+strings.Join(r.ConventionIssues, "; "))
	}

	return r
}

func checkTransportHTTP(root, modName string, contract map[string]interface{}) []string {
	transportRaw, ok := contract["transport"].(map[string]interface{})
	if !ok {
		return nil
	}
	httpRaw, ok := transportRaw["http"].(map[string]interface{})
	if !ok {
		return nil
	}
	routesRaw, ok := httpRaw["routes"].(map[string]interface{})
	if !ok || len(routesRaw) == 0 {
		return []string{"transport.http.routes is empty"}
	}

	// Load interface entries for entry name validation
	ifaceRaw, _ := contract["interface"].(map[string]interface{})
	entrySet := map[string]bool{}
	if ifaceRaw != nil {
		if ents, hasE := ifaceRaw["entries"].(map[string]interface{}); hasE {
			for k := range ents {
				entrySet[k] = true
			}
		} else if entry, _ := ifaceRaw["entry"].(string); entry != "" {
			entrySet[entry] = true
		} else {
			entrySet["handler"] = true
		}
	}

	var issues []string
	validMethods := map[string]bool{"GET": true, "POST": true, "PUT": true, "DELETE": true, "PATCH": true, "HEAD": true, "OPTIONS": true}
	for entryName, routeRaw := range routesRaw {
		route, _ := routeRaw.(map[string]interface{})
		if route == nil {
			issues = append(issues, fmt.Sprintf("transport.http route %q: invalid route definition", entryName))
			continue
		}
		// Check entry exists
		if len(entrySet) > 0 && !entrySet[entryName] {
			issues = append(issues, fmt.Sprintf("transport.http route %q: entry not declared in interface", entryName))
		}
		// Check method
		if method, _ := route["method"].(string); method != "" {
			if !validMethods[method] {
				issues = append(issues, fmt.Sprintf("transport.http route %q: invalid method %q", entryName, method))
			}
		}
		// Check path
		if path, _ := route["path"].(string); path != "" {
			if path[0] != '/' {
				issues = append(issues, fmt.Sprintf("transport.http route %q: path must start with /", entryName))
			}
		} else {
			issues = append(issues, fmt.Sprintf("transport.http route %q: missing path", entryName))
		}
	}
	return issues
}

func checkModuleCalls(root, modName string, contract map[string]interface{}) []string {
	ifaceRaw, _ := contract["interface"].(map[string]interface{})
	if ifaceRaw == nil { return nil }
	callsRaw, ok := ifaceRaw["calls"].(map[string]interface{})
	if !ok || len(callsRaw) == 0 { return nil }

	var issues []string
	for targetModule, entriesRaw := range callsRaw {
		entries, ok := entriesRaw.(map[string]interface{})
		if !ok { continue }
		// Check target module exists
		targetJSON := filepath.Join(root, "source", "modules", targetModule, "module.json")
		targetData, err := os.ReadFile(targetJSON)
		if err != nil {
			issues = append(issues, fmt.Sprintf("call target module %q does not exist", targetModule))
			continue
		}
		var targetContract map[string]interface{}
		if json.Unmarshal(targetData, &targetContract) != nil { continue }

		// Build target entry set
		targetEntries := map[string]bool{}
		if tIface, _ := targetContract["interface"].(map[string]interface{}); tIface != nil {
			if ents, hasE := tIface["entries"].(map[string]interface{}); hasE {
				for k := range ents { targetEntries[k] = true }
			} else if entry, _ := tIface["entry"].(string); entry != "" {
				targetEntries[entry] = true
			} else {
				targetEntries["handler"] = true
			}
		}
		for entryName := range entries {
			if !targetEntries[entryName] {
				issues = append(issues, fmt.Sprintf("call %s.%s: entry not found in target module", targetModule, entryName))
			}
		}
	}
	return issues
}

func checkMiddlewareTargets(root, modName string, contract map[string]interface{}) []string {
	ifaceRaw, _ := contract["interface"].(map[string]interface{})
	if ifaceRaw == nil { return nil }
	mwRaw, ok := ifaceRaw["middleware"].(map[string]interface{})
	if !ok { return nil }

	var issues []string
	for _, dir := range []string{"before", "after"} {
		listRaw, ok := mwRaw[dir].([]interface{})
		if !ok { continue }
		for _, item := range listRaw {
			ref, _ := item.(string)
			if ref == "" { continue }
			parts := strings.SplitN(ref, ".", 2)
			if len(parts) != 2 {
				issues = append(issues, fmt.Sprintf("invalid middleware ref %q (expected module.entry)", ref))
				continue
			}
			tgtModule, tgtEntry := parts[0], parts[1]
			targetJSON := filepath.Join(root, "source", "modules", tgtModule, "module.json")
			targetData, err := os.ReadFile(targetJSON)
			if err != nil {
				issues = append(issues, fmt.Sprintf("middleware target module %q does not exist", tgtModule))
				continue
			}
			var tgtContract map[string]interface{}
			if json.Unmarshal(targetData, &tgtContract) != nil { continue }
			found := false
			if tIface, _ := tgtContract["interface"].(map[string]interface{}); tIface != nil {
				if ents, hasE := tIface["entries"].(map[string]interface{}); hasE {
					_, found = ents[tgtEntry]
				} else {
					e, _ := tIface["entry"].(string)
					found = (e == tgtEntry) || (e == "" && tgtEntry == "handler")
				}
			}
			if !found {
				issues = append(issues, fmt.Sprintf("middleware %s.%s: entry not found in target module", tgtModule, tgtEntry))
			}
		}
	}
	return issues
}

func checkDeprecatedDeps(root, modName string, contract map[string]interface{}) []string {
	depsRaw, _ := contract["dependencies"].([]interface{})
	if len(depsRaw) == 0 { return nil }
	var deprecated []string
	for _, d := range depsRaw {
		depName, ok := d.(string)
		if !ok { continue }
		targetJSON := filepath.Join(root, "source", "modules", depName, "module.json")
		targetData, err := os.ReadFile(targetJSON)
		if err != nil { continue }
		var tgtContract map[string]interface{}
		if json.Unmarshal(targetData, &tgtContract) != nil { continue }
		if status, _ := tgtContract["status"].(string); status == "deprecated" || status == "archived" {
			deprecated = append(deprecated, fmt.Sprintf("%s (status: %s)", depName, status))
		}
	}
	return deprecated
}

func checkDownstreamCompatibility(root, changedModule string, contract map[string]interface{}) []string {
	// Build current module's entry set (new interface)
	ifaceRaw, _ := contract["interface"].(map[string]interface{})
	if ifaceRaw == nil { return nil }
	newEntries := map[string]bool{}
	if ents, hasE := ifaceRaw["entries"].(map[string]interface{}); hasE {
		for k := range ents { newEntries[k] = true }
	} else if entry, _ := ifaceRaw["entry"].(string); entry != "" {
		newEntries[entry] = true
	} else {
		newEntries["handler"] = true
	}

	// Scan all modules for calls targeting this module
	var issues []string
	modDir := filepath.Join(root, "source", "modules")
	dirs, err := os.ReadDir(modDir)
	if err != nil { return nil }
	for _, d := range dirs {
		if !d.IsDir() || d.Name() == changedModule { continue }
		otherJSON := filepath.Join(modDir, d.Name(), "module.json")
		data, err := os.ReadFile(otherJSON)
		if err != nil { continue }
		var other map[string]interface{}
		if json.Unmarshal(data, &other) != nil { continue }
		otherIface, _ := other["interface"].(map[string]interface{})
		if otherIface == nil { continue }
		callsRaw, _ := otherIface["calls"].(map[string]interface{})
		if callsRaw == nil { continue }
		for tgtMod, entriesRaw := range callsRaw {
			if tgtMod != changedModule { continue }
			entries, _ := entriesRaw.(map[string]interface{})
			for entryName := range entries {
				if !newEntries[entryName] {
					issues = append(issues, fmt.Sprintf("%s calls %s.%s which no longer exists", d.Name(), changedModule, entryName))
				}
			}
		}
	}
	return issues
}

// ConventionRule describes a single project convention rule.
type ConventionRule struct {
	ID          string `json:"id"`
	Rule        string `json:"rule"`
	Check       string `json:"check"`       // "regex" | "module_name" | "entry_count"
	Pattern     string `json:"pattern,omitempty"`
	Forbidden   []string `json:"forbidden,omitempty"`
	MaxEntries  int    `json:"max_entries,omitempty"`
	Severity    string `json:"severity"`     // "warning" | "error"
}

func checkConventions(root, modName, src, lang string, entryList []string) []string {
	data, err := os.ReadFile(filepath.Join(root, "project-memory", "conventions.json"))
	if err != nil {
		return nil
	}
	var rules []ConventionRule
	if json.Unmarshal(data, &rules) != nil {
		return nil
	}
	var issues []string
	for _, rule := range rules {
		switch rule.Check {
		case "regex":
			if rule.Pattern == "" {
				continue
			}
			matched, _ := regexp.MatchString(rule.Pattern, src)
			if !matched {
				issues = append(issues, rule.Rule)
			}
		case "module_name":
			for _, f := range rule.Forbidden {
				if modName == f {
					issues = append(issues, rule.Rule)
				}
			}
		case "entry_count":
			if rule.MaxEntries > 0 && len(entryList) > rule.MaxEntries {
				issues = append(issues, rule.Rule)
			}
		}
	}
	return issues
}

func autoSyncEntries(root, modName string, ifaceRaw map[string]interface{}, newEntries []string, lang string) {
	modJSONPath := filepath.Join(root, "source", "modules", modName, "module.json")
	data, err := os.ReadFile(modJSONPath)
	if err != nil {
		return
	}
	var contract map[string]interface{}
	if json.Unmarshal(data, &contract) != nil {
		return
	}
	ifaceRaw2, _ := contract["interface"].(map[string]interface{})
	if ifaceRaw2 == nil {
		return
	}

	// Check if module uses multi-entry format
	if _, hasEntries := ifaceRaw2["entries"].(map[string]interface{}); hasEntries {
		// Multi-entry: add each new entry
		entriesMap, _ := ifaceRaw2["entries"].(map[string]interface{})
		if entriesMap == nil {
			entriesMap = make(map[string]interface{})
			ifaceRaw2["entries"] = entriesMap
		}
		for _, fn := range newEntries {
			entryHn := fn
			if lang == "go" && len(fn) > 0 {
				entryHn = strings.ToUpper(fn[:1]) + fn[1:]
			}
			entriesMap[fn] = map[string]interface{}{
				"description": fn + " entry (auto-synced)",
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
			_ = entryHn
		}
	} else {
		// Single entry: upgrade to multi-entry format
		oldEntry, _ := ifaceRaw2["entry"].(string)
		oldDesc, _ := ifaceRaw2["description"].(string)
		oldInput, _ := ifaceRaw2["input_schema"]
		oldOutput, _ := ifaceRaw2["output_schema"]

		entriesMap := make(map[string]interface{})
		if oldEntry != "" {
			entryDef := map[string]interface{}{"description": oldDesc}
			if oldInput != nil {
				entryDef["input_schema"] = oldInput
			}
			if oldOutput != nil {
				entryDef["output_schema"] = oldOutput
			}
			entriesMap[oldEntry] = entryDef
		}
		for _, fn := range newEntries {
			entriesMap[fn] = map[string]interface{}{
				"description": fn + " entry (auto-synced)",
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
		// Replace flat interface with entries format
		delete(ifaceRaw2, "entry")
		delete(ifaceRaw2, "description")
		delete(ifaceRaw2, "input_schema")
		delete(ifaceRaw2, "output_schema")
		ifaceRaw2["entries"] = entriesMap
		ifaceRaw2["description"] = modName + " module"
	}

	// Write back
	newData, _ := json.MarshalIndent(contract, "", "  ")
	os.WriteFile(modJSONPath, newData, 0644)
}

func checkImports(root, modName string) map[string]interface{} {
	modJSONPath := filepath.Join(root, "source", "modules", modName, "module.json")
	data, _ := os.ReadFile(modJSONPath)
	var contract map[string]interface{}
	json.Unmarshal(data, &contract)
	depsRaw, _ := contract["dependencies"].([]interface{})
	declared := map[string]bool{}
	var declList []string
	for _, d := range depsRaw { if s, ok := d.(string); ok { declared[s] = true; declList = append(declList, s) } }
	lang, _ := contract["language"].(string)
	if lang == "" { lang = "python" }
	extMap := map[string]string{"python": "py", "typescript": "ts", "javascript": "js", "go": "go"}
	ext := extMap[lang]
	if ext == "" { ext = "py" }
	src, err := os.ReadFile(filepath.Join(root, "source", "modules", modName, modName+"."+ext))
	if err != nil { return map[string]interface{}{"ok": false, "error": "cannot read impl"} }
	importRe := regexp.MustCompile(`from\s+source\.modules\.(\w+)\.\w+\s+import\s+\w+`)
	switch lang {
	case "go":
		importRe = regexp.MustCompile(`"(?:\w+/)?source/modules/(\w+)(?:/\w+)?[""]`)
	case "typescript", "javascript":
		importRe = regexp.MustCompile(`(?:from|require)\s*\(\s*['"].*source/modules/(\w+)/\w+['"]\s*\)`)
	}
	impSet := map[string]bool{}
	var impList []string
	for _, m := range importRe.FindAllStringSubmatch(string(src), -1) {
		if len(m) > 1 && m[1] != modName && !impSet[m[1]] { impSet[m[1]] = true; impList = append(impList, m[1]) }
	}
	var undecl, unused []string
	for _, imp := range impList { if !declared[imp] { undecl = append(undecl, imp) } }
	for _, dec := range declList { if !impSet[dec] { unused = append(unused, dec) } }
	ok := len(undecl) == 0 && len(unused) == 0
	errMsg := ""
	if len(undecl) > 0 { errMsg = fmt.Sprintf("undeclared: %v", undecl) }
	if len(unused) > 0 { if errMsg != "" { errMsg += "; " }; errMsg += fmt.Sprintf("unused: %v", unused) }
	return map[string]interface{}{"ok": ok, "declared": declList, "imported": impList, "undeclared": undecl, "unused": unused, "error": errMsg}
}

func generateTestCases(inSchema, outSchema map[string]interface{}, fname string) []TestResult {
	propsRaw, ok := inSchema["properties"].(map[string]interface{})
	if !ok { return nil }
	required := map[string]bool{}
	if reqList, ok := inSchema["required"].([]interface{}); ok {
		for _, r := range reqList { if s, ok := r.(string); ok { required[s] = true } }
	}
	var tests []TestResult
	input := make(map[string]interface{})
	for key, propRaw := range propsRaw {
		prop, _ := propRaw.(map[string]interface{})
		if enumVals, ok := prop["enum"].([]interface{}); ok && len(enumVals) > 0 {
			input[key] = enumVals[0]
		} else {
			switch prop["type"].(string) {
			case "string": input[key] = "test"
			case "number": input[key] = 42.0
			case "integer": input[key] = 42
			case "boolean": input[key] = true
			default: input[key] = "test"
			}
		}
	}
	if len(input) > 0 { tests = append(tests, TestResult{Input: input, Expected: detExp(outSchema)}) }
	for key, propRaw := range propsRaw {
		prop, _ := propRaw.(map[string]interface{})
		if enumVals, ok := prop["enum"].([]interface{}); ok && len(enumVals) > 1 {
			for _, val := range enumVals {
				tc := make(map[string]interface{})
				for k, v := range input { tc[k] = v }
				tc[key] = val
				tests = append(tests, TestResult{Input: tc, Expected: detExp(outSchema)})
			}
			inv := make(map[string]interface{})
			for k, v := range input { inv[k] = v }
			inv[key] = "INVALID"
			tests = append(tests, TestResult{Input: inv, Expected: "error"})
		}
	}
	for key := range required {
		mtc := make(map[string]interface{})
		for k, v := range input { mtc[k] = v }
		delete(mtc, key)
		tests = append(tests, TestResult{Input: mtc, Expected: "error"})
	}
	if len(required) == 0 { tests = append(tests, TestResult{Input: map[string]interface{}{}, Expected: detExp(outSchema)}) }
	return tests
}

func detExp(outSchema map[string]interface{}) string {
	props, _ := outSchema["properties"].(map[string]interface{})
	if props == nil { return "non-null object" }
	parts := []string{}
	for name, prop := range props {
		p, _ := prop.(map[string]interface{})
		parts = append(parts, fmt.Sprintf("%s: %s", name, p["type"]))
	}
	return fmt.Sprintf("{%s}", strings.Join(parts, ", "))
}

// ── Multi-language test execution ──

// runLifecycleTest runs a lifecycle function (setup/teardown/health) with no input.
func runLifecycleTest(root, modName, fname, lang string) TestResult {
	tr := TestResult{Input: map[string]interface{}{}, Expected: "ok"}
	var script string
	switch lang {
	case "python":
		script = fmt.Sprintf(`
import sys, json
sys.path.insert(0, %q)
sys.path.insert(0, %q)
import %s
r = %s.%s()
print(json.dumps(r))
`, filepath.Join(root, "source", "modules", modName), root, modName, modName, fname)
		return runCmd("python", []string{"-c", script}, tr)
	case "typescript", "javascript":
		script = fmt.Sprintf(`const {%s} = require("%s/source/modules/%s/%s");
console.log(JSON.stringify(%s()));
`, fname, filepath.ToSlash(root), modName, modName, fname)
		rt := "node"
		if lang == "typescript" { rt = "npx"; script = "require('ts-node/register');" + script }
		return runCmd(rt, []string{"-e", script}, tr)
	case "go":
		script = fmt.Sprintf(`package main
import ("encoding/json";"fmt";_ "%s/source/modules/%s")
func main() {
    r := %s.%s()
    b, _ := json.Marshal(r)
    fmt.Println(string(b))
}
`, root, modName, modName, fname)
		tmpFile := filepath.Join(os.TempDir(), "yanxi-lc-"+modName+"-"+fname+".go")
		os.WriteFile(tmpFile, []byte(script), 0644)
		defer os.Remove(tmpFile)
		return runCmd("go", []string{"run", tmpFile}, tr)
	default:
		tr.Passed = true; tr.Note = "no runtime for " + lang
		return tr
	}
}

func runPyTest(root, modName, fname string, input map[string]interface{}, expected string) TestResult {
	tr := TestResult{Input: input, Expected: expected}
	inputJSON, _ := json.Marshal(input)
	script := fmt.Sprintf(`
import sys, json
sys.path.insert(0, %q)
sys.path.insert(0, %q)
import %s
r = %s.%s(json.loads(%q))
print(json.dumps(r))
`, filepath.Join(root, "source", "modules", modName), root, modName, modName, fname, string(inputJSON))
	return runCmd("python", []string{"-c", script}, tr)
}

func runTSTest(root, modName, fname string, input map[string]interface{}, expected, lang string) TestResult {
	tr := TestResult{Input: input, Expected: expected}
	inputJSON, _ := json.Marshal(input)
	script := fmt.Sprintf(`const {%s} = require("%s/source/modules/%s/%s");
console.log(JSON.stringify(%s(%s)));
`, fname, filepath.ToSlash(root), modName, modName, fname, string(inputJSON))
	runtime := "node"
	if lang == "typescript" { runtime = "npx"; script = "require('ts-node/register');" + script }
	return runCmd(runtime, []string{"-e", script}, tr)
}

func runGoTest(root, modName, fname string, input map[string]interface{}, expected string) TestResult {
	tr := TestResult{Input: input, Expected: expected}
	inputJSON, _ := json.Marshal(input)
	script := fmt.Sprintf(`package main
import ("encoding/json";"fmt";_ "%s/source/modules/%s")
func main() {
    var d map[string]interface{}
    json.Unmarshal([]byte(%q), &d)
    r := %s.%s(d)
    b, _ := json.Marshal(r)
    fmt.Println(string(b))
}
`, root, modName, string(inputJSON), modName, fname)
	tmpFile := filepath.Join(os.TempDir(), "yanxi-gotest-"+modName+".go")
	os.WriteFile(tmpFile, []byte(script), 0644)
	defer os.Remove(tmpFile)
	return runCmd("go", []string{"run", tmpFile}, tr)
}

func runCmd(cmd string, args []string, tr TestResult) TestResult {
	c := exec.Command(cmd, args...)
	out, err := c.CombinedOutput()
	outStr := strings.TrimSpace(string(out))
	if err != nil {
		tr.Passed = false
		tr.Error = fmt.Sprintf("%s: %v\n%s", cmd, err, outStr)
		return tr
	}
	tr.Actual = outStr
	var output map[string]interface{}
	if json.Unmarshal([]byte(outStr), &output) != nil {
		tr.Passed = false; tr.Error = "output not JSON: " + outStr
		return tr
	}
	if errObj, hasErr := output["error"]; hasErr && errObj != nil {
		if es, ok := errObj.(string); ok && es != "" { tr.Passed = false; tr.Error = "handler error: " + es; return tr }
	}
	if _, hr := output["result"]; !hr { tr.Note = "no 'result' field" }
	tr.Passed = true
	return tr
}

func hasBreakingVersion(breakingChanges []BreakingChange) bool {
	for _, bc := range breakingChanges {
		for _, ch := range bc.Changes {
			if !ch.Compatible {
				return true
			}
		}
	}
	return false
}

func bumpVersion(contract map[string]interface{}, level string) {
	v, _ := contract["version"].(string)
	if v == "" {
		contract["version"] = "0.1.0"
		return
	}
	parts := strings.SplitN(v, ".", 3)
	major, minor, patch := 0, 0, 0
	if len(parts) > 0 {
		fmt.Sscanf(parts[0], "%d", &major)
	}
	if len(parts) > 1 {
		fmt.Sscanf(parts[1], "%d", &minor)
	}
	if len(parts) > 2 {
		fmt.Sscanf(parts[2], "%d", &patch)
	}
	switch level {
	case "major":
		major++
		minor = 0
		patch = 0
	case "minor":
		minor++
		patch = 0
	default:
		patch++
	}
	contract["version"] = fmt.Sprintf("%d.%d.%d", major, minor, patch)
}

// inferModuleCalls scans source code for cross-module call patterns.
// Only includes calls targeting known yanxi modules.
func inferModuleCalls(src, currentModule, lang string, root string) []string {
	// Build known module set
	knownMods := knownModuleSet(root)
	if len(knownMods) == 0 {
		return nil
	}

	var calls []string
	seen := map[string]bool{}

	// Pattern: module_name.function_name()
	re := regexp.MustCompile(`(\w+)\.(\w+)\s*\(`)
	for _, m := range re.FindAllStringSubmatch(src, -1) {
		if len(m) < 3 {
			continue
		}
		mod, entry := m[1], m[2]
		// Only accept if mod is a known yanxi module
		if !knownMods[mod] {
			continue
		}
		// Skip self-references
		if mod == currentModule {
			continue
		}
		key := mod + "." + entry
		if !seen[key] {
			seen[key] = true
			calls = append(calls, key)
		}
	}
	return calls
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

// autoSyncCalls writes discovered calls into module.json's interface.calls.
func autoSyncCalls(root, modName string, contract map[string]interface{}, inferred []string) {
	// Re-read the file to get latest state (entries may have been auto-synced first)
	modJSONPath := filepath.Join(root, "source", "modules", modName, "module.json")
	data, err := os.ReadFile(modJSONPath)
	if err != nil {
		return
	}
	var fresh map[string]interface{}
	if json.Unmarshal(data, &fresh) != nil {
		return
	}
	ifaceRaw, _ := fresh["interface"].(map[string]interface{})
	if ifaceRaw == nil {
		return
	}

	// Read existing calls
	callsRaw, _ := ifaceRaw["calls"].(map[string]interface{})
	if callsRaw == nil {
		callsRaw = make(map[string]interface{})
		ifaceRaw["calls"] = callsRaw
	}

	for _, callStr := range inferred {
		parts := strings.SplitN(callStr, ".", 2)
		if len(parts) != 2 {
			continue
		}
		tgtModule, tgtEntry := parts[0], parts[1]

		// Check if already declared
		if existingEntries, ok := callsRaw[tgtModule].(map[string]interface{}); ok {
			if _, exists := existingEntries[tgtEntry]; exists {
				continue
			}
			existingEntries[tgtEntry] = map[string]interface{}{}
		} else {
			callsRaw[tgtModule] = map[string]interface{}{
				tgtEntry: map[string]interface{}{},
			}
		}
	}

	// Write back (use fresh to preserve entry auto-sync changes)
	if newData, err := json.MarshalIndent(fresh, "", "  "); err == nil {
		os.WriteFile(modJSONPath, newData, 0644)
	}
}
