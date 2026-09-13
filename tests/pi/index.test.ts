import assert from "node:assert/strict";
import { EventEmitter } from "node:events";
import { readFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import test from "node:test";

import type { Client } from "@modelcontextprotocol/sdk/client/index.js";
import type { StdioClientTransport } from "@modelcontextprotocol/sdk/client/stdio.js";

import {
  BRIDGE_CLOSED_MESSAGE,
  connectionIsLost,
  RoundtableBridge,
  type RoundtableBridgeOptions,
} from "../../extensions/pi/bridge.ts";
import { textFromRoundtableResult } from "../../extensions/pi/index.ts";

const presentBinary = fileURLToPath(new URL("./fixtures/fake-roundtable.mjs", import.meta.url));
const missingBinary = "/nonexistent/.pi-bin/roundtable";

function bridgeWith(options: RoundtableBridgeOptions, commands: string[], warnings: string[]) {
  return new RoundtableBridge({
    environment: { PATH: "/bin" },
    loadConfig: () => ({ env: {} }),
    warn: (message) => void warnings.push(message),
    createTransport(parameters) {
      commands.push(parameters.command);
      return { stderr: new EventEmitter(), async close() {} } as unknown as StdioClientTransport;
    },
    createClient: () => ({
      async connect() {},
      async close() {},
      async callTool() {
        return { content: [{ type: "text", text: "ok" }] };
      },
    }),
    ...options,
  });
}

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

test("a missing package binary is announced once, before the first call, not on every call", async () => {
  const commands: string[] = [];
  const warnings: string[] = [];
  const bridge = bridgeWith({ bundledBinary: missingBinary }, commands, warnings);

  // Twelve resolutions of the registration, one of them before any call at all. The warning is
  // memoized with the registration, so the count is the point of this test: a per-call warning
  // would put twelve lines into the session's output.
  bridge.checkInstallation();
  bridge.checkInstallation();
  for (let index = 0; index < 10; index += 1) {
    await bridge.callTool("roundtable-canvass", { prompt: `call ${index}` }, "/repo");
  }
  await bridge.close();

  assert.equal(warnings.length, 1, "one warning per session, whatever the call count");
  assert.match(warnings[0] ?? "", /WARNING Roundtable/);
  assert.match(warnings[0] ?? "", /\/nonexistent\/\.pi-bin\/roundtable/);
  // PATH here is /bin, which holds no roundtable, so the line must not claim one.
  assert.match(warnings[0] ?? "", /no 'roundtable' is on PATH either/);
  assert.match(warnings[0] ?? "", /postinstall download/);
  assert.match(warnings[0] ?? "", /pi install git:github\.com\/TejGandham\/roundtable/);
  // One child for ten calls in one directory, spawned from the bare PATH name.
  assert.deepEqual(commands, ["roundtable"]);
});

test("a present package binary is used and says nothing", async () => {
  const commands: string[] = [];
  const warnings: string[] = [];
  const bridge = bridgeWith({ bundledBinary: presentBinary }, commands, warnings);

  bridge.checkInstallation();
  await bridge.callTool("roundtable-canvass", { prompt: "one" }, "/repo");
  await bridge.close();

  assert.deepEqual(warnings, []);
  assert.deepEqual(commands, [presentBinary]);
});

test("a deliberately named binary is not a failed install, so it warns about nothing", async () => {
  const commands: string[] = [];
  const warnings: string[] = [];
  const configured = bridgeWith(
    { bundledBinary: missingBinary, loadConfig: () => ({ command: "/registered/roundtable", env: {} }) },
    commands,
    warnings,
  );
  const overridden = bridgeWith(
    { bundledBinary: missingBinary, environment: { PATH: "/bin", ROUNDTABLE_BIN: "/opt/roundtable" } },
    commands,
    warnings,
  );

  configured.checkInstallation();
  overridden.checkInstallation();

  assert.deepEqual(warnings, []);
});

test("an unreadable registration file still fails at the call, not at tool registration", async () => {
  const warnings: string[] = [];
  const bridge = bridgeWith(
    {
      bundledBinary: missingBinary,
      loadConfig: () => {
        throw new Error("Roundtable config /x/roundtable.json is not valid JSON: bad");
      },
    },
    [],
    warnings,
  );

  bridge.checkInstallation();
  assert.deepEqual(warnings, []);
  await assert.rejects(bridge.callTool("roundtable-canvass", { prompt: "x" }, "/repo"), /is not valid JSON/);
});

// ---------------------------------------------------------------------------
// Connection lifecycle. One connection per session, opened for the cwd of the
// call that opened it, closed by the bridge that opened it, and no connection
// adopted or spawned after the session has shut down.
// ---------------------------------------------------------------------------

class LifecycleTransport {
  stderr = new EventEmitter();
  onclose?: () => void;
  onerror?: (error: Error) => void;
  closeCalls = 0;

  constructor(readonly cwd: string) {}

  async close(): Promise<void> {
    this.closeCalls += 1;
  }
}

/** An MCP SDK request-level failure: RequestTimeout covers both a per-call timeout and an abort. */
class RequestTimeoutError extends Error {
  readonly code = -32001;
}

/** An MCP SDK transport-level failure: the child is gone, so the connection is unusable. */
class ConnectionClosedError extends Error {
  readonly code = -32000;
}

const tick = () => new Promise((resolve) => setImmediate(resolve));

interface Harness {
  bridge: RoundtableBridge;
  transports: LifecycleTransport[];
}

function lifecycleHarness(options: {
  connectGate?: (attempt: number) => Promise<void> | undefined;
  call?: (attempt: number) => Promise<{ content: unknown[] }>;
} = {}): Harness {
  const transports: LifecycleTransport[] = [];
  let connects = 0;
  let calls = 0;
  const bridge = new RoundtableBridge({
    command: "/tmp/roundtable",
    environment: { PATH: "/bin" },
    loadConfig: () => ({ env: {} }),
    warn: () => {},
    createTransport(parameters) {
      const transport = new LifecycleTransport(String(parameters.cwd));
      transports.push(transport);
      return transport as unknown as StdioClientTransport;
    },
    createClient: () => ({
      async connect() {
        await options.connectGate?.(connects++);
      },
      async close() {},
      async callTool() {
        const result = (await options.call?.(calls++)) ?? { content: [{ type: "text", text: "ok" }] };
        return result as unknown as Awaited<ReturnType<Client["callTool"]>>;
      },
    }),
  });
  return { bridge, transports };
}

test("two calls racing with different working directories leave no connection behind", async () => {
  let release!: () => void;
  const gate = new Promise<void>((resolve) => { release = resolve; });
  const { bridge, transports } = lifecycleHarness({
    connectGate: (attempt) => (attempt === 0 ? gate : undefined),
  });

  const callOne = bridge.callTool("roundtable-canvass", { prompt: "a" }, "/repo/project-one");
  await tick();
  const callTwo = bridge.callTool("roundtable-canvass", { prompt: "b" }, "/repo/project-two");
  await tick();
  release();
  await Promise.all([callOne, callTwo]);

  // The cwd of a connection is pinned to the call that opened it, and the connection the second
  // call displaces is closed rather than abandoned.
  assert.deepEqual(transports.map((transport) => transport.cwd), ["/repo/project-one", "/repo/project-two"]);
  assert.equal(transports[0]?.closeCalls, 1);
  assert.equal(transports[1]?.closeCalls, 0);

  await bridge.close();
  assert.equal(transports.length, 2);
  assert.equal(transports.filter((transport) => transport.closeCalls === 0).length, 0);
});

test("a call that hits its own deadline keeps the connection its sibling is using", async () => {
  let finishSlow!: (result: { content: unknown[] }) => void;
  const slow = new Promise<{ content: unknown[] }>((resolve) => { finishSlow = resolve; });
  const { bridge, transports } = lifecycleHarness({
    call: (attempt) => {
      if (attempt === 0) return slow;
      throw new RequestTimeoutError("MCP error -32001: Request timed out");
    },
  });

  const slowCall = bridge.callTool("roundtable-critique", { prompt: "long panel" }, "/repo");
  await tick();
  await assert.rejects(
    bridge.callTool("roundtable-canvass", { prompt: "quick", timeout: 1 }, "/repo"),
    /Request timed out/,
  );

  assert.equal(transports.length, 1);
  assert.equal(transports[0]?.closeCalls, 0, "the failed call must not close its sibling's transport");

  finishSlow({ content: [{ type: "text", text: "panel A result" }] });
  assert.deepEqual((await slowCall).content, [{ type: "text", text: "panel A result" }]);
  await bridge.close();
  assert.equal(transports[0]?.closeCalls, 1);
});

test("a lost connection is closed and cleared, so the next call reconnects", async () => {
  const { bridge, transports } = lifecycleHarness({
    call: (attempt) => {
      if (attempt === 0) throw new ConnectionClosedError("MCP error -32000: Connection closed");
      return Promise.resolve({ content: [{ type: "text", text: "ok" }] });
    },
  });

  await assert.rejects(bridge.callTool("roundtable-canvass", { prompt: "x" }, "/repo"), /Connection closed/);
  assert.equal(transports[0]?.closeCalls, 1);

  await bridge.callTool("roundtable-canvass", { prompt: "y" }, "/repo");
  assert.equal(transports.length, 2);
  await bridge.close();
  assert.deepEqual(transports.map((transport) => transport.closeCalls), [1, 1]);
});

test("request-level failures are told apart from a dead connection", () => {
  const aborted = AbortSignal.abort();
  const rpcError = (code: number, message: string) => Object.assign(new Error(message), { code });

  assert.equal(connectionIsLost(new RequestTimeoutError("Request timed out")), false);
  assert.equal(connectionIsLost(new Error("Maximum total timeout exceeded")), false);
  assert.equal(connectionIsLost(Object.assign(new Error("aborted"), { name: "AbortError" })), false);
  assert.equal(connectionIsLost(new Error("anything"), aborted), false);

  // Ordinary application-level JSON-RPC errors. The call failed; the connection did not.
  assert.equal(connectionIsLost(rpcError(-32602, "MCP error -32602: Invalid params")), false);
  assert.equal(connectionIsLost(rpcError(-32601, "MCP error -32601: Method not found")), false);
  assert.equal(connectionIsLost(rpcError(-32603, "MCP error -32603: Internal error")), false);
  assert.equal(connectionIsLost(rpcError(-32700, "MCP error -32700: Parse error")), false);
  assert.equal(connectionIsLost(new Error("roundtable: provider returned no verdict")), false);

  // The two that do mean the child is gone.
  assert.equal(connectionIsLost(new ConnectionClosedError("Connection closed")), true);
  assert.equal(connectionIsLost(new Error("write EPIPE")), true);
  assert.equal(connectionIsLost(new Error("read ECONNRESET")), true);
  assert.equal(connectionIsLost(new Error("broken pipe")), true);
});

test("a call after session shutdown is refused instead of spawning an orphan", async () => {
  const { bridge, transports } = lifecycleHarness();

  await bridge.callTool("roundtable-canvass", { prompt: "x" }, "/repo");
  await bridge.close();
  assert.deepEqual(transports.map((transport) => transport.closeCalls), [1]);

  await assert.rejects(
    bridge.callTool("roundtable-canvass", { prompt: "after shutdown" }, "/repo"),
    new RegExp(BRIDGE_CLOSED_MESSAGE),
  );
  assert.equal(transports.length, 1, "no child may be spawned after the session that owned it ended");

  // Idempotent: a second shutdown closes nothing twice and throws nothing.
  await bridge.close();
  assert.deepEqual(transports.map((transport) => transport.closeCalls), [1]);
});

test("a connection that finishes opening after shutdown is closed, not adopted", async () => {
  let release!: () => void;
  const gate = new Promise<void>((resolve) => { release = resolve; });
  const { bridge, transports } = lifecycleHarness({ connectGate: () => gate });

  const inFlight = bridge.callTool("roundtable-canvass", { prompt: "x" }, "/repo");
  await tick();
  const closing = bridge.close();
  await tick();
  release();

  await assert.rejects(inFlight, new RegExp(BRIDGE_CLOSED_MESSAGE));
  await closing;
  assert.deepEqual(transports.map((transport) => transport.closeCalls), [1]);

  await assert.rejects(
    bridge.callTool("roundtable-canvass", { prompt: "y" }, "/repo"),
    new RegExp(BRIDGE_CLOSED_MESSAGE),
  );
  assert.equal(transports.length, 1);
});

test("the Pi package exposes one native extension and the shared skill plus the Pi-side one", async () => {
  const packageJson = JSON.parse(await readFile(new URL("../../package.json", import.meta.url), "utf8"));
  assert.deepEqual(packageJson.pi.extensions, ["./extensions/pi/index.ts"]);
  assert.deepEqual(packageJson.pi.skills, ["./skills/roundtable", "./skills/roundtable-pi"]);
  assert.equal(packageJson.dependencies["@modelcontextprotocol/sdk"], "1.30.0");
  assert.equal(packageJson.engines.node, ">=22.19.0");
});
