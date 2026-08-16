import assert from "node:assert/strict";
import { execFile, spawn } from "node:child_process";
import { access, mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { pathToFileURL, fileURLToPath } from "node:url";
import { promisify } from "node:util";

const exec = promisify(execFile);
const ROOT = resolve(fileURLToPath(new URL("..", import.meta.url)));

async function run(command, args, options = {}) {
  return exec(command, args, {
    maxBuffer: 20 * 1024 * 1024,
    timeout: 180_000,
    ...options,
  });
}

function rpcCommands(cwd, agentDir) {
  return new Promise((resolveCommands, rejectCommands) => {
    const child = spawn("pi", ["--mode", "rpc", "--no-session", "--approve", "--no-context-files"], {
      cwd,
      env: { ...process.env, PI_CODING_AGENT_DIR: agentDir },
      stdio: ["pipe", "pipe", "pipe"],
    });
    let buffered = "";
    let stderr = "";
    let settled = false;
    const timer = setTimeout(() => finish(new Error(`Pi RPC timed out: ${stderr}`)), 20_000);
    const finish = (error, commands) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      child.stdin.end();
      child.kill("SIGTERM");
      const settle = () => error ? rejectCommands(error) : resolveCommands(commands);
      if (child.exitCode === null) child.once("exit", settle);
      else settle();
    };
    child.stderr.setEncoding("utf8");
    child.stderr.on("data", (chunk) => { stderr += chunk; });
    child.stdout.setEncoding("utf8");
    child.stdout.on("data", (chunk) => {
      buffered += chunk;
      const lines = buffered.split("\n");
      buffered = lines.pop() ?? "";
      for (const line of lines) {
        if (!line.trim()) continue;
        const message = JSON.parse(line);
        if (message.type !== "response" || message.id !== "commands") continue;
        if (!message.success) finish(new Error(String(message.error ?? "get_commands failed")));
        else finish(undefined, message.data.commands);
      }
    });
    child.once("error", (error) => finish(error));
    child.once("exit", (code) => {
      if (!settled) finish(new Error(`Pi RPC exited ${code}: ${stderr}`));
    });
    child.stdin.write(`${JSON.stringify({ id: "commands", type: "get_commands" })}\n`);
  });
}

async function probeBinary(packageRoot, consumer) {
  const probe = join(packageRoot, "probe-binary.mjs");
  const clientUrl = pathToFileURL(join(packageRoot, "node_modules", "@modelcontextprotocol", "sdk", "dist", "esm", "client", "index.js")).href;
  const transportUrl = pathToFileURL(join(packageRoot, "node_modules", "@modelcontextprotocol", "sdk", "dist", "esm", "client", "stdio.js")).href;
  const binary = join(packageRoot, ".pi-bin", "roundtable");
  await writeFile(probe, `
import assert from "node:assert/strict";
import { Client } from ${JSON.stringify(clientUrl)};
import { StdioClientTransport } from ${JSON.stringify(transportUrl)};
const client = new Client({ name: "packed-smoke", version: "1" }, { capabilities: {} });
const transport = new StdioClientTransport({
  command: ${JSON.stringify(binary)},
  args: ["stdio"],
  cwd: ${JSON.stringify(consumer)},
  env: Object.fromEntries(Object.entries(process.env).filter(([, value]) => typeof value === "string")),
  stderr: "pipe"
});
await client.connect(transport, { timeout: 30000, maxTotalTimeout: 30000 });
const listed = await client.listTools({}, { timeout: 30000, maxTotalTimeout: 30000 });
assert.deepEqual(listed.tools.map((tool) => tool.name).sort(), [
  "roundtable-blueprint",
  "roundtable-canvass",
  "roundtable-converge",
  "roundtable-critique",
  "roundtable-crosscheck",
  "roundtable-deliberate"
]);
await client.close();
`);
  await run("node", [probe], { cwd: packageRoot });
  await rm(probe, { force: true });
}

async function main() {
  const sourceManifest = JSON.parse(await readFile(join(ROOT, "package.json"), "utf8"));
  const actualPi = (await run("pi", ["--version"])).stdout.trim();
  assert.equal(actualPi, sourceManifest.devDependencies["@earendil-works/pi-coding-agent"]);

  const tempRoot = await mkdtemp(join(tmpdir(), "roundtable-pi-packed-"));
  const tarballs = join(tempRoot, "tarballs");
  const extracted = join(tempRoot, "extracted");
  const packageRoot = join(extracted, "package");
  const consumer = join(tempRoot, "consumer");
  const agentDir = join(tempRoot, "agent");
  let installed = false;

  try {
    await Promise.all([
      mkdir(tarballs, { recursive: true }),
      mkdir(extracted, { recursive: true }),
      mkdir(consumer, { recursive: true }),
      mkdir(agentDir, { recursive: true }),
    ]);
    const packed = await run("npm", ["pack", "--silent", "--ignore-scripts", "--pack-destination", tarballs], { cwd: ROOT });
    const filename = packed.stdout.trim().split("\n").at(-1);
    assert.ok(filename?.endsWith(".tgz"));
    await run("tar", ["-xzf", join(tarballs, filename), "-C", extracted]);

    const packedManifest = JSON.parse(await readFile(join(packageRoot, "package.json"), "utf8"));
    assert.equal(packedManifest.name, "roundtable");
    assert.equal(packedManifest.version, sourceManifest.version);
    await access(join(packageRoot, "extensions", "pi", "index.ts"));
    await access(join(packageRoot, "skills", "roundtable", "SKILL.md"));
    await access(join(packageRoot, "scripts", "install-roundtable-binary.mjs"));
    await assert.rejects(() => access(join(packageRoot, "tests")));
    await assert.rejects(() => access(join(packageRoot, ".pi-bin")));

    await run("npm", ["install", "--omit=dev", "--ignore-scripts"], { cwd: packageRoot });
    await run("node", ["scripts/install-roundtable-binary.mjs"], { cwd: packageRoot });
    const version = await run(join(packageRoot, ".pi-bin", "roundtable"), ["version"], { cwd: consumer });
    assert.equal(version.stdout.trim(), `roundtable ${sourceManifest.version}`);
    await probeBinary(packageRoot, consumer);

    const env = { ...process.env, PI_CODING_AGENT_DIR: agentDir };
    await run("pi", ["install", packageRoot], { cwd: consumer, env });
    installed = true;
    const listed = await run("pi", ["list"], { cwd: consumer, env });
    assert.match(listed.stdout, /roundtable/i);
    const commands = await rpcCommands(consumer, agentDir);
    assert.equal(commands.filter((command) => command.name === "skill:roundtable").length, 1);

    await run("pi", ["remove", packageRoot], { cwd: consumer, env });
    installed = false;
    process.stdout.write(`${JSON.stringify({
      package: `${packedManifest.name}@${packedManifest.version}`,
      pi: actualPi,
      mcpTools: 6,
      skills: 1,
      installRemove: "pass",
      result: "pass",
    })}\n`);
  } finally {
    if (installed) {
      const env = { ...process.env, PI_CODING_AGENT_DIR: agentDir };
      await run("pi", ["remove", packageRoot], { cwd: consumer, env }).catch(() => {});
    }
    await rm(tempRoot, { recursive: true, force: true });
  }
}

await main();
