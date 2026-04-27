#!/usr/bin/env python3
"""PreToolUse hook: reminds about safety check when editing critical modules.
Fires BEFORE the edit happens — can block with exit 2.

CUSTOMIZE: Update the file pattern matchers below to match your project's
critical modules (the ones where domain invariant violations would be dangerous).

Examples:
    Git project:  */git.ex, */git/*.ex, */repo_server.ex
    API project:  */auth/*, */middleware/*, */db/queries/*
    Data pipeline: */transforms/*, */ingestion/*, */schema/*
"""

import json
import sys

input_data = json.loads(sys.stdin.read()) if not sys.stdin.isatty() else {}
file_path = input_data.get("tool_input", {}).get("file_path", "")

if not file_path:
    sys.exit(0)

# CUSTOMIZE: Replace this pattern with your critical file paths
CRITICAL_PATTERNS = [
    "REPLACE_WITH_YOUR_CRITICAL_PATTERN",
]

if any(pattern in file_path for pattern in CRITICAL_PATTERNS):
    output = {
        "hookSpecificOutput": {
            "hookEventName": "PreToolUse",
            "additionalContext": "KEEL SAFETY: You are editing a file that touches critical domain operations. Run /safety-check before committing.",
        }
    }
    json.dump(output, sys.stdout)
