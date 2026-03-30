# Roundtable

Multi-model consensus skill for Claude Code.

## Requirements

- Erlang/OTP and Elixir: `brew install elixir`
- `gemini` CLI installed and authenticated
- `codex` CLI installed and authenticated
- `claude` CLI installed and authenticated

## Build

```bash
mix deps.get
mix escript.build
```

This produces `./roundtable` — the escript binary.

## Test

```bash
mix test
```

## Usage

```bash
./roundtable --prompt "Your question here" --role planner --files src/auth.ts
```

See `SKILL.md` for full documentation.
