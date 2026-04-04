# Releasing Roundtable

Step-by-step guide for cutting a release. Covers version bumping, building artifacts, and publishing to both Forgejo (primary) and GitHub (mirror).

## Prerequisites

On the build machine:
- Erlang/OTP 28+ and Elixir 1.19+
- `tea` CLI authenticated to Forgejo (`tea login list` to verify)
- `gh` CLI authenticated to GitHub (`gh auth status` to verify)
- Git credentials for both remotes

## 1. Determine Version

Roundtable uses semantic versioning. Check the current version:

```bash
grep 'version:' mix.exs
```

Decide the bump:
- **Patch** (0.4.0 → 0.4.1): Bug fixes only, no new features
- **Minor** (0.4.0 → 0.5.0): New feature, backward compatible
- **Major** (0.4.0 → 1.0.0): Breaking changes

Set for the rest of this guide:

```bash
NEW_VERSION="0.5.1"  # Change this
```

## 2. Bump Version

### mix.exs

```bash
# mix.exs line 7
sed -i "s/version: \".*\"/version: \"${NEW_VERSION}\"/" mix.exs
```

Verify: `grep 'version:' mix.exs` should show `version: "${NEW_VERSION}"`.

### INSTALL.md

```bash
# INSTALL.md line 20 — the VERSION= in the install script
sed -i "s/^VERSION=.*/VERSION=${NEW_VERSION}/" INSTALL.md
```

Verify: `grep '^VERSION=' INSTALL.md` should show `VERSION=${NEW_VERSION}`.

### release/SKILL.md

Copy the source SKILL.md into the release directory. This file ships with the tarball and gets installed to users' skill directories.

```bash
cp SKILL.md release/SKILL.md
```

Verify: `diff SKILL.md release/SKILL.md` should show no output.

## 3. Build Artifacts

### Escript binary

The escript is a standalone Elixir binary that ships in the release tarball. It provides the CLI entrypoint (`roundtable-cli`).

```bash
mix deps.get
mix escript.build
cp roundtable-cli release/roundtable
chmod +x release/roundtable
```

Verify: `./release/roundtable --help` runs without error.

### MCP server release

The OTP release is the primary artifact. It produces the `roundtable_mcp` binary that the MCP wrapper script (`roundtable-mcp`) execs.

```bash
MIX_ENV=prod mix release roundtable_mcp
```

Output lands in `_build/prod/rel/roundtable_mcp/`. The release includes:
- `bin/roundtable_mcp` — OTP release binary (start/stop/etc.)
- `lib/` — compiled BEAM files
- `releases/` — release metadata
- Role prompts are auto-bundled from `priv/roles/`

The `rel/overlays/bin/roundtable-mcp` wrapper script is automatically included in the release. It sets `ROUNDTABLE_MCP=1` and execs the binary.

### Release tarball

Package the release into a tarball for distribution:

```bash
cd _build/prod/rel
tar czf roundtable-mcp-${NEW_VERSION}.tar.gz roundtable_mcp/
cd -
```

### SHA256 checksum

```bash
cd _build/prod/rel
sha256sum roundtable-mcp-${NEW_VERSION}.tar.gz > SHA256SUMS
cd -
```

Verify: `cat _build/prod/rel/SHA256SUMS` should show one line with the hash and filename.

## 4. Verify Docs

Check that docs referencing version numbers, architecture, or runtime behavior are current.

```bash
# Stale version references (should only match places you already bumped)
grep -rn '0\.OLD\.VERSION' *.md docs/ --include='*.md'

# RELEASING.md example version
grep 'NEW_VERSION=' docs/RELEASING.md

# project-context.md — verify supervisor policy, crash dump, and other
# runtime notes still match the code
grep -n 'max_restarts\|watchdog\|crash.dump\|ERL_CRASH_DUMP' docs/project-context.md
```

Update any stale references, then include the changed docs in the version-bump commit below.

## 5. Commit and Tag

```bash
git add mix.exs INSTALL.md release/SKILL.md release/roundtable
git commit -m "chore: bump version to ${NEW_VERSION}"
git tag -a "v${NEW_VERSION}" -m "v${NEW_VERSION} — <short description>"
```

## 6. Push to Forgejo (Primary)

Push commits and tags:

```bash
git push origin main
git push origin --tags
```

If HTTPS credentials aren't configured, use the tea token:

```bash
TEA_TOKEN=$(grep 'token:' ~/.config/tea/config.yml | head -1 | awk '{print $2}')
FORGEJO_HOST=$(grep 'url:' ~/.config/tea/config.yml | head -1 | awk '{print $2}' | sed 's|https://||')
TEA_USER=$(grep 'user:' ~/.config/tea/config.yml | head -1 | awk '{print $2}')

git remote set-url origin "https://${TEA_USER}:${TEA_TOKEN}@${FORGEJO_HOST}/stackhouse/roundtable"
git push origin main && git push origin --tags
git remote set-url origin "https://${FORGEJO_HOST}/stackhouse/roundtable"  # restore clean URL
```

## 7. Push to GitHub (Mirror)

Ensure the `github` remote exists:

```bash
git remote add github https://github.com/TejGandham/roundtable.git 2>/dev/null || true
```

Push using gh token:

```bash
GH_TOKEN=$(gh auth token)
git remote set-url github "https://TejGandham:${GH_TOKEN}@github.com/TejGandham/roundtable.git"
git push github main && git push github --tags
git remote set-url github "https://github.com/TejGandham/roundtable.git"  # restore clean URL
```

## 8. Create Release on Forgejo

```bash
tea release create \
  --tag "v${NEW_VERSION}" \
  --title "Roundtable v${NEW_VERSION}" \
  --note "## <Title>

<Description of what changed.>

### What's New
- **Feature name** — short description
- **Another feature** — short description

### Assets
- \`roundtable-mcp-${NEW_VERSION}.tar.gz\` — full release package
- \`SHA256SUMS\` — integrity checksums" \
  --asset "_build/prod/rel/roundtable-mcp-${NEW_VERSION}.tar.gz" \
  --asset "_build/prod/rel/SHA256SUMS"
```

## 9. Create Release on GitHub

```bash
gh release create "v${NEW_VERSION}" \
  --repo TejGandham/roundtable \
  --title "Roundtable v${NEW_VERSION}" \
  --notes "$(cat <<'EOF'
## <Title>

<Description of what changed.>

### What's New
- **Feature name** — short description
- **Another feature** — short description

### Assets
- `roundtable-mcp-VERSION.tar.gz` — full release package
- `SHA256SUMS` — integrity checksums
EOF
)" \
  "_build/prod/rel/roundtable-mcp-${NEW_VERSION}.tar.gz" \
  "_build/prod/rel/SHA256SUMS"
```

## 10. Post-Release Verification

```bash
# Verify Forgejo release
tea release list

# Verify GitHub release
gh release list --repo TejGandham/roundtable

# Verify download works (use the INSTALL.md URL pattern)
curl -sL "https://github.com/TejGandham/roundtable/releases/download/v${NEW_VERSION}/SHA256SUMS"
```

## Quick Reference

| Step | Command | What it does |
|-|-|-|
| Version bump | Edit `mix.exs`, `INSTALL.md` | Update version string |
| Sync SKILL.md | `cp SKILL.md release/SKILL.md` | Keep release skill file current |
| Build escript | `mix escript.build` | CLI binary |
| Build release | `MIX_ENV=prod mix release roundtable_mcp` | MCP server release |
| Package | `tar czf roundtable-mcp-X.Y.Z.tar.gz roundtable_mcp/` | Distribution tarball |
| Checksum | `sha256sum ... > SHA256SUMS` | Integrity verification |
| Verify docs | `grep` for stale versions in `*.md`, `docs/` | Catch outdated references |
| Commit + tag | `git commit`, `git tag -a vX.Y.Z` | Version control |
| Push Forgejo | `git push origin main --tags` | Primary remote |
| Push GitHub | `git push github main --tags` | Mirror remote |
| Release Forgejo | `tea release create --tag ... --asset ...` | Forgejo release with assets |
| Release GitHub | `gh release create ... <assets>` | GitHub release with assets |

## File Reference

| File | Role |
|-|-|
| `mix.exs:7` | Version string (`version: "X.Y.Z"`) |
| `INSTALL.md:20` | Install script version (`VERSION=X.Y.Z`) |
| `release/SKILL.md` | Skill file shipped in tarball |
| `release/roundtable` | Escript binary shipped in tarball |
| `release/roles/` | Role prompts shipped in tarball |
| `rel/env.sh.eex` | Release env config (`RELEASE_DISTRIBUTION=none`) |
| `rel/overlays/bin/roundtable-mcp` | MCP wrapper script (sets `ROUNDTABLE_MCP=1`) |
| `priv/roles/` | Role prompts bundled by OTP release |

## Environment

| Tool | Purpose | Auth check |
|-|-|-|
| `tea` | Forgejo CLI | `tea login list` |
| `gh` | GitHub CLI | `gh auth status` |
| `mix` | Elixir build tool | `mix --version` |
| Erlang/OTP 28+ | Runtime | `erl -noshell -eval 'io:format("~s~n", [erlang:system_info(otp_release)]), halt().'` |

## Notes

- **`include_erts: false`**: The release does NOT bundle Erlang. The target machine must have a compatible Erlang/OTP installed.
- **Dep patching**: `mix deps.get` is aliased to also run `mix deps.patch`, which applies `patches/*.patch` to `deps/hermes_mcp`. Patches that are already applied are skipped.
- **`ROUNDTABLE_MCP` must be exactly `"1"`**: The check is `== "1"`, not truthy. `true`/`yes` will not start the MCP server.
- **Two entrypoints**: The escript (`roundtable-cli`) is for CLI/scripting use. The OTP release (`roundtable_mcp`) is for MCP server mode. Both share the same `Roundtable.run/1` core logic.
- **Role directory resolution differs by entrypoint**: MCP uses `:code.priv_dir(:roundtable)`. CLI escript uses sibling `roles/` dir relative to the binary.
