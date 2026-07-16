# Project Architecture

## Module Registry

| Module | Version | Status | Language | Interface |
|--------|---------|--------|----------|-----------|
| hello | 0.1.0 | wip | python | `handler(input: dict) -> dict` |
| world | 0.1.0 | wip | python | `handler(input: dict) -> dict` |

## How to Read
- `AIexplain/<module>/<module>.md` — understand each module
- `AIexplain/modules/<name>/interface.md` — API reference

## Convention
All modules expose `handler(input: dict) -> dict`.
