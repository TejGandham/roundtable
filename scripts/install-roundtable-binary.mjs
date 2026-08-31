import { createHash } from "node:crypto";
import { chmod, mkdir, mkdtemp, readFile, rename, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";

const VERSION = "2.3.0";
const ROOT = resolve(fileURLToPath(new URL("..", import.meta.url)));
const DESTINATION = join(ROOT, ".pi-bin", "roundtable");
const RELEASE_BASE = `https://github.com/TejGandham/roundtable/releases/download/v${VERSION}`;

const releases = {
  "darwin-x64": {
    archive: `roundtable-${VERSION}-darwin-amd64.tar.gz`,
    binary: "roundtable-darwin-amd64",
    sha256: "dd5d51833952a3e0fcd48d1fe002305dd1204b680c4814b7044ecaf9a48f7b43",
  },
  "darwin-arm64": {
    archive: `roundtable-${VERSION}-darwin-arm64.tar.gz`,
    binary: "roundtable-darwin-arm64",
    sha256: "e1e5389e4550d5bab67caff74850bf72f49e4682043567807610798210e80171",
  },
  "linux-x64": {
    archive: `roundtable-${VERSION}-linux-amd64.tar.gz`,
    binary: "roundtable-linux-amd64",
    sha256: "471e58804e1cf440ae7f0e2754ec27eabced2e0d4730bdaa6b47f960133ff975",
  },
  "linux-arm64": {
    archive: `roundtable-${VERSION}-linux-arm64.tar.gz`,
    binary: "roundtable-linux-arm64",
    sha256: "42e58119887d17130c4ca93904978b9cc5404958a7a84d5d85d86633189b5732",
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
