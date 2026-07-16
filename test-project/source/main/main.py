"""Main entry point — auto-generated."""
import sys, json
from pathlib import Path
PROJECT_ROOT = Path(__file__).resolve().parent.parent.parent
sys.path.insert(0, str(PROJECT_ROOT))

from source.modules.hello.hello import handler as handler_hello
from source.modules.world.world import handler as handler_world

def run(request):
    module = request.get("module", "")
    if module == "hello":
        return handler_hello(request)
    if module == "world":
        return handler_world(request)

    return {"result": None, "error": {"code": "GATEWAY_ROUTE_NOT_FOUND", "message": f"Unknown module: {module}", "retryable": false, "source_module": "main"}}

def main():
    if len(sys.argv) < 2:
        print(json.dumps({"result": None, "error": {"code": "GATEWAY_ROUTE_NOT_FOUND", "message": "Usage: main <module> [key=value ...]", "retryable": false, "source_module": "main"}}))
        sys.exit(1)
    req = {"module": sys.argv[1]}
    for arg in sys.argv[2:]:
        if "=" in arg:
            k, v = arg.split("=", 1)
            req[k] = v
    result = run(req)
    print(json.dumps(result, indent=2))

if __name__ == "__main__":
    main()
