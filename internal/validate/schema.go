// Package validate — schema diff, strict mode, and compatibility checking.
package validate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// SchemaChange describes a single change between two JSON Schemas.
type SchemaChange struct {
	Path       string      `json:"path"`
	ChangeType string      `json:"change_type"` // FIELD_ADDED, FIELD_REMOVED, TYPE_CHANGED, CONSTRAINT_CHANGED, ENUM_CHANGED, REQUIRED_CHANGED
	OldValue   interface{} `json:"old_value,omitempty"`
	NewValue   interface{} `json:"new_value,omitempty"`
	Compatible bool        `json:"compatible"`
	Message    string      `json:"message"`
}

// BreakingChange groups all schema changes for one entry side.
type BreakingChange struct {
	Module       string         `json:"module"`
	Entry        string         `json:"entry"`
	Side         string         `json:"side"` // "input" or "output"
	Changes      []SchemaChange `json:"changes"`
	AffectedMods []string       `json:"affected_modules,omitempty"`
}

// schemaCachePath returns the file path for caching a module's schema.
func schemaCacheDir(root string) string {
	return filepath.Join(root, ".yanxi", "schema_cache")
}

func schemaCachePath(root, modName string) string {
	return filepath.Join(schemaCacheDir(root), modName+".json")
}

// loadSchemaCache loads a previously cached schema for a module.
// Returns nil if no cache exists (first-time validation).
func loadSchemaCache(root, modName string) map[string]interface{} {
	data, err := os.ReadFile(schemaCachePath(root, modName))
	if err != nil {
		return nil
	}
	var cached map[string]interface{}
	if json.Unmarshal(data, &cached) != nil {
		return nil
	}
	return cached
}

// saveSchemaCache persists the current module's interface + version for future diff.
func saveSchemaCache(root, modName string, contract map[string]interface{}) {
	schema := map[string]interface{}{
		"version":   contract["version"],
		"interface": contract["interface"],
	}
	dir := schemaCacheDir(root)
	os.MkdirAll(dir, 0755)
	data, _ := json.MarshalIndent(schema, "", "  ")
	os.WriteFile(schemaCachePath(root, modName), data, 0644)
}

// CompareModuleSchemas detects schema changes between old and new contracts.
// Returns a list of breaking changes per entry.
// If no old cache exists (first validation), returns nil with no error.
func CompareModuleSchemas(root, modName string, contract map[string]interface{}) []BreakingChange {
	old := loadSchemaCache(root, modName)
	if old == nil {
		// First validation — no comparison possible
		return nil
	}

	var changes []BreakingChange

	oldIface, _ := old["interface"].(map[string]interface{})
	newIface, _ := contract["interface"].(map[string]interface{})
	if oldIface == nil || newIface == nil {
		return nil
	}

	// Collect entry names from both old and new
	oldEntries := collectEntries(oldIface)
	newEntries := collectEntries(newIface)

	// Check for deleted entries (breaking)
	for entryName := range oldEntries {
		if _, ok := newEntries[entryName]; !ok {
			changes = append(changes, BreakingChange{
				Module: modName, Entry: entryName, Side: "",
				Changes: []SchemaChange{{
					Path: "entry", ChangeType: "ENTRY_REMOVED",
					OldValue: entryName, Compatible: false,
					Message: fmt.Sprintf("entry %q has been deleted", entryName),
				}},
			})
		}
	}

	// For each entry in both old and new, diff input/output schemas
	for entryName, newEntry := range newEntries {
		oldEntry, exists := oldEntries[entryName]
		if !exists {
			changes = append(changes, BreakingChange{
				Module: modName, Entry: entryName, Side: "",
				Changes: []SchemaChange{{
					Path: "entry", ChangeType: "ENTRY_ADDED",
					NewValue: entryName, Compatible: true,
					Message: fmt.Sprintf("new entry %q added (compatible)", entryName),
				}},
			})
			continue
		}

		// Compare input_schema
		oldIn, _ := oldEntry["input_schema"].(map[string]interface{})
		newIn, _ := newEntry["input_schema"].(map[string]interface{})
		if oldIn != nil && newIn != nil {
			inChanges := compareSchemas(oldIn, newIn, "input")
			if len(inChanges) > 0 {
				hasBreaking := false
				for _, ch := range inChanges {
					if !ch.Compatible { hasBreaking = true; break }
				}
				changes = append(changes, BreakingChange{
					Module: modName, Entry: entryName, Side: "input",
					Changes: inChanges,
				})
				if !hasBreaking {
					_ = hasBreaking // mark last change's incompatible flag
				}
			}
		}

		// Compare output_schema
		oldOut, _ := oldEntry["output_schema"].(map[string]interface{})
		newOut, _ := newEntry["output_schema"].(map[string]interface{})
		if oldOut != nil && newOut != nil {
			outChanges := compareSchemas(oldOut, newOut, "output")
			if len(outChanges) > 0 {
				changes = append(changes, BreakingChange{
					Module: modName, Entry: entryName, Side: "output",
					Changes: outChanges,
				})
			}
		}
	}

	return changes
}

// collectEntries extracts the entry definitions from an interface map,
// supporting both old-style (single entry) and new-style (entries map).
func collectEntries(iface map[string]interface{}) map[string]map[string]interface{} {
	result := make(map[string]map[string]interface{})

	if entries, ok := iface["entries"].(map[string]interface{}); ok {
		for name, raw := range entries {
			if entry, ok := raw.(map[string]interface{}); ok {
				result[name] = entry
			}
		}
	} else if entry, ok := iface["entry"].(string); ok && entry != "" {
		result[entry] = map[string]interface{}{
			"input_schema":  iface["input_schema"],
			"output_schema": iface["output_schema"],
		}
	} else {
		// Default: single "handler" entry
		result["handler"] = map[string]interface{}{
			"input_schema":  iface["input_schema"],
			"output_schema": iface["output_schema"],
		}
	}

	return result
}

// compareSchemas recursively compares two JSON Schema objects.
// Returns all changes found; each change has a Compatible flag.
func compareSchemas(oldSchema, newSchema map[string]interface{}, path string) []SchemaChange {
	var changes []SchemaChange

	// 1. Compare type
	oldType, _ := oldSchema["type"].(string)
	newType, _ := newSchema["type"].(string)
	if oldType != "" && newType != "" && oldType != newType {
		changes = append(changes, SchemaChange{
			Path: path + ".type", ChangeType: "TYPE_CHANGED",
			OldValue: oldType, NewValue: newType, Compatible: false,
			Message: fmt.Sprintf("type changed from %q to %q", oldType, newType),
		})
	}

	// 2. Compare required fields
	oldRequired := toStringSet(oldSchema["required"])
	newRequired := toStringSet(newSchema["required"])

	for req := range newRequired {
		if !oldRequired[req] {
			changes = append(changes, SchemaChange{
				Path: path + ".required", ChangeType: "REQUIRED_CHANGED",
				NewValue: req, Compatible: false,
				Message: fmt.Sprintf("new required field: %q", req),
			})
		}
	}
	for req := range oldRequired {
		if !newRequired[req] {
			changes = append(changes, SchemaChange{
				Path: path + ".required", ChangeType: "REQUIRED_CHANGED",
				OldValue: req, Compatible: true,
				Message: fmt.Sprintf("required field %q became optional (compatible)", req),
			})
		}
	}

	// 3. Compare properties recursively
	oldProps, _ := oldSchema["properties"].(map[string]interface{})
	newProps, _ := newSchema["properties"].(map[string]interface{})

	if oldProps != nil && newProps != nil {
		for propName, oldPropRaw := range oldProps {
			newPropRaw, exists := newProps[propName]
			propPath := path + ".properties." + propName
			if !exists {
				changes = append(changes, SchemaChange{
					Path: propPath, ChangeType: "FIELD_REMOVED",
					OldValue: propName, Compatible: false,
					Message: fmt.Sprintf("field %q removed", propName),
				})
				continue
			}
			oldProp, _ := oldPropRaw.(map[string]interface{})
			newProp, _ := newPropRaw.(map[string]interface{})
			if oldProp != nil && newProp != nil {
				subChanges := comparePropertyConstraints(oldProp, newProp, propPath)
				changes = append(changes, subChanges...)
			}
		}

		for propName := range newProps {
			if _, exists := oldProps[propName]; !exists {
				changes = append(changes, SchemaChange{
					Path: path + ".properties." + propName, ChangeType: "FIELD_ADDED",
					NewValue: propName, Compatible: true,
					Message: fmt.Sprintf("new optional field %q (compatible)", propName),
				})
			}
		}
	} else if oldProps == nil && newProps != nil {
		changes = append(changes, SchemaChange{
			Path: path + ".properties", ChangeType: "FIELD_ADDED",
			NewValue: "properties", Compatible: true,
			Message: "new properties block added (compatible)",
		})
	} else if oldProps != nil && newProps == nil {
		changes = append(changes, SchemaChange{
			Path: path + ".properties", ChangeType: "FIELD_REMOVED",
			OldValue: "properties", Compatible: false,
			Message: "properties block removed (breaking)",
		})
	}

	return changes
}

// comparePropertyConstraints compares constraints on a single property.
func comparePropertyConstraints(oldProp, newProp map[string]interface{}, path string) []SchemaChange {
	var changes []SchemaChange

	// Type
	oldType, _ := oldProp["type"].(string)
	newType, _ := newProp["type"].(string)
	if oldType != "" && newType != "" && oldType != newType {
		changes = append(changes, SchemaChange{
			Path: path + ".type", ChangeType: "TYPE_CHANGED",
			OldValue: oldType, NewValue: newType, Compatible: false,
			Message: fmt.Sprintf("type changed from %q to %q", oldType, newType),
		})
	}

	// Enum
	oldEnum := toInterfaceSlice(oldProp["enum"])
	newEnum := toInterfaceSlice(newProp["enum"])
	if len(oldEnum) > 0 && len(newEnum) > 0 {
		oldSet := make(map[string]bool)
		for _, v := range oldEnum { oldSet[fmt.Sprintf("%v", v)] = true }
		newSet := make(map[string]bool)
		for _, v := range newEnum { newSet[fmt.Sprintf("%v", v)] = true }

		var removed []string
		for v := range oldSet {
			if !newSet[v] { removed = append(removed, v) }
		}
		if len(removed) > 0 {
			changes = append(changes, SchemaChange{
				Path: path + ".enum", ChangeType: "ENUM_CHANGED",
				OldValue: oldEnum, NewValue: newEnum, Compatible: false,
				Message: fmt.Sprintf("enum values removed: %v", removed),
			})
		}
	}

	// Constraints: compare numeric constraints
	for _, c := range []string{"minimum", "maximum", "minLength", "maxLength", "minItems", "maxItems"} {
		oldVal, oldHas := oldProp[c]
		newVal, newHas := newProp[c]
		if oldHas && newHas {
			ov, _ := toFloat64(oldVal)
			nv, _ := toFloat64(newVal)
			if ov != nv {
				// Tightening is generally compatible; loosening is not
				isTightening := false
				switch c {
				case "minimum", "minLength", "minItems":
					isTightening = nv > ov
				case "maximum", "maxLength", "maxItems":
					isTightening = nv < ov
				}
				changes = append(changes, SchemaChange{
					Path: path + "." + c, ChangeType: "CONSTRAINT_CHANGED",
					OldValue: ov, NewValue: nv, Compatible: isTightening,
					Message: fmt.Sprintf("%s changed from %v to %v (compatible: %v)", c, ov, nv, isTightening),
				})
			}
		}
	}

	// Pattern
	oldPat, oldPatHas := oldProp["pattern"].(string)
	newPat, newPatHas := newProp["pattern"].(string)
	if oldPatHas && newPatHas && oldPat != newPat {
		changes = append(changes, SchemaChange{
			Path: path + ".pattern", ChangeType: "CONSTRAINT_CHANGED",
			OldValue: oldPat, NewValue: newPat, Compatible: false,
			Message: fmt.Sprintf("pattern changed from %q to %q", oldPat, newPat),
		})
	}

	return changes
}

// ── Strict mode validation ──

// ValidateStrict checks whether actual input/output values conform to a JSON Schema.
// Returns error messages for each violation.
func ValidateStrict(schema map[string]interface{}, data map[string]interface{}, path string) []string {
	var errs []string

	propSchema, _ := schema["properties"].(map[string]interface{})
	if propSchema == nil {
		return nil
	}

	required := toStringSet(schema["required"])

	// Check required fields exist
	for req := range required {
		if _, ok := data[req]; !ok {
			errs = append(errs, fmt.Sprintf("%s: missing required field %q", path, req))
		}
	}

	// Check each data field against schema
	for key, val := range data {
		propRaw, ok := propSchema[key]
		if !ok {
			errs = append(errs, fmt.Sprintf("%s: unexpected field %q (not in schema)", path, key))
			continue
		}
		prop, _ := propRaw.(map[string]interface{})
		if prop == nil {
			continue
		}

		fieldPath := path + "." + key
		expectedType, _ := prop["type"].(string)

		// Type check
		if expectedType != "" {
			actualType := goTypeToJSONType(val)
			if actualType != expectedType && actualType != "null" {
				errs = append(errs, fmt.Sprintf("%s: expected type %q, got %q (value: %v)", fieldPath, expectedType, actualType, val))
			}
		}

		// Enum check
		if enumVals := toInterfaceSlice(prop["enum"]); len(enumVals) > 0 {
			valStr := fmt.Sprintf("%v", val)
			found := false
			for _, ev := range enumVals {
				if fmt.Sprintf("%v", ev) == valStr { found = true; break }
			}
			if !found {
				errs = append(errs, fmt.Sprintf("%s: value %v not in allowed enum %v", fieldPath, val, enumVals))
			}
		}
	}

	return errs
}

// goTypeToJSONType maps Go value types to JSON Schema types.
func goTypeToJSONType(v interface{}) string {
	switch v.(type) {
	case string:
		return "string"
	case float64:
		return "number"
	case bool:
		return "boolean"
	case map[string]interface{}:
		return "object"
	case []interface{}:
		return "array"
	case nil:
		return "null"
	default:
		return "string"
	}
}

// ── Helpers ──

func toStringSet(v interface{}) map[string]bool {
	result := make(map[string]bool)
	list, ok := v.([]interface{})
	if !ok {
		return result
	}
	for _, item := range list {
		if s, ok := item.(string); ok {
			result[s] = true
		}
	}
	return result
}

func toInterfaceSlice(v interface{}) []interface{} {
	if list, ok := v.([]interface{}); ok {
		return list
	}
	return nil
}

func toFloat64(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}
