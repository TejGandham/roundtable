import assert from "node:assert/strict";
import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import { loadRoundtableConfig, roundtableConfigPath } from "../../extensions/pi/config.ts";

function writeConfig(contents: string): string {
  const path = join(mkdtempSync(join(tmpdir(), "roundtable-config-")), "roundtable.json");
  writeFileSync(path, contents);
  return path;
}

test("the registration file lives in the active Pi agent directory", () => {
  assert.equal(roundtableConfigPath({ PI_CODING_AGENT_DIR: "/agents/pi" }), "/agents/pi/roundtable.json");
  assert.match(roundtableConfigPath({}), /\.pi\/agent\/roundtable\.json$/);
});

test("an absent file is the empty registration", () => {
  assert.deepEqual(loadRoundtableConfig("/missing/roundtable.json"), { env: {} });
});

test("a command and env block are read verbatim", () => {
  const path = writeConfig(JSON.stringify({
    command: " /bin/roundtable ",
    env: { ROUNDTABLE_PROVIDERS: "[]", ROUNDTABLE_CLAUDE_PATH: "/bin/claude" },
  }));

  assert.deepEqual(loadRoundtableConfig(path), {
    command: "/bin/roundtable",
    env: { ROUNDTABLE_PROVIDERS: "[]", ROUNDTABLE_CLAUDE_PATH: "/bin/claude" },
  });
});

test("malformed registrations fail closed with the offending file named", () => {
  for (const [contents, expected] of [
    ["{not json", /is not valid JSON/],
    ['["array"]', /must be a JSON object/],
    ['{"provider": "codex"}', /unknown key 'provider'/],
    ['{"command": ""}', /'command' must be a non-empty string/],
    ['{"env": []}', /'env' must be a JSON object/],
    ['{"env": {"ROUNDTABLE_PROVIDERS": 7}}', /env\['ROUNDTABLE_PROVIDERS'\] must be a string/],
  ] as const) {
    const path = writeConfig(contents);
    assert.throws(() => loadRoundtableConfig(path), expected, contents);
  }
});
