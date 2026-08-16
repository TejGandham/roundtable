import { existsSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import {
  StdioClientTransport,
  type StdioServerParameters,
} from "@modelcontextprotocol/sdk/client/stdio.js";

export const ROUNDTABLE_SERVER_NAME = "roundtable";
export const ROUNDTABLE_SERVER_VERSION = "2.1.3";
export const DEFAULT_AGENT_TIMEOUT_SECONDS = 900;
export const MCP_OVERHEAD_MILLISECONDS = 120_000;
export const CONNECT_TIMEOUT_MILLISECONDS = 30_000;
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
  command?: string;
  createClient?: () => ClientLike;
  createTransport?: (parameters: StdioServerParameters) => TransportLike;
  environment?: NodeJS.ProcessEnv;
}

interface ActiveConnection {
  client: ClientLike;
  cwd: string;
  transport: TransportLike;
}

function stringEnvironment(environment: NodeJS.ProcessEnv): Record<string, string> {
  return Object.fromEntries(
    Object.entries(environment).filter((entry): entry is [string, string] => typeof entry[1] === "string"),
  );
}

export function resolveRoundtableCommand(
  environment: NodeJS.ProcessEnv = process.env,
  bundledBinary?: string,
): string {
  const configured = environment.ROUNDTABLE_BIN?.trim();
  if (configured) return configured;
  if (bundledBinary && existsSync(bundledBinary)) return bundledBinary;
  return "roundtable";
}

export function mcpRequestTimeoutMilliseconds(arguments_: RoundtableArguments): number {
  const requested = arguments_.timeout;
  const seconds = typeof requested === "number" && Number.isInteger(requested) && requested >= 1 && requested <= 900
    ? requested
    : DEFAULT_AGENT_TIMEOUT_SECONDS;
  return seconds * 1_000 + MCP_OVERHEAD_MILLISECONDS;
}

export class RoundtableBridge {
  readonly command: string;

  private readonly createClient: () => ClientLike;
  private readonly createTransport: (parameters: StdioServerParameters) => TransportLike;
  private readonly environment: Record<string, string>;
  private active?: ActiveConnection;
  private connecting?: Promise<ActiveConnection>;
  private connectingCwd?: string;
  private stderr = "";

  constructor(options: RoundtableBridgeOptions = {}) {
    const sourceEnvironment = options.environment ?? process.env;
    this.command = options.command ?? resolveRoundtableCommand(sourceEnvironment, BUNDLED_ROUNDTABLE_BINARY);
    this.environment = stringEnvironment(sourceEnvironment);
    this.createClient = options.createClient ?? (() => new Client(
      { name: "roundtable-pi", version: ROUNDTABLE_SERVER_VERSION },
      { capabilities: {} },
    ));
    this.createTransport = options.createTransport ?? ((parameters) => new StdioClientTransport(parameters));
  }

  async callTool(
    name: string,
    arguments_: RoundtableArguments,
    cwd: string,
    signal?: AbortSignal,
  ): Promise<RoundtableResult> {
    const connection = await this.connection(cwd, signal);
    const timeout = mcpRequestTimeoutMilliseconds(arguments_);

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
      await this.discard(connection);
      throw this.describeFailure(error);
    }
  }

  async close(): Promise<void> {
    const connecting = this.connecting;
    this.connecting = undefined;
    this.connectingCwd = undefined;

    if (connecting) {
      try {
        const connection = await connecting;
        await this.discard(connection);
      } catch {
        // A failed connection has already closed its transport.
      }
    }

    const active = this.active;
    this.active = undefined;
    if (active) await this.closeConnection(active);
  }

  private async connection(cwd: string, signal?: AbortSignal): Promise<ActiveConnection> {
    if (this.active?.cwd === cwd) return this.active;
    if (this.connecting && this.connectingCwd === cwd) return this.connecting;

    if (this.active) {
      const previous = this.active;
      this.active = undefined;
      await this.closeConnection(previous);
    }

    this.connectingCwd = cwd;
    const pending = this.open(cwd, signal);
    this.connecting = pending;

    try {
      const connection = await pending;
      this.active = connection;
      return connection;
    } finally {
      if (this.connecting === pending) {
        this.connecting = undefined;
        this.connectingCwd = undefined;
      }
    }
  }

  private async open(cwd: string, signal?: AbortSignal): Promise<ActiveConnection> {
    this.stderr = "";
    const transport = this.createTransport({
      command: this.command,
      args: ["stdio"],
      cwd,
      env: this.environment,
      stderr: "pipe",
      maxBufferSize: 10 * 1024 * 1024,
    });
    const client = this.createClient();
    const connection = { client, cwd, transport };

    transport.stderr?.on("data", (chunk) => {
      this.stderr = `${this.stderr}${String(chunk)}`.slice(-16_384);
    });
    transport.onerror = () => {
      if (this.active?.transport === transport) this.active = undefined;
    };
    transport.onclose = () => {
      if (this.active?.transport === transport) this.active = undefined;
    };

    try {
      await client.connect(transport, {
        signal,
        timeout: CONNECT_TIMEOUT_MILLISECONDS,
        maxTotalTimeout: CONNECT_TIMEOUT_MILLISECONDS,
      });
      return connection;
    } catch (error) {
      await this.closeConnection(connection);
      throw this.describeFailure(error);
    }
  }

  private async discard(connection: ActiveConnection): Promise<void> {
    if (this.active === connection) this.active = undefined;
    await this.closeConnection(connection);
  }

  private async closeConnection(connection: ActiveConnection): Promise<void> {
    await Promise.allSettled([
      connection.client.close(),
      connection.transport.close(),
    ]);
  }

  private describeFailure(error: unknown): Error {
    const message = error instanceof Error ? error.message : String(error);
    const stderr = this.stderr.trim();
    const detail = stderr ? `${message}\n${stderr}` : message;
    if (/ENOENT|not found|spawn/i.test(detail)) {
      return new Error(
        `Roundtable could not start '${this.command}'. Install the matching release binary or set ROUNDTABLE_BIN. ${detail}`,
      );
    }
    return new Error(`Roundtable MCP call failed: ${detail}`);
  }
}
