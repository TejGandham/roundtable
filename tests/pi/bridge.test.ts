import assert from "node:assert/strict";
import { EventEmitter } from "node:events";
import { dirname } from "node:path";
import test, { mock } from "node:test";
import { fileURLToPath } from "node:url";

import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import type { StdioClientTransport, StdioServerParameters } from "@modelcontextprotocol/sdk/client/stdio.js";

import {
  BRIDGE_CLOSED_MESSAGE,
  CONNECT_TIMEOUT_MILLISECONDS,
  DEFAULT_AGENT_TIMEOUT_SECONDS,
  EXPIRY_GRACE_MILLISECONDS,
  MCP_OVERHEAD_MILLISECONDS,
  RoundtableBridge,
  bundledBinaryFallbackWarning,
  commandLocation,
  mcpRequestTimeoutMilliseconds,
  resolveRoundtableCommand,
  serverWarningLine,
} from "../../extensions/pi/bridge.ts";
import { ScriptedTransport, type ToolResponder } from "./fixtures/scripted-transport.ts";

class FakeTransport {
  stderr = new EventEmitter();
  onclose?: () => void;
  onerror?: (error: Error) => void;
  closeCalls = 0;

  async close(): Promise<void> {
    this.closeCalls += 1;
  }
}

const fixtureBinary = fileURLToPath(new URL("./fixtures/fake-roundtable.mjs", import.meta.url));
const fixtureDirectory = dirname(fixtureBinary);
const sleep = (milliseconds: number) => new Promise((resolve) => setTimeout(resolve, milliseconds));
/** Drain every pending microtask chain without a timer, so tests work under mocked timers too. */
const settled = () => new Promise((resolve) => setImmediate(resolve));
/** Let the wall clock move on, which a mocked timer clock does not do for it. */
const wallClockMoves = async (milliseconds = 2) => {
  const started = Date.now();
  while (Date.now() - started < milliseconds) await settled();
};

/**
 * The drain index, which is private because nothing outside the bridge may touch it. A test about
 * what close() leaves behind has to read it: every other symptom of a connection stranded there is
 * a child that outlives the session, which a fake transport cannot show.
 */
function draining(bridge: RoundtableBridge): Map<string, Set<unknown>> {
  return (bridge as unknown as { retiring: Map<string, Set<unknown>> }).retiring;
}

/**
 * A bridge over fake transports whose calls settle only when the test says so. No SDK request timer
 * runs here, which is the point: it leaves the bridge's own drain expiry as the only thing that can
 * end a drain, and makes what it is armed for observable.
 */
function hangingBridge(transports: FakeTransport[]): {
  bridge: RoundtableBridge;
  answer: (prompt: string) => void;
} {
  type CallToolResult = Awaited<ReturnType<Client["callTool"]>>;
  const ok = { content: [{ type: "text", text: "ok" }] } as CallToolResult;
  const pending = new Map<string, (result: CallToolResult) => void>();
  const bridge = new RoundtableBridge({
    command: "/tmp/roundtable",
    environment: { PATH: "/bin" },
    loadConfig: () => ({ env: {} }),
    warn: () => {},
    createTransport() {
      const transport = new FakeTransport();
      transports.push(transport);
      return transport as unknown as StdioClientTransport;
    },
    createClient: () => ({
      async connect() {},
      async close() {},
      async callTool(parameters) {
        const prompt = String((parameters as { arguments?: { prompt?: unknown } }).arguments?.prompt);
        if (prompt === "quick") return ok;
        return new Promise<CallToolResult>((resolve) => pending.set(prompt, resolve));
      },
    }),
  });
  return {
    bridge,
    answer(prompt: string) {
      pending.get(prompt)?.(ok);
      pending.delete(prompt);
    },
  };
}

/** The real MCP client, so the SDK's own connection behaviour is what the test exercises. */
function realClient(): Client {
  return new Client({ name: "roundtable-pi-test", version: "0.0.0" }, { capabilities: {} });
}

test("uses the env override, then the registered command, then the bundled binary, then PATH", () => {
  const fixture = new URL("./fixtures/fake-roundtable.mjs", import.meta.url).pathname;
  assert.deepEqual(
    resolveRoundtableCommand({ ROUNDTABLE_BIN: " /opt/roundtable " }, fixture, "/etc/rt"),
    { command: "/opt/roundtable", source: "env" },
  );
  assert.deepEqual(resolveRoundtableCommand({}, fixture, "/etc/rt"), { command: "/etc/rt", source: "config" });
  assert.deepEqual(resolveRoundtableCommand({}, fixture), { command: fixture, source: "bundled" });
  assert.deepEqual(
    resolveRoundtableCommand({}, "/missing/bundled-roundtable"),
    { command: "roundtable", source: "path" },
  );
});

test("the registration file supplies providers while the process keeps the secrets", async () => {
  const transportParameters: StdioServerParameters[] = [];
  const bridge = new RoundtableBridge({
    environment: { PATH: "/bin", FIREWORKS_API_KEY: "secret", ROUNDTABLE_PROVIDERS: "[]" },
    loadConfig: () => ({
      command: "/registered/roundtable",
      env: { ROUNDTABLE_PROVIDERS: '[{"id":"fireworks-kimi"}]', ROUNDTABLE_DEFAULT_AGENTS: "[]" },
    }),
    createTransport(parameters) {
      transportParameters.push(parameters);
      return new FakeTransport() as unknown as StdioClientTransport;
    },
    createClient() {
      return {
        async connect() {},
        async close() {},
        async callTool() {
          return { content: [{ type: "text", text: "ok" }] };
        },
      };
    },
  });

  await bridge.callTool("roundtable-canvass", { prompt: "x" }, "/repo");
  await bridge.close();

  assert.equal(transportParameters[0]?.command, "/registered/roundtable");
  assert.equal(transportParameters[0]?.env?.ROUNDTABLE_PROVIDERS, '[{"id":"fireworks-kimi"}]');
  assert.equal(transportParameters[0]?.env?.ROUNDTABLE_DEFAULT_AGENTS, "[]");
  assert.equal(transportParameters[0]?.env?.FIREWORKS_API_KEY, "secret");
});

test("a ROUNDTABLE_BIN in the registration file's env block selects the binary it names", async () => {
  async function commandFor(config: { command?: string; env: Record<string, string> }): Promise<string | undefined> {
    let command: string | undefined;
    const bridge = new RoundtableBridge({
      environment: { PATH: "/bin" },
      loadConfig: () => config,
      warn: () => {},
      createTransport(parameters) {
        command = parameters.command;
        return new FakeTransport() as unknown as StdioClientTransport;
      },
      createClient: () => ({
        async connect() {},
        async close() {},
        async callTool() {
          return { content: [{ type: "text", text: "ok" }] };
        },
      }),
    });
    await bridge.callTool("roundtable-canvass", { prompt: "x" }, "/repo");
    await bridge.close();
    return command;
  }

  // BUG-0003: the value was merged into the child's environment but never used to pick the child.
  assert.equal(await commandFor({ env: { ROUNDTABLE_BIN: "/opt/rt/roundtable" } }), "/opt/rt/roundtable");
  // Precedence: ROUNDTABLE_BIN outranks the registration file's own `command`, as INSTALL.md says.
  assert.equal(
    await commandFor({ command: "/registered/roundtable", env: { ROUNDTABLE_BIN: "/opt/rt/roundtable" } }),
    "/opt/rt/roundtable",
  );
});

test("a malformed registration file blocks the call instead of shrinking the panel", async () => {
  const bridge = new RoundtableBridge({
    loadConfig: () => {
      throw new Error("Roundtable config /x/roundtable.json is not valid JSON: bad");
    },
    createTransport() {
      return new FakeTransport() as unknown as StdioClientTransport;
    },
  });

  await assert.rejects(
    bridge.callTool("roundtable-canvass", { prompt: "x" }, "/repo"),
    /is not valid JSON/,
  );
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
    loadConfig: () => ({ env: {} }),
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
    loadConfig: () => ({ env: {} }),
    // The missing-binary warning has its own test; keep it out of this suite's output.
    warn: () => {},
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

// ---------------------------------------------------------------------------
// Connection lifecycle against the real MCP SDK client. A fake client cannot
// show what closing a shared transport does to a sibling call, because that
// happens inside the SDK's own `Protocol._onclose()`; these drive the real
// client over a scripted transport instead.
// ---------------------------------------------------------------------------

function scriptedBridge(options: {
  onTool?: ToolResponder;
  initializeDelay?: number | "never";
  transports?: ScriptedTransport[];
}): RoundtableBridge {
  return new RoundtableBridge({
    command: "/tmp/roundtable",
    environment: { PATH: "/bin" },
    loadConfig: () => ({ env: {} }),
    warn: () => {},
    createClient: realClient,
    createTransport() {
      const transport = new ScriptedTransport({ onTool: options.onTool, initializeDelay: options.initializeDelay });
      options.transports?.push(transport);
      return transport as unknown as StdioClientTransport;
    },
  });
}

test("an app-level JSON-RPC error rejects its own call and leaves a sibling's connection alive", async () => {
  const transports: ScriptedTransport[] = [];
  const bridge = scriptedBridge({
    transports,
    async onTool(name) {
      if (name === "roundtable-critique") {
        return { error: { code: -32602, message: "Invalid params: unknown field 'fils'" } };
      }
      await sleep(60);
      return { result: { content: [{ type: "text", text: "panel answered" }] } };
    },
  });

  const sibling = bridge.callTool("roundtable-canvass", { prompt: "slow" }, "/repo");
  await sleep(20);
  await assert.rejects(
    bridge.callTool("roundtable-critique", { prompt: "bad args" }, "/repo"),
    /-32602/,
  );

  // The sibling was mid-call on the shared connection: an argument the server rejected is not a
  // transport failure, so it must still get its panel rather than a spurious ConnectionClosed.
  assert.deepEqual((await sibling).content, [{ type: "text", text: "panel answered" }]);
  assert.equal(transports.length, 1);
  assert.equal(transports[0]?.isClosed, false);

  await bridge.close();
  assert.equal(transports[0]?.isClosed, true);
});

test("an unknown tool or a server fault keeps the connection, and only -32000 loses it", async () => {
  const transports: ScriptedTransport[] = [];
  const errors = [
    { code: -32601, message: "Method not found" },
    { code: -32603, message: "Internal error" },
  ];
  const bridge = scriptedBridge({
    transports,
    async onTool() {
      const error = errors.shift();
      return error ? { error } : { result: { content: [{ type: "text", text: "ok" }] } };
    },
  });

  await assert.rejects(bridge.callTool("roundtable-nope", { prompt: "x" }, "/repo"), /-32601/);
  await assert.rejects(bridge.callTool("roundtable-canvass", { prompt: "x" }, "/repo"), /-32603/);
  assert.deepEqual((await bridge.callTool("roundtable-canvass", { prompt: "x" }, "/repo")).content, [
    { type: "text", text: "ok" },
  ]);

  // One connection throughout: neither error reconnected, and no child was left open.
  assert.equal(transports.length, 1);
  await bridge.close();
  assert.equal(transports[0]?.isClosed, true);
});

test("a cwd switch retires the displaced connection only after its in-flight call settles", async () => {
  const transports: ScriptedTransport[] = [];
  const bridge = scriptedBridge({
    transports,
    async onTool() {
      await sleep(80);
      return { result: { content: [{ type: "text", text: "panel answered" }] } };
    },
  });

  const first = bridge.callTool("roundtable-canvass", { prompt: "one" }, "/repo/one");
  await sleep(20);
  const second = bridge.callTool("roundtable-canvass", { prompt: "two" }, "/repo/two");

  assert.deepEqual((await second).content, [{ type: "text", text: "panel answered" }]);
  // The call in /repo/one was in flight when the switch happened; closing under it would have
  // rejected it with ConnectionClosed.
  assert.deepEqual((await first).content, [{ type: "text", text: "panel answered" }]);

  assert.equal(transports.length, 2);
  assert.equal(transports[0]?.isClosed, true, "the displaced connection closes once its call settles");

  await bridge.close();
  assert.deepEqual(transports.map((transport) => transport.isClosed), [true, true]);
});

test("a later call for a draining connection's directory adopts it instead of starting a child", async () => {
  const transports: ScriptedTransport[] = [];
  const bridge = scriptedBridge({
    transports,
    async onTool(_name, arguments_) {
      if (arguments_.prompt === "slow") await sleep(120);
      return { result: { content: [{ type: "text", text: "panel answered" }] } };
    },
  });

  const first = bridge.callTool("roundtable-canvass", { prompt: "slow" }, "/repo/one");
  await sleep(20);
  // Displaces /repo/one, which is now draining `first`.
  await bridge.callTool("roundtable-canvass", { prompt: "two" }, "/repo/two");
  const third = await bridge.callTool("roundtable-canvass", { prompt: "three" }, "/repo/one");

  assert.deepEqual(third.content, [{ type: "text", text: "panel answered" }]);
  // Two children for two directories, not three: the call back to /repo/one took over the
  // connection that was draining there rather than starting a second server on the same tree.
  assert.equal(transports.length, 2);
  assert.equal(transports[0]?.isClosed, false, "the adopted connection is active again, not draining");
  assert.equal(transports[1]?.isClosed, true, "the idle /repo/two connection closed when it was displaced");
  // And adopting did not disturb the call that was draining on it.
  assert.deepEqual((await first).content, [{ type: "text", text: "panel answered" }]);

  await bridge.close();
  assert.deepEqual(transports.map((transport) => transport.isClosed), [true, true]);
});

test("a drain ends at its call's deadline, and that call is timed out rather than cut off", async () => {
  const transports: ScriptedTransport[] = [];
  const bridge = scriptedBridge({
    transports,
    async onTool(_name, arguments_) {
      // A panel that never comes back: the drain that used to last until the session ended.
      if (arguments_.prompt === "unanswered") await new Promise(() => {});
      return { result: { content: [{ type: "text", text: "panel answered" }] } };
    },
  });

  // One clock for the drain expiry and for the SDK's own request timeout, so the test does not sit
  // through a real panel deadline. Everything else here is driven by promises, not timers.
  mock.timers.enable({ apis: ["setTimeout"] });
  try {
    const hung = bridge.callTool("roundtable-canvass", { prompt: "unanswered", timeout: 1 }, "/repo/one");
    // Handled the moment it exists: this call is answered while nothing is awaiting it yet.
    const outcome = hung.then(() => undefined, (error: unknown) => error as Error);
    await settled();
    // The drain inherits this call's deadline, read a wall-clock millisecond or two after the SDK
    // armed its own timer for it — so an ungraced expiry is strictly the earlier of the two here,
    // rather than a tie the SDK would win for having been armed first.
    await wallClockMoves();

    await bridge.callTool("roundtable-canvass", { prompt: "two" }, "/repo/two");
    assert.equal(transports.length, 2);
    assert.equal(transports[0]?.isClosed, false, "the displaced connection is draining, not closed");

    // Exactly the hung call's own MCP deadline. The bridge read the clock a moment before the SDK
    // armed its timer for the same span, so an expiry on the deadline itself would fire first and
    // hand this caller a closed connection instead of the timeout it was owed.
    mock.timers.tick(mcpRequestTimeoutMilliseconds({ timeout: 1 }));
    await settled();

    const message = (await outcome)?.message ?? "";
    assert.match(message, /-32001/, `the caller was owed its timeout, and got: ${message}`);
    assert.doesNotMatch(message, /-32000/, "the drain must not cut the call off under the SDK");
    // And the drain still ends there rather than at close(): the timed-out call releases the last
    // deadline on a retired connection, which closes it.
    assert.equal(transports[0]?.isClosed, true, "the drain ended at its call's deadline, not at close()");
  } finally {
    mock.timers.reset();
  }

  await bridge.close();
  assert.deepEqual(transports.map((transport) => transport.isClosed), [true, true]);
});

test("a drain re-arms on the deadlines left behind when one of its calls settles", async () => {
  const transports: FakeTransport[] = [];
  const { bridge, answer } = hangingBridge(transports);
  const long = mcpRequestTimeoutMilliseconds({ timeout: 900 });
  const short = mcpRequestTimeoutMilliseconds({ timeout: 1 });

  mock.timers.enable({ apis: ["setTimeout"] });
  try {
    const patient = bridge.callTool("roundtable-canvass", { prompt: "patient", timeout: 900 }, "/repo/one");
    const impatient = bridge.callTool("roundtable-canvass", { prompt: "impatient", timeout: 1 }, "/repo/one");
    void impatient.catch(() => undefined);
    await settled();

    // Displaces /repo/one, whose drain is armed at the later of the two deadlines.
    await bridge.callTool("roundtable-canvass", { prompt: "quick" }, "/repo/two");
    answer("patient");
    await patient;
    await settled();

    // Past what the one remaining call can still be owed. The deadline the drain was armed for left
    // with the call that settled; without re-arming, this child sits open for the fifteen minutes
    // the settled call had asked for, holding a panel nobody can still be waiting on.
    mock.timers.tick(short + EXPIRY_GRACE_MILLISECONDS + 1);
    await settled();
    assert.equal(transports[0]?.closeCalls, 1, "the drain did not re-arm on the deadline left behind");
    assert.ok(short + EXPIRY_GRACE_MILLISECONDS < long, "the two deadlines must differ for this to mean anything");
  } finally {
    mock.timers.reset();
  }

  await bridge.close();
});

test("an adopted connection outlives the expiry its earlier drain armed", async () => {
  const transports: FakeTransport[] = [];
  const { bridge } = hangingBridge(transports);

  mock.timers.enable({ apis: ["setTimeout"] });
  try {
    const hung = bridge.callTool("roundtable-canvass", { prompt: "hung", timeout: 1 }, "/repo/one");
    void hung.catch(() => undefined);
    await settled();
    await bridge.callTool("roundtable-canvass", { prompt: "quick" }, "/repo/two");
    // Back to /repo/one: the drain is over and its expiry with it.
    await bridge.callTool("roundtable-canvass", { prompt: "quick" }, "/repo/one");

    mock.timers.tick(mcpRequestTimeoutMilliseconds({ timeout: 1 }) + EXPIRY_GRACE_MILLISECONDS + 1);
    await settled();
    assert.equal(transports[0]?.closeCalls, 0, "a retirement's expiry closed a connection that was adopted back");
  } finally {
    mock.timers.reset();
  }

  await bridge.close();
  assert.equal(transports[0]?.closeCalls, 1);
});

test("a transport error closes its child instead of stranding it outside both lists", async () => {
  const transports: FakeTransport[] = [];
  const { bridge } = hangingBridge(transports);

  await bridge.callTool("roundtable-canvass", { prompt: "quick" }, "/repo");
  // What a malformed stdout line looks like from here: the SDK reports it through onerror and the
  // child keeps running. Dropping the connection from `active` and nothing else left that child in
  // neither `active` nor the drain index, where close() could never reach it.
  transports[0]?.onerror?.(new Error("Unexpected token } in JSON at position 12"));
  await settled();
  assert.equal(transports[0]?.closeCalls, 1, "the errored child was left running");

  // And the next call for that directory starts a fresh child rather than adopting a dead pipe.
  const again = await bridge.callTool("roundtable-canvass", { prompt: "quick" }, "/repo");
  assert.deepEqual(again.content, [{ type: "text", text: "ok" }]);
  assert.equal(transports.length, 2, "the errored connection was adopted back");

  await bridge.close();
  assert.equal(transports[0]?.closeCalls, 1, "close() must not close an already-closed child twice");
  assert.equal(transports[1]?.closeCalls, 1);
});

test("a connection lost after close() is closed, not put back in the drain index", async () => {
  const transports: FakeTransport[] = [];
  const { bridge } = hangingBridge(transports);

  const hung = bridge.callTool("roundtable-canvass", { prompt: "hung" }, "/repo");
  void hung.catch(() => undefined);
  await settled();
  await bridge.close();

  // The child dies during or after teardown, with a call still on it. close() has already emptied
  // the drain index by now, so remembering this connection there arms an expiry for a bridge that
  // is gone and hands the child to nobody: the call it is waiting on never settles.
  transports[0]?.onerror?.(new Error("EPIPE"));
  await settled();

  assert.equal(draining(bridge).size, 0, "close() is terminal: nothing may re-enter the drain index");
  assert.equal(transports[0]?.closeCalls, 1);
});

test("close() does not wait out a connect the child is never going to answer", async () => {
  const transports: ScriptedTransport[] = [];
  const bridge = scriptedBridge({ transports, initializeDelay: "never" });

  const call = bridge.callTool("roundtable-canvass", { prompt: "x" }, "/repo");
  await sleep(20);

  const started = Date.now();
  await bridge.close();
  const elapsed = Date.now() - started;

  // Before the abort, close() sat on the chain behind the connect and returned in ~30s.
  assert.ok(elapsed < 2_000, `close() took ${elapsed}ms, which is the connect timeout, not a prompt close`);
  assert.ok(CONNECT_TIMEOUT_MILLISECONDS >= 2_000, "the bound this test is proving we no longer wait for");
  await assert.rejects(call, new RegExp(BRIDGE_CLOSED_MESSAGE));
  assert.equal(transports[0]?.isClosed, true, "the abandoned child is still closed");
});

test("the fallback warning tells a PATH install apart from nothing to fall back on", async () => {
  assert.equal(commandLocation("fake-roundtable.mjs", { PATH: fixtureDirectory }), fixtureBinary);
  assert.equal(commandLocation("roundtable", { PATH: "/nonexistent-directory" }), undefined);
  assert.equal(commandLocation(fixtureBinary, {}), fixtureBinary);
  assert.equal(commandLocation("/nonexistent/roundtable", {}), undefined);

  const found = bundledBinaryFallbackWarning("/gone/.pi-bin/roundtable", "roundtable", "/usr/local/bin/roundtable");
  assert.match(found, /runs 'roundtable' from PATH \(\/usr\/local\/bin\/roundtable\)/);
  assert.match(found, /unknown version/);

  const absent = bundledBinaryFallbackWarning("/gone/.pi-bin/roundtable", "roundtable");
  assert.match(absent, /no 'roundtable' is on PATH either/);
  assert.doesNotMatch(absent, /from PATH \(/);

  // And the bridge picks the right one: nothing named roundtable exists on this PATH.
  const warnings: string[] = [];
  const bridge = new RoundtableBridge({
    bundledBinary: "/gone/.pi-bin/roundtable",
    environment: { PATH: "/nonexistent-directory" },
    loadConfig: () => ({ env: {} }),
    warn: (message) => void warnings.push(message),
    createTransport: () => new FakeTransport() as unknown as StdioClientTransport,
  });
  bridge.checkInstallation();
  await bridge.close();
  assert.equal(warnings.length, 1);
  assert.match(warnings[0] ?? "", /no 'roundtable' is on PATH either/);
});

test("missing binaries produce an actionable Pi error", async () => {
  async function failureFrom(options: { onConnect?: Error; onCall?: Error }): Promise<string> {
    const bridge = new RoundtableBridge({
      command: "/missing/roundtable",
      loadConfig: () => ({ env: {} }),
      createTransport() {
        return new FakeTransport() as unknown as StdioClientTransport;
      },
      createClient() {
        return {
          async connect() {
            if (options.onConnect) throw options.onConnect;
          },
          async close() {},
          async callTool() {
            if (options.onCall) throw options.onCall;
            return { content: [] };
          },
        };
      },
    });
    const message = await bridge.callTool("roundtable-canvass", { prompt: "x" }, "/repo")
      .then(() => "no failure", (error: Error) => error.message);
    await bridge.close();
    return message;
  }

  const advice = /Install the matching release binary or set ROUNDTABLE_BIN/;
  // Node's own spawn failure, classified from the code the error carries.
  const spawnFailure = Object.assign(new Error("spawn /missing/roundtable ENOENT"), { code: "ENOENT" });
  assert.match(await failureFrom({ onConnect: spawnFailure }), advice);
  // And from the message alone, for an error wrapped on its way up and stripped of its code.
  assert.match(await failureFrom({ onConnect: new Error("spawn /missing/roundtable ENOENT") }), advice);

  // BUG-0010: a call is made over a connection that already started, so no failure of one is a
  // start failure, however its text or its code reads.
  const lateFailure = Object.assign(new Error("ENOENT: no such file or directory, open 'notes.md'"), {
    code: "ENOENT",
  });
  assert.match(await failureFrom({ onCall: lateFailure }), /^Roundtable MCP call failed: ENOENT/);
});

test("a start failure names what is actually missing, and a connect failure says it was one", async () => {
  async function connectFailure(error: Error, options: { command: string; cwd?: string }): Promise<string> {
    const bridge = new RoundtableBridge({
      command: options.command,
      environment: { PATH: "/bin" },
      loadConfig: () => ({ env: {} }),
      createTransport: () => new FakeTransport() as unknown as StdioClientTransport,
      createClient: () => ({
        async connect(): Promise<never> {
          throw error;
        },
        async close() {},
        async callTool() {
          return { content: [] };
        },
      }),
    });
    const message = await bridge.callTool("roundtable-canvass", { prompt: "x" }, options.cwd ?? "/repo")
      .then(() => "no failure", (failure: Error) => failure.message);
    await bridge.close();
    return message;
  }

  /** What Node hands back from a failed spawn: the code and the syscall, not a sentence to parse. */
  const spawnError = (command: string, code: string) =>
    Object.assign(new Error(`spawn ${command} ${code}`), { code, syscall: `spawn ${command}`, path: command });

  // The binary is there and cannot be run. Reinstalling it changes nothing; chmod does.
  const denied = await connectFailure(spawnError(fixtureBinary, "EACCES"), { command: fixtureBinary });
  assert.match(denied, /is not executable/);
  assert.ok(denied.includes(`chmod +x ${fixtureBinary}`), `the remedy names no file in: ${denied}`);
  assert.doesNotMatch(denied, /Install the matching release binary/);

  // A path with a space in it, from an error wrapped on its way up and stripped of its code: the
  // message is all that is left to read, and the old pattern's `\S+` never matched a command
  // like this one. The line must still name the executable that was configured.
  const spaced = "/opt/My Tools/roundtable server";
  const wrapped = await connectFailure(new Error(`spawn ${spaced} ENOENT`), { command: spaced });
  assert.match(wrapped, /Install the matching release binary or set ROUNDTABLE_BIN/);
  assert.ok(wrapped.includes(`could not start '${spaced}'`), `the configured executable is missing from: ${wrapped}`);

  // The same ENOENT, the same sentence from Node, a different missing file: the binary is installed
  // and the directory the call was made for is not. Reinstall advice here sends the reader to
  // replace a binary that is sitting right where it should be.
  const missingCwd = await connectFailure(spawnError(fixtureBinary, "ENOENT"), {
    command: fixtureBinary,
    cwd: "/nonexistent-directory/repo",
  });
  assert.match(missingCwd, /working directory \/nonexistent-directory\/repo does not exist/);
  assert.doesNotMatch(missingCwd, /Install the matching release binary/);

  // The same permission failure, wrapped on its way up and stripped of its fields: the sentence is
  // all that is left to read, and it still has to carry the remedy that fits. Classified as merely
  // "some other connection failure", it sent the reader off to reinstall a binary that is sitting
  // right there with its executable bit off.
  const wrappedDenied = await connectFailure(new Error(`spawn ${fixtureBinary} EACCES`), { command: fixtureBinary });
  assert.match(wrappedDenied, /is not executable/);
  assert.ok(wrappedDenied.includes(`chmod +x ${fixtureBinary}`), `the remedy names no file in: ${wrappedDenied}`);
  assert.doesNotMatch(wrappedDenied, /Install the matching release binary/);

  // Not a start failure at all: the child started and the handshake did not finish. That is a
  // connection failure, and calling it a failed call names a phase this error never reached.
  const handshake = await connectFailure(new Error("MCP error -32001: Request timed out"), {
    command: fixtureBinary,
  });
  assert.match(handshake, /^Roundtable connection failed: MCP error -32001/);
  assert.doesNotMatch(handshake, /MCP call failed/);
});

// ---------------------------------------------------------------------------
// Final-gate findings: what happens to a connection opened, promoted or
// reported on at the wrong moment, and whose stderr it is.
// ---------------------------------------------------------------------------

/** A child that takes its time going away, which is the window a shutdown can arrive in. */
class SlowClosingTransport extends FakeTransport {
  override async close(): Promise<void> {
    await sleep(50);
    await super.close();
  }
}

test("a shutdown during a retirement spawns nothing and does not wait out a connect", async () => {
  const transports: SlowClosingTransport[] = [];
  let connects = 0;
  const bridge = new RoundtableBridge({
    command: "/tmp/roundtable",
    environment: { PATH: "/bin" },
    loadConfig: () => ({ env: {} }),
    warn: () => {},
    createTransport() {
      const transport = new SlowClosingTransport();
      transports.push(transport);
      return transport as unknown as StdioClientTransport;
    },
    createClient: () => ({
      async connect() {
        connects += 1;
        // A second child that never answers its handshake. Nothing may reach this.
        if (connects > 1) await sleep(5_000);
      },
      async close() {},
      async callTool() {
        return { content: [{ type: "text", text: "ok" }] };
      },
    }),
  });

  await bridge.callTool("roundtable-canvass", { prompt: "one" }, "/repo/one");
  // The cwd switch retires the first connection, and that retirement is where the session ends.
  const second = bridge.callTool("roundtable-canvass", { prompt: "two" }, "/repo/two");
  void second.catch(() => undefined);
  await settled();

  const started = Date.now();
  const closing = bridge.close();
  await assert.rejects(second, new RegExp(BRIDGE_CLOSED_MESSAGE));
  await closing;
  const elapsed = Date.now() - started;

  assert.equal(transports.length, 1, "a child was spawned after the bridge had closed");
  assert.equal(connects, 1);
  // Without the refusal, teardown queues behind that connect and the host waits out its timeout.
  assert.ok(elapsed < 2_000, `close() took ${elapsed}ms, which is a connect it should never have started`);
  assert.ok(CONNECT_TIMEOUT_MILLISECONDS >= 2_000, "the bound this test is proving we no longer wait for");
});

test("a connection lost while it was being established is not promoted to active", async () => {
  const transports: FakeTransport[] = [];
  let connects = 0;
  const bridge = new RoundtableBridge({
    command: "/tmp/roundtable",
    environment: { PATH: "/bin" },
    loadConfig: () => ({ env: {} }),
    warn: () => {},
    createTransport() {
      const transport = new FakeTransport();
      transports.push(transport);
      return transport as unknown as StdioClientTransport;
    },
    createClient() {
      let live = true;
      return {
        async connect(transport) {
          connects += 1;
          // The first child answers the handshake and then goes away, which a stdio transport
          // reports while connect is still settling. The SDK hands back a client whose transport
          // is already gone, and every request on it is answered `Not connected` — which is not a
          // transport error, so nothing would ever reconnect.
          if (connects === 1) {
            live = false;
            (transport as unknown as FakeTransport).onclose?.();
            await settled();
          }
        },
        async close() {
          live = false;
        },
        async callTool() {
          if (!live) throw new Error("Not connected");
          return { content: [{ type: "text", text: "ok" }] };
        },
      };
    },
  });

  await assert.rejects(
    bridge.callTool("roundtable-canvass", { prompt: "one" }, "/repo"),
    /closed while it was being established/,
  );

  const second = await bridge.callTool("roundtable-canvass", { prompt: "two" }, "/repo");
  assert.deepEqual(second.content, [{ type: "text", text: "ok" }], "the session never reconnected");
  assert.equal(transports.length, 2, "the dead connection was promoted and handed to the next call");
  assert.equal(transports[0]?.closeCalls, 1);

  await bridge.close();
  assert.deepEqual(transports.map((transport) => transport.closeCalls), [1, 1]);
});

test("a lost connection is not indexed with the deadline of the call that lost it", async () => {
  const transports: FakeTransport[] = [];
  const bridge = new RoundtableBridge({
    command: "/tmp/roundtable",
    environment: { PATH: "/bin" },
    loadConfig: () => ({ env: {} }),
    warn: () => {},
    createTransport() {
      const transport = new FakeTransport();
      transports.push(transport);
      return transport as unknown as StdioClientTransport;
    },
    createClient: () => ({
      async connect() {},
      async close() {},
      async callTool(): Promise<never> {
        throw Object.assign(new Error("MCP error -32000: Connection closed"), { code: -32000 });
      },
    }),
  });

  // The arming is undone a microtask later by the same call's release, so the drain index cannot
  // show it: the timer itself is the only evidence that a connection known to be dead was handed
  // to the drain with fifteen minutes of patience it can no longer use.
  const armed: number[] = [];
  const realSetTimeout = globalThis.setTimeout;
  globalThis.setTimeout = ((handler: () => void, milliseconds?: number, ...rest: unknown[]) => {
    armed.push(milliseconds ?? 0);
    return (realSetTimeout as (...args: unknown[]) => unknown)(handler, milliseconds, ...rest);
  }) as unknown as typeof setTimeout;
  try {
    await assert.rejects(bridge.callTool("roundtable-canvass", { prompt: "x" }, "/repo"), /-32000/);
  } finally {
    globalThis.setTimeout = realSetTimeout;
  }

  const drain = mcpRequestTimeoutMilliseconds({});
  assert.ok(
    !armed.some((milliseconds) => milliseconds >= drain),
    `a dead connection was given a drain expiry of ${armed.join()}ms`,
  );
  assert.equal(draining(bridge).size, 0, "a lost connection with no call left on it belongs nowhere");
  assert.equal(transports[0]?.closeCalls, 1);
  await bridge.close();
});

test("each connection is reported with its own stderr, not whichever child wrote last", async () => {
  const transports: FakeTransport[] = [];
  const bridge = new RoundtableBridge({
    command: "/tmp/roundtable",
    environment: { PATH: "/bin" },
    loadConfig: () => ({ env: {} }),
    warn: () => {},
    createTransport(parameters) {
      const transport = new FakeTransport();
      transports.push(transport);
      // Each child says one thing, about itself, the way a server does on startup.
      queueMicrotask(() => transport.stderr.emit("data", `probe line for ${String(parameters.cwd)}\n`));
      return transport as unknown as StdioClientTransport;
    },
    createClient: () => ({
      async connect() {},
      async close() {},
      async callTool(parameters) {
        const prompt = String((parameters as { arguments?: { prompt?: unknown } }).arguments?.prompt);
        if (prompt === "hang") return new Promise(() => {}) as never;
        if (prompt === "fail") throw new Error("MCP request timed out");
        return { content: [{ type: "text", text: "ok" }] };
      },
    }),
  });

  const failureFor = (cwd: string) => bridge.callTool("roundtable-canvass", { prompt: "fail" }, cwd)
    .then(() => "no failure", (error: Error) => error.message);

  // /repo/one stays open around a call that never settles, so two children are live at once.
  const hung = bridge.callTool("roundtable-canvass", { prompt: "hang" }, "/repo/one");
  void hung.catch(() => undefined);
  await settled();

  const two = await failureFor("/repo/two");
  assert.ok(two.includes("probe line for /repo/two"), `its own child's stderr is missing from: ${two}`);
  assert.doesNotMatch(two, /probe line for \/repo\/one/);

  // Back to /repo/one, which adopts the connection draining there. One tail per bridge meant the
  // open() for /repo/two had already wiped this child's diagnostics and written its own over them.
  const one = await failureFor("/repo/one");
  assert.ok(one.includes("probe line for /repo/one"), `the adopted child's own stderr is missing from: ${one}`);
  assert.doesNotMatch(one, /probe line for \/repo\/two/);
  assert.equal(transports.length, 2);

  await bridge.close();
});

test("a server warning reaches the user on a call that succeeds, once for the session", async () => {
  const warning = 'time=2026-09-13T00:12:00.000Z level=WARN '
    + 'msg="ROUNDTABLE_DEFAULT_AGENTS ignored; dispatching to the built-in CLI panel instead" '
    + 'error="invalid character \',\' looking for beginning of value"';
  const chatter = 'time=2026-09-13T00:12:00.001Z level=INFO msg="serving roundtable stdio"';

  assert.equal(serverWarningLine(chatter), undefined, "ordinary server chatter is not a warning");
  assert.equal(serverWarningLine("not a slog line at all"), undefined);
  // The timestamp is dropped, or the same advice from a second child reads as a second advice.
  assert.doesNotMatch(serverWarningLine(warning) ?? "", /time=/);

  const warnings: string[] = [];
  const bridge = new RoundtableBridge({
    command: "/tmp/roundtable",
    environment: { PATH: "/bin" },
    loadConfig: () => ({ env: {} }),
    warn: (message) => void warnings.push(message),
    createTransport() {
      const transport = new FakeTransport();
      queueMicrotask(() => {
        // Split mid-line: a warning that arrived in two reads is one warning, not two halves.
        transport.stderr.emit("data", warning.slice(0, 40));
        transport.stderr.emit("data", `${warning.slice(40)}\n${chatter}\n`);
      });
      return transport as unknown as StdioClientTransport;
    },
    createClient: () => ({
      async connect() {},
      async close() {},
      async callTool() {
        return { content: [{ type: "text", text: "ok" }] };
      },
    }),
  });

  // A session whose calls all succeed is exactly the session that never sees the child's stderr,
  // and exactly the session where the operator is being given a panel they did not configure.
  await bridge.callTool("roundtable-canvass", { prompt: "x" }, "/repo");
  assert.equal(warnings.length, 1, `the server's own warning never reached the user: ${warnings.join()}`);
  assert.match(warnings[0] ?? "", /^WARNING Roundtable server: /);
  assert.match(warnings[0] ?? "", /ROUNDTABLE_DEFAULT_AGENTS ignored/);
  assert.doesNotMatch(warnings[0] ?? "", /serving roundtable stdio/);

  // Nine more calls and a second child for another directory, all saying the same thing: still one
  // line. A warning repeated per call or per connection is a log, not a warning.
  for (let index = 0; index < 9; index += 1) {
    await bridge.callTool("roundtable-canvass", { prompt: "x" }, "/repo");
  }
  await bridge.callTool("roundtable-canvass", { prompt: "x" }, "/other");
  assert.equal(warnings.length, 1, `the warning was repeated: ${warnings.join(" | ")}`);

  await bridge.close();
});

test("a panelist reported as not_found does not make a failed call a failed install", async () => {
  // A server that started, connected and answered, with one uninstalled panelist. INSTALL.md calls
  // that a supported state: the slot comes back `status: not_found` and the dispatch still
  // succeeds. The diagnostic lands in the child's stderr, which is where "not found" is most
  // likely to appear and says nothing at all about whether the server itself started.
  const successShapedPayload = JSON.stringify({
    status: "ok",
    results: [
      { agent: "codex", status: "not_found", stderr: "/usr/bin/codex: not found" },
      { agent: "claude", status: "ok", response: "the panel answered" },
    ],
  });

  async function failureWithStderr(stderr: string): Promise<string> {
    const bridge = new RoundtableBridge({
      command: "/home/dev/.local/bin/roundtable",
      environment: { PATH: "/bin" },
      loadConfig: () => ({ env: {} }),
      createTransport() {
        const transport = new FakeTransport();
        queueMicrotask(() => transport.stderr.emit("data", `${stderr}\n`));
        return transport as unknown as StdioClientTransport;
      },
      createClient: () => ({
        async connect() {},
        async close() {},
        async callTool(): Promise<never> {
          throw new Error("MCP request timed out");
        },
      }),
    });
    const message = await bridge.callTool("roundtable-canvass", { prompt: "x" }, "/repo")
      .then(() => "no failure", (error: Error) => error.message);
    await bridge.close();
    return message;
  }

  for (const stderr of [successShapedPayload, "ERROR codex probe failed: /usr/bin/codex: not found"]) {
    const message = await failureWithStderr(stderr);
    assert.match(message, /^Roundtable MCP call failed: MCP request timed out/);
    assert.doesNotMatch(message, /could not start/);
    // Still printed, just no longer read: the stderr tail is the most useful part of the message.
    assert.ok(message.includes(stderr), `the stderr tail is missing from: ${message}`);
  }
});
