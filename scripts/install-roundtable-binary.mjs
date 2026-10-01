import { createHash } from "node:crypto";
import { chmod, copyFile, mkdir, mkdtemp, readFile, rename, rm, unlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";

const VERSION = "2.5.2";
const ROOT = resolve(fileURLToPath(new URL("..", import.meta.url)));
const DESTINATION = join(ROOT, ".pi-bin", "roundtable");
const RELEASE_BASE = `https://github.com/TejGandham/roundtable/releases/download/v${VERSION}`;

const releases = {
  "darwin-x64": {
    archive: `roundtable-${VERSION}-darwin-amd64.tar.gz`,
    binary: "roundtable-darwin-amd64",
    sha256: "3fd009b6f27fc2800fd3c0d4f84f8ec753a8ab25b470d18986618744670d7505",
  },
  "darwin-arm64": {
    archive: `roundtable-${VERSION}-darwin-arm64.tar.gz`,
    binary: "roundtable-darwin-arm64",
    sha256: "67c40247c11d869017dbc444212cf4a9300e69134d5ea2ae64fd145c18f49a1b",
  },
  "linux-x64": {
    archive: `roundtable-${VERSION}-linux-amd64.tar.gz`,
    binary: "roundtable-linux-amd64",
    sha256: "3345418dd268caa426da80d0765344a5b4447e014591483ea1de2338baa133fd",
  },
  "linux-arm64": {
    archive: `roundtable-${VERSION}-linux-arm64.tar.gz`,
    binary: "roundtable-linux-arm64",
    sha256: "ed7851fd0fed71b56b8d95cda2ed4f03ec5912b4bb7de9a553b826b2336e3a4b",
  },
};

/**
 * @typedef {object} MoveOperations
 * @property {(source: string, destination: string) => Promise<void>} [rename]
 * @property {(source: string, destination: string) => Promise<void>} [copyFile]
 * @property {(target: string) => Promise<void>} [unlink]
 */

function describeCause(error) {
  const text = error instanceof Error ? error.message : String(error);
  return text.trim() || "unknown error";
}

/**
 * Move a downloaded file into its destination, across a filesystem boundary if there is one.
 *
 * `rename(2)` is defined only within a single filesystem, and a download directory is routinely on
 * another one: `/tmp` is its own mount on most Linux distributions and `TMPDIR` is a documented
 * user setting. So `EXDEV` here is an ordinary condition, not a transient fault — retrying a
 * `rename()` would fail the same way every time. Copy across the boundary instead, then drop the
 * source.
 *
 * @param {string} source
 * @param {string} destination
 * @param {MoveOperations} [operations] injection seam for the tests
 * @returns {Promise<"rename" | "copy">} which path did the move
 */
export async function moveIntoPlace(source, destination, operations = {}) {
  const move = operations.rename ?? rename;
  const copy = operations.copyFile ?? copyFile;
  const remove = operations.unlink ?? unlink;

  try {
    await move(source, destination);
    return "rename";
  } catch (error) {
    if (error?.code !== "EXDEV") {
      throw new Error(
        `could not move ${source} to ${destination}: ${describeCause(error)}`,
        { cause: error },
      );
    }

    try {
      await copy(source, destination);
    } catch (copyError) {
      throw new Error(
        `could not move ${source} to ${destination}: the download directory is on a different `
        + `filesystem than the destination, and copying across that boundary failed: `
        + `${describeCause(copyError)}. Set TMPDIR to a directory on the same filesystem as `
        + `${dirname(destination)} and install again.`,
        { cause: copyError },
      );
    }

    // The source lives in the temp tree that the caller removes wholesale, so a failure to unlink
    // it now costs nothing and must not fail an otherwise completed move.
    await remove(source).catch(() => {});
    return "copy";
  }
}

export async function install() {
  if (process.env.ROUNDTABLE_SKIP_BINARY_INSTALL === "1") {
    process.stdout.write("Skipping the Roundtable binary download.\n");
    return;
  }

  const key = `${process.platform}-${process.arch}`;
  const release = releases[key];
  if (!release) {
    throw new Error(`Roundtable has no Pi binary for ${key}; supported targets are macOS and Linux on x64 or arm64.`);
  }

  const temporary = await mkdtemp(join(tmpdir(), "roundtable-pi-install-"));
  try {
    const archivePath = join(temporary, release.archive);
    const response = await fetch(`${RELEASE_BASE}/${release.archive}`, { redirect: "follow" });
    let bytes;
    if (response.ok) {
      bytes = Buffer.from(await response.arrayBuffer());
    } else {
      const downloaded = spawnSync("gh", [
        "release", "download", `v${VERSION}`,
        "--repo", "TejGandham/roundtable",
        "--pattern", release.archive,
        "--dir", temporary,
        "--clobber",
      ], {
        encoding: "utf8",
        stdio: ["ignore", "pipe", "pipe"],
      });
      if (downloaded.status !== 0) {
        throw new Error(
          `download returned HTTP ${response.status}, and authenticated gh download failed: ${(downloaded.stderr || downloaded.stdout || "gh is unavailable").trim()}`,
        );
      }
      bytes = await readFile(archivePath);
    }
    const actualHash = createHash("sha256").update(bytes).digest("hex");
    if (actualHash !== release.sha256) {
      throw new Error(`checksum mismatch for ${release.archive}: expected ${release.sha256}, got ${actualHash}`);
    }

    await writeFile(archivePath, bytes, { mode: 0o600 });
    const extracted = spawnSync("tar", ["-xzf", archivePath, "-C", temporary], {
      encoding: "utf8",
      stdio: ["ignore", "pipe", "pipe"],
    });
    if (extracted.status !== 0) {
      throw new Error(`tar failed: ${(extracted.stderr || extracted.stdout || "unknown error").trim()}`);
    }

    await mkdir(dirname(DESTINATION), { recursive: true });
    const staged = `${DESTINATION}.new`;
    await rm(staged, { force: true });
    await moveIntoPlace(join(temporary, release.binary), staged);
    await chmod(staged, 0o755);
    await moveIntoPlace(staged, DESTINATION);
    process.stdout.write(`Installed Roundtable ${VERSION} for Pi (${key}).\n`);
  } finally {
    await rm(temporary, { recursive: true, force: true });
  }
}

const invokedDirectly = process.argv[1] !== undefined
  && resolve(process.argv[1]) === fileURLToPath(import.meta.url);

if (invokedDirectly) {
  await install();
}
