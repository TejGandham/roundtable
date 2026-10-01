import { existsSync } from "node:fs";
import { delimiter, join, sep } from "node:path";
import { fileURLToPath } from "node:url";

import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import {
  StdioClientTransport,
  type StdioServerParameters,
} from "@modelcontextprotocol/sdk/client/stdio.js";

import { loadRoundtableConfig, roundtableConfigPath, type RoundtableConfig } from "./config.ts";

export const ROUNDTABLE_SERVER_NAME = "roundtable";
export const ROUNDTABLE_SERVER_VERSION = "2.5.0";
export const DEFAULT_AGENT_TIMEOUT_SECONDS = 900;
export const MCP_OVERHEAD_MILLISECONDS = 120_000;
export const CONNECT_TIMEOUT_MILLISECONDS = 30_000;
/**
 * How long past a draining call's own MCP deadline the bridge waits before closing the child under
 * it. The deadline is read from the clock a moment before the SDK arms its own request timer for
 * the same span, so an expiry armed exactly on it wins that race by a millisecond or so and the
 * caller is handed `Connection closed` instead of the timeout it was actually owed. The grace hands
 * the race back to the SDK: the call times out, its release closes the connection, and the expiry
 * only ever fires for a call the SDK never answered at all.
 */
export const EXPIRY_GRACE_MILLISECONDS = 3_000;
/** How much of a child's stderr is kept, per connection, for the message a failure is reported with. */
export const STDERR_TAIL_BYTES = 16_384;
/**
 * How many distinct server warnings one session will repeat to the user. A warning worth printing
 * is a startup advisory and there are a handful of those; a cap means a server that found a new
 * thing to warn about on every call cannot turn this channel into the log it was meant to replace.
 */
export const MAX_SURFACED_WARNINGS = 8;
export const BUNDLED_ROUNDTABLE_BINARY = fileURLToPath(new URL("../../.pi-bin/roundtable", import.meta.url));

export type RoundtableArguments = Record<string, unknown>;
export interface RoundtableResult {
  content: unknown[];
  isError?: boolean;
  structuredContent?: unknown;
  [key: string]: unknown;
}

type ClientLike = Pick<Client, "callTool" | "connect" | "close">;
type TransportLike = StdioClientTransport;

export interface RoundtableBridgeOptions {
  bundledBinary?: string;
  command?: string;
  createClient?: () => ClientLike;
  createTransport?: (parameters: StdioServerParameters) => TransportLike;
  environment?: NodeJS.ProcessEnv;
  loadConfig?: () => RoundtableConfig;
  warn?: (message: string) => void;
}

interface ActiveConnection {
  client: ClientLike;
  cwd: string;
  transport: TransportLike;
  /**
   * The MCP deadline of every call currently using this connection, one entry per call. Empty is
   * idle, which is when a retired connection closes; the latest entry is the moment past which a
   * retired connection has nothing left worth waiting for.
   */
  deadlines: number[];
  /** Detached from the bridge: no new call will be given it, and it closes once it falls idle. */
  retired: boolean;
  /** The child errored or went away. Such a connection is closed, never handed back to a call. */
  lost?: boolean;
  /** Armed while this connection drains, so a call that never settles cannot keep it alive. */
  expiry?: ReturnType<typeof setTimeout>;
  /** Set by the first close, so a connection is never torn down twice. */
  closing?: Promise<void>;
  /**
   * This child's stderr tail, and this child's alone. One tail per bridge could not say which
   * server a line came from: a second connection's open() wiped a draining connection's diagnostics
   * and then appended its own to them, so whichever call failed first was reported with the other
   * child's output.
   */
  stderr: string;
  /** The tail's unterminated last line, held back until its newline arrives with the next chunk. */
  stderrLine: string;
}

/**
 * Whether a connection may be handed to a call — promoted after it opens, adopted out of the drain
 * index, or reused as the active one. A connection already being torn down, or whose child has
 * errored or exited, may not be: handing one over gives the caller a pipe with nothing on the other
 * end, and the SDK answers every later call on it `Not connected` for the rest of the session.
 */
function isUsable(connection: ActiveConnection): boolean {
  return !connection.lost && connection.closing === undefined;
}

/**
 * The operator-facing part of a server stderr line, or `undefined` for everything else.
 *
 * The server logs through slog, one `level=...` field per line. A `WARN` is a session-level
 * advisory — a `ROUNDTABLE_DEFAULT_AGENTS` that would not parse, a panelist binary that is not
 * installed — decided once at startup and true for every call that follows, and on a call that
 * succeeds nothing else ever shows it to the user. `ERROR` is deliberately not included: it
 * accompanies a call that failed, and that call already carries the stderr tail in its own message.
 * The timestamp is dropped so the same advisory from a second child is recognised as the same line.
 */
export function serverWarningLine(line: string): string | undefined {
  if (!/(?:^|\s)level=WARN(?:\s|$)/.test(line)) return undefined;
  return line.replace(/(?:^|\s)time=\S+/, "").trim() || undefined;
}

function stringEnvironment(environment: NodeJS.ProcessEnv): Record<string, string> {
  return Object.fromEntries(
    Object.entries(environment).filter((entry): entry is [string, string] => typeof entry[1] === "string"),
  );
}

/**
 * Where the command came from. The caller needs this because the last branch serves two different
 * situations: no bundled binary was supplied at all (nothing to report), and one was supplied but
 * is absent (a failed install, which the user must be told about).
 */
export type RoundtableCommandSource = "env" | "config" | "bundled" | "path";

export interface ResolvedRoundtableCommand {
  command: string;
  source: RoundtableCommandSource;
}

export function resolveRoundtableCommand(
  environment: NodeJS.ProcessEnv = process.env,
  bundledBinary?: string,
  configuredCommand?: string,
): ResolvedRoundtableCommand {
  const override = environment.ROUNDTABLE_BIN?.trim();
  if (override) return { command: override, source: "env" };
  if (configuredCommand) return { command: configuredCommand, source: "config" };
  if (bundledBinary && existsSync(bundledBinary)) return { command: bundledBinary, source: "bundled" };
  return { command: "roundtable", source: "path" };
}

/**
 * Where `command` would actually be found, or `undefined` when nothing by that name exists. A
 * command containing a separator is a path and is checked as one; a bare name is searched along
 * `PATH` the way the OS will search it when the child is spawned.
 */
export function commandLocation(
  command: string,
  environment: NodeJS.ProcessEnv = process.env,
): string | undefined {
  if (command.includes("/") || command.includes(sep)) {
    return existsSync(command) ? command : undefined;
  }
  for (const directory of (environment.PATH ?? "").split(delimiter)) {
    if (!directory) continue;
    const candidate = join(directory, command);
    if (existsSync(candidate)) return candidate;
  }
  return undefined;
}

/**
 * One line, written once per session, when the package's own binary is gone. The two cases are not
 * the same failure and must not read the same: falling back to a `PATH` install is a version risk,
 * while having nothing to fall back to means every tool call in the session is already doomed. The
 * caller passes where the fallback was found, so this line never claims a binary it has not seen.
 */
export function bundledBinaryFallbackWarning(
  bundledBinary: string,
  command: string,
  pathLocation?: string,
): string {
  const remedy = `The postinstall download (scripts/install-roundtable-binary.mjs) did not complete. `
    + `Reinstall with 'pi install git:github.com/TejGandham/roundtable', or set ROUNDTABLE_BIN to the `
    + `binary you mean to run.`;

  if (!pathLocation) {
    return `WARNING Roundtable: the package binary is missing at ${bundledBinary}, and no '${command}' is on `
      + `PATH either, so every Roundtable tool call in this session will fail to start a server. ${remedy}`;
  }

  return `WARNING Roundtable: the package binary is missing at ${bundledBinary}, so this session runs `
    + `'${command}' from PATH (${pathLocation}) instead — an install of unknown version, not the `
    + `checksum-verified ${ROUNDTABLE_SERVER_VERSION} binary this package ships. ${remedy}`;
}

/**
 * Raised when a tool call arrives after the session that owned the bridge has shut down. The call
 * is refused rather than served: its panel has no consumer left, and the child it would spawn has
 * no handler left to close it. Pi's own tool wrapper already rejects post-shutdown calls, so this
 * is the guard for hosts that keep a bridge alive longer than Pi does.
 */
export const BRIDGE_CLOSED_MESSAGE = "Roundtable bridge is closed for this session.";

/** A dead pipe or a reset socket: the child is gone, whatever the call was asking for. */
const TRANSPORT_FAILURE = /EPIPE|ECONNRESET|broken pipe/i;

/**
 * Whether a failed call means the connection itself is gone.
 *
 * The test is a whitelist, and deliberately so. Almost everything a call can fail with is
 * call-scoped: a rejected argument (`InvalidParams`, -32602), an unknown tool (-32601), a server
 * fault (-32603), a per-call timeout or a caller abort (both reported as `RequestTimeout`, -32001).
 * The connection is fine in every one of those cases, and a concurrent sibling may be mid-call on
 * it — tearing it down turns one call's ordinary error into a spurious `ConnectionClosed` for the
 * sibling. So the connection is declared lost only on the two signals that actually mean it:
 * `ConnectionClosed` (-32000) from the SDK, and a transport-level pipe failure in the message.
 * A child that dies on its own still clears `active` through `transport.onclose`, so the next call
 * reconnects regardless.
 */
export function connectionIsLost(error: unknown, signal?: AbortSignal): boolean {
  if (signal?.aborted) return false;
  if (error instanceof Error && error.name === "AbortError") return false;
  const code = (error as { code?: unknown } | null)?.code;
  if (code === -32000) return true;
  const message = error instanceof Error ? error.message : String(error);
  return TRANSPORT_FAILURE.test(message);
}

export function mcpRequestTimeoutMilliseconds(arguments_: RoundtableArguments): number {
  const requested = arguments_.timeout;
  const seconds = typeof requested === "number" && Number.isInteger(requested) && requested >= 1 && requested <= 900
    ? requested
    : DEFAULT_AGENT_TIMEOUT_SECONDS;
  return seconds * 1_000 + MCP_OVERHEAD_MILLISECONDS;
}

export class RoundtableBridge {
  private readonly bundledBinary?: string;
  private readonly explicitCommand?: string;
  private readonly warn: (message: string) => void;
  private readonly loadConfig: () => RoundtableConfig;
  private readonly createClient: () => ClientLike;
  private readonly createTransport: (parameters: StdioServerParameters) => TransportLike;
  private readonly processEnvironment: Record<string, string>;
  private readonly sourceEnvironment: NodeJS.ProcessEnv;
  private registration?: { command: string; env: Record<string, string> };
  private active?: ActiveConnection;
  /**
   * The connection being opened. Read only by close(), as the last place a child that finished
   * opening during teardown can still be closed from; no call consults it, because calls are
   * serialised on `connectionChain` and an establish that is running is the only one there is.
   */
  private connecting?: Promise<ActiveConnection>;
  private connectAbort?: AbortController;
  /** Draining connections, indexed by the directory they were opened for, so a later call in that
   *  directory can find one instead of starting a second child for the same tree. */
  private readonly retiring = new Map<string, Set<ActiveConnection>>();
  private connectionChain: Promise<unknown> = Promise.resolve();
  private closed = false;
  /** Server warnings already repeated to the user, so one advisory is one line per session. */
  private readonly surfacedWarnings = new Set<string>();

  constructor(options: RoundtableBridgeOptions = {}) {
    const sourceEnvironment = options.environment ?? process.env;
    this.bundledBinary = options.bundledBinary ?? BUNDLED_ROUNDTABLE_BINARY;
    this.explicitCommand = options.command;
    this.warn = options.warn ?? ((message: string) => console.error(message));
    this.sourceEnvironment = sourceEnvironment;
    this.processEnvironment = stringEnvironment(sourceEnvironment);
    this.loadConfig = options.loadConfig
      ?? (() => loadRoundtableConfig(roundtableConfigPath(sourceEnvironment)));
    this.createClient = options.createClient ?? (() => new Client(
      { name: "roundtable-pi", version: ROUNDTABLE_SERVER_VERSION },
      { capabilities: {} },
    ));
    this.createTransport = options.createTransport ?? ((parameters) => new StdioClientTransport(parameters));
  }

  /**
   * Resolve the command up front, so a fallback to `PATH` is reported before the first tool call
   * rather than after it. A registration file that cannot be read is not reported here — that
   * error belongs to the call that needs the file, and is raised there unchanged.
   */
  checkInstallation(): void {
    try {
      this.resolveRegistration();
    } catch {
      // Reported by the first tool call instead.
    }
  }

  async callTool(
    name: string,
    arguments_: RoundtableArguments,
    cwd: string,
    signal?: AbortSignal,
  ): Promise<RoundtableResult> {
    if (this.closed) throw new Error(BRIDGE_CLOSED_MESSAGE);
    const connection = await this.connection(cwd, signal);
    const timeout = mcpRequestTimeoutMilliseconds(arguments_);

    // Recorded before the call is sent and released in `finally`, so a connection displaced by a
    // cwd change knows whether anyone is still talking on it, and until when.
    const deadline = Date.now() + timeout;
    connection.deadlines.push(deadline);
    try {
      return await connection.client.callTool(
        { name, arguments: arguments_ },
        undefined,
        {
          signal,
          timeout,
          maxTotalTimeout: timeout,
          resetTimeoutOnProgress: false,
        },
      ) as RoundtableResult;
    } catch (error) {
      // Only a lost connection is torn down. A sibling call sharing this connection keeps it.
      if (connectionIsLost(error, signal)) {
        // Released first. This call is over, so its deadline is not something a drain can still be
        // owed, and leaving it in place puts a connection known to be dead into the drain index
        // with an expiry armed on it — up to seventeen minutes for a call that asked for the
        // maximum. The `finally` below undid that a microtask later; not doing it is simpler than
        // undoing it, and a connection with no other call on it now closes inside `retire`.
        this.release(connection, deadline);
        await this.retire(connection, "lost");
      }
      throw this.describeFailure(error, "call", connection);
    } finally {
      this.release(connection, deadline);
      if (connection.retired) {
        // A drain ends when its last call settles. While others are still on it the drain is
        // shorter than it was, because the deadline it was armed for has just left: re-arm on
        // what remains, or a call with a long deadline keeps the child alive long after the
        // short-deadline call it was sharing with has given up.
        if (connection.deadlines.length === 0) await this.discard(connection);
        else this.armExpiry(connection);
      }
    }
  }

  /**
   * Terminal. The flag is set before the first await, so a call arriving during teardown is
   * refused rather than opening a child that nothing is left to close, and the in-flight
   * connection bookkeeping is cleared so nothing can be adopted from a half-torn state.
   * Idempotent: a second call closes nothing and throws nothing.
   */
  async close(): Promise<void> {
    this.closed = true;
    // Aborted before anything is awaited. Teardown is queued behind the connect on the same chain,
    // so without this a session shutdown waits out CONNECT_TIMEOUT_MILLISECONDS on a child that is
    // never going to answer — thirty seconds of a host blocked on a connection nobody wants.
    this.connectAbort?.abort(new Error(BRIDGE_CLOSED_MESSAGE));
    const teardown = async () => {
      const connecting = this.connecting;
      this.connecting = undefined;

      if (connecting) {
        try {
          await this.retire(await connecting);
        } catch {
          // A failed connection has already closed its transport.
        }
      }

      const active = this.active;
      this.active = undefined;
      if (active) await this.closeConnection(active);

      // Terminal means terminal: a displaced connection still waiting on a call is closed too,
      // rather than outliving the session that owns it.
      const retiring = [...this.retiring.values()].flatMap((connections) => [...connections]);
      this.retiring.clear();
      await Promise.all(retiring.map((connection) => this.closeConnection(connection)));
    };

    // Queued on the same chain as connecting, so teardown cannot interleave with an open.
    const next = this.connectionChain.then(teardown, teardown);
    this.connectionChain = next.then(() => undefined, () => undefined);
    await next;
  }

  /**
   * Serialised, so open, promote and close never interleave. Two calls that arrive together with
   * different working directories are handled one after the other: the second retires the first
   * connection before opening its own, instead of both completing and one being dropped unclosed.
   * Retiring, not closing — the displaced connection is closed by whichever call on it settles last.
   */
  private connection(cwd: string, signal?: AbortSignal): Promise<ActiveConnection> {
    const establish = () => this.establish(cwd, signal);
    const next = this.connectionChain.then(establish, establish);
    this.connectionChain = next.then(() => undefined, () => undefined);
    return next;
  }

  private async establish(cwd: string, signal?: AbortSignal): Promise<ActiveConnection> {
    if (this.closed) throw new Error(BRIDGE_CLOSED_MESSAGE);
    // A connection whose child went away is not reused, whatever directory the next call is for.
    // `retire` normally clears it from here as it loses it; this covers the order where it was
    // lost before it was ever installed. Dropping the reference strands nothing: whoever marked it
    // lost or closing is already closing it.
    if (this.active && !isUsable(this.active)) this.active = undefined;
    if (this.active?.cwd === cwd) return this.active;

    // The connection's cwd is pinned to the call that opened it. A call for another directory needs
    // its own child, and the one it displaces is retired rather than abandoned — retired, not
    // closed, because a sibling call may still be waiting on it, and closing under a live call
    // rejects that call with a ConnectionClosed it did nothing to earn.
    if (this.active) await this.retire(this.active);

    // That retire yielded, and close() may have run underneath it. Refuse here, before anything is
    // opened or handed out: close() is queued behind this establish on the chain, so a child
    // started now is a child spawned after teardown, and teardown then waits out this connect —
    // CONNECT_TIMEOUT_MILLISECONDS of a host blocked on a server nobody will ever call.
    if (this.closed) throw new Error(BRIDGE_CLOSED_MESSAGE);

    // A connection for this directory may still be draining a call that outlived a cwd switch.
    // Adopt it rather than start a second child for the same tree: the server is already running
    // there, and leaving it to drain while its replacement serves the same directory is two
    // children doing one directory's work.
    const draining = this.adoptable(cwd);
    if (draining) return this.adopt(draining);

    const pending = this.open(cwd, signal);
    this.connecting = pending;

    try {
      const connection = await pending;
      // The chain serialises callers, but transport.onclose and close() can still run while open()
      // is awaited. Promote defensively: never overwrite a live connection, never install one into
      // a bridge that has since closed.
      if (this.active && this.active !== connection) await this.retire(this.active);
      if (this.closed) {
        await this.closeConnection(connection);
        throw new Error(BRIDGE_CLOSED_MESSAGE);
      }
      // A child can answer the handshake and go away while the connect is still settling — a stdio
      // transport reports a malformed line through `onerror` and the exit through `onclose`, and
      // either marks this connection lost before it is ever installed. Promoting it anyway pinned
      // a dead pipe as the session's connection: every later call was answered `Not connected`,
      // which is not a transport error the bridge reconnects on, so the session never recovered.
      if (!isUsable(connection)) {
        await this.closeConnection(connection);
        throw new Error(
          `Roundtable connection failed: the server for ${cwd} closed while it was being established.`,
        );
      }
      this.active = connection;
      return connection;
    } finally {
      if (this.connecting === pending) this.connecting = undefined;
    }
  }

  /**
   * Resolve the command and the child environment once per session. The
   * registration file supplies the provider and CLI-path block; the ambient
   * process environment still supplies the secrets those entries name.
   */
  private resolveRegistration(): { command: string; env: Record<string, string> } {
    if (this.registration) return this.registration;
    const config = this.loadConfig();
    // One environment governs both fields. Resolving the command from the unmerged process
    // environment is what made a ROUNDTABLE_BIN in the registration file's own `env` block reach the
    // child while never selecting it (BUG-0003).
    const env = { ...this.processEnvironment, ...config.env };
    const resolved = this.explicitCommand
      ? { command: this.explicitCommand, source: "config" as const }
      : resolveRoundtableCommand(env, this.bundledBinary, config.command);

    // Memoized with the registration, so this is one warning per session rather than one per call.
    if (resolved.source === "path" && this.bundledBinary) {
      this.warn(bundledBinaryFallbackWarning(
        this.bundledBinary,
        resolved.command,
        commandLocation(resolved.command, env),
      ));
    }

    this.registration = { command: resolved.command, env };
    return this.registration;
  }

  private async open(cwd: string, signal?: AbortSignal): Promise<ActiveConnection> {
    const registration = this.resolveRegistration();
    const transport = this.createTransport({
      command: registration.command,
      args: ["stdio"],
      cwd,
      env: registration.env,
      stderr: "pipe",
      maxBufferSize: 10 * 1024 * 1024,
    });
    const client = this.createClient();
    const connection: ActiveConnection = {
      client,
      cwd,
      transport,
      deadlines: [],
      retired: false,
      stderr: "",
      stderrLine: "",
    };

    transport.stderr?.on("data", (chunk) => {
      const text = String(chunk);
      connection.stderr = `${connection.stderr}${text}`.slice(-STDERR_TAIL_BYTES);
      this.surfaceWarnings(connection, text);
    });
    // A child that errors or exits takes its connection with it. Retire it rather than merely drop
    // it: `active = undefined` on its own leaves the child in neither `active` nor the drain index,
    // and close() closes only what it can see, so the child outlives the session that started it.
    // This is not a theoretical path — a stdio transport reports one malformed stdout line through
    // `onerror` and keeps running, so the orphan it made was a live child. Retiring marks the
    // connection lost (never adopted again), closes it when nothing is in flight, and otherwise
    // hands it to the drain index, where its last call or its expiry closes it. `retire` does that
    // bookkeeping before its first await, so it is done by the time this callback returns.
    const lose = () => void this.retire(connection, "lost").catch(() => undefined);
    transport.onerror = lose;
    transport.onclose = lose;

    // close() aborts through this controller. The caller's own signal is honoured as well, so a
    // cancelled tool call does not leave a connect running for a panel nobody is waiting for.
    const controller = new AbortController();
    this.connectAbort = controller;
    // A bridge that closed between the caller's check and here gets its abort now rather than
    // through close(), which has already made whatever abort it was going to make.
    if (this.closed) controller.abort(new Error(BRIDGE_CLOSED_MESSAGE));
    const connectSignal = signal ? AbortSignal.any([signal, controller.signal]) : controller.signal;

    try {
      await client.connect(transport, {
        signal: connectSignal,
        timeout: CONNECT_TIMEOUT_MILLISECONDS,
        maxTotalTimeout: CONNECT_TIMEOUT_MILLISECONDS,
      });
      return connection;
    } catch (error) {
      await this.closeConnection(connection);
      if (this.closed) throw new Error(BRIDGE_CLOSED_MESSAGE);
      throw this.describeFailure(error, "connect", connection);
    } finally {
      if (this.connectAbort === controller) this.connectAbort = undefined;
    }
  }

  /**
   * Repeat the server's own session-level warnings to the user, once each.
   *
   * The child's stderr is only ever read back on a failure, so an advisory decided at startup — a
   * `ROUNDTABLE_DEFAULT_AGENTS` that would not parse, and the built-in panel used in its place —
   * reached nobody at all in a session whose calls all succeeded, which is exactly the session in
   * which the operator is being given a panel they did not configure. It goes out through the same
   * channel as the missing-binary warning, deduplicated across children and capped, so a cwd switch
   * does not repeat what the first server already said.
   */
  private surfaceWarnings(connection: ActiveConnection, chunk: string): void {
    // Whole lines only: a warning split across two reads is one warning, not two halves.
    const lines = `${connection.stderrLine}${chunk}`.split("\n");
    connection.stderrLine = lines.pop() ?? "";
    for (const line of lines) {
      const warning = serverWarningLine(line);
      if (warning === undefined || this.surfacedWarnings.has(warning)) continue;
      if (this.surfacedWarnings.size >= MAX_SURFACED_WARNINGS) return;
      this.surfacedWarnings.add(warning);
      this.warn(`WARNING Roundtable server: ${warning}`);
    }
  }

  /** Drop one call's deadline from its connection. Idempotent: a call releases its own once. */
  private release(connection: ActiveConnection, deadline: number): void {
    const entry = connection.deadlines.indexOf(deadline);
    if (entry !== -1) connection.deadlines.splice(entry, 1);
  }

  /**
   * Detach a connection from the bridge. It closes immediately when idle, and otherwise when its
   * last in-flight call settles — see the release step in `callTool` — or when that call's own
   * deadline passes, whichever comes first. `health` says whether the connection is still worth
   * adopting: a connection retired because its transport failed is not.
   */
  private async retire(connection: ActiveConnection, health: "healthy" | "lost" = "healthy"): Promise<void> {
    if (this.active === connection) this.active = undefined;
    connection.retired = true;
    if (health === "lost") connection.lost = true;
    // Three ways there is nothing to drain into. Idle is the ordinary one. The other two are the
    // shutdown path: close() has already emptied the drain index, and a connection put back into it
    // after that is a child nothing will ever close again, still holding an expiry timer for a
    // bridge that is gone. Closing is terminal for the same reason.
    if (connection.deadlines.length === 0 || this.closed || connection.closing !== undefined) {
      await this.discard(connection);
      return;
    }
    this.remember(connection);
    this.armExpiry(connection);
  }

  /** The draining connection for `cwd` that a new call can still be given, if there is one. */
  private adoptable(cwd: string): ActiveConnection | undefined {
    for (const connection of this.retiring.get(cwd) ?? []) {
      if (isUsable(connection)) return connection;
    }
    return undefined;
  }

  /** Flip a draining connection back to active. The drain is over, so its expiry is disarmed. */
  private adopt(connection: ActiveConnection): ActiveConnection {
    this.disarmExpiry(connection);
    this.forget(connection);
    connection.retired = false;
    this.active = connection;
    return connection;
  }

  private remember(connection: ActiveConnection): void {
    const draining = this.retiring.get(connection.cwd) ?? new Set<ActiveConnection>();
    draining.add(connection);
    this.retiring.set(connection.cwd, draining);
  }

  /** Drop a connection from the drain index. Safe on a connection that was never in it. */
  private forget(connection: ActiveConnection): void {
    const draining = this.retiring.get(connection.cwd);
    if (!draining) return;
    draining.delete(connection);
    if (draining.size === 0) this.retiring.delete(connection.cwd);
  }

  /**
   * A drain is bounded by the calls draining on it. A grace period past the last of their deadlines
   * the caller has already been answered — with a timeout, if nothing else — so a child still
   * holding the line is closed rather than left running until the session ends. Armed from the
   * deadlines as they are now: a call that settles re-arms this on what it left behind.
   */
  private armExpiry(connection: ActiveConnection): void {
    this.disarmExpiry(connection);
    if (connection.deadlines.length === 0) return;
    const last = Math.max(...connection.deadlines) + EXPIRY_GRACE_MILLISECONDS;
    const expiry = setTimeout(
      () => void this.expire(connection).catch(() => undefined),
      Math.max(0, last - Date.now()),
    );
    // A drain must never be the reason a host process stays up.
    expiry.unref?.();
    connection.expiry = expiry;
  }

  private async expire(connection: ActiveConnection): Promise<void> {
    connection.expiry = undefined;
    // Adopted back, or settled and closed already, between the timer being armed and firing.
    if (!connection.retired || connection.deadlines.length === 0) return;
    await this.discard(connection);
  }

  /** Take a connection out of the drain index and close it. */
  private async discard(connection: ActiveConnection): Promise<void> {
    this.forget(connection);
    await this.closeConnection(connection);
  }

  private disarmExpiry(connection: ActiveConnection): void {
    if (connection.expiry === undefined) return;
    clearTimeout(connection.expiry);
    connection.expiry = undefined;
  }

  private async closeConnection(connection: ActiveConnection): Promise<void> {
    this.disarmExpiry(connection);
    if (connection.closing !== undefined) {
      await connection.closing;
      return;
    }
    // Published before either close is called rather than after both have started. A transport that
    // reports its own close synchronously re-enters here through `lose` while these two calls are
    // still being made, and a teardown that is invisible until it returns would be started twice.
    let finished: () => void;
    connection.closing = new Promise<void>((resolve) => {
      finished = resolve;
    });
    await Promise.allSettled([connection.client.close(), connection.transport.close()]);
    finished!();
    await connection.closing;
  }

  /**
   * Classify on the error, display the text.
   *
   * The install advice is only actionable for a child that never started, so only a start failure
   * may select it. Three things decide that, and none of them is the child's stderr: the phase,
   * because a call that failed was made over a connection that had already been established, and
   * the error's own `syscall` and `code`. Each start failure then gets the remedy that fits it —
   * a missing binary, a binary that is not executable, or a working directory that is gone — and a
   * connect that failed for any other reason says so rather than borrowing the call phase's words.
   * The stderr tail is the server's diagnostic channel about its panelists
   * — a CLI that is not installed is a supported state that reports `status: not_found`, and such a
   * line, or a whole success-shaped payload carrying one, used to be enough to blame a working
   * install for a timeout. It is still printed, because it is the most useful part of the message.
   * It is no longer read. A `level=WARN` line from it is also repeated to the user as it arrives
   * (`surfaceWarnings`), because a session whose calls all succeed never reaches this method.
   */
  private describeFailure(error: unknown, phase: "connect" | "call", connection?: ActiveConnection): Error {
    const message = error instanceof Error ? error.message : String(error);
    // This connection's own tail. A bridge-wide one reported whichever child last wrote a line.
    const stderr = (connection?.stderr ?? "").trim();
    const detail = stderr ? `${message}\n${stderr}` : message;
    // A call is made over a connection that had already been established, so no failure of one is a
    // start failure, however its text or its code reads.
    if (phase === "call") return new Error(`Roundtable MCP call failed: ${detail}`);

    const { code, syscall } = (error ?? {}) as { code?: unknown; syscall?: unknown };
    const failedCode = typeof code === "string" ? code : undefined;
    // Node reports a failed spawn with `syscall: 'spawn <command>'` and the OS code beside it. Both
    // are read as fields: the command they name may contain spaces, which is exactly what a pattern
    // over the message gets wrong. The pattern survives only as a fallback for an error wrapped on
    // its way up and stripped of its fields, so it is consulted only when there is no code at all.
    const spawned = typeof syscall === "string" && syscall.startsWith("spawn");
    // A stripped error leaves only the sentence, and the sentence names the OS code. Both codes are
    // read out of it, not just the missing-binary one: a wrapped EACCES classified as "some other
    // connection failure" sent the reader to reinstall a binary whose permission bit is the whole
    // problem, when `chmod +x` was one line away.
    const stripped = failedCode === undefined ? /^spawn .+ (ENOENT|EACCES)$/.exec(message)?.[1] : undefined;
    const startFailure = spawned
      || failedCode === "ENOENT"
      || failedCode === "EACCES"
      || stripped !== undefined;
    if (!startFailure) return new Error(`Roundtable connection failed: ${detail}`);

    const command = this.registration?.command ?? "roundtable";
    if (failedCode === "EACCES" || stripped === "EACCES") {
      return new Error(
        `Roundtable could not start '${command}': the file exists but is not executable. `
        + `Run 'chmod +x ${command}', or set ROUNDTABLE_BIN to a binary you can run. ${detail}`,
      );
    }
    if (failedCode !== undefined && failedCode !== "ENOENT") {
      return new Error(`Roundtable could not start '${command}'. ${detail}`);
    }

    // ENOENT from a spawn names the command whichever file was missing, so a working directory that
    // no longer exists reads exactly like an uninstalled binary — and sends the reader off to
    // reinstall a binary that is sitting right there. Look at both, and report the one that is gone.
    const environment = this.registration?.env ?? this.processEnvironment;
    const cwd = connection?.cwd;
    if (commandLocation(command, environment) && cwd !== undefined && !existsSync(cwd)) {
      return new Error(
        `Roundtable could not start '${command}': its working directory ${cwd} does not exist. `
        + `The binary is installed; the directory the call was made for is not. ${detail}`,
      );
    }
    return new Error(
      `Roundtable could not start '${command}'. Install the matching release binary or set ROUNDTABLE_BIN. ${detail}`,
    );
  }
}
