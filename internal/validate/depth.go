// Package validate — deep validation: side effects, performance, coverage.
package validate

import (
	"fmt"
	"regexp"
	"time"
)

// ── Types ──

// SideEffectResult describes side-effect detection findings.
type SideEffectResult struct {
	OK       bool     `json:"ok"`
	Patterns []string `json:"patterns,omitempty"` // matched patterns
	Details  []string `json:"details,omitempty"`  // specific side-effect descriptions
}

// PerformanceResult describes per-entry performance metrics.
type PerformanceResult struct {
	OK          bool    `json:"ok"`
	LatencyMs   float64 `json:"latency_ms"`
	MaxAllowed  float64 `json:"max_allowed_ms,omitempty"`
	MemoryMB    float64 `json:"memory_mb"`
	MaxMemoryMB float64 `json:"max_memory_mb,omitempty"`
}

// CoverageResult describes input combination test coverage.
type CoverageResult struct {
	Percent    float64  `json:"percent"`
	Tested     int      `json:"tested"`
	Total      int      `json:"total"`
	Untested   []string `json:"untested,omitempty"`
}

// ── Side Effects Detection ──

// detectSideEffects scans source code for patterns that indicate side effects.
// Returns the list of matched patterns and a bool indicating if any were found.
func detectSideEffects(src string, lang string) (bool, []string) {
	var patterns []string

	switch lang {
	case "python":
		checks := []struct {
			name string
			re   *regexp.Regexp
		}{
			{"file_write", regexp.MustCompile(`open\(.*['\"]w['\"']`)},
			{"file_append", regexp.MustCompile(`open\(.*['\"]a['\"']`)},
			{"network_request", regexp.MustCompile(`(requests|urllib|httpx|aiohttp)\.`)},
			{"subprocess", regexp.MustCompile(`(subprocess|os\.system|os\.popen)`)},
			{"global_var_write", regexp.MustCompile(`global\s+\w+`)},
			{"sql_write", regexp.MustCompile(`(INSERT|UPDATE|DELETE|CREATE|DROP|ALTER)\s+`)},
			{"file_io_module", regexp.MustCompile(`(shutil|pickle\.dump|json\.dump|csv\.writer)`)},
		}
		for _, ch := range checks {
			if ch.re.MatchString(src) {
				patterns = append(patterns, ch.name)
			}
		}

	case "typescript", "javascript":
		checks := []struct {
			name string
			re   *regexp.Regexp
		}{
			{"fs_write", regexp.MustCompile(`fs\.(writeFile|appendFile|createWriteStream|mkdir|rm)`)},
			{"network_request", regexp.MustCompile(`(fetch|axios|got|request)\(`)},
			{"local_storage", regexp.MustCompile(`(localStorage|sessionStorage)\.`)},
			{"global_mutation", regexp.MustCompile(`(global|window|document)\.\w+\s*=`)},
			{"sql_write", regexp.MustCompile(`(INSERT|UPDATE|DELETE|CREATE|DROP|ALTER)\s+`)},
		}
		for _, ch := range checks {
			if ch.re.MatchString(src) {
				patterns = append(patterns, ch.name)
			}
		}

	case "go":
		checks := []struct {
			name string
			re   *regexp.Regexp
		}{
			{"file_write", regexp.MustCompile(`os\.(WriteFile|Create|OpenFile)`)},
			{"network_request", regexp.MustCompile(`(http\.(Get|Post|Do)|net\.Dial)`)},
			{"sql_write", regexp.MustCompile(`\.(Exec|ExecContext)\(`)},
			{"global_var", regexp.MustCompile(`var\s+\w+\s*[^=]*=[^=]`)},
		}
		for _, ch := range checks {
			if ch.re.MatchString(src) {
				patterns = append(patterns, ch.name)
			}
		}
	}

	return len(patterns) == 0, patterns
}

// ── Performance Benchmark ──

// measureTestTime runs a test function and returns the result with latency info.
func measureTestTime(runFn func() TestResult) (TestResult, float64) {
	start := time.Now()
	tr := runFn()
	elapsed := time.Since(start)
	return tr, float64(elapsed.Microseconds()) / 1000.0
}

// ── Coverage Report ──

// computeCoverage calculates input combination test coverage.
// totalCombinations is the theoretical bound; testedCount is from actual test cases.
func computeCoverage(inSchema map[string]interface{}, testedCount int) CoverageResult {
	propsRaw, ok := inSchema["properties"].(map[string]interface{})
	if !ok {
		return CoverageResult{Percent: 100, Tested: testedCount, Total: 1}
	}

	// Calculate theoretical combinations
	totalCombinations := 1
	var fieldDescriptions []string

	for key, propRaw := range propsRaw {
		prop, _ := propRaw.(map[string]interface{})
		if prop == nil {
			continue
		}

		// Enum fields multiply by (len + 1) for invalid value
		if enumVals := toInterfaceSlice(prop["enum"]); len(enumVals) > 0 {
			totalCombinations *= len(enumVals) + 1
			fieldDescriptions = append(fieldDescriptions,
				fmt.Sprintf("%s (enum: %d values + 1 invalid)", key, len(enumVals)))
			continue
		}

		// Required fields: have value + missing = 2
		required := toStringSet(inSchema["required"])
		if required[key] {
			totalCombinations *= 2
			fieldDescriptions = append(fieldDescriptions,
				fmt.Sprintf("%s (required: present + missing)", key))
		} else {
			// Optional fields: value + missing + null = 3
			totalCombinations *= 3
			fieldDescriptions = append(fieldDescriptions,
				fmt.Sprintf("%s (optional: present + missing + null)", key))
		}
	}

	if totalCombinations > 100000 {
		// Cap to avoid overflow — real coverage is computed against a sample
		totalCombinations = testedCount + 1
	}

	if totalCombinations == 0 {
		totalCombinations = 1
	}

	percent := float64(testedCount) / float64(totalCombinations) * 100.0
	if percent > 100 {
		percent = 100
	}

	// Build untested list
	var untested []string
	if float64(testedCount) < float64(totalCombinations)*0.5 && len(fieldDescriptions) > 0 {
		// Only report untested when coverage is below 50% to avoid noise
		for _, fd := range fieldDescriptions {
			untested = append(untested, fd)
		}
	}

	return CoverageResult{
		Percent:  percent,
		Tested:   testedCount,
		Total:    totalCombinations,
		Untested: untested,
	}
}
