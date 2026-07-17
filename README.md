# Yanxi (盐析) — Agent-First Micro-Module Architecture

<p align="center">
  <b>A 6 MB, zero-dependency MCP server that structures codebases for AI agents.</b>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/go-1.21%2B-blue" alt="Go version">
  <img src="https://img.shields.io/badge/license-MIT-green" alt="License">
  <img src="https://img.shields.io/badge/build-passing-brightgreen" alt="Build">
  <img src="https://img.shields.io/badge/dependencies-zero-success" alt="Zero dependencies">
</p>

---

## What

Yanxi is an MCP server that helps AI agents navigate and maintain structured codebases. It plugs into any MCP-compatible client (Claude Desktop, Cursor, VS Code, or custom) and gives the agent a structured view of the project — not raw source code.

> **MCP (Model Context Protocol)** is an open standard that lets AI tools communicate with external servers. Think of it as a USB-C port for AI: one protocol, many tools. Yanxi is one such tool.

## Why

When an AI agent enters a project, it reads source files until it understands the structure. With 5+ modules, that's thousands of lines of code. With 50 modules, the context window overflows entirely — the agent starts making decisions with an incomplete picture. This is the **Context Cliff**.

Yanxi's answer: **information density over information volume.**

```
Without yanxi:   14 modules × ~2000 lines = ~28,000 tokens to understand the project
With yanxi:      module_discover()         = ~500 tokens for the full picture
```

It does three things:

1. **Give a map** — Agent enters a project, reads ~500 tokens, understands the full landscape.
2. **Catch mistakes** — Agent writes code, gets instant feedback across 6 validation stages.
3. **Remember experience** — Agent makes a mistake, yanxi writes it to `lessons-learned.md`. The next agent never repeats it.

```
Agent's job:         decide, design, implement
Yanxi's job:         structure, validate, wire, document, remember
```

## How it works

### The Three-Layer Information Architecture

```
Level 1 — Project Panorama (~200 tokens)
  module_discover()
  → Project summary, module count, build order
  → Warnings: deprecated modules, circular deps, unvalidated modules
  → Lessons learned from previous agent sessions

Level 2 — Module Digest (~300 tokens)
  → Each module: name, version, status, language, entry count, dependents
  → Agent decides which module to inspect further

Level 3 — Module Detail (~300 tokens each, on demand)
  module_read("auth")
  → AIexplain knowledge card (auto-generated)
  → interface.md with typed schemas
  → Source preview (first 2000 characters)
```

### The AIexplain Knowledge Layer

Every module gets auto-generated documentation that agents read instead of source code:

```
source/modules/config/config.go      → ~2000 tokens of raw Go
AIexplain/modules/config/config.md   → ~150 tokens of structured knowledge

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

Descriptions are extracted from function docstrings. Dependents are calculated from the dependency graph. Design intent must be written by the agent — yanxi warns if descriptions are generic.

### What Makes Yanxi Different

**Micro-module contracts.** Every module has a machine-readable `module.json`. Not a document for humans — data for yanxi to validate, diff, and reason about. Typed entry schemas, cross-module call declarations, interface provides/uses, middleware references, error codes.

**Push-based contract verification.** `module_validate("auth")` walks a 6-stage pipeline automatically. Every issue is surfaced to the agent without the agent needing to know what to ask:

- Call references pointing to missing modules or entries
- Middleware functions that don't exist
- Upstream modules that have been deprecated
- Downstream modules whose calls no longer match the changed schema
- Schema changes that break backward compatibility

These two form a closed loop: contracts enable validation, validation protects contracts.

## Quick Start

```powershell
# 1. Build
cd cmd\yanxi-mcp
go build -o yanxi-mcp.exe .
# → 6 MB single binary, zero dependencies

# 2. Configure your MCP client
# Add to your .mcp.json:
{
  "mcpServers": {
    "yanxi-single": {
      "command": "path\\to\\yanxi-mcp.exe",
      "args": []
    }
  }
}

# 3. Start using it (your AI agent will call these)
module_discover()        → understand the project
memory_init()            → set up project memory
module_create("my-module", "go") → add a module
module_validate("my-module")     → check everything
module_wire()            → generate routing + HTTP server
```

## 18 Tools

### Project entry
| Tool | What it does |
|------|-------------|
| `module_discover` | Level 1 + Level 2. Project summary, module digest, warnings, lessons. Agent's first call. |
| `module_report` | Project health dashboard: heatmap, risk score, dead module detection. |

### Module lifecycle
| Tool | What it does |
|------|-------------|
| `module_create` | Generate module skeleton (module.json + handler stub). Supports Go/Python/TS/JS + LLM-bootstrapped languages. |
| `module_read` | Level 3 detail: AIexplain card + interface + source preview. |
| `module_validate` | 6-stage validation pipeline. The heart of yanxi. |
| `module_wire` | Generate main entry routing + HTTP server (`main -http 8080`). |
| `module_sync` | Apply pending changes: sync entries, calls, version from source. Yanxi detects → warns → agent confirms. |
| `module_bootstrap` | One-shot: create + wire + sync. Rolls back on failure. |
| `module_adopt` | Analyse external directory for adoption. Returns LLM transformation prompt. |
| `module_adopt_commit` | Finalise adoption: write module, delete original, wire, sync. |
| `module_deprecate` | Mark module as deprecated/archived. Auto-writes ADR, warns dependents. |

### Knowledge & memory
| Tool | What it does |
|------|-------------|
| `aiexplain_generate` | Incrementally regenerate AIexplain cards + rebuild search index. |
| `memory_init` | Create project-memory + conventions + test templates. Idempotent. |
| `memory_write` | Append to ADR/lesson/convention. Auto-deduplicates. Supports global scope. |

### Search & analysis
| Tool | What it does |
|------|-------------|
| `module_search` | BM25/vector search across AIexplain + source code. |
| `module_search_loose` | Search any directory without micro-architecture. |
| `module_check_imports` | Compare declared dependencies vs actual imports. |

### Language extension
| Tool | What it does |
|------|-------------|
| `save_lang_template` | Save an LLM-generated language template. After saving, `module_create` supports the new language with full validate coverage. |

## Validation Pipeline (6 Stages)

| Stage | Checks | Fail mode |
|-------|--------|-----------|
| 1 — Structure | module.json valid, required fields present, file exists | hard fail |
| 2 — Source | entry functions exist, lifecycle hooks exist, imports consistent | hard fail |
| 3 — Cross-module | calls target exists, middleware exists, no deprecated deps, downstream compatible, provides/uses match, granularity | warning/fail |
| 4 — Deep analysis | import classification, side effects, streaming, conventions | warning |
| 5 — Runtime | custom tests (test_cases.json), auto-generated tests, latency, strict mode | warning/fail |
| 6 — Change detection | schema diff, version suggestion, downstream notification | warning |

## The Module Contract

```
source/modules/auth/
├── auth.py          ← handler/interface implementation
└── module.json      ← machine-readable contract
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
        "input_schema": { "type": "object", "required": ["username", "password"], ... },
        "output_schema": { "type": "object", "properties": { "token": {"type": "string"}, ... } }
      }
    },
    "provides": { "AuthService": { "methods": ["Login", "Logout", "VerifyToken"] } },
    "uses": { "storage.StorageService": { "methods": ["Save", "Load"] } },
    "calls": { "storage": {"save_session": {}} }
  }
}
```

## Yanxi vs Agent Boundary

| Agent owns | Yanxi automates |
|-----------|----------------|
| Analysing requirements | module.json skeleton (module_create) |
| Designing module decomposition | Routing + HTTP server (module_wire) |
| Writing handler code | 6-stage validation (module_validate) |
| Writing test cases | Schema diff + version suggestion |
| Describing design intent | Knowledge card generation (aiexplain_generate) |
| Making architectural decisions | Project memory + lesson tracking |

**Rule**: mechanical, repetitive, forgettable → yanxi. Decisions, design, business logic → agent.

## Current Status

Yanxi is **early but working**. I'm using it to develop a 14-module desktop application — the validation pipeline catches real mistakes daily.

**What works well:**
- Three-layer project discovery (50 modules → ~800 tokens)
- Cross-module contract validation (calls, middleware, provides/uses, deprecation)
- Auto-sync from source to contract (entries, calls)
- Project memory with auto-deduplication

**What's rough:**
- Business logic validation depends on agent-written test cases
- Interface injection patterns (Go struct + pubsub) need more display work
- Documentation and onboarding are minimal
- GUI and tutorial don't exist yet

## What Yanxi Does NOT Do

- **Business logic validation** — validates structure, not correctness. Agent writes test_cases.json.
- **Code generation beyond stubs** — generates module.json + routes. Agent writes business logic.
- **Sandbox or process isolation** — single-machine development only.
- **Dockerfile or CI generation** — deployment varies per project.

## Contributing

This is a one-person summer project. Issues, bug reports, and feature ideas are welcome.
If you try it and hit something broken, open an issue — I will read it.

## License

MIT
