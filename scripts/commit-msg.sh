#!/usr/bin/env bash
# commit-msg hook for td — enforces conventional commit format
# Install: make install-hooks  (or: ln -sf ../../scripts/commit-msg.sh .git/hooks/commit-msg)
set -euo pipefail

MSG_FILE="$1"
MSG=$(cat "$MSG_FILE")

# Skip merge commits
if echo "$MSG" | head -1 | grep -qE '^Merge '; then
  exit 0
fi

# Skip fixup/squash/amend commits
FIRST=$(echo "$MSG" | head -1)
if echo "$FIRST" | grep -qE '^(fixup|squash|amend)! '; then
  exit 0
fi

SUBJECT=$(echo "$MSG" | head -1)

# --- Check conventional commit format: type[(scope)][!]: description ---
if ! echo "$SUBJECT" | grep -qE '^[a-zA-Z]+(\([a-zA-Z0-9._-]+\))?!?: .+'; then
  echo "❌ commit-msg: subject must match conventional commit format"
  echo "   Expected: type[(scope)]: description"
  echo "   Got:      $SUBJECT"
  echo ""
  echo "   Approved types: feat fix docs style refactor perf test build ci chore revert"
  exit 1
fi

# Extract type (everything before optional scope/bang/colon)
TYPE=$(echo "$SUBJECT" | sed -E 's/^([a-zA-Z]+).*/\1/')
TYPE_LOWER=$(echo "$TYPE" | tr '[:upper:]' '[:lower:]')

# Validate type
APPROVED="feat fix docs style refactor perf test build ci chore revert"
VALID=0
for t in $APPROVED; do
  if [ "$TYPE_LOWER" = "$t" ]; then
    VALID=1
    break
  fi
done

if [ "$VALID" -eq 0 ]; then
  echo "❌ commit-msg: unknown type '$TYPE'"
  echo "   Approved types: $APPROVED"
  exit 1
fi

# Check subject length
if [ "${#SUBJECT}" -gt 72 ]; then
  echo "❌ commit-msg: subject is ${#SUBJECT} chars (max 72)"
  echo "   $SUBJECT"
  exit 1
fi

# Extract description (after ": ")
DESC=$(echo "$SUBJECT" | sed -E 's/^[^:]+: //')
if [ -z "$DESC" ]; then
  echo "❌ commit-msg: description is required after type prefix"
  exit 1
fi

# --- Auto-normalize minor issues ---
CHANGED=0

# Lowercase type if needed
if [ "$TYPE" != "$TYPE_LOWER" ]; then
  MSG=$(echo "$MSG" | sed "1 s/^$TYPE/$TYPE_LOWER/")
  CHANGED=1
fi

# Lowercase first char of description
FIRST_CHAR=$(echo "$DESC" | cut -c1)
FIRST_LOWER=$(echo "$FIRST_CHAR" | tr '[:upper:]' '[:lower:]')
if [ "$FIRST_CHAR" != "$FIRST_LOWER" ]; then
  # Replace only the first occurrence of the description's first char after ": "
  MSG=$(echo "$MSG" | sed -E "1 s/: $FIRST_CHAR/: $FIRST_LOWER/")
  CHANGED=1
fi

# Strip trailing period from subject
if echo "$MSG" | head -1 | grep -qE '\.\s*$'; then
  MSG=$(echo "$MSG" | sed -E '1 s/\.(\s*)$/\1/')
  CHANGED=1
fi

# Normalize Co-Authored-By casing
if echo "$MSG" | grep -qi 'co-authored-by:'; then
  MSG=$(echo "$MSG" | sed -E 's/[Cc][Oo]-[Aa][Uu][Tt][Hh][Oo][Rr][Ee][Dd]-[Bb][Yy]:/Co-Authored-By:/g')
  CHANGED=1
fi

# Normalize Signed-Off-By casing
if echo "$MSG" | grep -qi 'signed-off-by:'; then
  MSG=$(echo "$MSG" | sed -E 's/[Ss][Ii][Gg][Nn][Ee][Dd]-[Oo][Ff][Ff]-[Bb][Yy]:/Signed-Off-By:/g')
  CHANGED=1
fi

if [ "$CHANGED" -eq 1 ]; then
  echo "$MSG" > "$MSG_FILE"
  echo "✨ commit-msg: auto-normalized commit message"
fi
