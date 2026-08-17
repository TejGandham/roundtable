import assert from "node:assert/strict";
import { mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { fileURLToPath } from "node:url";
import test from "node:test";

import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";

import roundtablePiExtension from "../../extensions/pi/index.ts";

interface CapturedTool {
  name: string;
  execute: (
    toolCallId: string,
    parameters: Record<string, unknown>,
    signal: AbortSignal,
    onUpdate: undefined,
    context: { cwd: string },
  ) => Promise<{ content: Array<{ type: string; text: string }> }>;
}

test("the native Pi tool crosses stdio MCP and closes on session shutdown", async () => {
  const originalBinary = process.env.ROUNDTABLE_BIN;
  const originalAgentDir = process.env.PI_CODING_AGENT_DIR;
  process.env.ROUNDTABLE_BIN = fileURLToPath(new URL("./fixtures/fake-roundtable.mjs", import.meta.url));
  process.env.PI_CODING_AGENT_DIR = mkdtempSync(`${tmpdir()}/roundtable-agent-`);

  const tools = new Map<string, CapturedTool>();
  const handlers = new Map<string, () => Promise<void>>();
  const pi = {
    registerTool(tool: CapturedTool) {
      tools.set(tool.name, tool);
    },
    on(event: string, handler: () => Promise<void>) {
      handlers.set(event, handler);
    },
  } as unknown as ExtensionAPI;

  try {
    roundtablePiExtension(pi);
    assert.deepEqual([...tools.keys()], [
      "roundtable-canvass",
      "roundtable-deliberate",
      "roundtable-blueprint",
      "roundtable-critique",
      "roundtable-crosscheck",
      "roundtable-converge",
    ]);

    const result = await tools.get("roundtable-critique")?.execute(
      "call-1",
      { prompt: "review this", timeout: 900 },
      new AbortController().signal,
      undefined,
      { cwd: fileURLToPath(new URL("../..", import.meta.url)) },
    );

    assert.match(result?.content[0]?.text ?? "", /PI_PORT_OK:roundtable-critique/);
    await handlers.get("session_shutdown")?.();
  } finally {
    if (originalBinary === undefined) delete process.env.ROUNDTABLE_BIN;
    else process.env.ROUNDTABLE_BIN = originalBinary;
    if (originalAgentDir === undefined) delete process.env.PI_CODING_AGENT_DIR;
    else process.env.PI_CODING_AGENT_DIR = originalAgentDir;
  }
});
