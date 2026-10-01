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

const EXPECTED_TOOLS = [
  "roundtable-canvass",
  "roundtable-deliberate",
  "roundtable-blueprint",
  "roundtable-critique",
  "roundtable-crosscheck",
  "roundtable-converge",
];

function restoreEnv(name: string, value: string | undefined): void {
  if (value === undefined) delete process.env[name];
  else process.env[name] = value;
}

function fakePi(): {
  pi: ExtensionAPI;
  tools: Map<string, CapturedTool>;
  handlers: Map<string, () => Promise<void>>;
} {
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
  return { pi, tools, handlers };
}

test("a roundtable-spawned pi (ROUNDTABLE_ACTIVE=1) registers no tools, no handlers, and writes nothing", () => {
  const originalActive = process.env.ROUNDTABLE_ACTIVE;
  const originalLog = console.log;
  const originalWarn = console.warn;
  const originalError = console.error;
  const logged: unknown[][] = [];
  const warned: unknown[][] = [];
  const errored: unknown[][] = [];
  process.env.ROUNDTABLE_ACTIVE = "1";
  const { pi, tools, handlers } = fakePi();

  try {
    console.log = (...args: unknown[]) => { logged.push(args); };
    console.warn = (...args: unknown[]) => { warned.push(args); };
    console.error = (...args: unknown[]) => { errored.push(args); };
    roundtablePiExtension(pi);
  } finally {
    console.log = originalLog;
    console.warn = originalWarn;
    console.error = originalError;
    restoreEnv("ROUNDTABLE_ACTIVE", originalActive);
  }

  assert.equal(tools.size, 0);
  assert.equal(handlers.size, 0);
  assert.deepEqual(logged, []);
  assert.deepEqual(warned, []);
  assert.deepEqual(errored, []);
});

test("a ROUNDTABLE_ACTIVE value other than \"1\" does not trip the re-entry guard", async () => {
  const originalActive = process.env.ROUNDTABLE_ACTIVE;
  const originalBinary = process.env.ROUNDTABLE_BIN;
  process.env.ROUNDTABLE_ACTIVE = "0";
  process.env.ROUNDTABLE_BIN = fileURLToPath(new URL("./fixtures/fake-roundtable.mjs", import.meta.url));
  const { pi, tools, handlers } = fakePi();

  try {
    roundtablePiExtension(pi);
    assert.deepEqual([...tools.keys()], EXPECTED_TOOLS);
    assert.ok(handlers.has("session_shutdown"));
    await handlers.get("session_shutdown")?.();
  } finally {
    restoreEnv("ROUNDTABLE_ACTIVE", originalActive);
    restoreEnv("ROUNDTABLE_BIN", originalBinary);
  }
});

test("the native Pi tool crosses stdio MCP and closes on session shutdown", async () => {
  const originalActive = process.env.ROUNDTABLE_ACTIVE;
  const originalBinary = process.env.ROUNDTABLE_BIN;
  const originalAgentDir = process.env.PI_CODING_AGENT_DIR;
  delete process.env.ROUNDTABLE_ACTIVE;
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
    assert.deepEqual([...tools.keys()], EXPECTED_TOOLS);

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
    restoreEnv("ROUNDTABLE_ACTIVE", originalActive);
    if (originalBinary === undefined) delete process.env.ROUNDTABLE_BIN;
    else process.env.ROUNDTABLE_BIN = originalBinary;
    if (originalAgentDir === undefined) delete process.env.PI_CODING_AGENT_DIR;
    else process.env.PI_CODING_AGENT_DIR = originalAgentDir;
  }
});
