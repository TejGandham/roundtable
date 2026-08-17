import { readFileSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";

export interface RoundtableConfig {
  command?: string;
  env: Record<string, string>;
}

const ALLOWED_KEYS = new Set(["command", "env"]);

/** The Pi-owned registration file, the native equivalent of one MCP server entry. */
export function roundtableConfigPath(environment: NodeJS.ProcessEnv = process.env): string {
  const agentDir = environment.PI_CODING_AGENT_DIR?.trim() || join(homedir(), ".pi", "agent");
  return join(agentDir, "roundtable.json");
}

/**
 * Read the operator's registration file. An absent file is the empty
 * registration; a present but malformed one fails closed, because silently
 * dropping a provider block would produce a smaller panel that still looks fine.
 */
export function loadRoundtableConfig(path: string): RoundtableConfig {
  let raw: string;
  try {
    raw = readFileSync(path, "utf8");
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code === "ENOENT") return { env: {} };
    throw new Error(`Roundtable could not read ${path}: ${(error as Error).message}`);
  }

  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch (error) {
    throw new Error(`Roundtable config ${path} is not valid JSON: ${(error as Error).message}`);
  }

  if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
    throw new Error(`Roundtable config ${path} must be a JSON object`);
  }

  const entries = parsed as Record<string, unknown>;
  for (const key of Object.keys(entries)) {
    if (!ALLOWED_KEYS.has(key)) {
      throw new Error(`Roundtable config ${path} has unknown key '${key}'; expected 'command' or 'env'`);
    }
  }

  const config: RoundtableConfig = { env: {} };

  if (entries.command !== undefined) {
    if (typeof entries.command !== "string" || entries.command.trim() === "") {
      throw new Error(`Roundtable config ${path}: 'command' must be a non-empty string`);
    }
    config.command = entries.command.trim();
  }

  if (entries.env !== undefined) {
    if (typeof entries.env !== "object" || entries.env === null || Array.isArray(entries.env)) {
      throw new Error(`Roundtable config ${path}: 'env' must be a JSON object`);
    }
    for (const [key, value] of Object.entries(entries.env as Record<string, unknown>)) {
      if (typeof value !== "string") {
        throw new Error(`Roundtable config ${path}: env['${key}'] must be a string`);
      }
      config.env[key] = value;
    }
  }

  return config;
}
