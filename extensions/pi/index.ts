import { defineTool, type ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { Type, type Static } from "typebox";

import { RoundtableBridge, type RoundtableResult } from "./bridge.ts";

const jsonObject = Type.Record(Type.String(), Type.Unknown());

const dispatchParameters = Type.Object({
  prompt: Type.String({ description: "Question or task for the panel" }),
  files: Type.Optional(Type.String({ description: "Comma-separated relative file paths" })),
  timeout: Type.Optional(Type.Integer({ minimum: 1, maximum: 900, description: "Seconds per provider" })),
  codex_model: Type.Optional(Type.String()),
  claude_model: Type.Optional(Type.String()),
  copilot_model: Type.Optional(Type.String()),
  codex_resume: Type.Optional(Type.String()),
  claude_resume: Type.Optional(Type.String()),
  antigravity_resume: Type.Optional(Type.String()),
  copilot_resume: Type.Optional(Type.String()),
  agents: Type.Optional(Type.String({ description: "JSON-encoded selective agent specification" })),
  schema: Type.Optional(Type.Union([jsonObject, Type.Null()])),
}, { additionalProperties: false });

type DispatchParameters = Static<typeof dispatchParameters>;

const convergeParameters = Type.Object({
  prior_result: jsonObject,
  original_prompt: Type.String(),
}, { additionalProperties: false });

type ConvergeParameters = Static<typeof convergeParameters>;

const dispatchTools = [
  {
    name: "roundtable-canvass",
    label: "Roundtable Canvass",
    description: "Canvass the panel in parallel. Each provider answers independently under the default analyst role; the caller synthesizes.",
  },
  {
    name: "roundtable-deliberate",
    label: "Roundtable Deliberate",
    description: "Deliberate a hard problem. Each provider weighs alternatives and states confidence under the planner role.",
  },
  {
    name: "roundtable-blueprint",
    label: "Roundtable Blueprint",
    description: "Blueprint an implementation. Each provider produces phases, dependencies, risks, and milestones under the planner role.",
  },
  {
    name: "roundtable-critique",
    label: "Roundtable Critique",
    description: "Critique adversarially. Each provider hunts for flaws, risks, and weaknesses under the code-reviewer role.",
  },
  {
    name: "roundtable-crosscheck",
    label: "Roundtable Crosscheck",
    description: "Crosscheck one prompt from mixed planner, code-reviewer, and generalist roles across the selected panel.",
  },
] as const;

function isTextBlock(block: unknown): block is { type: "text"; text: string } {
  return typeof block === "object"
    && block !== null
    && (block as { type?: unknown }).type === "text"
    && typeof (block as { text?: unknown }).text === "string";
}

export function textFromRoundtableResult(result: RoundtableResult): string {
  const text = result.content
    .filter(isTextBlock)
    .map((block) => block.text)
    .join("\n")
    .trim();

  if (text) return text;
  if (result.structuredContent !== undefined) return JSON.stringify(result.structuredContent);
  return JSON.stringify(result);
}

export default function roundtablePiExtension(pi: ExtensionAPI): void {
  const bridge = new RoundtableBridge();

  for (const spec of dispatchTools) {
    pi.registerTool(defineTool({
      name: spec.name,
      label: spec.label,
      description: spec.description,
      parameters: dispatchParameters,
      async execute(_toolCallId, parameters: DispatchParameters, signal, _onUpdate, context) {
        const result = await bridge.callTool(spec.name, parameters, context.cwd, signal);
        const text = textFromRoundtableResult(result);
        if (result.isError) throw new Error(text || `${spec.label} failed`);
        return {
          content: [{ type: "text" as const, text }],
          details: { server: "roundtable", tool: spec.name, status: "ok" },
        };
      },
    }));
  }

  pi.registerTool(defineTool({
    name: "roundtable-converge",
    label: "Roundtable Converge",
    description: "Replay a prior dispatch through redacted-peer convergence so each provider can hold or revise its stance.",
    parameters: convergeParameters,
    async execute(_toolCallId, parameters: ConvergeParameters, signal, _onUpdate, context) {
      const result = await bridge.callTool("roundtable-converge", parameters, context.cwd, signal);
      const text = textFromRoundtableResult(result);
      if (result.isError) throw new Error(text || "Roundtable Converge failed");
      return {
        content: [{ type: "text" as const, text }],
        details: { server: "roundtable", tool: "roundtable-converge", status: "ok" },
      };
    },
  }));

  pi.on("session_shutdown", async () => {
    await bridge.close();
  });
}
