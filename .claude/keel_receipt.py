# scripts/keel_receipt.py
"""KEEL install receipt: read/write/hash helpers.

Stdlib-only. No third-party imports.
"""
from __future__ import annotations

import hashlib
import json
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

from keel_manifest import RECEIPT_PATH, RECEIPT_SCHEMA_VERSION


def normalize_bytes(raw: bytes) -> bytes:
    """Normalize for hash comparison: LF line endings, no trailing
    whitespace per line. Content inside lines is preserved verbatim.
    """
    text = raw.replace(b"\r\n", b"\n").replace(b"\r", b"\n")
    lines = [line.rstrip(b" \t") for line in text.split(b"\n")]
    return b"\n".join(lines)


def hash_file(path: Path) -> str:
    """Return `sha256:<hex>` of the file's normalized content."""
    raw = path.read_bytes()
    h = hashlib.sha256(normalize_bytes(raw)).hexdigest()
    return f"sha256:{h}"


def hash_dir(path: Path) -> str:
    """Return `sha256:<hex>` of a directory's contents.

    Deterministic: sorted relative paths, each file's normalized bytes
    preceded by its path and a null terminator.
    """
    h = hashlib.sha256()
    files = sorted(p for p in path.rglob("*") if p.is_file())
    for f in files:
        rel = f.relative_to(path).as_posix().encode("utf-8")
        h.update(rel + b"\0")
        h.update(normalize_bytes(f.read_bytes()))
        h.update(b"\0")
    return f"sha256:{h.hexdigest()}"


def new_receipt(
    keel_version: str,
    *,
    git_sha: str | None = None,
    git_dirty: bool = False,
    source_url: str | None = None,
    optional_features: dict[str, bool] | None = None,
) -> dict[str, Any]:
    return {
        "receipt_schema_version": RECEIPT_SCHEMA_VERSION,
        "keel_version": keel_version,
        "installed_at": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
        "git_sha": git_sha,
        "git_dirty": git_dirty,
        "source_url": source_url,
        "optional_features": dict(optional_features or {}),
        "managed_paths": {},
        "skipped_paths": [],
        "settings_json": None,
    }


def set_managed_path(receipt: dict, path: str, *, kind: str, hash: str) -> None:
    receipt["managed_paths"][path] = {"installed_hash": hash, "kind": kind}


def record_skipped(receipt: dict, path: str, reason: str) -> None:
    receipt["skipped_paths"].append({"path": path, "reason": reason})


def set_settings(receipt: dict, *, mode: str,
                 inserted_commands: list[str]) -> None:
    """Record settings.json ownership on the receipt.

    Receipts record exact command strings KEEL inserted into
    `.claude/settings.json`; uninstall removes them by exact match.
    """
    receipt["settings_json"] = {
        "mode": mode,
        "inserted_hook_commands": list(inserted_commands),
    }


def _receipt_path(project_dir: Path) -> Path:
    return project_dir / RECEIPT_PATH


def load(project_dir: Path) -> dict[str, Any] | None:
    rp = _receipt_path(project_dir)
    if not rp.exists():
        return None
    return json.loads(rp.read_text("utf-8"))


def save(project_dir: Path, receipt: dict[str, Any]) -> None:
    rp = _receipt_path(project_dir)
    rp.parent.mkdir(parents=True, exist_ok=True)
    # Stable serialization: sorted keys, 2-space indent, trailing newline.
    text = json.dumps(receipt, indent=2, sort_keys=True) + "\n"
    rp.write_text(text, encoding="utf-8")
