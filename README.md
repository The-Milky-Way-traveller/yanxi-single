# Yanxi — Agent-First Micro-Module Architecture

**A 6 MB, zero-dependency MCP server that structures codebases for AI agents.**

Yanxi doesn't run pipelines. It doesn't manage agents. It doesn't generate business logic.

It does three things:

1. **Give a map** — Agent enters a project, reads ~500 tokens, understands the full landscape.
2. **Catch mistakes** — Agent writes code, gets instant feedback across 6 validation stages.
3. **Remember experience** — Agent makes a mistake, yanxi writes it to lessons-learned. Next agent never repeats it.

```
Agent's job:         decide, design, implement, deploy
Yanxi's job:         structure, validate, wire, document, remember, test
```

### What makes yanxi different

Yanxi's answer to "how do agents keep working at scale?" rests on two mechanisms:

**Micro-module contracts.** Every module has a machine-readable `module.json` — typed entry schemas, cross-module call declarations, interface provides/uses, dependencies, middleware references, and error codes. The contract isn't documentation for a human to read; it's data for yanxi to validate, diff, and reason about.

**Push-based contract verification.** When an agent runs `module_validate("auth")`, yanxi walks a 6-stage pipeline automatically. Every issue is surfaced to the agent — broken call references, schema incompatibilities, middleware functions that don't exist, upstream modules that have been deprecated, downstream modules whose calls no longer match. The agent doesn't need to know what to ask; yanxi pushes the answers.

These two form a closed loop: contracts enable validation, validation protects contracts.

---

## Contents

- [The Information Architecture](#the-information-architecture)
- [18 MCP Tools](#18-mcp-tools)
- [Agent Workflow](#agent-workflow)
- [The Module Contract](#the-module-contract)
- [Validation Pipeline (6 Stages)](#validation-pipeline-6-stages)
- [Yanxi vs Agent Boundary](#yanxi-vs-agent-boundary)
- [What Yanxi Does NOT Do](#what-yanxi-does-not-do)
- [Build & Run](#build--run)

---

## The Information Architecture

Yanxi organizes project knowledge into three layers. An agent reads ~500 tokens total to understand the full project, then drills into details only for modules it needs to touch.

```
Level 1 — Project Panorama (~200 tokens)
  module_discover()
  → Project summary: language, module count, build order, warnings
  → Project memory: ADRs, lessons learned, conventions
  → Deprecation warnings: ⚠ marked modules, circular deps

Level 2 — Module Digest (~300 tokens)
  module_discover() → module list
  → Each module: name, version, status, language, entry count, dependents
  → Agent decides which module to work on

Level 3 — Module Detail (~300 tokens each, on demand)
  module_read("auth")
  → AIexplain card: purpose, interfaces, entries, usage, errors
  → interface.md: entry points with schemas
  → Source preview: first 2000 characters
```

### The AIexplain Layer (Level 3 Core)

AIexplain is the knowledge layer. Every module gets auto-generated documentation that agents read instead of source code:

```
source/modules/config/config.go      → ~2000 tokens of raw Go code
AIexplain/modules/config/config.md   → ~150 tokens of structured knowledge

AIexplain card for config module:

  # config Module
  **Status**: wip | **Version**: 0.1.0 | **Language**: go

  ## Purpose
  Package config manages application configuration from various sources.
  **Depended by**: agent, api, mcpclient, permission, tools

  ## Interface
  func Get() *Config {

  ### Entries
  - **Get**: Get returns the global config instance.
  - **Load**: Load initializes the configuration from environment variables...
  - **Save**: Save persists the current config to disk.
  - **Validate**: Validate validates the configuration

  ## Usage Example
  config.UpdateAgentModel(input)

  ## Depended by
  agent, api, mcpclient, permission, tools
```

Each entry's description is extracted from function docstrings in source code.
The "Depended by" list is calculated from the dependency graph.
Design intent descriptions must be written by the agent (yanxi warns if missing).

---

## 18 MCP Tools

### Project Entry

| Tool | What it does |
|------|-------------|
| `module_discover` | Level 1 + Level 2. Project summary, module digest, dependency graph, warnings, lessons. Agent's first call. |
| `module_report` | Project health dashboard: heatmap (core modules by dependents), risk score (unvalidated/failed/deprecated ratio), dead module detection. |

### Module Lifecycle

| Tool | What it does |
|------|-------------|
| `module_create` | Generate module skeleton (module.json + handler stub) in Go/Python/TS/JS. Non-built-in languages get an LLM bootstrap prompt. |
| `module_bootstrap` | One-shot: create + wire + sync. Rolls back on failure. |
| `module_read` | Level 3 detail: AIexplain card + interface + source preview. |
| `module_validate` | 6-stage validation pipeline — see below. |
| `module_wire` | Generate main entry routing + HTTP server (`main -http 8080`). Blocks on unvalidated modules. |
| `module_sync` | Apply detected changes: sync entries, calls, version from source code. Yanxi detects → warns → agent calls sync. |
| `module_adopt` | Analyse external code directory for adoption. Returns LLM prompt for transformation. |
| `module_adopt_commit` | Finalise adoption: write module, delete original, wire, sync. |
| `module_deprecate` | Mark module as deprecated/archived. Auto-writes ADR, warns dependents. |

### Knowledge & Memory

| Tool | What it does |
|------|-------------|
| `aiexplain_generate` | Incrementally regenerate AIexplain cards + rebuild search index. Only touches changed modules. |
| `memory_init` | Create project-memory + conventions.json + test_cases.json templates. Idempotent. |
| `memory_write` | Append to ADR/lesson/convention. Auto-deduplicates lessons. Supports global scope (`~/.yanxi/memory/`). |

### Search & Analysis

| Tool | What it does |
|------|-------------|
| `module_search` | BM25/vector search across AIexplain + source code. |
| `module_search_loose` | Search any directory without micro-architecture. For legacy projects. |
| `module_check_imports` | Compare declared dependencies vs actual source imports. Flags undeclared/unused. |

### Language Extension

| Tool | What it does |
|------|-------------|
| `save_lang_template` | Save an LLM-generated language template. After saving, `module_create` supports the new language with full validate coverage. |

---

## Agent Workflow

### Entering a project
```
module_discover() → ~500 tokens, full picture
  ↓
module_read("auth") → AIexplain card, understand module
  ↓
edit source code
  ↓
module_validate("auth") → 6-stage check
  ↓
module_sync("auth") → apply detected changes (if any)
  ↓
module_wire() → regenerate routing
  ↓
aiexplain_generate() → sync knowledge cards
```

### Creating a new module
```
module_create("email", "go") → skeleton
  ↓ write description (yanxi warns if generic)
  ↓ write handler code
  ↓ write test_cases.json (yanxi warns if missing)
module_validate("email") → check everything
module_wire() + aiexplain_generate()
```

### Adopting legacy code
```
module_adopt("pkg/util") → analysis + LLM prompt
  ↓ LLM transforms code
module_adopt_commit(...) → write + delete original + wire + sync
```

### Health check
```
module_report() → risk score + core modules + dead modules
```

---

## The Module Contract

Every module is a directory with source code + a machine-readable contract.

```
source/modules/auth/
├── auth.py          ← handler/interface implementation
└── module.json      ← contract (name, version, interface, deps, calls)
```

```json
{
  "name": "auth",
  "version": "1.0.0",
  "status": "active",
  "language": "go",
  "dependencies": ["storage", "session"],
  "interface": {
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
      }
    },
    "provides": {
      "AuthService": {
        "description": "Authentication service interface",
        "methods": ["Login", "Logout", "VerifyToken"]
      }
    },
    "uses": {
      "storage.StorageService": {
        "methods": ["Save", "Load"]
      }
    },
    "calls": {
      "storage": {"save_session": {}},
      "config": {"handler": {}}
    }
  }
}
```

Key fields:

| Field | Purpose |
|-------|---------|
| `entries` | Dispatchable entry points with JSON Schema i/o |
| `provides` | Interfaces this module exports (for Go struct/pubsub) |
| `uses` | Interfaces this module consumes (cross-module contract) |
| `calls` | Direct module-to-module function calls |
| `dependencies` | Upstream modules for dependency graph |
| `middleware` | Before/after hooks referencing other modules |

---

## Validation Pipeline (6 Stages)

`module_validate("auth")` runs these stages in order:

### Stage 1 — Structure (hard fail)
- module.json exists, JSON valid
- Required fields: name, version, status, interface
- Implementation file exists and readable

### Stage 2 — Source (hard fail)
- Entry functions exist (language-specific regex from template)
- Lifecycle hooks: setup/teardown/health exist if declared
- Dependencies exist as target module.json files
- Import consistency: declared vs actual cross-module imports

### Stage 3 — Cross-Module Contracts
- **Calls validity**: every call targets a real module + real entry
- **Middleware validity**: every middleware ref points to a real entry
- **Deprecated upstream**: any dependency is deprecated/archived → warning
- **Downstream compatibility**: schema changes → scan affected callers
- **Interface contracts**: `uses` declarations match provider's `provides`
- **Module granularity**: >7 entries or vague name → warning

### Stage 4 — Deep Analysis (warnings)
- Import classification: known/local/third_party/stdlib
- Side-effect detection: file/net/global mutations
- Streaming check: declared streaming but no yield/generator
- Convention checks: project-specific rules from conventions.json

### Stage 5 — Runtime
- **Custom test cases**: loads test_cases.json if present. WARNING if missing.
- Auto-generated tests: from input_schema (normal values, missing required, invalid enum)
- Multi-language execution: python -c / go run / node -e subprocess
- Latency benchmark vs max_latency_ms
- Strict mode: input/output validated against JSON Schema types

### Stage 6 — Change Detection
- Schema diff: compare against previous version cache
- Version bump suggestion if breaking changes detected
- Downstream broadcasting to dependent modules

---

## Yanxi vs Agent Boundary

```
Agent's task                       Who owns it
─────────────────────────────────────────────────────────────
Analyse requirements               Agent (LLM)
Design module decomposition        Agent
Write handler code                 Agent
Write test cases                   Agent (yanxi provides framework)
Describe design intent             Agent (yanxi warns if missing)
Create module.json skeleton        Yanxi (module_create)
Register module in routing         Yanxi (module_wire)
Validate everything                Yanxi (module_validate — 6 stages)
Detect breaking interface changes  Yanxi (schema diff)
Sync knowledge cards               Yanxi (aiexplain_generate)
Record design decisions            Yanxi (memory_write)
Tell next agent about pitfalls     Yanxi (lessons-learned.md)
Start HTTP server                  Yanxi (module_wire generates net/http)
```

**Rule**: mechanical, repetitive, forgettable tasks → yanxi automates.
Tasks requiring domain knowledge, design intent, or business logic → agent owns.

---

## What Yanxi Does NOT Do

| Not done | Why |
|----------|-----|
| Business logic validation | Yanxi validates structure, not correctness. Agent writes test_cases.json. |
| Code generation beyond stubs | Yanxi generates module.json + routes. Agent writes business logic. |
| HTTP server framework | Yanxi generates net/http. Agent can wrap with FastAPI/Gin. |
| Module sandbox / process isolation | Single-machine development only. |
| Dockerfile / CI generation | Deployment varies per project — agent decides. |

---

## Build & Run

```powershell
cd cmd\yanxi-mcp
go build -o yanxi-mcp.exe .
# → 6 MB single binary, zero external dependencies

# Configure MCP client
{
  "mcpServers": {
    "yanxi-single": {
      "command": "path\\to\\yanxi-mcp.exe",
      "args": []
    }
  }
}
```

### First steps

```
1. module_discover() → understand project
2. memory_init() → create project-memory + test templates
3. module_create("first", "go") → add a module
4. module_validate("first") → run validation
5. module_wire() → generate routing
```

---

## License

MIT
