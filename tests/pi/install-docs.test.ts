import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { mkdtemp, readFile, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

import { roundtableConfigPath } from "../../extensions/pi/config.ts";

const run = promisify(execFile);
const installMarkdown = fileURLToPath(new URL("../../INSTALL.md", import.meta.url));

/**
 * The troubleshooting check is run by a reader, not by the code, so nothing else keeps it honest.
 * It is extracted from the document and executed here: a command that checks a different file from
 * the one the package reads sends the reader to look at the wrong thing and conclude the wrong
 * cause.
 */
async function documentedConfigCheck(): Promise<string> {
  const markdown = await readFile(installMarkdown, "utf8");
  const blocks = [...markdown.matchAll(/```sh\n([\s\S]*?)```/g)].map((match) => match[1] ?? "");
  const check = blocks.find((block) => block.includes("roundtable.json") && block.includes("node -e"));
  assert.ok(check, "INSTALL.md must document a way to check the registration file");
  return check;
}

test("the documented registration-file check reads the file the package reads", async () => {
  const check = await documentedConfigCheck();
  assert.match(check, /PI_CODING_AGENT_DIR/, "the check must honour the agent directory override");

  const agentDirectory = await mkdtemp(join(tmpdir(), "roundtable-install-docs-"));
  const environment = { ...process.env, PI_CODING_AGENT_DIR: agentDirectory };
  const expected = roundtableConfigPath(environment);

  // Absent file: valid, and the check says where it looked rather than failing.
  const absent = await run("bash", ["-c", check], { env: environment });
  assert.match(absent.stdout, new RegExp(expected.replace(/[/\\]/g, "\\$&")));
  assert.match(absent.stdout, /no registration file/);

  await writeFile(expected, '{ "env": { "ROUNDTABLE_PROVIDERS": "[]" } }');
  const valid = await run("bash", ["-c", check], { env: environment });
  assert.match(valid.stdout, /valid JSON/);
  assert.match(valid.stdout, new RegExp(expected.replace(/[/\\]/g, "\\$&")));

  await writeFile(expected, "{ not json");
  await assert.rejects(run("bash", ["-c", check], { env: environment }), /JSON/);
});
