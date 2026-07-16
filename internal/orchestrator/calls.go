// Package orchestrator — call graph: declares which module calls which entry.
package orchestrator

import "fmt"

// CallDecl describes one cross-module call: "module.entry".
type CallDecl struct {
	Module string `json:"module"` // target module name
	Entry  string `json:"entry"`  // target entry name
	Input  string `json:"input,omitempty"`
	Output string `json:"output,omitempty"`
}

// CallGraph maps each module to the set of calls it makes.
type CallGraph map[string][]CallDecl

// ParseCalls extracts the "calls" field from a module.json interface.
// Returns nil if no calls field exists.
func ParseCalls(iface map[string]interface{}) []CallDecl {
	if iface == nil {
		return nil
	}
	callsRaw, ok := iface["calls"].(map[string]interface{})
	if !ok || len(callsRaw) == 0 {
		return nil
	}
	var decls []CallDecl
	for modName, entriesRaw := range callsRaw {
		entries, ok := entriesRaw.(map[string]interface{})
		if !ok {
			continue
		}
		for entryName, detailRaw := range entries {
			detail, ok := detailRaw.(map[string]interface{})
			d := CallDecl{Module: modName, Entry: entryName}
			if ok {
				if v, _ := detail["input"].(string); v != "" {
					d.Input = v
				}
				if v, _ := detail["output"].(string); v != "" {
					d.Output = v
				}
			}
			decls = append(decls, d)
		}
	}
	return decls
}

// BuildCallGraph builds a call graph from all modules' calls.
func BuildCallGraph(modules []ModuleDiscoverResult) CallGraph {
	g := make(CallGraph)
	for _, m := range modules {
		if iface := m.Interface; iface != nil {
			if calls := ParseCalls(iface); len(calls) > 0 {
				g[m.Name] = calls
			}
		}
	}
	return g
}

// VerifyModuleCalls checks that every declared call targets a real module+entry.
func VerifyModuleCalls(root string, modName string, iface map[string]interface{}) []string {
	calls := ParseCalls(iface)
	if len(calls) == 0 {
		return nil
	}

	// Discover all modules to verify targets
	ov := ModuleDiscoverLazy(root)
	targetMap := make(map[string]map[string]bool) // module → set of entry names
	for _, m := range ov.Modules {
		entries := make(map[string]bool)
		if m.Interface != nil {
			if ents, ok := m.Interface["entries"].(map[string]interface{}); ok {
				for k := range ents {
					entries[k] = true
				}
			} else {
				entry, _ := m.Interface["entry"].(string)
				if entry == "" {
					entry = "handler"
				}
				entries[entry] = true
			}
		}
		targetMap[m.Name] = entries
	}

	var issues []string
	for _, c := range calls {
		entries, ok := targetMap[c.Module]
		if !ok {
			issues = append(issues, fmt.Sprintf("call target module %q does not exist", c.Module))
			continue
		}
		if !entries[c.Entry] {
			issues = append(issues, fmt.Sprintf("call %s.%s: entry not found in target module", c.Module, c.Entry))
		}
	}
	return issues
}
