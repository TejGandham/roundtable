import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import { textFromRoundtableResult } from "../../extensions/pi/index.ts";

test("returns MCP text without wrapping the panel JSON", () => {
  const panel = '{"codex":{"status":"ok","response":"reviewed"}}';
  assert.equal(textFromRoundtableResult({ content: [{ type: "text", text: panel }] }), panel);
});

test("falls back to structured content when an MCP response has no text", () => {
  assert.equal(
    textFromRoundtableResult({ content: [], structuredContent: { status: "ok" } }),
    '{"status":"ok"}',
  );
});

test("the Pi package exposes one native extension and the canonical skill", async () => {
  const packageJson = JSON.parse(await readFile(new URL("../../package.json", import.meta.url), "utf8"));
  assert.deepEqual(packageJson.pi.extensions, ["./extensions/pi/index.ts"]);
  assert.deepEqual(packageJson.pi.skills, ["./skills/roundtable"]);
  assert.equal(packageJson.dependencies["@modelcontextprotocol/sdk"], "1.30.0");
  assert.equal(packageJson.engines.node, ">=22.19.0");
});
