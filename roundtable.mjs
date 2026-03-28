#!/usr/bin/env node

import { spawn } from 'node:child_process';
import { readFileSync, statSync, accessSync, constants as fsConstants } from 'node:fs';
import { resolve, dirname, join, delimiter } from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = dirname(fileURLToPath(import.meta.url));

// --- Recursion guard ---
// Prevents infinite loops when Codex/Gemini try to run roundtable inside themselves
const RECURSION_ENV = 'ROUNDTABLE_ACTIVE';
if (process.env[RECURSION_ENV]) {
  console.log(JSON.stringify({
    error: 'Recursive invocation detected. Roundtable is already running in a parent process.',
  }));
  process.exit(1);
}

// --- Argument parsing ---

class ArgError extends Error {}

function parseArgs(argv) {
  const args = {
    prompt: '',
    role: 'default',
    geminiRole: null,
    codexRole: null,
    files: [],
    geminiModel: null,       // null = let CLI use its configured default
    codexModel: null,        // null = let CLI use its configured default
    codexReasoning: null,    // e.g. 'xhigh', 'high', 'medium' — passed as -c reasoning_effort="..."
    timeout: 900,
    rolesDir: resolve(__dirname, 'roles'),
    projectRolesDir: null,
    geminiResume: null,
    codexResume: null,
  };

  const requireNext = (flag, next) => {
    if (next === undefined || next.startsWith('--')) {
      throw new ArgError(`Flag ${flag} requires a value`);
    }
    return next;
  };

  for (let i = 2; i < argv.length; i++) {
    const arg = argv[i];
    const next = argv[i + 1];
    switch (arg) {
      case '--prompt': args.prompt = requireNext(arg, next); i++; break;
      case '--role': args.role = requireNext(arg, next); i++; break;
      case '--gemini-role': args.geminiRole = requireNext(arg, next); i++; break;
      case '--codex-role': args.codexRole = requireNext(arg, next); i++; break;
      case '--files': args.files = requireNext(arg, next).split(',').map(f => f.trim()).filter(Boolean); i++; break;
      case '--gemini-model': args.geminiModel = requireNext(arg, next); i++; break;
      case '--codex-model': args.codexModel = requireNext(arg, next); i++; break;
      case '--timeout': args.timeout = parseInt(requireNext(arg, next), 10); i++; break;
      case '--roles-dir': args.rolesDir = resolve(requireNext(arg, next)); i++; break;
      case '--project-roles-dir': args.projectRolesDir = resolve(requireNext(arg, next)); i++; break;
      case '--codex-reasoning': args.codexReasoning = requireNext(arg, next); i++; break;
      case '--gemini-resume': args.geminiResume = requireNext(arg, next); i++; break;
      case '--codex-resume': args.codexResume = requireNext(arg, next); i++; break;
      default:
        if (!arg.startsWith('--') && !args.prompt) {
          args.prompt = arg;
        }
    }
  }

  if (!args.prompt) {
    throw new ArgError('Missing required --prompt argument');
  }

  if (isNaN(args.timeout) || args.timeout <= 0) {
    throw new ArgError('--timeout must be a positive integer');
  }

  return args;
}

// --- Portable CLI lookup ---

function findExecutable(name) {
  const pathDirs = (process.env.PATH || '').split(delimiter);
  for (const dir of pathDirs) {
    const candidate = join(dir, name);
    try {
      accessSync(candidate, fsConstants.X_OK);
      return candidate;
    } catch { /* not here */ }
  }
  return null;
}

// --- CLI health probe (fast fail before committing to timeout) ---

function probeCli(executable, testArgs, probeTimeoutMs = 5000) {
  return new Promise((resolvePromise) => {
    const proc = spawn(executable, testArgs, {
      stdio: ['ignore', 'pipe', 'pipe'],
      env: { ...process.env, [RECURSION_ENV]: '1' },
    });

    let stdout = '';
    proc.stdout.on('data', (chunk) => { stdout += chunk.toString(); });

    const timer = setTimeout(() => {
      proc.kill('SIGTERM');
      setTimeout(() => {
        try { proc.kill('SIGKILL'); } catch { /* already dead */ }
      }, 2000);
      resolvePromise({ alive: false, reason: 'probe timeout' });
    }, probeTimeoutMs);

    proc.on('close', (code) => {
      clearTimeout(timer);
      resolvePromise({
        alive: code === 0,
        exit_code: code,
        stdout: stdout.trim(),
        reason: code !== 0 ? `exited with code ${code}` : undefined,
      });
    });

    proc.on('error', (err) => {
      clearTimeout(timer);
      resolvePromise({ alive: false, reason: err.message });
    });
  });
}

// --- Role prompt resolution (project-local then global, ENOENT-only fallthrough) ---

function loadRolePrompt(roleName, globalDir, projectDir) {
  const filename = `${roleName}.txt`;

  if (projectDir) {
    try {
      return readFileSync(resolve(projectDir, filename), 'utf-8');
    } catch (err) {
      if (err.code !== 'ENOENT') throw err; // permission errors surface, not swallowed
    }
  }

  try {
    return readFileSync(resolve(globalDir, filename), 'utf-8');
  } catch (err) {
    if (err.code === 'ENOENT') {
      throw new Error(`Role prompt not found: ${roleName} (searched ${projectDir || 'none'}, ${globalDir})`);
    }
    throw err; // permission errors surface
  }
}

// --- File references (path + size, not content) ---

function formatFileReferences(filePaths) {
  if (!filePaths.length) return '';

  const refs = filePaths.map(fp => {
    try {
      const st = statSync(fp);
      return `- ${fp} (${st.size} bytes)`;
    } catch {
      return `- ${fp} (unavailable)`;
    }
  });

  return '=== FILES ===\n' + refs.join('\n') +
    '\n\nReview the files listed above using your own tools to read their contents.';
}

// --- Prompt assembly ---

function assemblePrompt(rolePrompt, userRequest, fileRefs) {
  const sections = [rolePrompt.trim()];
  sections.push('=== REQUEST ===\n' + userRequest.trim());
  if (fileRefs) sections.push(fileRefs);
  return sections.join('\n\n');
}

// --- Gemini JSON parser ---

function parseGeminiOutput(stdout, stderr) {
  try {
    const data = JSON.parse(stdout);
    const response = typeof data.response === 'string' ? data.response : '';
    const metadata = {};

    if (data.stats?.models) {
      const modelName = Object.keys(data.stats.models)[0];
      if (modelName) {
        metadata.model_used = modelName;
        metadata.tokens = data.stats.models[modelName]?.tokens;
      }
    }

    if (data.error) {
      return {
        response: data.error.message || JSON.stringify(data.error),
        status: 'error',
        parse_error: null,
        metadata,
        session_id: null,
      };
    }

    return { response, status: 'ok', parse_error: null, metadata, session_id: data.session_id || null };
  } catch (err) {
    // Error recovery: try extracting JSON error block from stderr
    try {
      const errData = JSON.parse(stderr);
      if (errData.error) {
        return {
          response: errData.error.message || stderr,
          status: 'error',
          parse_error: null,
          metadata: {},
          session_id: null,
        };
      }
    } catch { /* not JSON stderr */ }

    return {
      response: stdout || stderr,
      status: 'error',
      parse_error: `JSON parse failed: ${err.message}`,
      metadata: {},
      session_id: null,
    };
  }
}

// --- Codex JSONL parser ---

function parseCodexOutput(stdout, stderr) {
  const messages = [];
  const errors = [];
  let usage = null;
  let threadId = null;

  const lines = stdout.split('\n');
  for (const line of lines) {
    const trimmed = line.trim();
    if (!trimmed || !trimmed.startsWith('{')) continue;

    try {
      const event = JSON.parse(trimmed);
      const eventType = event.type;

      if (eventType === 'item.completed') {
        const item = event.item || {};
        if (item.type === 'agent_message' && typeof item.text === 'string' && item.text.trim()) {
          messages.push(item.text.trim());
        }
      } else if (eventType === 'thread.started') {
        threadId = event.thread_id || null;
      } else if (eventType === 'turn.completed') {
        usage = event.usage || null;
      } else if (eventType === 'error') {
        errors.push(event.message || JSON.stringify(event));
      }
    } catch { /* skip non-JSON lines */ }
  }

  if (messages.length > 0) {
    return {
      response: messages.join('\n\n'),
      status: 'ok',
      parse_error: null,
      metadata: { usage },
      session_id: threadId,
    };
  }

  if (errors.length > 0) {
    return {
      response: errors.join('\n'),
      status: 'error',
      parse_error: null,
      metadata: { usage },
      session_id: threadId,
    };
  }

  // Recovery: no JSONL events parsed, try raw text
  // Status stays 'error' here — no structured events means something went wrong,
  // even if there's raw text output. Caller can still use the response text.
  const raw = stdout.trim() || stderr.trim();
  if (raw) {
    return {
      response: raw,
      status: 'error',
      parse_error: 'No JSONL events found; using raw output',
      metadata: {},
      session_id: null,
    };
  }

  return {
    response: '',
    status: 'error',
    parse_error: 'No output from codex',
    metadata: {},
    session_id: null,
  };
}

// --- Active child process tracking (for SIGINT/SIGTERM cleanup) ---
const activeChildren = new Set();

// --- Spawn a CLI with output monitoring ---

function runCli(command, cliArgs, timeoutMs) {
  return new Promise((resolvePromise) => {
    const startTime = Date.now();
    let stdout = '';
    let stderr = '';
    let killed = false;
    const MAX_OUTPUT = 1024 * 1024; // 1MB cap
    let truncated = false;

    const env = {
      ...process.env,
      [RECURSION_ENV]: '1', // prevent recursive roundtable invocations
    };

    const proc = spawn(command, cliArgs, {
      stdio: ['ignore', 'pipe', 'pipe'],
      detached: true,  // create process group so we can kill the entire tree
      env,
    });

    activeChildren.add(proc);

    proc.stdout.on('data', (chunk) => {
      if (stdout.length < MAX_OUTPUT) {
        stdout += chunk.toString();
      } else {
        truncated = true;
      }
    });

    proc.stderr.on('data', (chunk) => {
      if (stderr.length < MAX_OUTPUT) {
        stderr += chunk.toString();
      }
    });

    // Timeout: hard deadline only. No stall detection — CLIs legitimately go
    // silent for minutes during model inference (Gemini buffers entire JSON,
    // Codex has gaps between JSONL events during reasoning).
    // Kill entire process group (negative PID) so child tools don't orphan
    const killTree = (signal) => {
      try { process.kill(-proc.pid, signal); } catch { /* already dead */ }
    };

    const timeoutTimer = setTimeout(() => {
      killed = true;
      killTree('SIGTERM');
      setTimeout(() => killTree('SIGKILL'), 3000);
    }, timeoutMs);

    proc.on('close', (code) => {
      activeChildren.delete(proc);
      clearTimeout(timeoutTimer);
      resolvePromise({
        stdout,
        stderr,
        exit_code: code,
        elapsed_ms: Date.now() - startTime,
        timed_out: killed,
        truncated,
      });
    });

    proc.on('error', (err) => {
      activeChildren.delete(proc);
      clearTimeout(timeoutTimer);
      resolvePromise({
        stdout: '',
        stderr: err.message,
        exit_code: -1,
        elapsed_ms: Date.now() - startTime,
        timed_out: false,
        truncated: false,
      });
    });
  });
}

// --- Build result object ---

function buildResult(cliName, path, model, args, settledResult) {
  if (!path) {
    return {
      response: '', model, status: 'not_found',
      exit_code: null, stderr: `${cliName} CLI not found in PATH`,
      elapsed_ms: 0, parse_error: null, truncated: false, session_id: null,
    };
  }

  if (settledResult.status === 'rejected') {
    return {
      response: '', model, status: 'error',
      exit_code: null, stderr: settledResult.reason?.message || 'unknown error',
      elapsed_ms: 0, parse_error: null, truncated: false, session_id: null,
    };
  }

  const raw = settledResult.value;
  if (!raw) {
    return {
      response: '', model, status: 'error',
      exit_code: null, stderr: 'probe failed',
      elapsed_ms: 0, parse_error: null, truncated: false, session_id: null,
    };
  }

  const parser = cliName === 'gemini' ? parseGeminiOutput : parseCodexOutput;
  const parsed = parser(raw.stdout, raw.stderr);

  let status = parsed.status;
  if (raw.timed_out) status = 'timeout';
  // Non-zero exit with parsed 'ok' means CLI reported success format but failed — downgrade
  if (raw.exit_code !== 0 && raw.exit_code !== null && status === 'ok') status = 'error';

  return {
    response: parsed.response,
    model: parsed.metadata?.model_used || model || 'cli-default',
    status,
    exit_code: raw.exit_code,
    stderr: raw.stderr,
    elapsed_ms: raw.elapsed_ms,
    parse_error: parsed.parse_error,
    truncated: raw.truncated,
    session_id: parsed.session_id || null,
  };
}

// --- Main ---

async function main() {
  const args = parseArgs(process.argv);

  const geminiRole = args.geminiRole || args.role;
  const codexRole = args.codexRole || args.role;

  const geminiRolePrompt = loadRolePrompt(geminiRole, args.rolesDir, args.projectRolesDir);
  const codexRolePrompt = loadRolePrompt(codexRole, args.rolesDir, args.projectRolesDir);

  const fileRefs = formatFileReferences(args.files);
  const geminiPrompt = assemblePrompt(geminiRolePrompt, args.prompt, fileRefs);
  const codexPrompt = assemblePrompt(codexRolePrompt, args.prompt, fileRefs);
  const timeoutMs = args.timeout * 1000;

  // Check CLI availability (instant, no network)
  const geminiPath = findExecutable('gemini');
  const codexPath = findExecutable('codex');

  // Health probe: fast fail if CLI exists but can't start (auth issues, broken install)
  // Gemini: --version is instant; Codex: --version is instant
  const probes = {};
  if (geminiPath) {
    probes.gemini = probeCli(geminiPath, ['--version'], 5000);
  }
  if (codexPath) {
    probes.codex = probeCli(codexPath, ['--version'], 5000);
  }

  const probeResults = {};
  if (probes.gemini) probeResults.gemini = await probes.gemini;
  if (probes.codex) probeResults.codex = await probes.codex;

  // Determine which CLIs are healthy (symmetric for both)
  const geminiHealthy = geminiPath && (!probeResults.gemini || probeResults.gemini.alive);
  const codexHealthy = codexPath && (!probeResults.codex || probeResults.codex.alive);

  // Build CLI argument arrays
  const geminiArgs = buildGeminiArgs(args, geminiPrompt);
  const codexArgs = buildCodexArgs(args, codexPrompt);

  // Run healthy CLIs in parallel, report probe failures immediately
  const geminiTask = geminiHealthy
    ? runCli('gemini', geminiArgs, timeoutMs)
    : Promise.resolve(null);

  const codexTask = codexHealthy
    ? runCli('codex', codexArgs, timeoutMs)
    : Promise.resolve(null);

  const [geminiSettled, codexSettled] = await Promise.allSettled([geminiTask, codexTask]);

  // Build results — probe failures get immediate status
  const probeFailResult = (name, model, reason) => ({
    response: '', model, status: 'probe_failed',
    exit_code: null, stderr: `${name} CLI probe failed: ${reason}. Run ${name.toLowerCase()} --version to diagnose.`,
    elapsed_ms: 0, parse_error: null, truncated: false, session_id: null,
  });

  const results = {
    gemini: (geminiPath && !geminiHealthy)
      ? probeFailResult('Gemini', args.geminiModel, probeResults.gemini?.reason)
      : buildResult('gemini', geminiPath, args.geminiModel, args, geminiSettled),
    codex: (codexPath && !codexHealthy)
      ? probeFailResult('Codex', args.codexModel, probeResults.codex?.reason)
      : buildResult('codex', codexPath, args.codexModel, args, codexSettled),
    meta: {},
  };

  results.meta = buildMeta(results, geminiRole, codexRole, args.files);
  console.log(JSON.stringify(results, null, 2));
}

// --- Argument builders ---

function buildGeminiArgs(args, prompt) {
  const base = ['-o', 'json', '--yolo'];
  if (args.geminiModel) base.push('-m', args.geminiModel);
  if (args.geminiResume) {
    return ['--resume', args.geminiResume, '-p', prompt, ...base];
  }
  return ['-p', prompt, ...base];
}

function buildCodexArgs(args, prompt) {
  const base = ['exec', '--json', '--dangerously-bypass-approvals-and-sandbox'];
  if (args.codexModel) base.push('-c', `model=${args.codexModel}`);
  if (args.codexReasoning) base.push('-c', `reasoning_effort=${args.codexReasoning}`);
  if (args.codexResume) {
    return [...base, 'resume', args.codexResume === 'last' ? '--last' : args.codexResume, prompt];
  }
  return [...base, prompt];
}

function buildMeta(results, geminiRole, codexRole, files) {
  return {
    total_elapsed_ms: Math.max(
      results.gemini?.elapsed_ms || 0,
      results.codex?.elapsed_ms || 0
    ),
    gemini_role: geminiRole,
    codex_role: codexRole,
    files_referenced: files,
  };
}

// --- Signal handling (clean up child processes) ---
function cleanupAndExit() {
  for (const proc of activeChildren) {
    try { process.kill(-proc.pid, 'SIGTERM'); } catch { /* already dead */ }
  }
  // Give process groups 2s to die, then force kill and exit
  setTimeout(() => {
    for (const proc of activeChildren) {
      try { process.kill(-proc.pid, 'SIGKILL'); } catch { /* already dead */ }
    }
    process.exit(1);
  }, 2000);
  if (activeChildren.size === 0) process.exit(1);
}
process.on('SIGINT', cleanupAndExit);
process.on('SIGTERM', cleanupAndExit);

main().catch(err => {
  const msg = err instanceof ArgError
    ? { error: err.message, usage: 'roundtable --prompt "..." [--role default|planner|codereviewer] [--files a.ts,b.ts]' }
    : { error: err.message };
  console.error(JSON.stringify(msg));
  process.exit(1);
});
