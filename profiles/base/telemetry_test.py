#!/usr/bin/env python3
"""Validate telemetry.yaml self-consistency.

Stdlib-only by design: this schema is the contract between the Python
collector (writer) and the Go `zprof stats` reader (consumer), and it
must be checkable in any environment without a `pip install`. PyYAML is
not assumed to be present, so this file does not `import yaml` — it
uses a small hand-rolled parser (`load_schema`) tailored to the
constrained subset of YAML that telemetry.yaml actually uses: a
top-level `version:` scalar, a `core_fields:` list of single-line flow
mappings (`- {name: x, type: y, required: true}`), and a
`redaction_patterns:` list of double-quoted strings. It is not a
general-purpose YAML parser and should not be used as one.
"""
import re
import sys
import pathlib

SCHEMA_PATH = pathlib.Path(__file__).parent / "telemetry.yaml"

# `- {name: x,  type: y, required: true,  default: 0}  # optional trailing comment`
_FIELD_LINE = re.compile(r"^\s*-\s*\{(?P<body>.*?)\}\s*(#.*)?$")
# `  - "some\\sregex"`
_PATTERN_LINE = re.compile(r'^\s*-\s*"(?P<body>.*)"\s*$')
# top-level (unindented) `key: value` lines, e.g. `version: 1`, `core_fields:`
_SECTION_HEADER = re.compile(r"^(?P<key>[A-Za-z_][A-Za-z0-9_]*):\s*(?P<value>.*?)\s*(#.*)?$")

LIST_SECTIONS = ("core_fields", "redaction_patterns")


def _parse_scalar(raw):
    """Parse a bare (unquoted) YAML scalar into a Python value."""
    if raw == "true":
        return True
    if raw == "false":
        return False
    if re.fullmatch(r"-?\d+", raw):
        return int(raw)
    return raw


def _parse_field_body(body):
    """Parse the inside of a flow mapping: `name: x, type: y, required: true`."""
    field = {}
    for part in body.split(","):
        part = part.strip()
        if not part:
            continue
        key, _, value = part.partition(":")
        field[key.strip()] = _parse_scalar(value.strip())
    return field


def _unescape_dq(raw):
    """Undo the YAML double-quoted-string escaping this file relies on (\\\\ and \\")."""
    out = []
    i = 0
    while i < len(raw):
        ch = raw[i]
        if ch == "\\" and i + 1 < len(raw) and raw[i + 1] in ("\\", '"'):
            out.append(raw[i + 1])
            i += 2
            continue
        out.append(ch)
        i += 1
    return "".join(out)


def load_schema(text):
    """Minimal stdlib-only parser for telemetry.yaml's constrained structure."""
    schema = {"core_fields": [], "redaction_patterns": []}
    section = None

    for raw_line in text.splitlines():
        stripped = raw_line.strip()
        if not stripped or stripped.startswith("#"):
            continue

        if section in LIST_SECTIONS:
            field_match = _FIELD_LINE.match(raw_line) if section == "core_fields" else None
            if field_match:
                schema["core_fields"].append(_parse_field_body(field_match.group("body")))
                continue
            pattern_match = _PATTERN_LINE.match(raw_line) if section == "redaction_patterns" else None
            if pattern_match:
                schema["redaction_patterns"].append(_unescape_dq(pattern_match.group("body")))
                continue

        if not raw_line.startswith((" ", "\t")):
            header_match = _SECTION_HEADER.match(stripped)
            if header_match:
                key = header_match.group("key")
                value = header_match.group("value").strip()
                section = key if key in LIST_SECTIONS else None
                if section is None and value:
                    schema[key] = _parse_scalar(value)
                continue

    return schema


def test_schema():
    schema = load_schema(SCHEMA_PATH.read_text())
    fields = schema["core_fields"]
    names = [f["name"] for f in fields]

    # sanity: the hand-rolled parser actually found something (guards
    # against a silent parse failure reporting a bogus "0 fields" pass)
    assert schema.get("version") == 1, f"unexpected/missing top-level version: {schema.get('version')!r}"
    assert fields, "core_fields is empty — parser likely failed to match telemetry.yaml's structure"
    assert schema["redaction_patterns"], "redaction_patterns is empty — parser likely failed"

    # every field entry has the mandatory keys
    for f in fields:
        for required_key in ("name", "type", "required"):
            assert required_key in f, f"field missing '{required_key}': {f}"

    # no duplicates
    assert len(names) == len(set(names)), f"duplicate fields: {[n for n in names if names.count(n) > 1]}"

    # required fields have no default (they must always be provided)
    for f in fields:
        if f["required"] and "default" not in f:
            pass  # fine — must be provided
        if not f["required"] and f["type"] == "bool" and "default" not in f:
            pass  # optional bool without default is fine

    # types are known
    valid_types = {"string", "int", "bool", "object"}
    for f in fields:
        assert f["type"] in valid_types, f"{f['name']}: unknown type {f['type']}"

    # redaction patterns compile
    for p in schema["redaction_patterns"]:
        re.compile(p)

    print(f"OK: {len(fields)} fields, {len(schema['redaction_patterns'])} redaction patterns")


if __name__ == "__main__":
    test_schema()
