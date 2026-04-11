# Roundtable

How many times this week did you ask a second model?

You have access to Claude, Gemini, Codex — you may even be paying for more than one. But the tab switch and the copy-paste never happen. So every answer you ship comes from one model's opinion.

One of them has already been wrong in a way you haven't caught yet.

The second opinion exists. The workflow to get it doesn't.

**Roundtable is that workflow.**

## The problem

Not the obvious hallucination. The dangerous one: correct pattern, correct library, wrong detail. A parameter name that changed two versions ago. A concurrency fix that looks elegant and quietly reintroduces a race condition. An infrastructure block that reads like real documentation but doesn't exist.

It compiles. It passes your smell test. It ships. You find out at 2am.

You cross-check sometimes. Just not often enough to catch the subtle ones — because cross-checking means re-establishing context in another terminal, copy-pasting a prompt, waiting, and mentally diffing two walls of prose. So you only do it for decisions you *already* think are risky. The ones that burn you are the ones you didn't think were risky.

## What it does

Roundtable is an MCP server that sends your prompt to Claude, Gemini, and Codex CLIs — in parallel — and returns structured JSON with all their responses. One tool call from inside your existing agent. It uses the CLIs already in your PATH, already authenticated. Runs locally. Nothing stored or proxied — Roundtable itself never sees your prompts; it passes them directly to your CLIs, which talk to their providers as usual.

You can run the same CLI with different models in a single dispatch. Claude with Opus for the architecture review, Claude with Sonnet for the quick sanity check. Gemini for the edge cases. Codex for an independent take. Compose your own panel.

## Why disagreement matters

When models agree, that's useful triage — not proof, but a strong signal you're on the right track.

When they disagree, that's the real value. Disagreement surfaces tradeoffs you would have missed, edge cases one model sees and another doesn't, or a hallucination the others don't share. You don't need three models to be right. You need them to be *different enough* to catch each other.

```json
{
  "claude": { "status": "ok", "response": "Use a message queue — decouples the producer..." },
  "gemini": { "status": "ok", "response": "Use a message queue with dead-letter handling..." },
  "codex":  { "status": "ok", "response": "A cron job is simpler here — the volume doesn't justify a queue..." }
}
```

![Roundtable: Cross-checking your code decisions](docs/roundtable.png)

Two models agree on the queue. One says it's overengineered. That disagreement is worth more than any single answer.

## How it's built

Single Go binary. HTTP MCP server on `127.0.0.1:4040` dispatching to Gemini and Claude via subprocess-per-request and to Codex via a long-lived `codex app-server` JSON-RPC connection. Each CLI runs in its own process group with atomic kill on timeout. If a backend hangs, Go kills it at the deadline and returns a structured error. If the Go server dies, the HTTP connection fails immediately. Health endpoints (`/healthz`, `/readyz`) and burn-in metrics (`/metricsz`) are built in. Cross-platform: Linux, macOS.

Selective dispatch controls cost. Route architecture decisions to the heavy models. Route boilerplate to the fast ones. The `agents` parameter takes a JSON array — pick exactly who sits at the table.

`ROUNDTABLE_DEFAULT_AGENTS` — configure which agents run by default. Per-call `agents` parameter always overrides.

```bash
# Only use Claude and Gemini by default
ROUNDTABLE_DEFAULT_AGENTS='[{"cli":"claude"},{"cli":"gemini"}]'
```

## Quick start

Have your favorite agent read [INSTALL.md](INSTALL.md). Then ask all of them the question you were about to ask just one. See where they disagree. That's where you should look twice.

## MCP Tools

Each tool assigns a role to each agent, shaping its system prompt.

| Tool | Role | Use Case |
|-|-|-|
| `roundtable_hivemind` | default | General multi-model consensus |
| `roundtable_deepdive` | planner | Extended reasoning / deep analysis |
| `roundtable_architect` | planner | Implementation planning |
| `roundtable_challenge` | codereviewer | Devil's advocate / stress-test |
| `roundtable_xray` | gemini=planner, codex=codereviewer | Architecture + code quality review |

All tools support an `agents` parameter for selective dispatch. See [SKILL.md](SKILL.md) for full parameter docs.

## Docs

|Doc|Contents|
|-|-|
|[INSTALL.md](INSTALL.md)|Install guide (written for AI agents to execute directly)|
|[SKILL.md](SKILL.md)|Tool parameters, selective dispatch, output format, synthesis guide|
|[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)|Architecture details, components, request flow, Codex RPC protocol, concurrency model|
|[docs/RELEASING.md](docs/RELEASING.md)|Release process — build, tag, publish|
|[DESIGN.md](DESIGN.md)|Original design document (historical)|
