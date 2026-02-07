# Shortcuts Implementation Guide

This guide has been split into focused documents:

| Guide | Purpose |
|-------|---------|
| [cli-commands-guide.md](cli-commands-guide.md) | CLI commands (Cobra-based, `cmd/*.go`) |
| [monitor-shortcuts-guide.md](monitor-shortcuts-guide.md) | Monitor TUI shortcuts (bubbletea, `pkg/monitor/`) |

## Quick Reference

### CLI Commands
- GroupID + aliases + `--json` + undo logging
- See `cli-commands-guide.md`

### Monitor TUI Shortcuts
Four things must match:
1. Command constant in `registry.go`
2. Key binding in `bindings.go`
3. Handler case in `model.go`
4. Export metadata in `export.go`

Priority: 1-3 = footer visible, 4+ = palette only

See `monitor-shortcuts-guide.md`

### Sidecar Integration
TD exports shortcuts to sidecar via `ExportBindings()` and `ExportCommands()`.
- TD side: `monitor-shortcuts-guide.md` (Sidecar Integration section)
- Sidecar side: see the Sidecar repository documentation (keyboard shortcuts reference lives there).
