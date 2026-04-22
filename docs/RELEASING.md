# Releasing Roundtable

Step-by-step guide for cutting a release. Covers version bumping, building artifacts, and publishing to both Forgejo (primary) and GitHub (mirror).

## Prerequisites

On the build machine:
- Go 1.26+ via `mise install`
- `tea` CLI authenticated to Forgejo (`tea login list` to verify)
- `gh` CLI authenticated to GitHub (`gh auth status` to verify)
- Git credentials for both remotes

## 1. Determine Version

Roundtable uses semantic versioning. Check the current version in `Makefile` (the `VERSION ?=` line). The runtime version string lives in `cmd/roundtable/main.go` as `var version = "dev"`, overridden at build time via `-ldflags "-X main.version=${VERSION}"` — `make build` and `make release` already do this, so bumping `Makefile` is the only source edit required.

Decide the bump:
- **Patch** (0.7.0 -> 0.7.1): Bug fixes only
- **Minor** (0.7.0 -> 0.8.0): New feature, backward compatible
- **Major** (0.7.0 -> 1.0.0): Breaking changes

Set for the rest of this guide:

```bash
NEW_VERSION="0.7.1"  # Change this
```

## 2. Bump Version

### Makefile

```bash
sed -i "s/^VERSION ?= .*/VERSION ?= ${NEW_VERSION}/" Makefile
```

`cmd/roundtable/main.go` intentionally does **not** get sed-patched — the
`version = "dev"` default is overridden at link time by `-ldflags
"-X main.version=$(VERSION)"` in the Makefile, so bumping `VERSION ?=`
is sufficient.

### INSTALL.md

```bash
sed -i "s/^VERSION=.*/VERSION=${NEW_VERSION}/" INSTALL.md
```

### release/SKILL.md

Sync the release copy of the skill file:

```bash
cp SKILL.md release/SKILL.md
```

Verify: `diff SKILL.md release/SKILL.md` should show no output.

## 3. Build Artifacts

### Quick build

```bash
make release
```

This builds the single Go binary and copies it to `release/`.

### Manual build

Post Phase C the binary is `roundtable` built from `./cmd/roundtable`:

```bash
mise exec go@1.26.2 -- env GOTOOLCHAIN=local GOMODCACHE=/tmp/gomodcache GOCACHE=/tmp/gocache \
  go build -ldflags "-s -w -X main.version=${NEW_VERSION}" \
  -o release/roundtable ./cmd/roundtable
chmod +x release/roundtable
```

Smoke-test — stdio servers have no HTTP endpoints, so verify via the
`version` subcommand and an MCP `initialize` exchange:

```bash
./release/roundtable version                 # prints "roundtable ${NEW_VERSION}"
./release/roundtable stdio </dev/null 2>&1 | head -5  # expect MCP startup logs on stderr, clean exit
```

## 4. Package Tarballs

`make release` produces two arch-suffixed binaries in `release/`:
`roundtable-darwin-arm64` and `roundtable-linux-amd64`. Package one
tarball per platform, each containing that platform's binary plus
`SKILL.md`, then generate a single `SHA256SUMS` covering both.

```bash
for pair in darwin-arm64 linux-amd64; do
  tar czf "roundtable-${NEW_VERSION}-${pair}.tar.gz" \
    -C release "roundtable-${pair}" SKILL.md
done
```

### SHA256 checksums

```bash
shasum -a 256 \
  "roundtable-${NEW_VERSION}-darwin-arm64.tar.gz" \
  "roundtable-${NEW_VERSION}-linux-amd64.tar.gz" \
  > SHA256SUMS
```

Verify: `cat SHA256SUMS` should show two lines — one hash + filename per tarball.

> The install script in `INSTALL.md` verifies each tarball against
> `SHA256SUMS` with `grep "  ${ASSET}$" SHA256SUMS | shasum -c -`, which
> depends on the exact filename appearing in the SHA256SUMS line. Do
> not rename the tarballs after checksumming; the binary inside can
> keep its arch suffix (the installer normalizes it to `roundtable` on
> extract).

## 5. Run Full Test Suite

```bash
make test
make vet
```

Both must succeed before tagging.

## 6. Commit and Tag

```bash
git add Makefile INSTALL.md release/SKILL.md
git commit -m "chore: bump version to ${NEW_VERSION}"
git tag -a "v${NEW_VERSION}" -m "v${NEW_VERSION} — <short description>"
```

## 7. Push to Forgejo (Primary)

```bash
git push origin main
git push origin --tags
```

## 8. Push to GitHub (Mirror)

```bash
git remote add github https://github.com/TejGandham/roundtable.git 2>/dev/null || true
git push github main && git push github --tags
```

## 9. Create Release on Forgejo

```bash
tea release create \
  --tag "v${NEW_VERSION}" \
  --title "Roundtable v${NEW_VERSION}" \
  --note "## <Title>

<Description of what changed.>

### Assets
- \`roundtable-${NEW_VERSION}-darwin-arm64.tar.gz\` — Apple Silicon binary + skill file
- \`roundtable-${NEW_VERSION}-linux-amd64.tar.gz\` — Linux x86_64 binary + skill file
- \`SHA256SUMS\` — integrity checksums (one line per tarball)" \
  --asset "roundtable-${NEW_VERSION}-darwin-arm64.tar.gz" \
  --asset "roundtable-${NEW_VERSION}-linux-amd64.tar.gz" \
  --asset "SHA256SUMS"
```

## 10. Create Release on GitHub

```bash
gh release create "v${NEW_VERSION}" \
  --repo TejGandham/roundtable \
  --title "Roundtable v${NEW_VERSION}" \
  --notes "Release notes here." \
  "roundtable-${NEW_VERSION}-darwin-arm64.tar.gz" \
  "roundtable-${NEW_VERSION}-linux-amd64.tar.gz" \
  "SHA256SUMS"
```

## File Reference

|File|Role|
|-|-|
|`Makefile` `VERSION ?=` |Build-time version string, injected via `-ldflags -X main.version=`|
|`cmd/roundtable/main.go` `var version`|Runtime default (`"dev"`), overridden at link time — no edit required on bump|
|`INSTALL.md` `VERSION=`|Install script version|
|`release/SKILL.md`|Skill file shipped in every tarball|
|`release/roundtable-darwin-arm64`|Apple Silicon binary shipped in the darwin-arm64 tarball|
|`release/roundtable-linux-amd64`|Linux x86_64 binary shipped in the linux-amd64 tarball|

## Notes

- **Static binary**: the Go binary embeds role prompts via `go:embed`, so no `roles/` directory needs to ship.
- **No Erlang**: Roundtable is pure Go. The build machine does not need Erlang, Elixir, or Node.
- **CLIs at runtime**: users must have `gemini`, `codex`, and/or `claude` on their PATH for the server to dispatch to them. Missing CLIs produce `status: "not_found"` per-backend.
