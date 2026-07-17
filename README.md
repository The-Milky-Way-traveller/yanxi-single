# Yanxi（盐析）— Agent-First Micro-Module Development Tool

<p align="center">
  <b>A 6 MB, zero-dependency MCP server that helps AI agents understand project structure without reading all source code.</b>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.21%2B-blue" alt="Go version">
  <img src="https://img.shields.io/badge/license-MIT-green" alt="License">
  <img src="https://img.shields.io/badge/dependencies-zero-success" alt="Zero dependencies">
</p>

<p align="center">
  <a href="README.zh.md">🇨🇳 中文版</a>
</p>

---

## A Word First

I'm a high school freshman with no professional software development experience. This project is a summer vacation vibecoding experiment.

Due to limited time and skill, this tool hasn't seen much real-world use and has plenty of rough edges. The actual implementation may differ from the descriptions below — that's exactly the "poor context management and documentation" problem mentioned later.

If you're willing to spend some time looking at this naive project idea, I'd really appreciate it.

---

## Why This Exists

### Context Cliff and Cross-Session Continuity

When vibecoding without proper context management and documentation, a common pattern emerges: the Agent forgets the project's intent, starts guessing, deletes working code, and breaks features that were fine before.

### The Thought Process

Existing tools use vector search to feed only relevant code to LLMs. Can we go further?

1. Organize related code together as you write — **micro-module architecture**
2. Simply splitting code doesn't help the Agent — give each module a **summary file** so the Agent only needs to read that to understand it, like a skill mechanism with lazy loading
3. Go further: **force validation** after each module to ensure it runs, and leave finished modules alone unless necessary

That's where yanxi started.

---

## What is Yanxi

Yanxi（盐析）is an MCP server (6 MB single binary, zero external dependencies) that provides 18 tools for AI agents.

Yanxi does three things and three things only:

1. **Give a map** — Agent enters a project and reads ~500 tokens to understand the full picture, no need to read all source code
2. **Catch mistakes** — After the Agent writes code, yanxi runs a 6-stage validation automatically
3. **Remember experience** — Mistakes are automatically written to project memory so the next session doesn't repeat them

---

## Core Workflow

### Reading

When an Agent enters a project in a new conversation, it should call yanxi to get structured information. The information is organized in three layers:

```
Layer 1 (~200 tokens):
  module_discover()
  Returns: what the project does, language, module count, dependencies, warnings

Layer 2 (~300 tokens):
  Module summary list → Agent decides which module to work on

Layer 3 (~300 tokens each, on demand):
  module_read("auth") → Full knowledge card for the selected module
  The Agent reads auto-generated knowledge cards, not source code
```

*A dedicated folder `AIexplain` is created in the project root, mirroring the source structure, to store these knowledge cards.*

Each layer has much higher information density than raw source code. The Agent understands the project after reading three layers, without touching source files.

For special cases (non-standard directories, legacy projects), BM25 full-text search (`module_search`) or loose search (`module_search_loose`) is also available. Vector search is planned but not yet fully adapted.

### Micro-Modules and Module Lifecycle

A micro-module is defined as a self-contained unit that can complete a task independently and be tested in isolation. Creation and wiring have dedicated tools to ensure correctness. The contract is maintained semi-automatically by yanxi + LLM. After writing, `module_validate` runs comprehensive checks. A module lifecycle (create → validate → wire → deprecate) is in place to improve module maintainability.

### New Projects

Yanxi automatically scaffolds the skeleton. `memory_init` initializes project memory and config in one step. `module_create` generates the module skeleton in one step.

### Adapting Non-Structured Code

For compatibility, LLM understands external code structure while yanxi handles the mechanical work: writing, deleting, wiring, and syncing (`module_adopt`).

### Multi-Language Support

Yanxi does mechanical work for the Agent (preventing errors), but this means it must support different execution modes for different languages. Currently 4 language templates are built in (Go, Python, TypeScript, JavaScript). Additional languages are added via LLM-generated templates. See the "Multi-Language Extension" section.

---

## 18 Tools in Detail

### Entry

#### `module_discover` — Project Overview

The first tool an Agent should call when entering a project. Returns three layers of information:

- Level 1: Project summary (language, module count, build order, design intent, warnings)
- Level 2: Module summaries (name, version, entry count, dependents)
- Level 3: On-demand detail via `module_read`

Supports lazy mode (`lazy=true`) for faster response with Level 1 + Level 2 only.

Warnings include: deprecated modules, circular dependencies, unvalidated modules, generic descriptions, missing custom test cases.

#### `module_report` — Project Health Report

Aggregates project data: heatmap (core modules highlighted), risk score, dead module detection. Warns "5 dependents will be affected" before modifying core modules.

### Module Creation

#### `module_create` — Generate Module Skeleton

Auto-generates `module.json` and handler template. Built-in support for Go, Python, TypeScript, JavaScript. Non-built-in languages trigger the LLM bootstrap flow. Parameters: `name` (required), `language` (default python), `description` (design intent, recommended).

#### `module_bootstrap` — One-Click Create + Register + Sync

Combines `module_create` → `module_validate` → `module_wire` → `aiexplain_generate` in one step. Automatically rolls back on failure.

#### `module_adopt` — Absorb External Code

Brings existing non-yanxi directories (e.g. `pkg/util/`) into the module system. Flow: analyze directory → return LLM transformation prompt → LLM writes JSON Schema → `module_adopt_commit` writes + deletes original + wires + syncs. Does not modify original function bodies.

### Validation

#### `module_validate` — Core Feature. 6-Stage Automatic Check

**1. Structure Check (hard fail)**: module.json exists, valid JSON, required fields present, implementation file exists.

**2. Source Check (hard fail)**: entry functions exist (regex from language template), lifecycle hooks exist, dependencies exist, imports consistent.

**3. Cross-Module Check**: calls target exists, middleware exists, upstream deprecation detection, downstream compatibility, interface contract (provides/uses), module granularity (>7 entries warning).

**4. Deep Analysis (warning)**: import classification (known/local/third_party/stdlib), side effects, streaming, custom convention checks (conventions.json).

**5. Runtime Tests**: custom tests (test_cases.json, warns if missing), auto-generated tests, multi-language subprocess execution (python/go/node + template languages), latency benchmark, strict mode.

**6. Change Detection**: Schema diff, version suggestion, downstream notification.

Returns: `valid`, `errors`/`warnings`, `tests`, `call_issues`, `deprecated_deps`, `middleware_issues`, `transport_issues`, `convention_issues`, `lifecycle`, `error_declarations`, `streaming_entries`, `import_scan`, `breaking_changes`.

#### `module_check_imports` — Dependency Consistency

Compares declared dependencies against actual imports.

#### `module_sync` — Contract Sync

After validate detects source changes, the Agent confirms whether to update `module.json`. The Agent can also choose to ignore warnings and edit manually.

### Wiring

#### `module_wire` — Generate Routing + HTTP Server

Generates `source/main/main.<ext>` with all module imports, route dispatch, and HTTP server (`main -http 8080`). Supports both CLI mode and HTTP mode. Unvalidated modules block generation.

### Documentation

#### `aiexplain_generate` — Generate AIexplain Knowledge Cards

Incremental mode: only updates changed modules. Generates `<name>.md` (module overview) and `interface.md` (interface reference). Card content sources: `module.json` description, source docstrings, dependency graph, error codes.

#### `memory_init` — Initialize Project Memory

Idempotent. Creates `architecture-decisions.md`, `lessons-learned.md`, `conventions.md`, `conventions.json` (structured rules, auto-checked by validate), `test_cases.json` (custom test template), `.yanxi/project.json`.

#### `memory_write` — Write to Project Memory

ADR/lesson/convention. Auto-deduplication. Supports `scope="project"` (local) and `scope="global"` (cross-project, writes to `~/.yanxi/memory/`).

### Deprecation

#### `module_deprecate` — Module Deprecation

Changes status to deprecated/archived, auto-writes ADR, notifies dependents.

### Search

#### `module_search` — Full-Text Search

BM25 search across AIexplain + source code. Compile with `-tags vector` to enable vector search.

#### `module_search_loose` — Loose Search

Search any directory without yanxi architecture. For legacy projects or non-standard directories.

### Multi-Language Extension

#### `save_lang_template` — Save LLM-Generated Language Template

Built-in: Go, Python, TypeScript, JavaScript. For other languages:

1. `module_create("auth", "rust")` → yanxi has no built-in template → returns LLM prompt
2. Agent sends prompt to LLM → prompt includes full Go and Python JSON examples → LLM returns Rust template JSON (with `entry_regex`, `import_extract_regex`, `test_runtime` etc.)
3. `save_lang_template("rust", <JSON>)` → yanxi saves to `.yanxi/lang-templates/rust.json`
4. Afterwards: `module_create` generates skeleton ✅, `module_validate` entry check and import classification ✅, test execution ✅ (template fallback), `module_wire` generates routing ✅

---

## Module Contract

```
source/modules/auth/
├── auth.py          ← handler/interface implementation
└── module.json      ← machine-readable contract
```

| Field | Purpose | Filled by |
|-------|---------|-----------|
| `name` | Module identifier | module_create auto |
| `version` | Semantic version | Agent manual / validate suggests |
| `status` | wip → active → deprecated → archived | Agent / deprecate tool |
| `language` | Programming language | module_create auto |
| `dependencies` | Yanxi module dependencies | Agent fills / validate checks |
| `interface.description` | **Design intent** — why this module exists | **Agent must fill**, validate checks |
| `interface.entries` | Callable entry functions | validate auto-sync |
| `interface.provides` | Interfaces this module provides | Agent fills |
| `interface.uses` | Interfaces this module consumes | Agent fills / validate checks |
| `interface.calls` | Cross-module function calls | validate auto-infers |
| `errors` | Error codes the module may return | Agent fills |

---

## Agent Workflow

```
module_discover()          → understand project (~500 tokens)
module_read("auth")        → understand target module
edit handler code
write test_cases.json      → validate warns if missing
write module.json description → validate warns if too generic
module_validate("auth")    → 6-stage validation
module_sync("auth")        → confirm changes (if new entries/calls)
module_wire()              → generate routing + HTTP
aiexplain_generate()       → sync knowledge cards
```

---

## Current Status

The project's usability is still being evaluated. I am currently using yanxi to develop a desktop application (yanxi-desktop) with 14 modules.

### What Works

- Three-layer project discovery (50 modules ~800 tokens)
- Cross-module contract validation (calls, middleware, provides/uses, deprecation)
- Source-to-contract auto-sync (entries, calls)
- Project memory with auto-deduplication
- provides/uses interface declarations
- Multi-language template framework

### What's Rough

- **This is a pure vibecoding project, very early stage, reliability not fully evaluated**
- Business logic validation depends on Agent-written test cases. Yanxi only provides the framework (test_cases.json). Auto-synced entry descriptions are placeholders that need Agent attention
- Module communication model favors function routing. Yanxi assumes `handler(input: dict) → dict` by default, but many Go projects use interface injection + pubsub. `provides/uses` has been added but the display and generation layers need more work
- Small projects (<3 modules) aren't worth it. Writing code directly is faster
- Documentation and onboarding are rough. No tutorial, no GUI,全靠 MCP tool calls
- **Adaptability to serious software development workflows is questionable**

### Next Steps

Summer break is limited, so this project may pause for a while — but it's not abandoned.

If you're willing to try it, file an issue, or tell me what I'm doing wrong, I will take it seriously and be very grateful.

GitHub: [https://github.com/The-Milky-Way-traveller/yanxi-single](https://github.com/The-Milky-Way-traveller/yanxi-single)

---

## Quick Start

```powershell
cd cmd\yanxi-mcp
go build -o yanxi-mcp.exe .
# → 6MB single binary, zero dependencies
```

---

## Project Structure

```
<project>/
├── .yanxi/                    ← tool state (auto-managed)
│   ├── project.json           ← project config
│   ├── discover_cache.json
│   ├── schema_cache/
│   ├── validation_state.json
│   ├── search_index.json
│   └── lang-templates/        ← LLM-generated language templates
├── source/
│   ├── main/main.{py|ts|go}   ← wiring entry point
│   └── modules/<name>/
│       ├── <name>.{py|ts|go}  ← handler
│       └── module.json        ← contract
├── AIexplain/                 ← knowledge layer (auto-generated)
├── project-memory/            ← project memory
│   ├── conventions.json       ← structured rules (auto-checked by validate)
│   └── test_cases.json        ← custom test template
└── .mcp.json
```

---

## License

MIT
