import { createHash } from "node:crypto";
import { chmod, mkdir, mkdtemp, readFile, rename, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";

const VERSION = "2.1.3";
const ROOT = resolve(fileURLToPath(new URL("..", import.meta.url)));
const DESTINATION = join(ROOT, ".pi-bin", "roundtable");
const RELEASE_BASE = `https://github.com/TejGandham/roundtable/releases/download/v${VERSION}`;

const releases = {
  "darwin-x64": {
    archive: `roundtable-${VERSION}-darwin-amd64.tar.gz`,
    binary: "roundtable-darwin-amd64",
    sha256: "27e7c41c3a206bf0487ecbffce7cdfd4d4178424b0a780a448a508451f45c918",
  },
  "darwin-arm64": {
    archive: `roundtable-${VERSION}-darwin-arm64.tar.gz`,
    binary: "roundtable-darwin-arm64",
    sha256: "a99ccfc7788f7caee039abdc4d4128c9edea6f8979c99ba5a66da350367274de",
  },
  "linux-x64": {
    archive: `roundtable-${VERSION}-linux-amd64.tar.gz`,
    binary: "roundtable-linux-amd64",
    sha256: "461bd4d2e17072e2e28a5b9357cc3580ef315cfbac9c560e5f213f2839024f39",
  },
  "linux-arm64": {
    archive: `roundtable-${VERSION}-linux-arm64.tar.gz`,
    binary: "roundtable-linux-arm64",
    sha256: "2a85c2c3a9e146e0be37b2474fd0e7c6d46c38aae35cfc27544b25a57e774f90",
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
