# Releasing Roundtable

Step-by-step guide for cutting a release. Covers version bumping, building artifacts, and publishing to both Forgejo (primary) and GitHub (mirror).

## Prerequisites

On the build machine:
- Go 1.26+ via `mise install`
- `tea` CLI authenticated to Forgejo (`tea login list` to verify)
- `gh` CLI authenticated to GitHub (`gh auth status` to verify)
- Git credentials for both remotes

## 1. Determine Version

Roundtable uses semantic versioning. Check the current version in `Makefile` (the `VERSION ?=` line) and in `internal/httpmcp/config.go` (`defaultVersion`).

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

### internal/httpmcp/config.go

```bash
sed -i "s/defaultVersion      = \".*\"/defaultVersion      = \"${NEW_VERSION}\"/" internal/httpmcp/config.go
```

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

```bash
mise exec go@1.26.2 -- env GOTOOLCHAIN=local GOMODCACHE=/tmp/gomodcache GOCACHE=/tmp/gocache \
  go build -o release/roundtable-http-mcp ./cmd/roundtable-http-mcp
chmod +x release/roundtable-http-mcp
```

Verify the binary runs:

```bash
./release/roundtable-http-mcp &
sleep 1
curl -s http://127.0.0.1:4040/healthz  # should print "ok"
pkill -f roundtable-http-mcp
```

## 4. Package Tarball

```bash
cd release
tar czf ../roundtable-${NEW_VERSION}.tar.gz roundtable-http-mcp SKILL.md
cd -
```

### SHA256 checksum

```bash
sha256sum roundtable-${NEW_VERSION}.tar.gz > SHA256SUMS
```

Verify: `cat SHA256SUMS` should show one line with the hash and filename.

## 5. Run Full Test Suite

```bash
make test
make vet
```

Both must succeed before tagging.

## 6. Commit and Tag

```bash
git add Makefile internal/httpmcp/config.go INSTALL.md release/SKILL.md
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
- \`roundtable-${NEW_VERSION}.tar.gz\` — Go binary + skill file
- \`SHA256SUMS\` — integrity checksum" \
  --asset "roundtable-${NEW_VERSION}.tar.gz" \
  --asset "SHA256SUMS"
```

## 10. Create Release on GitHub

```bash
gh release create "v${NEW_VERSION}" \
  --repo TejGandham/roundtable \
  --title "Roundtable v${NEW_VERSION}" \
  --notes "Release notes here." \
  "roundtable-${NEW_VERSION}.tar.gz" \
  "SHA256SUMS"
```

## File Reference

|File|Role|
|-|-|
|`Makefile` `VERSION ?=` |Build-time version string|
|`internal/httpmcp/config.go` `defaultVersion`|Reported MCP server version|
|`INSTALL.md` `VERSION=`|Install script version|
|`release/SKILL.md`|Skill file shipped in tarball|
|`release/roundtable-http-mcp`|Go binary shipped in tarball|

## Notes

- **Static binary**: the Go binary embeds role prompts via `go:embed`, so no `roles/` directory needs to ship.
- **No Erlang**: Roundtable is pure Go. The build machine does not need Erlang, Elixir, or Node.
- **CLIs at runtime**: users must have `gemini`, `codex`, and/or `claude` on their PATH for the server to dispatch to them. Missing CLIs produce `status: "not_found"` per-backend.
