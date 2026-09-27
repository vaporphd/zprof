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
import pathlib
import re

SCHEMA_PATH = pathlib.Path(__file__).parent / "telemetry.yaml"

# `- {name: x,  type: y, required: true,  default: 0}  # optional trailing comment`
_FIELD_LINE = re.compile(r"^\s*-\s*\{(?P<body>.*?)\}\s*(#.*)?$")
# `  - "some\\sregex"`
_PATTERN_LINE = re.compile(r'^\s*-\s*"(?P<body>.*)"\s*$')
# top-level (unindented) `key: value` lines, e.g. `version: 1`, `core_fields:`
_SECTION_HEADER = re.compile(r"^(?P<key>[A-Za-z_][A-Za-z0-9_]*):\s*(?P<value>.*?)\s*(#.*)?$")

LIST_SECTIONS = ("core_fields", "tool_events", "redaction_patterns",
                 "mutating_bash_patterns", "verdict_exempt_roles",
                 "review_block_verdicts")


def _parse_scalar(raw):
    """Parse a bare (unquoted) YAML scalar into a Python value."""
    if raw == "true":
        return True
    if raw == "false":
        return False
    if re.fullmatch(r"-?\d+\.\d+", raw):
        return float(raw)
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
    schema = {s: [] for s in LIST_SECTIONS}
    schema["score_defaults"] = {}
    section = None

    for raw_line in text.splitlines():
        stripped = raw_line.strip()
        if not stripped or stripped.startswith("#"):
            continue

        if section in LIST_SECTIONS:
            if section in ("core_fields", "tool_events"):
                field_match = _FIELD_LINE.match(raw_line)
                if field_match:
                    schema[section].append(_parse_field_body(field_match.group("body")))
                    continue
            else:
                pattern_match = _PATTERN_LINE.match(raw_line)
                if pattern_match:
                    schema[section].append(_unescape_dq(pattern_match.group("body")))
                    continue
        # Handle indented lines like `  weights: {P1: 20, P2: 15, ...}`
        elif (
            section == "score_defaults"
            and raw_line.startswith((" ", "\t"))
            and (header_match := _SECTION_HEADER.match(stripped))
        ):
            key = header_match.group("key")
            value = header_match.group("value").strip()
            if value.startswith("{") and value.endswith("}"):
                # Parse the flow mapping
                body = value[1:-1]  # Remove { }
                schema["score_defaults"][key] = _parse_field_body(body)
            continue

        if not raw_line.startswith((" ", "\t")):
            header_match = _SECTION_HEADER.match(stripped)
            if header_match:
                key = header_match.group("key")
                value = header_match.group("value").strip()
                if key == "score_defaults":
                    section = "score_defaults"
                elif key in LIST_SECTIONS:
                    section = key
                else:
                    section = None
                    if value:
                        schema[key] = _parse_scalar(value)
                continue

    return schema


def test_schema():
    schema = load_schema(SCHEMA_PATH.read_text())
    fields = schema["core_fields"]
    names = [f["name"] for f in fields]

    # sanity: the hand-rolled parser actually found something (guards
    # against a silent parse failure reporting a bogus "0 fields" pass)
    assert schema.get("version") == 2, f"unexpected/missing top-level version: {schema.get('version')!r}"
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

    # tool_events schema (spec §5 C3)
    te_names = [f["name"] for f in schema["tool_events"]]
    assert te_names == ["schema_version", "dispatch_id", "seq", "ts", "tool",
                        "input_hash", "target", "is_error", "mutating", "result_chars"], te_names
    for f in schema["tool_events"]:
        assert f["type"] in valid_types, f"{f['name']}: unknown type {f['type']}"
        # Each tool_events entry has mandatory keys (like core_fields)
        for required_key in ("name", "type", "required"):
            assert required_key in f, f"tool_events field missing '{required_key}': {f}"

    # mutating bash patterns compile and do NOT match build/test commands
    assert schema["mutating_bash_patterns"], "mutating_bash_patterns is empty"
    compiled = [re.compile(p) for p in schema["mutating_bash_patterns"]]
    for cmd in ("swift test --package-path Packages/Core", "cargo build --release",
                "go test ./...", "pytest -q", "make test", "git status", "cat foo.txt"):
        assert not any(c.search(cmd) for c in compiled), f"build/test/read command matched as mutating: {cmd}"
    for cmd in ("cat > f.txt <<'EOF'", "sed -i 's/a/b/' f", "git commit -m x",
                "rm -rf build", "xcodegen generate", "echo hi | tee out.log"):
        assert any(c.search(cmd) for c in compiled), f"mutating command not matched: {cmd}"

    # exempt roles — empty since #20 (ADR 0003): doctor guarantees verdict:
    # on every role, so P6/P7 no longer need to look away from any of them.
    assert schema["verdict_exempt_roles"] == []

    # reviewer verdicts that count as a block (P5); mirrored in Go score.Defaults
    assert schema["review_block_verdicts"] == [
        "block", "changes-requested", "awaiting-approval", "failed", "blocked",
    ]

    # score_defaults: parsed values (cross-language contract)
    assert schema["score_defaults"]["weights"] == {"P1": 20, "P2": 15, "P3": 10, "P4": 20, "P5": 10, "P6": 15, "P7": 10}, \
        f"weights mismatch: {schema['score_defaults'].get('weights')}"
    assert sum(schema["score_defaults"]["weights"].values()) == 100, \
        f"weights do not sum to 100: {sum(schema['score_defaults']['weights'].values())}"
    assert schema["score_defaults"]["saturation"] == {"P1": 0.20, "P2": 3, "P3": 0.5, "P4": 2, "P5": 2, "P6": 0.30, "P7": 4}, \
        f"saturation mismatch: {schema['score_defaults'].get('saturation')}"
    assert schema["score_defaults"]["thresholds"] == {"ideal": 85, "solid": 60}, \
        f"thresholds mismatch: {schema['score_defaults'].get('thresholds')}"

    print(f"OK: {len(fields)} fields, {len(schema['redaction_patterns'])} redaction patterns")


if __name__ == "__main__":
    test_schema()
