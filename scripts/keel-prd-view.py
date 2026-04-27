#!/usr/bin/env python3
# /// script
# requires-python = ">=3.14"
# dependencies = ["jsonschema>=4.25"]
# ///
"""Synthesizer: render a schema-valid KEEL JSON PRD as deterministic markdown.

JSON is canonical; this output is a read-time cache for humans. Same JSON
input always produces byte-identical markdown.

Usage:
  uv run scripts/keel-prd-view.py path/to/prd.json
  uv run scripts/keel-prd-view.py path/to/prd.json --feature F03
  uv run scripts/keel-prd-view.py path/to/prd.json --output rendered.md

Exit codes:
  0  rendered successfully
  1  PRD failed schema validation OR --feature ID not found
  2  invocation error (file not found, not JSON, not UTF-8, malformed flag)
"""
from __future__ import annotations

import argparse
import json
import re
import sys
from pathlib import Path
from typing import TypedDict

from jsonschema import Draft202012Validator

# --- Types ------------------------------------------------------------------

class Oracle(TypedDict, total=False):
    type: str
    tooling: str
    setup: str | None
    actions: list[str]
    assertions: list[str]
    gating: str


class Feature(TypedDict, total=False):
    id: str
    title: str
    layer: str
    needs: list[str]
    contract: dict
    oracle: Oracle


class DesignFact(TypedDict, total=False):
    topic: str
    decision: str
    rationale: str | None


class Invariant(TypedDict, total=False):
    invariant_id: str
    name: str
    how_exercised: str


class Scope(TypedDict):
    included: list[str]
    excluded: list[str]


class Prd(TypedDict, total=False):
    schema_version: int
    id: str
    title: str
    motivation: str
    scope: Scope
    design_facts: list[DesignFact]
    invariants_exercised: list[Invariant]
    features: list[Feature]


# --- Schema loader (same pattern as validate-prd-json.py) -------------------

SCHEMA_REL = Path("schemas") / "prd.schema.json"
_SCHEMA_SEARCH_DEPTH = 4

# --feature CLI value must match the schema's feature-ID pattern.
FEATURE_ID_RE = re.compile(r"^F\d{2,}$")

# Sentinel for empty optional scalars (Setup, Tooling, Gating, Needs).
EMPTY_SCALAR = "—"


def load_schema() -> dict:
    """Locate schemas/prd.schema.json by walking up from the script location.

    Mirrors validate-prd-json.py — KEEL scripts ship as standalone files,
    not as a packaged Python distribution, so a path-anchored lookup is the
    right tool. See AGENTS.md §Python conventions.
    """
    here = Path(__file__).resolve().parent
    candidates = [here, *list(here.parents)[:_SCHEMA_SEARCH_DEPTH]]
    for candidate in candidates:
        schema_path = candidate / SCHEMA_REL
        if schema_path.is_file() and not schema_path.is_symlink():
            return json.loads(schema_path.read_text(encoding="utf-8"))
    raise FileNotFoundError(
        f"Could not locate {SCHEMA_REL} within {_SCHEMA_SEARCH_DEPTH} levels "
        f"above {here}. Run from a KEEL-installed repo or framework source tree."
    )


def validate_prd(doc: dict, schema: dict) -> list[str]:
    """Return formatted schema-validation findings; empty list = clean."""
    v = Draft202012Validator(schema)
    errors = sorted(v.iter_errors(doc), key=lambda e: list(e.absolute_path))
    return [_format_error(e) for e in errors]


def _format_error(e) -> str:
    path = "/" + "/".join(str(p) for p in e.absolute_path)
    return f"{path}: {e.message.splitlines()[0]}"


# --- Renderers --------------------------------------------------------------

def render_full(prd: Prd) -> str:
    """Render the full PRD. Sections in fixed order; arrays in source order."""
    parts: list[str] = [
        f"# {prd['title']}",
        "",
        prd["motivation"],
        "",
        _render_scope(prd["scope"]),
        _render_design_facts(prd["design_facts"]),
        _render_invariants(prd["invariants_exercised"]),
        "## Features",
        "",
    ]
    for feature in prd["features"]:
        parts.append(_render_feature(feature, heading_level=3))
    return "\n".join(parts).rstrip() + "\n"


def render_feature_slice(prd: Prd, feature: Feature) -> str:
    """Render a single feature with PRD title for context. Heading promoted to H2."""
    parts = [
        f"# {prd['title']} — {feature['id']}",
        "",
        _render_feature(feature, heading_level=2),
    ]
    return "\n".join(parts).rstrip() + "\n"


def _render_scope(scope: Scope) -> str:
    lines = ["## Scope", "", "### Included", ""]
    lines.extend(_render_bullet_list(scope["included"]))
    lines.extend(["", "### Excluded", ""])
    lines.extend(_render_bullet_list(scope.get("excluded", [])))
    lines.append("")
    return "\n".join(lines)


def _render_design_facts(facts: list[DesignFact]) -> str:
    lines = ["## Design facts", ""]
    if not facts:
        lines.extend(["(none)", ""])
        return "\n".join(lines)
    for fact in facts:
        lines.append(f"- **{fact['topic']}** — {fact['decision']}")
        rationale = fact.get("rationale")
        if rationale:
            lines.append(f"  Rationale: {rationale}")
    lines.append("")
    return "\n".join(lines)


def _render_invariants(invs: list[Invariant]) -> str:
    lines = ["## Invariants exercised", ""]
    if not invs:
        lines.extend(["(none)", ""])
        return "\n".join(lines)
    for inv in invs:
        name = inv.get("name") or EMPTY_SCALAR
        lines.append(
            f"- **{inv['invariant_id']}** ({name}) — {inv['how_exercised']}"
        )
    lines.append("")
    return "\n".join(lines)


def _render_feature(feature: Feature, heading_level: int) -> str:
    h = "#" * heading_level
    needs = feature.get("needs") or []
    needs_str = ", ".join(needs) if needs else EMPTY_SCALAR
    lines = [
        f"{h} {feature['id']} — {feature['title']}",
        "",
        f"**Layer:** {feature['layer']}",
        f"**Needs:** {needs_str}",
        "",
        "**Contract:**",
        "",
        "```json",
        json.dumps(feature["contract"], indent=2, ensure_ascii=False),
        "```",
        "",
        "**Oracle:**",
        "",
    ]
    lines.extend(_render_oracle(feature["oracle"]))
    lines.append("")
    return "\n".join(lines)


def _render_oracle(oracle: Oracle) -> list[str]:
    setup = oracle.get("setup")
    tooling = oracle.get("tooling")
    gating = oracle.get("gating")
    actions = oracle.get("actions") or []
    assertions = oracle["assertions"]

    lines = [
        f"- Type: {oracle['type']}",
        f"- Setup: {setup if setup else EMPTY_SCALAR}",
        "- Actions:",
    ]
    if actions:
        lines.extend(f"  - {a}" for a in actions)
    else:
        lines.append(f"  - {EMPTY_SCALAR}")
    lines.append("- Assertions:")
    lines.extend(f"  - {a}" for a in assertions)
    lines.append(f"- Tooling: {tooling if tooling else EMPTY_SCALAR}")
    lines.append(f"- Gating: {gating if gating else EMPTY_SCALAR}")
    return lines


def _render_bullet_list(items: list[str]) -> list[str]:
    if not items:
        return ["(none)"]
    return [f"- {item}" for item in items]


# --- Entrypoint -------------------------------------------------------------

def main() -> int:
    parser = argparse.ArgumentParser(
        description="Render a schema-valid KEEL JSON PRD as deterministic markdown.",
    )
    parser.add_argument("prd", type=Path, help="Path to the JSON PRD")
    parser.add_argument(
        "--feature",
        help="Render only this feature (e.g. F03). Slice includes PRD title.",
    )
    parser.add_argument(
        "--output",
        type=Path,
        help="Write rendered markdown to this path (default: stdout).",
    )
    args = parser.parse_args()

    if not args.prd.is_file():
        print(f"halt: PRD not found at {args.prd}", file=sys.stderr)
        return 2

    try:
        doc = json.loads(args.prd.read_text(encoding="utf-8"))
    except json.JSONDecodeError as e:
        print(
            f"halt: {args.prd} is not valid JSON: "
            f"{e.msg} (line {e.lineno}, col {e.colno})",
            file=sys.stderr,
        )
        return 2
    except UnicodeDecodeError as e:
        print(
            f"halt: {args.prd} is not UTF-8 text: {e.reason} "
            f"(byte {e.start}-{e.end}). PRD files must be UTF-8 JSON.",
            file=sys.stderr,
        )
        return 2

    try:
        schema = load_schema()
    except FileNotFoundError as e:
        print(f"halt: {e}", file=sys.stderr)
        return 2

    findings = validate_prd(doc, schema)
    if findings:
        print(
            f"halt: {args.prd} fails KEEL PRD schema validation:",
            file=sys.stderr,
        )
        for f in findings:
            print(f"  {f}", file=sys.stderr)
        print(
            "\nFix the PRD or run "
            f"`uv run scripts/validate-prd-json.py {args.prd}` for full diagnostics.",
            file=sys.stderr,
        )
        return 1

    if args.feature is not None:
        if not FEATURE_ID_RE.match(args.feature):
            print(
                f"halt: --feature must match ^F\\d{{2,}}$, got '{args.feature}'. "
                f"Example: --feature F03.",
                file=sys.stderr,
            )
            return 2
        target = next(
            (f for f in doc["features"] if f["id"] == args.feature), None,
        )
        if target is None:
            available = ", ".join(f["id"] for f in doc["features"])
            print(
                f"halt: feature '{args.feature}' not found in {args.prd}. "
                f"Available: {available}",
                file=sys.stderr,
            )
            return 1
        output = render_feature_slice(doc, target)
    else:
        output = render_full(doc)

    if args.output is not None:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(output, encoding="utf-8")
    else:
        sys.stdout.write(output)
    return 0


if __name__ == "__main__":
    sys.exit(main())
