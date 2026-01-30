---
sidebar_position: 1
---

# Getting Started

**td** is a minimalist CLI for tracking tasks across AI coding sessions. It acts as external memory for your project -- so the next agent session picks up exactly where the last one left off, without pasting context or re-explaining state.

## The Problem

Context loss across AI sessions is expensive. An agent does meaningful work, the session ends, and the next session has no idea what happened. You paste context, get misunderstandings, waste time re-orienting. Or worse: the agent confidently continues from the wrong state, undoing good work or duplicating effort.

There's no structured way to say "here's what's done, here's what remains, here's what I was uncertain about."

## How td Solves It

Three mechanisms eliminate context loss:

- **Structured handoffs** -- Each session records what's done, what remains, decisions made, and uncertainties. The next session reads this instead of guessing.
- **Session isolation** -- The session that did the work can't approve its own output. A different session must review, catching errors the implementer is blind to.
- **Single-command context** -- `td usage` gives the incoming session everything it needs: current issues, recent logs, pending handoffs, and what to work on next.

## Installation

### Homebrew (macOS)

```bash
brew install marcus/tap/td
```

### Download Binary

Download pre-built binaries from [GitHub Releases](https://github.com/marcus/td/releases). Available for macOS and Linux (amd64/arm64).

### Go Install

```bash
# Requirements: Go 1.21+
go install github.com/marcus/td@latest

# Ensure ~/go/bin is in PATH
export PATH="$PATH:$HOME/go/bin"
```

### Verify

```bash
td version
```

:::tip
If `td version` prints nothing, confirm that `~/go/bin` is in your shell's PATH and restart your terminal.
:::

## Quick Start

```bash
cd /path/to/your/project
td init

# Create first issue
td create "Add user auth" --type feature --priority P1

# Start work
td start td-a1b2

# Log progress
td log "OAuth callback working"

# Hand off before context ends
td handoff td-a1b2 --done "OAuth flow" --remaining "Token refresh"

# Submit for review (different session approves)
td review td-a1b2
```

:::info
Issue IDs like `td-a1b2` are generated automatically when you create an issue. Use `td list` to see your current issues and their IDs.
:::

## Setting Up with AI Agents

Add this to your `CLAUDE.md`, system prompt, or agent instructions:

```
Run `td usage --new-session` at conversation start (or after /clear).
```

This gives the agent full project context on startup -- open issues, recent activity, pending reviews, and what to work on next.

Works with Claude Code, Cursor, Codex, Copilot, and Gemini CLI.

## Next Steps

- [Core Workflow](./core-workflow.md) -- Issue lifecycle, logging, handoffs, and reviews in depth
- [Boards](./boards.md) -- Visual overview of issue status across your project
- [Query Language](./query-language.md) -- Filter and search issues with structured queries
