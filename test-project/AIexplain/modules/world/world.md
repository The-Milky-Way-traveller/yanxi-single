# world Module

## Purpose

world module — see interface for details.

## Interface

`def handler(d)`

### Input Schema

```json
{
  "properties": {
    "action": {
      "type": "string"
    }
  },
  "type": "object"
}
```

### Output Schema

```json
{
  "properties": {
    "error": {
      "type": "string"
    },
    "result": {}
  },
  "type": "object"
}
```

## Usage Example

```python
handler({"action": "...", ...})
# → {"result": ...}
```

## Dependencies

None

_Source: `source/modules/world/world.py`_
