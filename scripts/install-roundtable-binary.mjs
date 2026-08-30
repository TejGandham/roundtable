import { createHash } from "node:crypto";
import { chmod, mkdir, mkdtemp, readFile, rename, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";

const VERSION = "2.2.2";
const ROOT = resolve(fileURLToPath(new URL("..", import.meta.url)));
const DESTINATION = join(ROOT, ".pi-bin", "roundtable");
const RELEASE_BASE = `https://github.com/TejGandham/roundtable/releases/download/v${VERSION}`;

const releases = {
  "darwin-x64": {
    archive: `roundtable-${VERSION}-darwin-amd64.tar.gz`,
    binary: "roundtable-darwin-amd64",
    sha256: "91df3087ea256c6b733d0a245b1f2445d7e383811b63b4b2912ce8094d5b3575",
  },
  "darwin-arm64": {
    archive: `roundtable-${VERSION}-darwin-arm64.tar.gz`,
    binary: "roundtable-darwin-arm64",
    sha256: "cd3f41372424304cb3c6d0b7efecb9127e073fca355bb9f88a834bd44aae07ff",
  },
  "linux-x64": {
    archive: `roundtable-${VERSION}-linux-amd64.tar.gz`,
    binary: "roundtable-linux-amd64",
    sha256: "7ec8f76458d43f20d21d5c0717b96f351bbc337fc154bd522b8f13befce4c4a6",
  },
  "linux-arm64": {
    archive: `roundtable-${VERSION}-linux-arm64.tar.gz`,
    binary: "roundtable-linux-arm64",
    sha256: "0c3bec2a15ed9d1ad10782e73424e96ca67fc3bf7e8c94d4483a3d4ede15e5bf",
  },
};

async function install() {
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
    await rename(join(temporary, release.binary), staged);
    await chmod(staged, 0o755);
    await rename(staged, DESTINATION);
    process.stdout.write(`Installed Roundtable ${VERSION} for Pi (${key}).\n`);
  } finally {
    await rm(temporary, { recursive: true, force: true });
  }
}

await install();
