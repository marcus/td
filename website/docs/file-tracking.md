---
sidebar_position: 8
---

# File Tracking

Track which files are associated with which issues. Files are SHA-tracked at link time, so you can detect what has changed since you started working.

## Linking Files

```bash
td link td-a1b2 src/auth/*.go        # Track files with an issue
td link td-a1b2 src/auth/login.go src/auth/token.go  # Multiple specific files
```

## Viewing File Status

```bash
td files td-a1b2
```

Output shows change status relative to when each file was linked:

```
src/auth/login.go     [modified]
src/auth/token.go     [unchanged]
src/auth/session.go   [new]
src/auth/old.go       [deleted]
```

## How It Works

Files are SHA-tracked when linked. `td files` compares the current SHA to the stored SHA to detect modifications. No more "did I already change this file?"

## Unlinking Files

```bash
td unlink td-a1b2 src/auth/old.go    # Remove file association
```
