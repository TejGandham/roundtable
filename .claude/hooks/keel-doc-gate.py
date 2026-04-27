#!/usr/bin/env python3
"""PostToolUse hook: reminds Claude to run doc-gardener after git commits.
Fires AFTER the commit succeeds.
"""

import json
import sys

input_data = json.loads(sys.stdin.read()) if not sys.stdin.isatty() else {}
command = input_data.get("tool_input", {}).get("command", "")

if not command:
    sys.exit(0)

if "git commit" in command:
    output = {
        "hookSpecificOutput": {
            "hookEventName": "PostToolUse",
            "additionalContext": "KEEL DOCS: Commit detected. Run doc-gardener agent to check for doc drift. (north-star.md: Garbage Collection)",
        }
    }
    json.dump(output, sys.stdout)
