---
name: micro-architecture
description: >
  Micro-module architecture conventions for Yanxi projects.
  Teaches agents how to discover, understand, modify, and
  document modules in a source/modules/ + AIexplain/ project.
runAs: inline
---

# Micro-Module Architecture Guide

This project follows the **micro-module architecture**. Every module is an independent unit
with a strict contract, structured for agent understanding.

## Project Anatomy

```
<project>/
├── source/
│   ├── main/                    ← entry point wiring
│   │   ├── main.py              ← routes to modules
│   │   └── shared_functions.py  ← shared utilities
│   ├── modules/<name>/          ← each module lives here
│   │   ├── <name>.<ext>         ← implementation (py/ts/js/go)
│   │   └── module.json          ← interface contract
│   └── ui/                      ← (optional) UI layer
├── AIexplain/                   ← AGENT'S KNOWLEDGE LAYER
│   ├── project-architecture.md  ← global architecture overview
│   ├── module-contracts.json    ← all module contracts in one file
│   ├── shared-functions-guide.md
│   └── modules/<name>/
│       ├── <name>.md            ← skill card (purpose + interface + usage)
│       └── interface.md         ← API reference
├── project-memory/              ← PROJECT'S COLLECTIVE MEMORY
│   ├── architecture-decisions.md ← ADRs (why decisions were made)
│   ├── lessons-learned.md       ← pitfalls encountered + solutions
│   └── conventions.md           ← coding conventions for this project
├── INDEX.md                     ← human-readable registry
└── agent-config.json            ← agent model routing (optional)
```

## CRITICAL RULES

### Rule 1: AIexplain First, Source Only When Modifying

**ALWAYS read AIexplain before source code.**

When you enter a project, your first action MUST be:
```
module_discover(project_dir=".")
```

This returns all modules with their AIexplain skill cards embedded. You understand
the entire project in one call without reading individual source files.

When you need to understand a specific module:
1. Read `AIexplain/modules/<name>/<name>.md` — purpose + interface + usage
2. Read `AIexplain/modules/<name>/interface.md` — API reference
3. **Only read `source/modules/<name>/<name>.<ext>` if you are about to modify it**

### Rule 2: Create Modules the Right Way

When adding a new module, use `module_create` to scaffold the skeleton:
```
module_create(name="auth", language="python", description="JWT authentication")
```

This generates a compliant skeleton:
- `source/modules/auth/auth.py` with `handler(input: dict) -> dict` stub
- `source/modules/auth/module.json` with full interface contract

After scaffolding, fill in the handler implementation. Then sync the knowledge layer.

### Rule 3: Always Sync AIexplain After Changes

After ANY modification to module source code:
- Calling `aiexplain_generate()` regenerates all AIexplain documents
- This ensures the next agent (or you next session) reads correct information
- The skill cards are your gift to your future self

The sync is automatic — one tool call, all documents updated:
```
aiexplain_generate(project_dir=".")
```

### Rule 4: The Four-Step Modification Loop

```
① module_discover()           → understand the project (one call)
② read AIexplain/<name>.md    → understand the target module
③ edit source + module.json   → change the code, bump version
④ aiexplain_generate()        → sync the knowledge layer
```

### Rule 5: Always Write to Project Memory

After significant actions, write to `project-memory/` so the next agent benefits:

| File | When to write | Content |
|------|--------------|---------|
| `architecture-decisions.md` | After any architectural decision | ADR-NNN: What you decided, why, what alternatives you considered |
| `lessons-learned.md` | After encountering and solving a problem | Date, problem description, root cause, solution, prevention |
| `conventions.md` | When establishing project-level conventions | The convention and the reasoning behind it |

When entering a project, `module_discover()` returns these files' content in the
`project_memory` field. **Read them before anything else** — they contain hard-won
knowledge that you should not rediscover.

### Rule 6: Standard Error Format

All handler responses with errors MUST use this format:
```json
{
  "result": null,
  "error": {
    "code": "\u003cMODULE\u003e_\u003cERROR_TYPE\u003e",
    "message": "Human-readable description",
    "retryable": true,
    "source_module": "\u003cname\u003e"
  }
}
```

Error codes follow `\u003cMODULE\u003e_\u003cERROR_TYPE\u003e` pattern:
- `AUTH_TOKEN_EXPIRED`, `AUTH_CREDENTIALS_WRONG`
- `STORAGE_QUERY_FAILED`, `STORAGE_CONNECTION_FAILED`
- `GATEWAY_ROUTE_NOT_FOUND`, `GATEWAY_MODULE_UNAVAILABLE`

If `retryable` is true, the caller should retry with backoff.

### Rule 6.5: Show Architecture Plan Before Building

When a user request involves **2+ modules** or any architectural decision (e.g., "make a todo API with auth, SQLite storage, CRUD"):

**Stop. Show a plan first. Get approval. Then build.**

1. `module_discover()` — understand the current project state
2. Draft an architecture plan: which modules, what language, why, dependency order
3. Present it to the user with three options: **[Approve] [Modify] [Reject]**
4. If "Modify" → incorporate feedback, re-present
5. Only after user approves → proceed to Rule 7 (or build it yourself if ≤1 module)

```
# Your architecture draft:
├── 📦 auth (Python, JWT-based)
│   └── 接口: register(), login(), verify_token()
├── 📦 storage (Go, SQLite, high-concurrency)
│   └── 接口: query(), execute(), migrate()
├── 📦 todo (Python, CRUD) — depends on auth + storage
└── 📦 gateway (TypeScript, Express) — depends on all above

📌 Notes:
- storage in Go because the project has concurrent writes
- auth depends on storage's users table → storage first
- Estimated: 3 sub-agents, ~30s

[Approve]  [Modify]  [Reject]
```

`ponytail:` Natural language draft is enough. No structured plan schema, no JSON — user reads it, says yes or no. Add validation when the team finds people approving plans they didn't read.

### Rule 7: Decompose Complex Requests — Use Sub-Agents

When a user request involves **2+ modules** (e.g., "add auth + storage + todo CRUD"):

**You are the orchestrator — do not write every module yourself.**

1. `module_discover()` — understand the full project + dependency graph
2. Identify which modules to create/modify, group by dependency order
3. Delegate each module to a sub-agent via `task()`:
   - Sub-agent scope: read AIexplain → edit handler → bump version in module.json
   - Independent modules run in parallel; dependent ones wait for their prereqs
4. Wait for all sub-agents to complete
5. `module_wire()` — regenerate the main entry point
6. `aiexplain_generate()` — sync the knowledge layer
7. Write to `project-memory/architecture-decisions.md` documenting what was done

```
# You are the orchestrator:
modules = ["auth", "storage", "todo"]  # from module_discover
parallel tasks: auth, storage           # no deps, run together
serial task: todo                       # depends on auth + storage
task("build auth") → task("build storage")  # parallel
task("build todo")                          # after both done
module_wire()
aiexplain_generate()
```

`ponytail:` No orchestrator framework needed here. A flat list of `task()` calls + one `WaitForAll()` covers everything. Reach for a scheduler only when you have 5+ concurrent sub-agents or a genuinely deep dependency tree.

## Interface Contract

Every module exposes a single entry point:
```python
def handler(input: dict) -> dict:
    """
    Args:
        input: dict with module-specific parameters
    Returns:
        {"result": <any>, "error": <string|null>}
    """
```

The contract is enforced by `module.json`:
```json
{
  "name": "calculator",
  "version": "1.2.0",
  "status": "stable",
  "interface": {
    "entry": "handler",
    "input_schema": { "type": "object", ... },
    "output_schema": { "type": "object", ... }
  }
}
```

## Module Boundary Rules (for decomposition)

When deciding whether to create a new module, ask:

1. **Data ownership**: Does this function own data that no other module owns?
   → Yes: new module.
2. **Change isolation**: Will this function change independently of others?
   → Yes: new module.
3. **External dependency**: Does this function wrap an external system (DB, API, filesystem)?
   → Yes: new module — isolate the dependency.
4. **Testability**: Can this function be validated standalone?
   → Yes: good candidate for a module.

If NONE of the above apply → merge into an existing module.

### Anti-patterns to avoid
- One function per module (too fine-grained)
- utils/helpers/common modules (dumpster drawers)
- Circular dependencies (merge or introduce a shared interface)
- Module names tied to implementation details (e.g. "sqlite-storage")

## Module Communication Rules

### Importing another module

When module A depends on module B:

1. **Declare** the dependency in module.json:
   ```json
   { "dependencies": ["B"] }
   ```
2. **Import** B's handler at the top of A's implementation, using the full package path:
   ```python
   import sys, json
   from pathlib import Path
   PROJECT_ROOT = Path(__file__).resolve().parent.parent.parent.parent
   sys.path.insert(0, str(PROJECT_ROOT))
   from source.modules.B.B import handler as handler_B
   ```
3. **Call** B through its handler contract:
   ```python
   result = handler_B({"action": "...", ...})
   if result.get("error"):
       return result  # bubble the error
   ```
4. **Verify** consistency after modifying A's code:
   ```
   module_check_imports(module="A")
   ```
   This checks that declared dependencies match actual imports.

### Do NOT import without declaring

If your code imports another module but module.json does not list it
as a dependency, `module_check_imports` will flag it as undeclared.
If module.json declares a dependency that is never imported,
it will be flagged as unused.

## INDEX.md

After adding/removing modules, update INDEX.md. It's a markdown table:
```markdown
| Module | Version | Status | Owner | Language | Interface |
|--------|---------|--------|-------|----------|-----------|
| auth   | 1.0.0   | stable | agent | python   | handler(input) -> dict |
```

## Legacy Project Adoption

For projects NOT yet using micro-module architecture:

### Strategy A: Zero-intrusion search
```
module_search_loose(query="<concept>", project_dir="<path>")
```
Indexes all code files without requiring any project restructuring.
Use for quick code understanding and bug hunting in any codebase.

### Strategy B: Thin-wrapper integration
Wrap existing code behind a `handler(input:dict)->dict` interface without
modifying the original logic:
```python
# source/modules/legacy_gateway/gateway.py
import subprocess
def handler(d):
    result = subprocess.run(["go", "run", "../../server.go"], ...)
    return {"result": result.stdout}
```
Add a `module.json` declaring the interface — the old code stays untouched.

### Strategy C: Progressive migration
1. Run `module_search_loose()` to understand the codebase
2. Generate AIexplain cards for key modules (manual or via aiexplain_generate)
3. Wrap one subsystem at a time behind handler interfaces
4. Use `module_bootstrap()` for new modules going forward
5. Every wrapped module gets `module_validate()` + AIexplain card

## Summary for Your System Prompt

You are working in a micro-module architecture project.
- **Discover**: `module_discover()` — one call, full picture
- **Create**: `module_create(name, language)` — scaffold
- **Sync**: `aiexplain_generate()` — regenerate knowledge layer
- **Read first**: AIexplain cards — faster, more semantic
- **Modify last**: source code — only when you know what to change
- **Always close the loop**: modify → sync → next agent benefits
