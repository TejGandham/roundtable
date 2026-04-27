#!/usr/bin/env python3
# /// script
# requires-python = ">=3.14"
# dependencies = ["jsonschema>=4.25"]
# ///
"""Resolve a feature's invariant-7 classification, PRD, and per-feature
content in ONE deterministic call.

Replaces ~15 steps of prose-described work across `.claude/agents/pre-check.md`
and `.claude/skills/keel-refine/SKILL.md` with a single invocation that
emits structured JSON on stdout (success) or a P7 CTA on stderr (halt).

Typical invocation from an agent prompt:

    uv run scripts/keel-feature-resolve.py \\
      --backlog docs/exec-plans/active/feature-backlog.md \\
      --feature F04 \\
      --prd docs/exec-plans/prds/my-feature.json

Exit codes encode halt class; see `HaltCode` in `scripts/keel_features.py`.

On exit 0, stdout is a JSON document with the fields every downstream agent
needs: `feature_index`, `feature_pointer_base`, `oracle`, `contract`,
`needs`, `layer`, `title`, `prd_invariants_exercised`, `backlog_fields`,
`classification`. Agents carry these verbatim in the handoff brief.

On any non-zero exit, stderr is a human-readable P7 halt message with
specific cause + concrete fix.

Usage:
    uv run scripts/keel-feature-resolve.py --backlog <path> --feature F## \\
        [--prd <prd-path>] [--repo <repo-root>]

See docs/design-docs/2026-04-24-structured-prds.md (direction),
`.claude/agents/pre-check.md` (primary caller),
and `scripts/keel_features.py` (domain logic)."""
from __future__ import annotations

import argparse
import dataclasses
import json
import re
import sys
from pathlib import Path

# Path-anchored import of the helper module (scripts/ ships as standalone
# files, not a Python package; see AGENTS.md §Python conventions).
_SCRIPT_DIR = Path(__file__).resolve().parent
if str(_SCRIPT_DIR) not in sys.path:
    sys.path.insert(0, str(_SCRIPT_DIR))

from keel_features import (  # noqa: E402
    BacklogParser,
    FeatureResolver,
    FeatureResolution,
    FileBacklogSource,
    FilePrdJsonSource,
    Halt,
    HaltCode,
    Invariant7Classifier,
    ResolveRequest,
    load_schema,
)


def _render_halt(halt: Halt) -> str:
    """Halts always render in human form on stderr. The message itself
    is the P7 CTA; wrapping it in a JSON envelope would defeat verbatim
    emission upstream (agents forward stderr directly)."""
    return (
        f"halt: [{halt.code.name}] {halt.message}\n\n"
        f"Exit code: {halt.code.value}"
    )


def _render_resolution_json(resolution: FeatureResolution) -> str:
    payload = {"ok": True, **dataclasses.asdict(resolution)}
    return json.dumps(payload, indent=2, sort_keys=False)


def main() -> int:
    parser = argparse.ArgumentParser(
        description=(
            "Resolve a feature's invariant-7 classification, PRD, and "
            "per-feature content in one call."
        )
    )
    parser.add_argument(
        "--backlog",
        type=Path,
        required=True,
        help="Path to the backlog file (e.g. docs/exec-plans/active/feature-backlog.md).",
    )
    parser.add_argument(
        "--feature",
        required=True,
        help="Feature ID, e.g. F04.",
    )
    parser.add_argument(
        "--prd",
        type=Path,
        default=None,
        help=(
            "Path supplied by the caller (e.g. the /keel-pipeline argument). "
            "If omitted, the canonical path is derived from the backlog's "
            "PRD: slug."
        ),
    )
    parser.add_argument(
        "--repo",
        type=Path,
        default=Path.cwd(),
        help="Repo root (default: cwd). Design refs and canonical PRD paths resolve against this.",
    )
    args = parser.parse_args()

    # Validate --feature format at the CLI boundary so malformed input
    # routes to INVOCATION, not FEATURE_NOT_IN_BACKLOG.
    if not re.fullmatch(r"F\d{2,}", args.feature):
        print(
            f"halt: [INVOCATION] --feature must match `F\\d{{2,}}` (e.g. F04). "
            f"Got: {args.feature!r}. Fix: pass the feature ID with an "
            f"uppercase `F` prefix and at least two digits.",
            file=sys.stderr,
        )
        return HaltCode.INVOCATION.value

    try:
        schema = load_schema(args.repo)
    except (OSError, ValueError) as e:
        # OSError covers FileNotFoundError + PermissionError; ValueError
        # covers json.JSONDecodeError (subclass) if the schema file
        # exists but is corrupt. All route to INVOCATION so the caller
        # fixes the invocation or the schema file itself.
        print(
            f"halt: [INVOCATION] {e}\n\n"
            f"Fix: invoke from the repo root (where `schemas/prd.schema.json` "
            f"lives), or pass `--repo <path-to-repo-root>`. If the schema "
            f"file exists but is unreadable/corrupt, repair it (it ships "
            f"with KEEL — reinstall if needed).",
            file=sys.stderr,
        )
        return HaltCode.INVOCATION.value

    resolver = FeatureResolver(
        backlog_parser=BacklogParser(),
        classifier=Invariant7Classifier(),
        prd_source=FilePrdJsonSource(repo_root=args.repo),
        schema=schema,
    )

    backlog_source = FileBacklogSource(backlog_path=args.backlog)
    request = ResolveRequest(
        repo_root=args.repo,
        backlog_path=args.backlog,
        feature_id=args.feature,
        supplied_prd_path=args.prd,
    )

    result = resolver.resolve(request, backlog_source)

    if isinstance(result, Halt):
        print(_render_halt(result), file=sys.stderr)
        return result.code.value

    print(_render_resolution_json(result))
    return HaltCode.OK.value


if __name__ == "__main__":
    sys.exit(main())
