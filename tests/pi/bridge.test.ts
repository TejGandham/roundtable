import assert from "node:assert/strict";
import { EventEmitter } from "node:events";
import test from "node:test";

import type { StdioClientTransport, StdioServerParameters } from "@modelcontextprotocol/sdk/client/stdio.js";

import {
  DEFAULT_AGENT_TIMEOUT_SECONDS,
  MCP_OVERHEAD_MILLISECONDS,
  RoundtableBridge,
  mcpRequestTimeoutMilliseconds,
  resolveRoundtableCommand,
} from "../../extensions/pi/bridge.ts";

class FakeTransport {
  stderr = new EventEmitter();
  onclose?: () => void;
  onerror?: (error: Error) => void;
  closeCalls = 0;

  async close(): Promise<void> {
    this.closeCalls += 1;
  }
}

test("uses an explicit override, then the bundled binary, then PATH", () => {
  const fixture = new URL("./fixtures/fake-roundtable.mjs", import.meta.url).pathname;
  assert.equal(resolveRoundtableCommand({ ROUNDTABLE_BIN: " /opt/roundtable " }, fixture), "/opt/roundtable");
  assert.equal(resolveRoundtableCommand({}, fixture), fixture);
  assert.equal(resolveRoundtableCommand({}, "/missing/bundled-roundtable"), "roundtable");
});

test("keeps MCP alive beyond the provider deadline", () => {
  assert.equal(
    mcpRequestTimeoutMilliseconds({ timeout: 900 }),
    900_000 + MCP_OVERHEAD_MILLISECONDS,
  );
  assert.equal(
    mcpRequestTimeoutMilliseconds({}),
    DEFAULT_AGENT_TIMEOUT_SECONDS * 1_000 + MCP_OVERHEAD_MILLISECONDS,
  );
});

test("starts one cwd-bound MCP server and forwards cancellation and the outer timeout", async () => {
  const transports: FakeTransport[] = [];
  const transportParameters: StdioServerParameters[] = [];
  const calls: Array<{ params: unknown; options: Record<string, unknown> | undefined }> = [];
  let connectCalls = 0;
  let closeCalls = 0;

  const bridge = new RoundtableBridge({
    command: "/tmp/roundtable",
    environment: { PATH: "/bin", ROUNDTABLE_PROVIDERS: "[]" },
    createTransport(parameters) {
      transportParameters.push(parameters);
      const transport = new FakeTransport();
      transports.push(transport);
      return transport as unknown as StdioClientTransport;
    },
    createClient() {
      return {
        async connect() {
          connectCalls += 1;
        },
        async close() {
          closeCalls += 1;
        },
        async callTool(params, _schema, options) {
          calls.push({ params, options: options as Record<string, unknown> | undefined });
          return { content: [{ type: "text", text: "ok" }] };
        },
      };
    },
  });

  const controller = new AbortController();
  await bridge.callTool("roundtable-canvass", { prompt: "review", timeout: 900 }, "/repo", controller.signal);
  await bridge.callTool("roundtable-critique", { prompt: "review" }, "/repo", controller.signal);

  assert.equal(connectCalls, 1);
  assert.equal(transportParameters[0]?.command, "/tmp/roundtable");
  assert.deepEqual(transportParameters[0]?.args, ["stdio"]);
  assert.equal(transportParameters[0]?.cwd, "/repo");
  assert.equal(transportParameters[0]?.env?.ROUNDTABLE_PROVIDERS, "[]");
  assert.equal(calls[0]?.options?.timeout, 1_020_000);
  assert.equal(calls[0]?.options?.maxTotalTimeout, 1_020_000);
  assert.equal(calls[0]?.options?.signal, controller.signal);

  await bridge.close();
  assert.equal(closeCalls, 1);
  assert.equal(transports[0]?.closeCalls, 1);
});

test("restarts the server when Pi changes project cwd", async () => {
  let connectCalls = 0;
  let closeCalls = 0;

  const bridge = new RoundtableBridge({
    createTransport() {
      return new FakeTransport() as unknown as StdioClientTransport;
    },
    createClient() {
      return {
        async connect() {
          connectCalls += 1;
        },
        async close() {
          closeCalls += 1;
        },
        async callTool() {
          return { content: [{ type: "text", text: "ok" }] };
        },
      };
    },
  });

  await bridge.callTool("roundtable-canvass", { prompt: "one" }, "/one");
  await bridge.callTool("roundtable-canvass", { prompt: "two" }, "/two");
  await bridge.close();

  assert.equal(connectCalls, 2);
  assert.equal(closeCalls, 2);
});

test("missing binaries produce an actionable Pi error", async () => {
  const bridge = new RoundtableBridge({
    command: "/missing/roundtable",
    createTransport() {
      return new FakeTransport() as unknown as StdioClientTransport;
    },
    createClient() {
      return {
        async connect() {
          throw new Error("spawn /missing/roundtable ENOENT");
        },
        async close() {},
        async callTool() {
          return { content: [] };
        },
      };
    },
  });

  await assert.rejects(
    bridge.callTool("roundtable-canvass", { prompt: "x" }, "/repo"),
    /Install the matching release binary or set ROUNDTABLE_BIN/,
  );
});
