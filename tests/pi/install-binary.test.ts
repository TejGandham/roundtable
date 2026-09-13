import assert from "node:assert/strict";
import { mkdtemp, readFile, rm, stat, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import { moveIntoPlace } from "../../scripts/install-roundtable-binary.mjs";

/** The error Node raises when rename(2) is asked to cross a filesystem boundary. */
function crossDeviceError(source: string, destination: string): NodeJS.ErrnoException {
  return Object.assign(
    new Error(`EXDEV: cross-device link not permitted, rename '${source}' -> '${destination}'`),
    { errno: -18, code: "EXDEV", syscall: "rename", path: source, dest: destination },
  );
}

async function scratch(): Promise<string> {
  return await mkdtemp(join(tmpdir(), "roundtable-move-"));
}

async function exists(path: string): Promise<boolean> {
  try {
    await stat(path);
    return true;
  } catch {
    return false;
  }
}

test("a rename that cannot cross the filesystem boundary falls back to copy-then-unlink", async () => {
  const directory = await scratch();
  try {
    const source = join(directory, "roundtable-linux-amd64");
    const destination = join(directory, "roundtable.new");
    await writeFile(source, "binary bytes");

    // Only rename is faked; the copy and the unlink are the real ones, so the assertions below
    // are about a file that genuinely landed.
    const attempted: Array<[string, string]> = [];
    const outcome = await moveIntoPlace(source, destination, {
      rename(from: string, to: string) {
        attempted.push([from, to]);
        return Promise.reject(crossDeviceError(from, to));
      },
    });

    assert.equal(outcome, "copy");
    assert.deepEqual(attempted, [[source, destination]], "rename is still tried first");
    assert.equal(await readFile(destination, "utf8"), "binary bytes");
    assert.equal(await exists(source), false, "the staged download is not left behind");
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});

test("a move within one filesystem still takes the rename fast path", async () => {
  const directory = await scratch();
  try {
    const source = join(directory, "roundtable-linux-amd64");
    const destination = join(directory, "roundtable.new");
    await writeFile(source, "binary bytes");

    let copies = 0;
    const outcome = await moveIntoPlace(source, destination, {
      copyFile() {
        copies += 1;
        return Promise.resolve();
      },
    });

    assert.equal(outcome, "rename");
    assert.equal(copies, 0, "nothing is copied when rename works");
    assert.equal(await readFile(destination, "utf8"), "binary bytes");
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});

test("a copy that fails across the boundary names the boundary and the TMPDIR workaround", async () => {
  const source = "/tmp/roundtable-pi-install-xxxx/roundtable-linux-amd64";
  const destination = "/home/dev/.pi/agent/git/roundtable/.pi-bin/roundtable.new";
  const full = Object.assign(new Error("ENOSPC: no space left on device, copyfile"), { code: "ENOSPC" });

  await assert.rejects(
    moveIntoPlace(source, destination, {
      rename: (from: string, to: string) => Promise.reject(crossDeviceError(from, to)),
      copyFile: () => Promise.reject(full),
    }),
    (error: Error & { cause?: unknown }) => {
      assert.match(error.message, /different filesystem/);
      assert.match(error.message, /Set TMPDIR to a directory on the same filesystem/);
      assert.match(error.message, /no space left on device/);
      assert.ok(error.message.includes(source) && error.message.includes(destination));
      assert.equal(error.cause, full);
      return true;
    },
  );
});

test("a failure that is not EXDEV is reported with both paths and its cause intact", async () => {
  const source = "/tmp/roundtable-pi-install-xxxx/roundtable-linux-amd64";
  const destination = "/opt/roundtable/.pi-bin/roundtable.new";
  const denied = Object.assign(new Error("EACCES: permission denied, rename"), { code: "EACCES" });

  await assert.rejects(
    moveIntoPlace(source, destination, { rename: () => Promise.reject(denied) }),
    (error: Error & { cause?: unknown }) => {
      assert.match(error.message, /permission denied/);
      assert.ok(error.message.includes(source) && error.message.includes(destination));
      assert.doesNotMatch(error.message, /different filesystem/, "do not blame a boundary that is not there");
      assert.equal(error.cause, denied);
      return true;
    },
  );
});

test("importing the installer does not run the install", async () => {
  // The postinstall entry point is guarded, so the module can be imported by a test without
  // reaching for the network. Reaching this line at all is the assertion.
  assert.equal(typeof moveIntoPlace, "function");
});
