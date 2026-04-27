"""Domain logic for feature resolution: backlog parse, invariant 7 classify,
PRD JSON read, feature extraction, JSON Pointer escaping.

Imported by scripts/keel-feature-resolve.py (CLI). Not a bootstrap module.

Design (SOLID):
- Single responsibility per class. Narrow interfaces (Protocols).
- Readers (BacklogSource, PrdJsonSource) separate from classifiers
  (Invariant7Classifier) separate from orchestrator (FeatureResolver).
- Open to extension (new classifier implementations) closed to modification.
- Main CLI depends on abstractions (Protocols + dataclasses), not on
  file-system specifics; tests substitute in-memory sources.

Declared-external deps: `jsonschema` (for structural PRD validation).
The CLI script carries the PEP 723 metadata; this module is imported.
"""
from __future__ import annotations

import json
import re
from dataclasses import dataclass, field
from enum import Enum
from pathlib import Path
from typing import Literal, Protocol, TypedDict

from jsonschema import Draft202012Validator

# --- Constants --------------------------------------------------------------

CANONICAL_PRD_DIR = Path("docs") / "exec-plans" / "prds"
ALLOWED_EXEMPT_REASONS = frozenset({"legacy", "bootstrap", "infra", "trivial"})
SCHEMA_REL = Path("schemas") / "prd.schema.json"

# Grandfather marker on the backlog preamble. Matches:
#   <!-- KEEL-INVARIANT-7: legacy-through=F<N> -->
_GRANDFATHER_RE = re.compile(
    r"<!--\s*KEEL-INVARIANT-7:\s*legacy-through=F(\d+)\s*-->"
)

# Backlog entry field extractors — each pattern matches its field name
# either at start-of-line OR after a pipe, to support KEEL's canonical
# inline format (e.g. `Spec: <path> | Needs: F02, F03`). Values end at
# the next pipe or end-of-line, whichever comes first.
#
# Example canonical entry (from /keel-refine output):
#   - [ ] **F02 Feature title**
#     Spec: path#anchor | Needs: F01
#     PRD: some-slug
_PRD_RE = re.compile(
    r"(?:^|\|)\s*PRD:\s*([a-z0-9](?:[a-z0-9-]*[a-z0-9])?)\s*(?=\||$)",
    re.MULTILINE,
)
_PRD_EXEMPT_RE = re.compile(
    r"(?:^|\|)\s*PRD-exempt:\s*(\S+?)\s*(?=\||$)",
    re.MULTILINE,
)
_SPEC_RE = re.compile(
    r"(?:^|\|)\s*Spec:\s*([^|\n]+?)\s*(?=\||$)",
    re.MULTILINE,
)
_DESIGN_RE = re.compile(
    r"(?:^|\|)\s*Design:\s*([^|\n]+?)\s*(?=\||$)",
    re.MULTILINE,
)
_NEEDS_RE = re.compile(
    r"(?:^|\|)\s*Needs:\s*([^|\n]+?)\s*(?=\||$)",
    re.MULTILINE,
)
_HUMAN_MARKER_RE = re.compile(r"<!--\s*HUMAN:\s*(.+?)\s*-->")


# --- Halt codes -------------------------------------------------------------

class HaltCode(int, Enum):
    """Exit codes the CLI returns. Encode the halt class so the invoker
    can route without parsing the stderr text.

    Stable integers — never reassign; add new codes at the end."""
    OK = 0
    INVOCATION = 2            # bad CLI args, missing file, unreadable
    BACKLOG_NOT_FOUND = 3
    FEATURE_NOT_IN_BACKLOG = 4
    HUMAN_MARKER_UNRESOLVED = 5
    INVARIANT7_XOR = 6        # both PRD: and PRD-exempt:
    INVARIANT7_MULTIPLICITY = 7  # multiple PRD: or multiple PRD-exempt:
    INVARIANT7_EXEMPT_REASON = 8
    INVARIANT7_VIOLATION = 9    # post-cutoff, missing both
    PRD_EXEMPT_NOT_PIPELINE = 10
    PRD_GRANDFATHERED_NO_LINK = 11
    PRD_FORMAT_NOT_JSON = 12
    PRD_PATH_MISMATCH = 13    # supplied != canonical
    PRD_FILE_MISSING = 14
    PRD_SCHEMA_INVALID = 15
    PRD_SLUG_ID_MISMATCH = 16
    FEATURE_NOT_IN_PRD = 17
    FEATURE_DUPLICATE_IN_PRD = 18
    DESIGN_REF_INVALID = 19
    FEATURE_DUPLICATE_IN_BACKLOG = 20  # backlog has >1 entries for same F##


class Classification(str, Enum):
    """Invariant 7 classification outcome. JSON_PRD_PATH is the only
    pipeline-eligible state; all others halt with a specific code."""
    JSON_PRD_PATH = "JSON_PRD_PATH"
    EXEMPT = "EXEMPT"
    GRANDFATHERED_NO_LINK = "GRANDFATHERED_NO_LINK"
    PREADOPTION_NO_LINK = "PREADOPTION_NO_LINK"
    VIOLATION = "VIOLATION"
    XOR_CONFLICT = "XOR_CONFLICT"
    MULTIPLICITY_CONFLICT = "MULTIPLICITY_CONFLICT"


# --- Typed records ----------------------------------------------------------

class Oracle(TypedDict, total=False):
    """Mirrors schema v1 oracle shape. Fields vary in nullability per schema."""
    type: str
    setup: str | None
    actions: list[str]
    assertions: list[str]
    tooling: str
    gating: str


class InvariantRef(TypedDict):
    invariant_id: str
    name: str
    how_exercised: str


@dataclass(slots=True, frozen=True)
class BacklogEntry:
    """Parsed backlog entry for one F## feature."""
    feature_id: str         # e.g. "F04"
    feature_id_num: int     # e.g. 4
    prd_slugs: tuple[str, ...]
    prd_exempt_reasons: tuple[str, ...]
    spec_ref: str | None
    design_refs: tuple[str, ...]
    needs_ids: tuple[str, ...]
    human_markers: tuple[str, ...]
    raw_block: str          # the raw text block this entry came from


@dataclass(slots=True, frozen=True)
class ClassificationResult:
    classification: Classification
    halt_code: HaltCode
    halt_message: str | None   # None when classification is JSON_PRD_PATH


@dataclass(slots=True, frozen=True)
class Halt:
    """A non-OK outcome. Encapsulates exit code + human-readable CTA."""
    code: HaltCode
    message: str


@dataclass(slots=True, frozen=True)
class FeatureResolution:
    """Successful pipeline-ready resolution of a feature.

    Emitted as stdout JSON on exit code 0.

    Note on `prd_invariants_exercised`: this is the PRD-level
    `invariants_exercised` array carried verbatim from the PRD root
    (schemas/prd.schema.json places this field at the PRD level, not
    per-feature). Consumers that need to decide *which features* are
    exercising which invariants must inspect the contract/oracle of
    the specific feature, not treat this array as a per-feature claim.
    Named with the `prd_` prefix to prevent that misreading downstream.
    """
    feature_id: str
    feature_index: int
    feature_pointer_base: str
    prd_path: str
    canonical_prd_path: str
    title: str
    layer: str
    oracle: dict
    contract: dict
    needs: list[str]
    prd_invariants_exercised: list[dict]
    backlog_fields: dict
    classification: str


# --- Protocols (Interface Segregation) --------------------------------------

class BacklogSource(Protocol):
    """Narrow interface: read backlog text + parse into entries.

    `grandfather_cutoff` takes the already-read text so callers don't
    pay a second disk round-trip per resolve() call."""
    def read(self) -> str: ...
    def grandfather_cutoff(self, text: str) -> int | None: ...


class PrdJsonSource(Protocol):
    """Narrow interface: read a single PRD JSON file + normalize path."""
    def canonical_path(self, slug: str) -> Path: ...
    def read_json(self, path: Path) -> dict: ...
    def exists(self, path: Path) -> bool: ...
    def resolve(self, path: Path) -> Path: ...


# --- Concrete file-backed sources ------------------------------------------

@dataclass(slots=True, frozen=True)
class FileBacklogSource:
    """BacklogSource backed by a filesystem path."""
    backlog_path: Path

    def read(self) -> str:
        return self.backlog_path.read_text(encoding="utf-8")

    def grandfather_cutoff(self, text: str) -> int | None:
        match = _GRANDFATHER_RE.search(text)
        return int(match.group(1)) if match else None


@dataclass(slots=True, frozen=True)
class FilePrdJsonSource:
    """PrdJsonSource backed by the filesystem at repo_root/<CANONICAL_PRD_DIR>."""
    repo_root: Path

    def canonical_path(self, slug: str) -> Path:
        return (self.repo_root / CANONICAL_PRD_DIR / f"{slug}.json").resolve()

    def read_json(self, path: Path) -> dict:
        return json.loads(path.read_text(encoding="utf-8"))

    def exists(self, path: Path) -> bool:
        return path.is_file()

    def resolve(self, path: Path) -> Path:
        return path.resolve()


# --- Backlog parsing (Single Responsibility) -------------------------------

class BacklogParser:
    """Parses the backlog file to extract one feature's entry."""

    _FEATURE_ID_RE = re.compile(r"^F(\d+)$")
    # A backlog entry block starts at `- [ ] F##` or `- [x] F##`, with
    # an optional `**` bold-wrap around the title (KEEL's canonical
    # /keel-refine output is `- [ ] **F## Title**`). Extends until the
    # next such marker or end of file.
    _ENTRY_START_RE = re.compile(
        r"^[-*]\s+\[[ xX]\]\s+(?:\*\*)?F(\d+)\b",
        re.MULTILINE,
    )

    def parse_entry(self, backlog_text: str, feature_id: str) -> BacklogEntry | None | Halt:
        """Return the BacklogEntry for `feature_id`, or None if not present,
        or a Halt if the backlog has duplicate entries for the same F##.

        `feature_id` must be in the form `F##` (e.g. `F04`, `F123`)."""
        m = self._FEATURE_ID_RE.fullmatch(feature_id)
        if not m:
            return None
        target_num = int(m.group(1))

        # Find all entry starts and their positions.
        starts = list(self._ENTRY_START_RE.finditer(backlog_text))
        if not starts:
            return None

        # Collect ALL matching blocks — halt if more than one (duplicate
        # F## entries with different PRD/exempt markers would silently
        # use the first otherwise).
        matching_blocks: list[tuple[int, str]] = []
        for i, start_match in enumerate(starts):
            entry_num = int(start_match.group(1))
            if entry_num != target_num:
                continue
            block_start = start_match.start()
            block_end = starts[i + 1].start() if i + 1 < len(starts) else len(backlog_text)
            matching_blocks.append((block_start, backlog_text[block_start:block_end]))

        if not matching_blocks:
            return None
        if len(matching_blocks) > 1:
            offsets = [off for off, _ in matching_blocks]
            return Halt(
                HaltCode.FEATURE_DUPLICATE_IN_BACKLOG,
                (
                    f"Backlog has {len(matching_blocks)} entries for "
                    f"feature `{feature_id}` (at byte offsets {offsets}). "
                    f"Each F## ID must appear exactly once. Fix: "
                    f"consolidate the duplicate entries into one, or "
                    f"rename one of them."
                ),
            )
        target_block = matching_blocks[0][1]

        prd_slugs = tuple(m.group(1) for m in _PRD_RE.finditer(target_block))
        prd_exempt_reasons = tuple(
            m.group(1) for m in _PRD_EXEMPT_RE.finditer(target_block)
        )
        spec_match = _SPEC_RE.search(target_block)
        spec_ref = spec_match.group(1) if spec_match else None
        design_match = _DESIGN_RE.search(target_block)
        design_refs: tuple[str, ...] = ()
        if design_match:
            design_refs = tuple(
                r.strip() for r in design_match.group(1).split(",") if r.strip()
            )
        needs_match = _NEEDS_RE.search(target_block)
        needs_ids: tuple[str, ...] = ()
        if needs_match:
            needs_ids = tuple(
                n.strip() for n in needs_match.group(1).split(",") if n.strip()
            )
        human_markers = tuple(
            m.group(1).strip() for m in _HUMAN_MARKER_RE.finditer(target_block)
        )

        return BacklogEntry(
            feature_id=feature_id,
            feature_id_num=target_num,
            prd_slugs=prd_slugs,
            prd_exempt_reasons=prd_exempt_reasons,
            spec_ref=spec_ref,
            design_refs=design_refs,
            needs_ids=needs_ids,
            human_markers=human_markers,
            raw_block=target_block,
        )


# --- Invariant 7 classification (Single Responsibility) --------------------

class Invariant7Classifier:
    """Classifies a BacklogEntry against invariant 7, given a grandfather cutoff.

    Returns a ClassificationResult. Does not read files or the PRD JSON — that
    is a separate concern handled by the PRD resolver."""

    def classify(
        self,
        entry: BacklogEntry,
        grandfather_cutoff: int | None,
    ) -> ClassificationResult:
        prds = entry.prd_slugs
        exempts = entry.prd_exempt_reasons

        # XOR conflict — both fields present.
        if prds and exempts:
            return ClassificationResult(
                Classification.XOR_CONFLICT,
                HaltCode.INVARIANT7_XOR,
                (
                    f"{entry.feature_id} has both `PRD:` and `PRD-exempt:` "
                    f"lines. Mutually exclusive — pick one. Remove the "
                    f"`PRD-exempt:` line if this feature has a PRD, or "
                    f"remove the `PRD:` line if this is genuinely exempt."
                ),
            )

        # Multiplicity — multiple PRD: or multiple PRD-exempt:.
        if len(prds) > 1 or len(exempts) > 1:
            return ClassificationResult(
                Classification.MULTIPLICITY_CONFLICT,
                HaltCode.INVARIANT7_MULTIPLICITY,
                (
                    f"{entry.feature_id} has multiple `PRD:` or "
                    f"`PRD-exempt:` lines. Only one of each is allowed. "
                    f"Consolidate to a single `PRD: <slug>` or "
                    f"`PRD-exempt: <reason>` line."
                ),
            )

        # PRD present — eligible for pipeline.
        if prds:
            return ClassificationResult(
                Classification.JSON_PRD_PATH,
                HaltCode.OK,
                None,
            )

        # PRD-exempt only — not pipeline-eligible.
        if exempts:
            reason = exempts[0]
            if reason not in ALLOWED_EXEMPT_REASONS:
                return ClassificationResult(
                    Classification.EXEMPT,
                    HaltCode.INVARIANT7_EXEMPT_REASON,
                    (
                        f"{entry.feature_id} declares `PRD-exempt:` with "
                        f"reason `{reason}`; must be one of "
                        f"{sorted(ALLOWED_EXEMPT_REASONS)}."
                    ),
                )
            return ClassificationResult(
                Classification.EXEMPT,
                HaltCode.PRD_EXEMPT_NOT_PIPELINE,
                (
                    f"{entry.feature_id} is declared `PRD-exempt: {reason}`. "
                    f"Exempt features do not flow through `/keel-pipeline` "
                    f"(which reads only structured JSON PRDs). Either run "
                    f"`/keel-refine` to promote this feature to a structured "
                    f"PRD (replacing `PRD-exempt:` with `PRD: <slug>`), or "
                    f"handle the work outside the pipeline."
                ),
            )

        # Neither PRD: nor PRD-exempt:. Apply grandfather rules.
        if grandfather_cutoff is None:
            # Pre-adoption. Pipeline still requires a PRD.
            return ClassificationResult(
                Classification.PREADOPTION_NO_LINK,
                HaltCode.PRD_GRANDFATHERED_NO_LINK,
                (
                    f"{entry.feature_id} has neither `PRD:` nor "
                    f"`PRD-exempt:`. `/keel-pipeline` requires a "
                    f"`PRD: <slug>` link pointing at a structured JSON PRD. "
                    f"Run `/keel-refine` to author the PRD and backlog link, "
                    f"then re-invoke `/keel-pipeline`."
                ),
            )

        if entry.feature_id_num <= grandfather_cutoff:
            # Grandfathered; still not pipeline-eligible (no PRD).
            return ClassificationResult(
                Classification.GRANDFATHERED_NO_LINK,
                HaltCode.PRD_GRANDFATHERED_NO_LINK,
                (
                    f"{entry.feature_id} is grandfathered pre-invariant-7 "
                    f"and carries no `PRD:` link. `/keel-pipeline` requires "
                    f"a structured JSON PRD. Run `/keel-refine` to author a "
                    f"PRD for this feature (it will add `PRD: <slug>` to the "
                    f"backlog entry), then re-invoke `/keel-pipeline`."
                ),
            )

        # Post-cutoff with neither field — invariant 7 violation.
        return ClassificationResult(
            Classification.VIOLATION,
            HaltCode.INVARIANT7_VIOLATION,
            (
                f"{entry.feature_id} is past the legacy cutoff "
                f"F{grandfather_cutoff:02d} and must carry either "
                f"`PRD: <slug>` or `PRD-exempt: <reason>` (reason: "
                f"legacy / bootstrap / infra / trivial). Run "
                f"`/keel-refine` to author the PRD and backlog link."
            ),
        )


# --- JSON Pointer (RFC 6901) ------------------------------------------------

def jsonptr_escape_segment(segment: str) -> str:
    """Escape one path segment per RFC 6901: `~` → `~0`, `/` → `~1`.

    Order matters: `~` MUST be escaped first, else `/` escapes would be
    double-encoded."""
    return segment.replace("~", "~0").replace("/", "~1")


def jsonptr_build(*segments: str | int) -> str:
    """Build a JSON Pointer from segments. Numeric segments are stringified.

    Examples:
        jsonptr_build("features", 0, "contract", "channel")
          → "/features/0/contract/channel"
        jsonptr_build("features", 0, "contract", "header/x-api-key")
          → "/features/0/contract/header~1x-api-key"
    """
    parts: list[str] = []
    for seg in segments:
        if isinstance(seg, int):
            parts.append(str(seg))
        else:
            parts.append(jsonptr_escape_segment(seg))
    return "/" + "/".join(parts)


# --- PRD resolver + schema validation --------------------------------------

@dataclass(slots=True, frozen=True)
class PrdLoadResult:
    """Intermediate product: the PRD JSON + resolved canonical path."""
    doc: dict
    canonical_path: Path


class PrdValidator:
    """Validates a PRD JSON: JSON Schema, then cross-reference integrity.

    Mirrors scripts/validate-prd-json.py's two-stage validation: schema
    first (short-circuits on failure), then XrefValidator for
    features[].needs[] and duplicate feature IDs. Duplicated here rather
    than shelled-out because keel-feature-resolve.py is called
    per-feature inside the pipeline hot path; a subprocess round-trip
    per call is unnecessary overhead.
    """

    def __init__(self, schema: dict) -> None:
        self._validator = Draft202012Validator(schema)

    def validate(self, doc: dict, prd_path: str) -> Halt | None:
        # Stage 1: JSON Schema.
        errors = sorted(
            self._validator.iter_errors(doc),
            key=lambda e: list(e.absolute_path),
        )
        if errors:
            lines = [f"PRD schema validation failed for {prd_path}:"]
            for err in errors:
                path = "/" + "/".join(str(p) for p in err.absolute_path)
                lines.append(f"  {path}: {err.message.splitlines()[0]}")
            lines.append(
                "\nFix: correct each listed finding in the PRD file. If the "
                "PRD was authored by /keel-refine, re-run /keel-refine to "
                "regenerate; if hand-edited, repair the specific fields. "
                "Then re-invoke /keel-pipeline."
            )
            return Halt(HaltCode.PRD_SCHEMA_INVALID, "\n".join(lines))

        # Stage 2: cross-reference integrity (dangling needs, duplicate IDs,
        # self-deps). Runs only after schema validation passes.
        xref_errors = self._check_xrefs(doc)
        if xref_errors:
            lines = [f"PRD cross-reference validation failed for {prd_path}:"]
            lines.extend(f"  {err}" for err in xref_errors)
            lines.append(
                "\nFix: correct the cross-references in the PRD. Dangling "
                "`needs[]` entries mean a referenced feature doesn't exist "
                "in this PRD (fix the reference or add the feature). "
                "Self-dependency means a feature lists its own ID in "
                "`needs[]` (remove it). Duplicate feature IDs must be "
                "consolidated or renamed."
            )
            return Halt(HaltCode.PRD_SCHEMA_INVALID, "\n".join(lines))

        return None

    def _check_xrefs(self, doc: dict) -> list[str]:
        """Post-schema cross-reference integrity checks."""
        errors: list[str] = []
        features = doc.get("features", [])
        if not isinstance(features, list):
            return errors  # schema stage would have caught this
        known_ids = {
            f["id"] for f in features
            if isinstance(f, dict) and isinstance(f.get("id"), str)
        }
        seen_ids: dict[str, list[int]] = {}
        for i, feature in enumerate(features):
            if not isinstance(feature, dict):
                continue
            fid = feature.get("id")
            needs = feature.get("needs", [])
            if isinstance(needs, list):
                for need in needs:
                    if isinstance(need, str) and need not in known_ids:
                        errors.append(
                            f"/features/{i}/needs: '{need}' does not "
                            f"resolve to any feature in this PRD"
                        )
                if isinstance(fid, str) and fid in needs:
                    errors.append(
                        f"/features/{i}/needs: feature '{fid}' declares "
                        f"itself as a dependency"
                    )
            if isinstance(fid, str):
                seen_ids.setdefault(fid, []).append(i)
        for fid, positions in seen_ids.items():
            if len(positions) > 1:
                first = positions[0]
                for dup_pos in positions[1:]:
                    errors.append(
                        f"/features/{dup_pos}/id: duplicate feature id "
                        f"'{fid}' (already declared at /features/{first}/id)"
                    )
        return errors


def load_schema(repo_root: Path) -> dict:
    """Load `schemas/prd.schema.json` strictly from repo_root.

    Does NOT walk up the tree — if repo_root is a subdirectory of the
    real repo (e.g. agent invoked from `scripts/`), we want the
    INVOCATION halt to fire rather than silently succeeding on a
    grandparent's schema while `FilePrdJsonSource` resolves PRD paths
    against the wrong root (which would then surface as a confusing
    `PRD_FILE_MISSING` downstream).

    Symlinks are rejected to prevent schema hijacks in vendored/monorepo
    layouts where `schemas/` could be a symlink to an unexpected
    location.
    """
    schema_path = repo_root / SCHEMA_REL
    if not schema_path.is_file():
        raise FileNotFoundError(
            f"Could not locate {SCHEMA_REL} at {schema_path}."
        )
    if schema_path.is_symlink():
        raise FileNotFoundError(
            f"{schema_path} is a symlink; resolve and commit the real "
            f"file in-tree to prevent schema hijack."
        )
    return json.loads(schema_path.read_text(encoding="utf-8"))


# --- Feature extraction (Single Responsibility) ----------------------------

@dataclass(slots=True, frozen=True)
class FeatureExtraction:
    """The per-feature product of an already-validated PRD."""
    feature_index: int
    title: str
    layer: str
    oracle: dict
    contract: dict
    needs: list[str]


class FeatureExtractor:
    """Extracts per-feature fields from a validated PRD JSON document.

    Count-gates on the feature ID: must be exactly one match."""

    def extract(self, doc: dict, feature_id: str) -> FeatureExtraction | Halt:
        features = doc.get("features")
        if not isinstance(features, list):
            # Schema validation should have caught this; defensive guard.
            return Halt(
                HaltCode.PRD_SCHEMA_INVALID,
                "PRD `features` is not an array after schema validation — "
                "this indicates a validator bug. Re-run `validate-prd-json.py` "
                "directly to reproduce.",
            )

        matches = [
            (i, f) for i, f in enumerate(features)
            if isinstance(f, dict) and f.get("id") == feature_id
        ]
        if len(matches) == 0:
            return Halt(
                HaltCode.FEATURE_NOT_IN_PRD,
                (
                    f"Feature `{feature_id}` not present in PRD. Either add "
                    f"the feature object to `features[]` with "
                    f"`id: \"{feature_id}\"`, or correct the backlog entry's "
                    f"ID to match an existing feature."
                ),
            )
        if len(matches) > 1:
            return Halt(
                HaltCode.FEATURE_DUPLICATE_IN_PRD,
                (
                    f"Duplicate feature ID `{feature_id}` in PRD. Feature "
                    f"IDs must be unique per PRD — remove duplicates. (The "
                    f"XrefValidator should have caught this; if you see "
                    f"this message, flag a validator bug.)"
                ),
            )

        idx, feat = matches[0]
        return FeatureExtraction(
            feature_index=idx,
            title=feat.get("title", ""),
            layer=feat.get("layer", ""),
            oracle=feat.get("oracle", {}),
            contract=feat.get("contract", {}),
            needs=list(feat.get("needs", [])),
        )


# --- Orchestrator -----------------------------------------------------------

@dataclass(slots=True, frozen=True)
class ResolveRequest:
    """Input to FeatureResolver.resolve — one feature + optional PRD path."""
    repo_root: Path
    backlog_path: Path
    feature_id: str
    supplied_prd_path: Path | None


class FeatureResolver:
    """Orchestrates backlog → classification → PRD → feature-extraction,
    short-circuiting on the first halt. Single responsibility: orchestration."""

    def __init__(
        self,
        backlog_parser: BacklogParser,
        classifier: Invariant7Classifier,
        prd_source: PrdJsonSource,
        schema: dict,
    ) -> None:
        self._backlog_parser = backlog_parser
        self._classifier = classifier
        self._prd_source = prd_source
        self._prd_validator = PrdValidator(schema)
        self._feature_extractor = FeatureExtractor()

    def resolve(
        self,
        req: ResolveRequest,
        backlog_source: BacklogSource,
    ) -> FeatureResolution | Halt:
        # --- Stage 1: backlog
        if not req.backlog_path.is_file():
            return Halt(
                HaltCode.BACKLOG_NOT_FOUND,
                (
                    f"Backlog not found at {req.backlog_path}. "
                    f"Fix: pass --backlog with the correct path (default "
                    f"lives at docs/exec-plans/active/feature-backlog.md), "
                    f"or initialize the backlog via /keel-adopt or "
                    f"/keel-setup if this is a new project."
                ),
            )
        backlog_text = backlog_source.read()
        parse_result = self._backlog_parser.parse_entry(backlog_text, req.feature_id)
        if isinstance(parse_result, Halt):
            return parse_result
        if parse_result is None:
            return Halt(
                HaltCode.FEATURE_NOT_IN_BACKLOG,
                (
                    f"Feature `{req.feature_id}` not found in backlog at "
                    f"{req.backlog_path}. Fix: add a `- [ ] {req.feature_id}: "
                    f"...` entry to the backlog, or correct the feature ID "
                    f"passed to `--feature`."
                ),
            )
        entry = parse_result

        if entry.human_markers:
            markers = "; ".join(entry.human_markers)
            # Substitute the concrete slug when the entry has exactly
            # one — gives the user a copy-pasteable invocation. Other
            # slug counts (zero, multiple) are classified as halts at
            # Stage 2, so the fallback placeholder only surfaces when
            # those classifier halts were suppressed upstream.
            refine_target = (
                entry.prd_slugs[0] if len(entry.prd_slugs) == 1 else "<slug>"
            )
            return Halt(
                HaltCode.HUMAN_MARKER_UNRESOLVED,
                (
                    f"{req.feature_id} carries unresolved `<!-- HUMAN: ... -->` "
                    f"marker(s): {markers}. Drafts must be resolved before "
                    f"pipeline entry. Run `/keel-refine {refine_target}` to "
                    f"walk the open question as a card — the skill auto-enters "
                    f"re-run mode when the PRD exists on disk. Then re-invoke "
                    f"`/keel-pipeline`."
                ),
            )

        # --- Stage 2: invariant 7 classification (reuses already-read text)
        cutoff = backlog_source.grandfather_cutoff(backlog_text)
        classification = self._classifier.classify(entry, cutoff)
        if classification.classification is not Classification.JSON_PRD_PATH:
            assert classification.halt_message is not None
            return Halt(classification.halt_code, classification.halt_message)

        slug = entry.prd_slugs[0]
        canonical = self._prd_source.canonical_path(slug)

        # --- Stage 3: PRD-format gate
        if req.supplied_prd_path is not None:
            # Resolve relative --prd against the repo root, not cwd, so
            # `--repo /x/repo --prd docs/exec-plans/prds/y.json` matches
            # the canonical path regardless of where the CLI was invoked.
            supplied = req.supplied_prd_path
            if not supplied.is_absolute():
                supplied = req.repo_root / supplied
            supplied_resolved = self._prd_source.resolve(supplied)
            if supplied_resolved.suffix != ".json":
                return Halt(
                    HaltCode.PRD_FORMAT_NOT_JSON,
                    (
                        f"PRD path `{req.supplied_prd_path}` is not a "
                        f"structured JSON PRD. `/keel-pipeline` reads JSON "
                        f"only. Run `/keel-refine` to produce "
                        f"`docs/exec-plans/prds/{slug}.json`, then re-invoke."
                    ),
                )
            if supplied_resolved != canonical:
                return Halt(
                    HaltCode.PRD_PATH_MISMATCH,
                    (
                        f"PRD path supplied (`{req.supplied_prd_path}`) does "
                        f"not match the canonical path for slug `{slug}` "
                        f"(`{canonical}`). Use the canonical path, or "
                        f"reconcile the backlog's `PRD:` slug."
                    ),
                )

        if not self._prd_source.exists(canonical):
            return Halt(
                HaltCode.PRD_FILE_MISSING,
                (
                    f"PRD file `{canonical}` does not exist. Run "
                    f"`/keel-refine` to author the structured JSON PRD, "
                    f"then re-invoke `/keel-pipeline`."
                ),
            )

        # --- Stage 4: read + schema-validate PRD
        try:
            doc = self._prd_source.read_json(canonical)
        except (OSError, json.JSONDecodeError, UnicodeDecodeError) as e:
            return Halt(
                HaltCode.PRD_SCHEMA_INVALID,
                (
                    f"PRD at `{canonical}` is not readable UTF-8 JSON: {e}. "
                    f"Fix: repair the file to be valid UTF-8 JSON. If "
                    f"authored by /keel-refine, re-run /keel-refine to "
                    f"regenerate. If hand-edited, check for unterminated "
                    f"strings, trailing commas, or encoding issues."
                ),
            )

        schema_halt = self._prd_validator.validate(doc, str(canonical))
        if schema_halt is not None:
            return schema_halt

        # --- Stage 5: slug/id cross-check
        doc_id = doc.get("id")
        if doc_id != slug:
            return Halt(
                HaltCode.PRD_SLUG_ID_MISMATCH,
                (
                    f"PRD slug mismatch: backlog says `PRD: {slug}` but the "
                    f"JSON file's `.id` is `{doc_id}`. Either rename the "
                    f"backlog slug, rename the JSON's `.id`, or correct the "
                    f"PRD file path."
                ),
            )

        # --- Stage 6: design-ref validation (guards: no URL, no absolute
        # paths, no escape-from-repo via `..` traversal or out-of-tree
        # symlink target)
        repo_root_resolved = req.repo_root.resolve()
        for ref in entry.design_refs:
            if ref.startswith(("http://", "https://")):
                return Halt(
                    HaltCode.DESIGN_REF_INVALID,
                    (
                        f"Design reference `{ref}` on backlog entry "
                        f"`{req.feature_id}` is an external URL. Design "
                        f"refs must be committed files. Commit the design "
                        f"to the repo and update the `Design:` field."
                    ),
                )
            ref_path_raw = Path(ref)
            if ref_path_raw.is_absolute():
                return Halt(
                    HaltCode.DESIGN_REF_INVALID,
                    (
                        f"Design reference `{ref}` on backlog entry "
                        f"`{req.feature_id}` is an absolute path. Design "
                        f"refs must be repo-relative. Fix: rewrite as a "
                        f"path relative to the repo root."
                    ),
                )
            ref_path = (req.repo_root / ref).resolve()
            if not ref_path.is_file():
                return Halt(
                    HaltCode.DESIGN_REF_INVALID,
                    (
                        f"Design reference `{ref}` on backlog entry "
                        f"`{req.feature_id}` does not resolve to a committed "
                        f"file under the repo. Commit the file or correct "
                        f"the `Design:` field."
                    ),
                )
            if not ref_path.is_relative_to(repo_root_resolved):
                return Halt(
                    HaltCode.DESIGN_REF_INVALID,
                    (
                        f"Design reference `{ref}` on backlog entry "
                        f"`{req.feature_id}` resolves outside the repo "
                        f"(to `{ref_path}`). Design refs must be committed "
                        f"files under the repo — no `..` traversal or "
                        f"symlinks pointing outside the tree."
                    ),
                )

        # --- Stage 7: feature extraction
        extraction = self._feature_extractor.extract(doc, req.feature_id)
        if isinstance(extraction, Halt):
            return extraction

        # --- Stage 8: assemble result
        return FeatureResolution(
            feature_id=req.feature_id,
            feature_index=extraction.feature_index,
            feature_pointer_base=jsonptr_build("features", extraction.feature_index),
            # Both `prd_path` and `canonical_prd_path` are resolved
            # absolute paths so downstream agents (who forward these
            # verbatim in handoff briefs) see a stable form regardless
            # of whether the caller supplied --prd.
            prd_path=str(canonical),
            canonical_prd_path=str(canonical),
            title=extraction.title,
            layer=extraction.layer,
            oracle=extraction.oracle,
            contract=extraction.contract,
            needs=extraction.needs,
            prd_invariants_exercised=list(doc.get("invariants_exercised", [])),
            backlog_fields={
                "prd_slug": slug,
                "prd_exempt_reason": None,
                "spec_ref": entry.spec_ref,
                "design_refs": list(entry.design_refs),
                "needs_ids": list(entry.needs_ids),
                "human_markers": list(entry.human_markers),
            },
            classification=classification.classification.value,
        )
