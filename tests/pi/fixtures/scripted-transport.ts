import { LATEST_PROTOCOL_VERSION, type JSONRPCMessage } from "@modelcontextprotocol/sdk/types.js";

/**
 * A transport the real `@modelcontextprotocol/sdk` client can drive, with the server side scripted
 * in-process. Fakes that stand in for the client cannot show what the SDK does to a sibling call
 * when a shared connection closes — that behaviour lives in `Protocol._onclose()`, which only runs
 * when a real client is wired to a real transport. This is the smallest transport that lets the
 * SDK do it for itself.
 */
export type ToolResponder = (
  name: string,
  arguments_: Record<string, unknown>,
) => Promise<{ result?: unknown; error?: { code: number; message: string } }>;

export interface ScriptedTransportOptions {
  /** How the scripted server answers `tools/call`. */
  onTool?: ToolResponder;
  /** Milliseconds before `initialize` is answered. `"never"` leaves the connect hanging. */
  initializeDelay?: number | "never";
}

export class ScriptedTransport {
  onclose?: () => void;
  onerror?: (error: Error) => void;
  onmessage?: (message: JSONRPCMessage) => void;
  sessionId?: string;
  /** The bridge reads `transport.stderr?.on(...)`; a scripted child has no stderr pipe. */
  readonly stderr = undefined;
  /** Both the SDK client and the bridge close a transport, so this counts more than one teardown. */
  closeCalls = 0;
  started = false;
  readonly sent: JSONRPCMessage[] = [];

  private closed = false;

  /** Whether this transport is shut, which is what a test about lifecycle actually means. */
  get isClosed(): boolean {
    return this.closed;
  }
  private readonly onTool: ToolResponder;
  private readonly initializeDelay: number | "never";

  constructor(options: ScriptedTransportOptions = {}) {
    this.onTool = options.onTool ?? (async () => ({ result: { content: [{ type: "text", text: "ok" }] } }));
    this.initializeDelay = options.initializeDelay ?? 0;
  }

  async start(): Promise<void> {
    this.started = true;
  }

  async send(message: JSONRPCMessage): Promise<void> {
    this.sent.push(message);
    const request = message as { id?: string | number; method?: string; params?: Record<string, unknown> };
    if (request.id === undefined || request.method === undefined) return;

    if (request.method === "initialize") {
      if (this.initializeDelay === "never") return;
      this.later(this.initializeDelay, () => this.reply(request.id!, {
        protocolVersion: LATEST_PROTOCOL_VERSION,
        capabilities: { tools: {} },
        serverInfo: { name: "scripted-roundtable", version: "0.0.0" },
      }));
      return;
    }

    if (request.method === "tools/call") {
      const parameters = (request.params ?? {}) as { name?: string; arguments?: Record<string, unknown> };
      void this.onTool(parameters.name ?? "", parameters.arguments ?? {}).then((outcome) => {
        if (this.closed) return;
        if (outcome.error) this.replyError(request.id!, outcome.error);
        else this.reply(request.id!, outcome.result ?? {});
      });
      return;
    }

    // Anything else (ping, tools/list) gets an empty result so the SDK is never left waiting.
    this.reply(request.id, {});
  }

  async close(): Promise<void> {
    this.closeCalls += 1;
    if (this.closed) return;
    this.closed = true;
    this.onclose?.();
  }

  private later(milliseconds: number, action: () => void): void {
    if (milliseconds <= 0) {
      queueMicrotask(action);
      return;
    }
    setTimeout(action, milliseconds).unref?.();
  }

  private reply(id: string | number, result: unknown): void {
    if (this.closed) return;
    this.onmessage?.({ jsonrpc: "2.0", id, result } as JSONRPCMessage);
  }

  private replyError(id: string | number, error: { code: number; message: string }): void {
    if (this.closed) return;
    this.onmessage?.({ jsonrpc: "2.0", id, error } as JSONRPCMessage);
  }
}
