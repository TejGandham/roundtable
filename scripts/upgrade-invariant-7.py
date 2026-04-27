#!/usr/bin/env python3
"""Stamp the KEEL-INVARIANT-7 marker in the backlog preamble.

Idempotent. Stdlib only. Python 3.10+.

See docs/design-docs/2026-04-23-keel-prd-scope-design.md §"Grandfather mechanism".
"""

from __future__ import annotations

import argparse
import re
import sys
from pathlib import Path

MARKER_RE = re.compile(r"<!--\s*KEEL-INVARIANT-7:\s*legacy-through=F(\d+)\s*-->")
F_ID_RE = re.compile(r"^\s*-\s*\[[ x]\]\s*\*\*F(\d+)\s", re.MULTILINE)


def parse_args() -> argparse.Namespace:
    p = argparse.ArgumentParser(description="Stamp KEEL-INVARIANT-7 marker.")
    p.add_argument("--repo", default=".", help="Repo root.")
    return p.parse_args()


def halt(msg: str) -> None:
    print(f"upgrade-invariant-7: HALT — {msg}", file=sys.stderr)
    sys.exit(1)


def main() -> int:
    args = parse_args()
    repo = Path(args.repo).resolve()
    backlog = repo / "docs" / "exec-plans" / "active" / "feature-backlog.md"

    if not backlog.exists():
        halt(f"no feature-backlog.md at {backlog}.")

    # Read with preserved line endings to avoid CRLF→LF translation.
    with open(backlog, "r", encoding="utf-8", newline="") as f:
        text = f.read()

    if MARKER_RE.search(text):
        print("upgrade-invariant-7: KEEL-INVARIANT-7 marker already present. No changes.")
        return 0

    f_ids = [int(m.group(1)) for m in F_ID_RE.finditer(text)]
    max_id = max(f_ids) if f_ids else 0
    marker = f"<!-- KEEL-INVARIANT-7: legacy-through=F{max_id:02d} -->"

    # Detect the file's dominant line ending to preserve it end-to-end.
    nl = "\r\n" if "\r\n" in text else "\n"

    # Insert after the H1 + any blank lines and before the first non-preamble content.
    lines = text.splitlines(keepends=True)
    insert_at = None
    for i, line in enumerate(lines):
        if line.startswith("# "):
            insert_at = i + 1
            break

    if insert_at is None:
        halt(
            f"backlog at {backlog} has no H1 heading (line starting with `# `). "
            "Add a canonical H1 like `# Feature Backlog` before running this script."
        )

    # Skip any immediately following blank lines.
    while insert_at < len(lines) and lines[insert_at].strip() == "":
        insert_at += 1

    lines.insert(insert_at, marker + nl)
    lines.insert(insert_at + 1, nl)

    # newline="" suppresses universal-newlines translation on both read and write.
    # The marker and blank lines are built with nl (detected from the file's
    # dominant line ending), so the output file preserves the original CRLF/LF
    # end-to-end without corruption or silent normalization.
    with open(backlog, "w", encoding="utf-8", newline="") as f:
        f.write("".join(lines))

    print(
        f"upgrade-invariant-7: Placed KEEL-INVARIANT-7 marker with "
        f"legacy-through=F{max_id:02d} based on current max feature ID.\n"
        f"Entries F01-F{max_id:02d} are grandfathered; new entries from "
        f"F{max_id+1:02d} forward must carry PRD: or PRD-exempt:.\n"
        f"Edit the marker value in {backlog.relative_to(repo)} if this is wrong."
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
