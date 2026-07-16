# Yanxi Architecture — Quick Reference

> yanxi-single v1.0.0 — Agent-First Micro-Module Architecture MCP Server

## Package Map

```
cmd/yanxi-mcp/main.go          — 16 MCP tool registrations
internal/
  orchestrator/
    orchestrator.go             — ModuleDiscover, ModuleRead, CreateModule, WireMain,
                                  BootstrapModule, dep graph, BuildOverview, discover cache
    aiexplain.go                — EnsureAIExplain (incremental via mtime)
    project.go                  — ProjectConfig, ProjectMemory, MemoryWrite, InitProjectMemory
    adopt.go                    — AnalyzeExternalDir, BuildAdoptPrompt, AdoptCommit,
                                  DeprecateModule, FindDependentsOf
    langtmpl/                   — Language-specific code templates (go/python/ts + LLM-bootstrapped)
  validate/
    validate.go                 — 6-stage validation pipeline (structure → source → cross-module → deep → runtime → diff)
    schema.go                   — Schema diff, strict mode, cache persistence
    depth.go                    — Side effects, benchmarks, coverage
  search/
    search.go                   — BM25 index, BuildIndex, BuildLooseIndex
    vector.go                   — Vector search (-tags vector)
    vector_stub.go              — No-op stub when vector tag absent
  check/
    check.go                    — Import audit + comprehensive import scanning (5 categories)
  mcp/
    server.go                   — JSON-RPC 2.0 over stdio
```

## Data Flow

### Entering a project
```
module_discover()
  [cache check] → mtimes match? → return cached overview (~2ms)
  [cache miss]  → scan modules/ → build overview → save cache (~50ms)

module.discover returns:
  Level 1: project summary + memory + warnings
  Level 2: module digest (name, version, status, deps, dependents)

module_read("auth")
  Level 3: module.json + AIexplain card + interface.md + source preview
```

### Creating a module
```
module_create("auth", language="python")
  → module.json skeleton + handler stub
  → auto-validate + mark validated
  → invalidate discover cache
```

### Modifying a module
```
module_validate("auth")
  Stage 1: structure (module.json, files)
  Stage 2: source (handler regex, lifecycle, deps, imports)
  Stage 3: cross-module (calls, middleware, deprecated upstream, downstream compat)
  Stage 4: deep (import scan, side effects, streaming, errors)
  Stage 5: runtime (tests, latency, coverage, strict mode)
  Stage 6: diff (schema diff + downstream broadcasting)

module_wire()
  → generates source/main/main.{py|ts|go} with imports + dispatch
  → blocks if any module unvalidated/failed

aiexplain_generate()
  → incremental: only changed modules
  → rebuilds BM25 search index
```

### Adopting external code
```
module_adopt("pkg/util")
  → scan directory (files, exports, package name)
  → return LLM transformation prompt

module_adopt_commit(name="util", adapted_source="...")
  → write source/modules/util/
  → delete original pkg/util/
  → wire + aiexplain + invalidate cache
```

### Deprecating a module
```
module_deprecate("old_auth", "deprecated", "replaced by auth v2")
  → set module.json status
  → write ADR
  → invalidate cache
  → discover shows ⚠ prefix
  → dependents warned
```

## Key Design Decisions

| Decision | Rationale |
|----------|-----------|
| **MCP over REST** | Agent-native protocol; stdio = zero ports, zero auth, zero network |
| **Single binary** | ~6 MB exe, zero runtime dependencies, pure Go stdlib |
| **Three-level discovery** | Agent sees project context first, then drills on demand |
| **Module contract over comments** | Machine-readable schema, not human prose |
| **Schema diff caching** | `.yanxi/schema_cache/<module>.json` for incremental diff |
| **BM25 over vector (default)** | Zero dependencies, works offline; vector optional |
| **DFS dependency graph** | Cycle detection O(V+E); topological sort gives build order |
| **Discover cache** | mtime comparison; 50 modules: ~2ms cached vs ~50ms full scan |
| **Lesson deduplication** | Substring match before appending new lessons |

## Agent-Authoring Conventions

- All modules expose `handler(input: dict) → dict`
- Module names: `lowercase_with_underscores`
- Error codes: `MODULE_ERROR_TYPE`
- Version: semver. Fix → patch, feature → minor, breaking → major
