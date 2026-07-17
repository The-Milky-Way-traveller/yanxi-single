# Yanxi — Agent-First Micro-Module Architecture

**A 6 MB, zero-dependency MCP server that teaches AI agents how to navigate and evolve structured codebases.**

Yanxi doesn't run pipelines. It doesn't manage agents. It doesn't generate your business logic.

It does three things:

1. **Give a map** — Agent enters a project, reads ~500 tokens, understands the full landscape. No need to read every source file.
2. **Catch mistakes** — Agent writes code, gets instant feedback: handler missing? import wrong? interface broken? schema changed? downstream modules affected?
3. **Remember experience** — Agent makes a mistake, yanxi writes it to `lessons-learned.md`. Next agent never repeats it.

```
Agent's job:         decide, design, implement, deploy
Yanxi's job:         structure, validate, wire, document, remember
```

### What makes yanxi different

Yanxi's answer to the question "how do agents keep working at scale?" rests on two mechanisms that work together:

**Micro-module contracts.** Every module comes with a machine-readable `module.json` — not just a name and description, but typed entry schemas, cross-module call declarations, middleware references, dependencies, and error codes. The contract isn't documentation for a human to read; it's data for yanxi to validate, diff, and reason about. This is what makes the second mechanism possible.

**Push-based contract verification.** When an agent runs `module_validate("auth")`, yanxi walks a 6-stage pipeline: structural integrity → source-level checks → cross-module contract checks (calls, middleware, deprecation, downstream compatibility) → deep analysis → runtime tests → schema diff. Every issue is surfaced to the agent automatically — broken call references, schema incompatibilities, middleware functions that don't exist, upstream modules that have been deprecated, downstream modules whose calls no longer match. The agent doesn't need to know what to ask; yanxi pushes the answers.

These two mechanisms form a closed loop: contracts enable validation, validation protects contracts, and both run without the agent needing to search for what might be wrong.

---

## Contents

- [The Problem: Context Cliff](#the-problem-context-cliff)
- [Agent-First Design](#agent-first-design)
- [Three Information Layers](#three-information-layers)
- [The Module Contract](#the-module-contract)
- [18 MCP Tools — Complete Reference](#18-mcp-tools--complete-reference)
- [The Validation Pipeline (6 Stages)](#the-validation-pipeline-6-stages)
- [Module Lifecycle](#module-lifecycle)
- [Project Structure](#project-structure)
- [What Yanxi Does NOT Do](#what-yanxi-explicitly-does-not-do)
- [Build & Run](#build--run)
- [License](#license)

---

## The Problem: Context Cliff

An AI agent has ~128K tokens of context. Reading 5-10 modules' source code fills it entirely. The agent starts making decisions with an incomplete picture — that's the **Context Cliff**.

Traditional approach:

```
n modules × k source lines = O(n·k) tokens to understand a project
n=50 modules: ~100,000 tokens (window full, detail lost)
```

Yanxi's approach:

```
n × 20 (summary) + T (on-demand detail) = O(20n + T) tokens
n=50 modules: ~1,200 tokens + 500/module on demand
```

The difference is **information density over information volume**. Every token must carry maximum decision value.

---

## Agent-First Design

Everything in yanxi is designed for how AI agents read code, not how humans do.

| Human habit | What agents need | Yanxi's answer |
|---|---|---|
| "The class name is self-documenting" | Explicit input/output schemas | `module.json` with JSON Schema per entry |
| "Look at the caller to understand usage" | Cross-module call graph | `calls` declarations + `module_discover` call graph |
| "I remember past bugs" | Persistent memory across sessions | `project-memory/` with ADRs + lessons + conventions |
| "This module touches a few files" | Dependency graph + impact analysis | `module_validate` schema diff + downstream notification |

### The Yanxi-Agent Boundary

```
Agent's task                        Who owns it
─────────────────────────────────────────────────────────────
Analyse requirements                Agent (LLM)
Design module decomposition         Agent (with yanxi's module.json template)
Write handler code                  Agent
Create module.json skeleton         Yanxi (module_create)
Register module in routing table    Yanxi (module_wire)
Validate everything was done right  Yanxi (module_validate — 6 stages)
Detect breaking interface changes   Yanxi (schema diff)
Record design decisions             Yanxi (memory_write / WriteADR)
Tell next agent about pitfalls      Yanxi (lessons-learned.md, auto-written on failure)
Start HTTP server                   Agent (LLM generates FastAPI/Gin in 5 seconds)
Write test cases                    Agent (LLM knows edge cases)
Deploy                              Agent (LLM writes Dockerfile)
```

**The rule**: mechanical, repetitive, forgettable tasks → yanxi automates. Tasks requiring domain knowledge, design decisions, or that vary per project → agent owns.

---

## Three Information Layers

An agent enters a project and gets **Level 1 + Level 2** in a single `module_discover()` call (~500 tokens). It then drills into **Level 3** only for modules it needs to touch.

```
Level 1 — Project Panorama (~200 tokens)
  "yanxi-desktop: AI chat desktop app. Go + Chi + SQLite.
   Architecture: agent → provider × tools → api(HTTP)
   Status: Core features done. 16 modules. Start: agent, provider."

Level 2 — Module Digest (~300 tokens)
  agent   → orchestrator (dep: provider, tools, session)  ✓ validated
  api     → HTTP server  (dep: agent, session, config)     ✓ validated
  auth    → JWT login    (dep: storage)                    ✗ has warnings
  config  → config loader (dep: none)                      ✓ validated
  ...

Level 3 — Module Detail (~500 tokens each, on demand)
  module_read("auth")
    → full contract (module.json)
    → AIexplain card (what it does + how to use)
    → interface.md (entry points + schemas)
    → source preview (first 2000 chars)
```

---

## The Module Contract

Every module in a yanxi project is a directory with source code + a machine-readable contract.

```
source/modules/auth/
├── auth.py              ← handler action dispatch
└── module.json          ← contract
```

```json
{
  "name": "auth",
  "version": "1.0.0",
  "status": "active",
  "language": "python",
  "dependencies": ["storage", "session"],
  "interface": {
    "description": "Authentication and authorisation",
    "entries": {
      "login": {
        "description": "Authenticate user, return JWT",
        "input_schema": {
          "type": "object",
          "required": ["username", "password"],
          "properties": {
            "username": {"type": "string"},
            "password": {"type": "string"}
          }
        },
        "output_schema": {
          "type": "object",
          "properties": {
            "token": {"type": "string"},
            "expires_in": {"type": "integer"}
          }
        }
      },
      "logout": {
        "description": "Invalidate session",
        "input_schema": {
          "type": "object",
          "required": ["token"],
          "properties": {
            "token": {"type": "string"}
          }
        },
        "output_schema": {
          "type": "object",
          "properties": {
            "ok": {"type": "boolean"}
          }
        }
      }
    },
    "calls": {
      "storage": {
        "save_session": {
          "input": "session_data",
          "output": "session_id"
        }
      }
    },
    "middleware": {
      "before": ["auth.verify_token"]
    }
  }
}
```

Key fields:

| Field | Purpose |
|-------|---------|
| `name` | Module identifier, matches directory name |
| `version` | SemVer — bump on every change (yanxi detects breaking diffs) |
| `status` | `wip` → `active` → `deprecated` → `archived` |
| `language` | `python` / `go` / `typescript` / `javascript` (+ extensible via LLM templates) |
| `dependencies` | Other yanxi modules this module relies on |
| `interface.entries` | Entry points: each is a function with input/output JSON Schema |
| `interface.calls` | Cross-module calls this module makes to other modules' entries |
| `interface.middleware` | Before/after hooks referencing other modules' entries |

---

## 18 MCP Tools — Complete Reference

### 1. `module_discover` — Enter a project
Agent's first call. Returns Level 1 (project summary) + Level 2 (module digest) in ~500 tokens. Supports:
- **Cached mode**: mtime-based cache, 50 modules in ~2ms instead of 50 reads
- **Deprecation marking**: deprecated/archived modules shown with ⚠ prefix
- **Dependency warnings**: broken deps + circular deps flagged immediately

### 2. `module_create` — Skeleton a new module
Generates `module.json` + handler stub in any supported language. If the language isn't built-in, returns a self-contained prompt for an LLM to generate the template — yanxi saves it and uses it going forward.

### 3. `module_read` — Deep-dive a module
Returns Level 3: full contract, AIexplain knowledge card, interface reference, and source preview (first 2000 chars). Agent decides what to do next.

### 4. `module_validate` — The heart of yanxi
A 6-stage validation pipeline (detailed [below](#the-validation-pipeline-6-stages)). Runs structural checks, source analysis, cross-module contract verification, runtime tests, schema diff, and downstream compatibility — in a single call.

### 5. `module_wire` — Connect modules
Generates the project's main entry point (`source/main/main.{py,ts,go}`) with all module imports and routing dispatch. Blocks if any module is unvalidated or has failed validation — prevents broken builds.

### 6. `module_bootstrap` — One-shot: create + wire + sync
Shortcut for `module_create → module_wire → aiexplain_generate`. Used when adding a new module to an existing project.

### 7. `aiexplain_generate` — Sync knowledge
Incrementally regenerates AIexplain knowledge cards (mtime-based, only touches changed modules). Also rebuilds BM25 search index. Run after any source change.

### 8. `module_search` — Find code by concept
BM25 search across all AIexplain cards + source code. Optional vector mode (compile with `-tags vector`). Filters by `kind` (aiexplain/source/all).

### 9. `module_search_loose` — Search anything
Full-text search against any directory, no architecture required. Use for legacy projects or codebases you haven't adopted yet.

### 10. `module_check_imports` — Import hygiene
Compares `module.json` declared dependencies against actual imports in source code. Flags undeclared and unused dependencies.

### 11. `memory_init` — Bootstrap project memory
Creates `.yanxi/project.json` + `project-memory/` with ADR/lesson/convention templates. Idempotent — safe to re-run. `module_discover` suggests this when project memory is missing.

### 12. `memory_write` — Record experience
Appends to `architecture-decisions.md`, `lessons-learned.md`, or `conventions.md`. Auto-deduplicates lessons to avoid repetitive entries.

### 13. `module_adopt` — Digest external code
Analyses a project-local directory (e.g. `pkg/util/`, `internal/helpers/`) and returns a self-contained LLM prompt for transforming it into a yanxi module. The prompt instructs the LLM to keep all internal functions unchanged and add a thin handler layer on top.

### 14. `module_adopt_commit` — Finalise adoption
Takes LLM-transformed code, writes it to `source/modules/<name>/`, generates `module.json`, **deletes the original directory** (atomicity), runs `module_wire` + `aiexplain_generate`. One call completes the adoption.

### 15. `module_deprecate` — Retire modules
Sets a module's status to `deprecated` or `archived`, automatically writes an ADR recording the decision and reason, and warns all modules that still depend on it.

### 16. `module_sync` — Apply pending changes
Scans source code for undeclared exports and cross-module calls, detects breaking schema changes, and writes them to `module.json` after agent confirmation. Yanxi detects → warns → agent calls `module_sync` to apply. Never writes without explicit action.

### 17. `module_report` — Project health dashboard
Aggregates project-level data: **heatmap** (core modules by dependency count), **risk score** (unvalidated + failed + deprecated ratio), **dead module detection** (zero dependents + zero changes). Pure data aggregation, no new infrastructure.

### 18. `save_lang_template` — Extend languages
Saves an LLM-generated language template to `.yanxi/lang-templates/<lang>.json`. After saving, `module_create` supports the new language. Built-in: Go, Python, TypeScript, JavaScript.

---

## The Validation Pipeline (6 Stages)

`module_validate("auth")` runs these stages in order. Each stage either passes (no action needed), warns (degraded but functional), or fails (module cannot wire).

### Stage 1 — Structural Integrity (hard fail)

| Check | Method |
|-------|--------|
| module.json exists | File presence |
| JSON is valid | `json.Unmarshal` |
| Required fields present | `name`, `version`, `status`, `interface` |
| Implementation file exists | `source/modules/<name>/<name>.<ext>` |
| Group modules | Recursive child validation |

### Stage 2 — Source-Level Checks (hard fail)

| Check | Method |
|-------|--------|
| Entry functions exist | Language-specific regex: `def handler(`, `func Handler(`, `function handler(` |
| Lifecycle hooks exist | `setup()`, `teardown()`, `health()` if declared |
| Dependencies exist | Target `module.json` presence |
| Import consistency | Declared vs actual cross-module imports |

### Stage 3 — Cross-Module Contract Checks

| Check | What it detects |
|-------|----------------|
| **Calls validity** | Every `calls` declaration points to a real module + real entry. Flags broken references. |
| **Middleware validity** | Every `middleware.before/after` ref points to a real module entry. Flags missing functions. |
| **Deprecated upstream** | Any dependency is marked `deprecated` or `archived`. Warns the caller to plan migration. |
| **Downstream compatibility** | When schema changed: scans all modules that call this module and checks if their calls still target valid entries. Flags affected callers. |
| **Interface contracts (provides/uses)** | Validates that `uses` declarations match a provider's `provides` interface and methods. Flags broken interface references. |
| **Module granularity** | Warnings for >7 entries (suggest split) and vague module names (`utils`, `common`, `stuff`). |

### Stage 4 — Deep Analysis (warnings)

| Check | Method |
|-------|--------|
| Import classification | `ScanImports` categorises every import as `known` (yanxi module), `local` (project package → suggests `module_adopt`), `third_party`, or `stdlib` |
| Side-effect detection | Source scan for `file.Write`, `net.Dial`, global mutation patterns |
| Streaming check | Declared streaming but no `yield`/`generator` pattern |
| Error declarations | Lists declared error codes for documentation |

### Stage 5 — Runtime Verification

| Check | Method |
|-------|--------|
| Auto-generated test cases | From `input_schema`: normal values, missing required fields, invalid enum values |
| Multi-language execution | `python -c` / `node -e` / `go run` subprocess |
| Latency benchmark | Execution time vs `max_latency_ms` constraint |
| Coverage report | `(tested input combinations / total) × 100%` |
| Strict mode | Input and output validated against JSON Schema types |

### Stage 6 — Change Detection & Notification

| Check | Method |
|-------|--------|
| Schema diff | Compare current `interface` against `.yanxi/schema_cache/` — detects added, removed, and incompatible changes |
| Downstream broadcasting | When incompatible changes detected: auto-writes to `lessons-learned.md` for every dependent module |
| Validation state persistence | Writes result to `.yanxi/validation_state.json` — `module_wire` blocks on failure |

---

## Module Lifecycle

```
                     ┌──────────────────┐
                     │  module_create   │  yanxi generates module.json + handler stub
                     └────────┬─────────┘
                              │
                              ▼
                     ┌──────────────────┐
                     │  write handler   │  Agent writes business logic
                     └────────┬─────────┘
                              │
            ┌─────────────────┼──────────────────┐
            ▼                 ▼                   ▼
   ┌────────────────┐ ┌────────────────┐ ┌────────────────┐
   │module_validate │ │ module_wire    │ │aiexplain_gen   │
   │ 6-stage check  │ │ auto-register  │ │ sync knowledge │
   └────────┬───────┘ └────────┬───────┘ └────────────────┘
            │                  │
            ▼                  ▼
   ┌─────────────────────────────────────┐
   │         active module               │
   │  module_adopt to add legacy code    │
   │  module_deprecate to retire         │
   └─────────────────────────────────────┘
```

---

## Project Structure

```
<project>/
├── .yanxi/                          ← tool state (auto-managed)
│   ├── project.json                 ← project metadata
│   ├── discover_cache.json          ← mtime-based discovery cache
│   ├── last_sync.json               ← AIexplain incremental sync
│   ├── search_index.json            ← BM25 index
│   ├── schema_cache/<module>.json   ← previous schema for diff
│   ├── validation_state.json        ← per-module pass/fail history
│   └── lang-templates/<lang>.json   ← LLM-generated language templates
├── source/
│   ├── main/main.{py|ts|go}        ← wired entry point (generated)
│   └── modules/<name>/
│       ├── <name>.{py|ts|go}       ← handler(input: dict) → dict
│       └── module.json             ← contract (name, version, interface, deps, calls)
├── AIexplain/                       ← agent-readable knowledge (auto-generated)
│   ├── project-architecture.md
│   ├── module-contracts.json
│   ├── shared-functions-guide.md
│   └── modules/<name>/
│       ├── <name>.md               ← module overview
│       └── interface.md            ← API reference
├── project-memory/                  ← collective memory
│   ├── architecture-decisions.md   ← ADR records
│   ├── lessons-learned.md          ← auto-written by validate failures
│   └── conventions.md              ← project coding conventions
├── INDEX.md                        ← module registry (generated by module_wire)
└── .mcp.json                       ← MCP client config
```

---

## What Yanxi Explicitly Does NOT Do

| Not done | Why |
|----------|-----|
| HTTP server generation | Agent generates FastAPI/Gin in 5 seconds with LLM. Yanxi doing it is over-engineering. |
| Module sandbox / process isolation | Single-machine development only. Production isolation is agent's job. |
| Event bus runtime | Yanxi doesn't prescribe a runtime. Agent adds one when needed. |
| Custom test framework | Agent uses `pytest` / `go test`. Yanxi only runs auto-generated smoke tests. |
| Dockerfile / CI generation | Deployment varies per project. Agent decides. |
| Code generation beyond stubs | Yanxi generates structure (module.json, routes). Agent writes business logic. |

---

## Build & Run

### Build

```powershell
cd cmd\yanxi-mcp
go build -o yanxi-mcp.exe .
# → 6 MB single binary, zero dependencies
```

### Configure MCP client

Add to your `.mcp.json`:

```json
{
  "mcpServers": {
    "yanxi-single": {
      "command": "path\\to\\yanxi-mcp.exe",
      "args": []
    }
  }
}
```

### Test

```powershell
cd test-project
.\test-all-tools.ps1
```

### Extend to a new language

```python
# 1. Call save_lang_template with an LLM-generated template
# 2. module_create("newmod", language="rust")  # works immediately
# See .yanxi/lang-templates/ for examples
```

---

## License

MIT
