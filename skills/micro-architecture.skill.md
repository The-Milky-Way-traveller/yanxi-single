---
name: micro-architecture
description: >
  Micro-module architecture conventions for Yanxi projects.
  Teaches agents how to discover, understand, modify, and
  document modules using the Yanxi MCP toolset.
runAs: inline
---

# Yanxi Micro-Module Architecture Guide

## What is Yanxi?

Yanxi is a set of MCP tools (this project's `yanxi-mcp.exe`) that help AI agents
navigate and maintain structured codebases. It does three things:

1. **Map** — understand the project in ~500 tokens via `module_discover()`
2. **Check** — validate modules automatically via `module_validate()`
3. **Remember** — persist lessons across sessions via `project-memory/`

### Yanxi's Boundary

Yanxi follows a strict rule:

```
Yanxi suggests → Agent decides → User confirms
```

- Yanxi detects issues and reports them as warnings
- Yanxi **never** modifies your code or contracts without explicit agent action
- When yanxi finds something (undeclared entries, mismatched imports, broken calls),
  it tells you. You decide whether to act.
- When yanxi's finding affects the user's project, **tell the user what yanxi found
  and ask for their decision**.

---

## 17 MCP Tools

| Tool | Purpose |
|------|---------|
| `module_discover` | Enter project: Level 1 (summary) + Level 2 (module digest) |
| `module_create` | Scaffold a new module skeleton |
| `module_read` | Full module details: contract + AIexplain + source preview |
| `module_validate` | 6-stage validation pipeline — see below |
| `module_wire` | Generate main entry routing code |
| `module_bootstrap` | One-shot: create + wire + sync |
| `module_search` | BM25/vector search across AIexplain + source |
| `module_search_loose` | Search any directory, no architecture required |
| `module_check_imports` | Verify declared vs actual imports |
| `module_adopt` | Analyse external code for adoption as a yanxi module |
| `module_adopt_commit` | Finalise adoption: write + delete original + wire + sync |
| `module_deprecate` | Mark module as deprecated/archived + write ADR |
| `module_sync` | Apply pending changes: sync entries, calls, version from source |
| `save_lang_template` | Save an LLM-generated language template |
| `aiexplain_generate` | Incrementally regenerate AIexplain cards + search index |
| `memory_init` | Create project-memory + config templates (idempotent) |
| `memory_write` | Write ADR/lesson/convention to project memory |

---

## Standard Workflow

### Entering a project
```
module_discover() — one call, full picture (~500 tokens)
```
Always call this first. It returns:
- Project summary + warnings
- Module list with dependency graph
- Any deprecated modules (shown with ⚠)
- Any dependent modules that will break

### Understanding a module
```
module_read("module_name") — full contract + AIexplain + source preview
```
Read the AIexplain card before touching source code. Only read source when
you are about to modify it.

### The Five-Step Modification Loop
```
① module_discover()          → understand current state
② module_read("module")      → understand the module
③ edit source code           → change handler logic
④ module_validate("module")  → check everything
⑤ module_wire() + aiexplain_generate() → sync
```

**When module_validate reports issues**:
```
validate found errors     → Agent reads errors → fixes source → re-runs validate
validate found warnings   → Agent reads warnings → decides whether to act
validate suggests changes → Agent runs module_sync() to apply them
```

### Adding a new module
```
module_create("name", language="go") → write handler → module_validate → module_wire
```

### Adopting external code
```
module_adopt("pkg/util")  → reads the prompt → LLM adapts → module_adopt_commit()
```

### Deprecating an old module
```
module_deprecate("old_mod", "deprecated", "replaced by new_mod")
→ ADR written automatically, dependents warned
```

---

## Validation (6 Stages)

`module_validate("module")` runs six stages in order:

1. **Structure** — module.json exists, required fields present, entry declared
2. **Source** — entry function exists in source, lifecycle hooks exist
3. **Cross-module** — calls target real module+entry, middleware exists,
   no deprecated dependencies, downstream compatibility
4. **Deep analysis** — import classification (known/local/3rd-party/stdlib),
   side effects, streaming patterns
5. **Runtime** — auto-generates test cases from schema, runs them,
   measures latency, checks strict mode
6. **Diff** — compares current schema against previous, detects breaking changes,
   suggests version bump

**When tests fail**: read the error output, fix the source code, re-run validate.
Tests that fail because of missing runtimes (e.g., Python not installed) are
warnings, not failures.

### What Yanxi Detects and Suggests

| Detection | Yanxi does | You do |
|-----------|-----------|--------|
| Undeclared exports in source | Warning with function list | `module_sync()` to add as entries |
| Cross-module calls in source | Warning with count | `module_sync()` to write to calls |
| Breaking schema changes | Warning | `module_sync()` to bump version |
| Import mismatch | Error with details | Fix module.json dependencies |
| Deprecated upstream | Warning | Plan migration to replacement |
| Vague module name | Warning | Rename to domain-specific name |
| Too many entries (>7) | Warning | Split into smaller modules |

---

## Project Anatomy

```
<project>/
├── .yanxi/                        ← tool state (auto-managed)
│   ├── project.json
│   ├── discover_cache.json
│   ├── schema_cache/<module>.json
│   ├── validation_state.json
│   ├── last_sync.json
│   ├── search_index.json
│   └── lang-templates/<lang>.json
├── source/
│   ├── main/main.{py|ts|go}      ← wired entry point (module_wire generates)
│   └── modules/<name>/
│       ├── <name>.{py|ts|go}     ← handler logic
│       └── module.json           ← contract + schema
├── AIexplain/                     ← agent-readable knowledge (auto-generated)
│   ├── project-architecture.md
│   ├── module-contracts.json
│   └── modules/<name>/
│       ├── <name>.md
│       └── interface.md
├── project-memory/                ← collective memory
│   ├── architecture-decisions.md
│   ├── lessons-learned.md
│   ├── conventions.md
│   └── conventions.json           ← structured rules (validated automatically)
├── INDEX.md
└── .mcp.json
```

---

## Interface Contract

Every module exposes:
```python
def handler(input: dict) -> dict:
    ...
```

Enforced by `module.json`:
```json
{
  "name": "auth",
  "version": "1.0.0",
  "status": "active",
  "language": "go",
  "dependencies": ["storage"],
  "interface": {
    "entries": {
      "login": {
        "input_schema": { "type": "object", "required": ["username", "password"], ... },
        "output_schema": { "type": "object", ... }
      }
    },
    "calls": {
      "storage": { "save_session": {} }
    }
  }
}
```

---

## Module Boundary Rules

When deciding whether to create a new module, ask:

1. **Data ownership** — does this function own data no other module owns? → new module
2. **Change isolation** — will this function change independently? → new module
3. **External dependency** — does it wrap a DB/API/filesystem? → new module
4. **Testability** — can it be validated standalone? → good candidate

**Anti-patterns**: one function per module (too fine), utils/common helpers (dumpster),
circular dependencies, names tied to implementation details.

---

## Architecture Planning

When a request involves **2+ modules**, stop and show a plan:

```
module_discover() — understand current state
Draft plan → present to user → [Approve] [Modify] [Reject]
```

Only proceed after user approval. Use sub-agents via `task()` for independent modules.

---

## Error Format

All handler responses with errors:
```json
{
  "result": null,
  "error": {
    "code": "MODULE_ERROR_TYPE",
    "message": "Human-readable",
    "retryable": true,
    "source_module": "name"
  }
}
```

---

## Project Memory

After significant actions, write to project-memory:
- `architecture-decisions.md` — architectural decisions with rationale
- `lessons-learned.md` — pitfalls + solutions (yanxi auto-writes on validate failure)
- `conventions.md` — project conventions
- `conventions.json` — structured rules validated automatically

Use `memory_write(kind="lesson", content="...")` to add entries.
Use `memory_init()` to create missing templates.

---

## Summary

```
Enter project   → module_discover()
Understand      → module_read("name")
Create          → module_create("name", language="go")
Modify          → edit → validate → sync → wire
Adopt legacy    → module_adopt → LLM → module_adopt_commit
Retire          → module_deprecate("name", "deprecated", reason)
Sync changes    → module_sync("name")
Check           → module_validate("name")
Report findings → tell the user what yanxi found, let them decide
```
