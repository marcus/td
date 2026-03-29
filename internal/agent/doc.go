// Package agent provides utilities for detecting and configuring AI agent
// instruction files across different agent types (Claude, Gemini, Codex,
// Copilot, Cursor, etc.).
//
// It handles discovering existing agent configuration files in a project
// directory, determining the preferred file based on a priority system
// (AGENTS.md is preferred), and installing or updating td task management
// instructions in those files.
//
// Key functions:
//
//   - [DetectAgentFile] finds the first existing agent file in a project.
//   - [PreferredAgentFile] determines the best file for instruction installation.
//   - [HasTDInstructions] checks whether a file already contains td instructions.
//   - [InstallInstructions] adds td usage instructions to an agent file.
package agent
