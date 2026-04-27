#!/usr/bin/env python3
"""KEEL Uninstall — Remove KEEL artifacts from your project.

Receipt-driven: reads .claude/.keel-install.json and removes only paths
KEEL actually wrote. Files modified since install are preserved unless
--purge. Without a receipt, exits 1 — there is nothing to do.

Flags:
  --purge       Delete receipt-owned paths regardless of hash drift.
  --dry-run     Print the plan; exit without touching disk.
  -y, --yes     Skip confirmation prompt.

Exit codes: 0 success; 1 no receipt (not a KEEL install); 2 user declined;
10 receipt schema too new; 11 receipt corrupt; 20 partial I/O failure
(re-runnable).
"""
from __future__ import annotations

import argparse
import json
import shutil
import sys
from pathlib import Path

if sys.version_info < (3, 14):
    sys.exit(
        "KEEL requires Python 3.14+ "
        f"(found {sys.version_info.major}.{sys.version_info.minor}.{sys.version_info.micro}).\n"
        "Install a supported Python and re-run, e.g.:\n"
        "  uv python install 3.14 && uv run --python 3.14 .claude/keel-uninstall.py -y\n"
        "  (or install 3.14 system-wide and invoke with python3.14)"
    )

_script_dir = Path(__file__).resolve().parent
sys.path.insert(0, str(_script_dir))
from keel_manifest import (
    SETTINGS_FILE, RECEIPT_PATH,
    RECEIPT_SCHEMA_VERSION,
)
import keel_receipt as kr
import keel_settings as ksj


def parse_args(argv: list[str]) -> argparse.Namespace:
    p = argparse.ArgumentParser(description="Remove KEEL artifacts from a project.")
    p.add_argument("--purge", action="store_true",
                   help="Delete receipt-owned paths regardless of hash drift.")
    p.add_argument("--dry-run", action="store_true",
                   help="Print plan; do not modify.")
    p.add_argument("-y", "--yes", action="store_true", help="Skip confirmation.")
    return p.parse_args(argv)


def _path_hash(p: Path) -> str:
    return kr.hash_dir(p) if p.is_dir() else kr.hash_file(p)


def _rmpath(path: Path) -> None:
    if path.is_dir():
        shutil.rmtree(path)
    else:
        path.unlink()


def _plan_receipt_mode(project_dir: Path, receipt: dict, purge: bool):
    to_delete: list[tuple[str, str]] = []
    preserved: list[tuple[str, str]] = []
    missing: list[str] = []

    for rel, entry in receipt.get("managed_paths", {}).items():
        abs_p = project_dir / rel
        if not abs_p.exists():
            missing.append(rel)
            continue
        expected = entry["installed_hash"]
        actual = _path_hash(abs_p)
        if purge:
            to_delete.append((rel, entry.get("kind", "unknown")))
        elif actual == expected:
            to_delete.append((rel, entry.get("kind", "unknown")))
        else:
            preserved.append((rel, "modified_since_install"))
    return to_delete, preserved, missing


def _orphan_scan(project_dir: Path, managed_rels: set[str]) -> list[str]:
    """Advisory: report `keel-*` files in .claude/ NOT in the receipt."""
    candidates: list[Path] = []
    for pattern in (".claude/agents/keel-*.md",
                    ".claude/skills/keel-*",
                    ".claude/hooks/keel-*.py"):
        candidates.extend(project_dir.glob(pattern))
    orphans = []
    for c in candidates:
        rel = c.relative_to(project_dir).as_posix()
        if rel not in managed_rels:
            orphans.append(rel)
    return sorted(orphans)


def _handle_settings(project_dir: Path, receipt: dict,
                     dry_run: bool) -> tuple[str, tuple[str, str] | None]:
    """Surgically remove KEEL hook entries.

    Uses the exact commands the install receipt recorded as KEEL-owned,
    so we only remove hooks KEEL actually inserted — not coincidental
    user hooks at the same command path.

    Returns (summary, error). `error` is None on success, else a tuple
    (SETTINGS_FILE, reason) for the caller to append to its errors list.

    On `JSONDecodeError`, if the receipt recorded hook ownership, we
    return a partial-failure error so the caller retains the receipt —
    the user can fix the JSON and re-run to complete uninstall rather
    than losing ownership metadata to a clean-run receipt deletion.
    """
    sp = project_dir / SETTINGS_FILE
    if not sp.exists():
        return "settings.json: absent", None
    receipt_settings = receipt.get("settings_json") or {}
    owned_cmds = list(receipt_settings.get("inserted_hook_commands") or [])
    mode = receipt_settings.get("mode")
    try:
        settings = json.loads(sp.read_text("utf-8"))
    except json.JSONDecodeError as e:
        if owned_cmds:
            return (f"settings.json: invalid JSON ({e}), KEEL hooks may "
                    "remain; fix JSON and retry"), (SETTINGS_FILE,
                                                    f"invalid JSON: {e}")
        return "settings.json: invalid JSON, left untouched", None
    except OSError as e:
        return f"settings.json: read failed ({e})", (SETTINGS_FILE, f"read: {e}")

    # Shape-guard both read paths. `has_non_keel_content` and
    # `remove_hooks_by_command` both call _validate_hooks_shape which
    # raises on valid-JSON-but-weird structures. Treat as partial
    # failure when ownership is recorded so the user can fix and retry.
    def _shape_error(e: Exception):
        msg = (f"settings.json: unexpected shape ({e}), KEEL hooks may "
               "remain; fix and retry") if owned_cmds else (
               f"settings.json: unexpected shape ({e}), left untouched")
        err = (SETTINGS_FILE, f"shape: {e}") if owned_cmds else None
        return msg, err

    try:
        is_keel_only = not ksj.has_non_keel_content(settings, owned_cmds)
    except (ValueError, TypeError) as e:
        return _shape_error(e)

    if mode == "created" and is_keel_only:
        if dry_run:
            return "settings.json: would delete (mode=created, no non-KEEL content)", None
        try:
            sp.unlink()
        except OSError as e:
            return (f"settings.json: delete failed ({e})",
                    (SETTINGS_FILE, f"delete: {e}"))
        return "settings.json: deleted (was KEEL-created)", None

    if not owned_cmds:
        return "settings.json: no KEEL-owned hooks recorded (nothing to remove)", None

    try:
        cleaned, removed = ksj.remove_hooks_by_command(settings, owned_cmds)
    except (ValueError, TypeError) as e:
        return _shape_error(e)
    if removed == 0:
        return "settings.json: no KEEL hooks found (nothing to remove)", None
    if dry_run:
        return f"settings.json: would remove {removed} KEEL hook entries", None
    try:
        sp.write_text(ksj.serialize_stable(cleaned), encoding="utf-8")
    except OSError as e:
        return (f"settings.json: write failed ({e})",
                (SETTINGS_FILE, f"write: {e}"))
    return f"settings.json: removed {removed} KEEL hook entries", None


def _run_receipt_mode(project_dir: Path, receipt: dict, args) -> int:
    schema = receipt.get("receipt_schema_version")
    if schema != RECEIPT_SCHEMA_VERSION:
        print(f"Receipt schema {schema} unknown to this uninstaller "
              f"(expected {RECEIPT_SCHEMA_VERSION}). Aborting.")
        return 10

    to_delete, preserved, missing = _plan_receipt_mode(
        project_dir, receipt, args.purge)
    managed_rels = set(receipt.get("managed_paths", {}).keys())
    orphans = _orphan_scan(project_dir, managed_rels)

    print("=" * 48)
    print("  KEEL — Uninstall (receipt mode)")
    print(f"  Project: {project_dir}")
    print("=" * 48)
    print()
    print(f"  Delete:    {len(to_delete)} paths recorded in receipt")
    print(f"  Preserve:  {len(preserved)} paths modified since install")
    if missing:
        print(f"  Missing:   {len(missing)} paths recorded but gone from disk")
    if orphans:
        print(f"  Advisory:  {len(orphans)} possible orphan keel-* paths not in receipt")
        for o in orphans:
            print(f"             {o}")
        print("             (NOT deleted; review and remove manually if unwanted)")
    print()

    if args.dry_run:
        print("--dry-run: exiting without changes.")
        return 0

    if not args.yes:
        ans = input("Proceed? (y/n): ").strip().lower()
        if ans != "y":
            print("Aborted.")
            return 2

    errors: list[tuple[str, str]] = []
    for rel, _kind in to_delete:
        p = project_dir / rel
        try:
            if p.exists():
                _rmpath(p)
                print(f"  Removed {rel}")
        except OSError as e:
            errors.append((rel, str(e)))

    settings_summary, settings_error = _handle_settings(
        project_dir, receipt, dry_run=False)
    print("  " + settings_summary)
    if settings_error is not None:
        errors.append(settings_error)

    if errors:
        # Rewrite the receipt to contain ONLY the paths that failed
        # deletion plus the preserved entries, so re-running uninstall
        # resumes from this state instead of exiting 1 with "no receipt".
        managed = receipt.get("managed_paths", {})
        remaining_rels = {rel for rel, _ in errors} | {rel for rel, _ in preserved}
        receipt["managed_paths"] = {k: v for k, v in managed.items() if k in remaining_rels}
        kr.save(project_dir, receipt)
    else:
        # Delete receipt before pruning so .claude/ itself can be rmdir'd.
        try:
            (project_dir / RECEIPT_PATH).unlink()
        except OSError:
            pass

    # Clear any Python bytecode cache the bundled uninstaller created
    # while importing its sibling modules — otherwise .claude/__pycache__
    # lingers and blocks the .claude/ rmdir below.
    pycache = project_dir / ".claude" / "__pycache__"
    if pycache.is_dir():
        try:
            shutil.rmtree(pycache)
        except OSError:
            pass

    # Remove now-empty dirs (best-effort)
    for rel in (".claude/agents", ".claude/skills", ".claude/hooks",
                "docs/process", "scripts", "schemas", ".claude"):
        d = project_dir / rel
        try:
            if d.is_dir() and not any(d.iterdir()):
                d.rmdir()
        except OSError:
            pass

    print()
    print("=" * 48)
    # Path-delete errors are a subset of `errors`; `errors` may also
    # include settings.json failures, which are not in `to_delete`.
    # Count deletions as to_delete rels that are NOT in the error set.
    error_rels = {rel for rel, _ in errors}
    deleted_count = sum(1 for rel, _ in to_delete if rel not in error_rels)
    print(f"  Deleted: {deleted_count} paths")
    if errors:
        print(f"  Failed: {len(errors)} (receipt retained; rerun to resume)")
        for rel, reason in errors:
            print(f"    {rel}  ({reason})")
    print(f"  Preserved (modified): {len(preserved)}")
    if preserved:
        receipt_still_exists = (project_dir / RECEIPT_PATH).exists()
        for rel, _ in preserved:
            if receipt_still_exists:
                print(f"    {rel}  (use --purge to force delete)")
            else:
                print(f"    {rel}  (now untracked — delete manually if unwanted)")
    if orphans:
        print(f"  Advisory orphans reported: {len(orphans)} (not touched)")
    print("=" * 48)
    return 20 if errors else 0


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv or sys.argv[1:])
    project_dir = Path.cwd()
    try:
        receipt = kr.load(project_dir)
    except json.JSONDecodeError:
        print("Receipt corrupt (JSON parse error). Refusing to proceed.")
        print("Try: python3 /path/to/install.py --repair-receipt (deferred).")
        return 11
    if receipt is None:
        print(f"No KEEL install receipt at {RECEIPT_PATH}. "
              "Not a KEEL install — nothing to uninstall.")
        return 1
    return _run_receipt_mode(project_dir, receipt, args)


if __name__ == "__main__":
    sys.exit(main())
