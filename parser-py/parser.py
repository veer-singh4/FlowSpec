#!/usr/bin/env python3
import json
import re
import sys
from pathlib import Path


APP_RE = re.compile(r"^app\s+([\w-]+)\s*\{$")
CLOUD_RE = re.compile(r"^cloud\s+(aws|azure)\s+([\w-]+)$")
DATABASE_RE = re.compile(r"^database\s+(postgres)$")
MODULE_RE = re.compile(r"^use\s+([\w.]+)\s+as\s+([\w-]+)\s*\{$")
CONFIG_RE = re.compile(r"^([A-Za-z_][A-Za-z0-9_-]*)\s+(.+)$")


class ParseError(Exception):
    pass


def parse_fs(file_path: str) -> dict:
    path = Path(file_path)
    lines = path.read_text(encoding="utf-8").splitlines()

    apps = []
    current_app = None
    current_module = None

    for line_num, raw_line in enumerate(lines, 1):
        line = raw_line.strip()

        if not line or line.startswith("#"):
            continue

        if current_module is not None:
            if line == "}":
                current_app["modules"].append(current_module)
                current_module = None
                continue

            match = CONFIG_RE.match(line)
            if not match:
                raise ParseError(f"line {line_num}: invalid module config line: {line}")

            key, value = match.group(1), match.group(2).strip().strip('"')
            current_module["config"][key] = value
            continue

        if current_app is None:
            match = APP_RE.match(line)
            if not match:
                raise ParseError(f"line {line_num}: expected 'app <name> {{' but got: {line}")

            current_app = {
                "name": match.group(1),
                "cloud": None,
                "database": None,
                "modules": [],
                "line": line_num,
            }
            continue

        if line == "}":
            apps.append(current_app)
            current_app = None
            continue

        cloud_match = CLOUD_RE.match(line)
        if cloud_match:
            current_app["cloud"] = {
                "provider": cloud_match.group(1),
                "region": cloud_match.group(2),
            }
            continue

        db_match = DATABASE_RE.match(line)
        if db_match:
            current_app["database"] = db_match.group(1)
            continue

        module_match = MODULE_RE.match(line)
        if module_match:
            current_module = {
                "module": module_match.group(1),
                "alias": module_match.group(2),
                "config": {},
                "line": line_num,
            }
            continue

        raise ParseError(f"line {line_num}: unsupported statement: {line}")

    if current_module is not None:
        raise ParseError("unexpected end of file: unclosed module block")

    if current_app is not None:
        raise ParseError("unexpected end of file: unclosed app block")

    return {"apps": apps}


def main() -> int:
    if len(sys.argv) != 2:
        print(json.dumps({"error": "usage: parser.py <file.ufl|file.ufs|file.fs>"}))
        return 1

    try:
        result = parse_fs(sys.argv[1])
        print(json.dumps(result, indent=2))
        return 0
    except Exception as exc:
        print(json.dumps({"error": str(exc)}))
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
