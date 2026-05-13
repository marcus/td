# PII Exposure Scan Report

**Repository:** `td` (github.com/marcus/td)
**Branch scanned:** `codex/lint-fix-linter-fixes` (working branch: `nightshift/pii-scanner`)
**Scan date:** 2026-05-13
**Scope:** All tracked files (`git ls-files`), excluding `website/node_modules/`, `website/build/`, `go.sum` vendor hashes, and `website/static/img/*.svg` binary blobs.

This report covers five categories: hardcoded PII literals, PII interpolation in logs/errors, plaintext secrets in env/config/CI files, unencrypted PII at rest, and `.gitignore` coverage gaps. The repository handles only one real PII class — **email addresses** — used for passwordless authentication of sync clients. It does not collect SSNs, phone numbers, credit cards, addresses, DOB, or names of real people. No tracked credential, key, or data-dump files were found.

---

## Findings

### Category: hardcoded-pii

| # | file | line | severity | detail | recommendation |
|---|------|------|----------|--------|----------------|
| 1 | `scripts/e2e-sync-test.sh` | 48-49, 287, 292 | low | E2E harness hardcodes `alice@test.local`, `bob@test.local` — reserved-TLD fake emails used as test accounts. | No action; `.test.local` is reserved and unroutable. Keep using `*.test`/`*.example` / `*.test.local` rather than real domains. |
| 2 | `scripts/e2e/harness.sh` | 217-251 | low | Same pattern: `alice@test.local`, `bob@test.local`, `carol@test.local` for shell-based sync tests. | No action; document the convention in `scripts/e2e/e2e-sync-test-guide.md` so contributors don't substitute real addresses. |
| 3 | `internal/serverdb/auth_events_test.go` | 12, 30, 44, 64, 89-92, 111-113, 129-133, 167, 215, 218, 246-247, 269-270 | medium | Test inserts use `user@example.com`, `alice@example.com`, `a@test.com`, `expired@test.com`, etc. Realistic-looking but RFC-2606 reserved domains. | No action required (RFC-2606 reserved); leave as-is. Treated medium per task brief ("realistic-looking PII as medium"). |
| 4 | `internal/serverdb/device_auth_test.go` | 11-181 | medium | Same pattern: 12 instances of `test@example.com`, `expired1@example.com`, etc. | No action; reserved-domain. |
| 5 | `internal/serverdb/auth_test.go` | 10-13 | medium | `owner@test.com`, `writer@test.com`, `reader@test.com`, `outsider@test.com`. | No action; reserved-domain. |
| 6 | `internal/serverdb/admin_test.go` | 11-68 | medium | `admin@test.com`, `user@test.com`, etc. | No action; reserved-domain. |
| 7 | `.claude/skills/td-integration-test/SKILL.md` | 17-104 | medium | Skill documentation uses `user@test.com`, `admin@test.com`. | No action; documentation pattern matches the test code it describes. |
| 8 | `.claude/skills/td-integration-test/references/harness-api.md` | 115-262 | medium | Harness API examples use the same reserved domains. | No action. |
| 9 | `internal/git/git_test.go` | 21 | low | Sets git config `user.email = test@test.com` in isolated test dir. | No action; required for git commits in tests. |
| 10 | `internal/api/admin_server_test.go` | 132-205 | low | RFC-1918 private IPs (`192.168.1.1`, `10.0.0.1`, `10.0.0.2`) used as fixture IPs for rate-limit tests. | No action; not real-user identifiers. |
| 11 | `internal/api/ratelimit_test.go` | 121-269 | low | `1.2.3.4`, `10.0.0.1`, `10.0.0.99`, `203.0.113.50` used as fixture IPs. `203.0.113.0/24` is TEST-NET-3 (RFC-5737). | No action; addresses are reserved test ranges. |
| 12 | `docs/sync-server-ops-guide.md` | 265 | low | nginx example `proxy_pass http://127.0.0.1:8080;` — loopback, not PII. | No action. |
| 13 | `docs/**/sync-plan-*.md`, `docs/guides/*.md`, `docs/sync-client-guide.md` | various | low | Sample CLI snippets use `user@example.com`, `alice@example.com`. | No action; reserved-domain in user docs is the correct pattern. |

**Not found in the scan:** SSNs (`\d{3}-\d{2}-\d{4}` → 0 matches in non-test paths), credit-card-shaped literals (matches were go.mod pseudo-versions, SVG path data, and 10-digit test session IDs — false positives), phone numbers, street addresses, DOBs, passport/DL numbers, or full person names in structured data.

---

### Category: pii-in-logs

| # | file | line | severity | detail | recommendation |
|---|------|------|----------|--------|----------------|
| 14 | `internal/api/auth.go` | 240 | **high** | `slog.Warn("signup denied", "email", ar.Email)` — server-side log writes the user's plaintext email at WARN level whenever signup is disabled and an unknown email attempts to verify. Sent to the configured slog handler (text/JSON to stdout in prod). | Hash or partially mask the email before logging (e.g., log the SHA-256 first 8 chars, or `user***@domain.com`). If full email is required for audit, route it to `auth_events` rather than `slog` and document the retention/access controls. |
| 15 | `internal/api/auth.go` | 263 | **high** | `logFor(r.Context()).Info("device verified", "email", ar.Email)` — INFO-level structured log writes every successful login email to operator logs. | Same fix as above; consider replacing `email` with `user_id` (already known at this point: `user.ID`). The `auth_events` table already captures the verified-email record. |
| 16 | `internal/api/auth.go` | 268-278 (`logAuthEvent`) | medium | `logAuthEvent` writes `email` + `user_agent` + IP into the `auth_events` SQLite table on every auth attempt (failed and successful). Stored plaintext, no retention policy in schema. | Add a retention/purge job for `auth_events` rows older than N days; consider storing a salted hash of email when only correlation (not display) is needed. Document who can read this table. |
| 17 | `internal/serverdb/users.go` | 143 | medium | `return fmt.Errorf("user not found: %s", email)` — email is interpolated into an error string that propagates up through `SetUserAdmin` and into admin CLI output / API responses. | Drop the email from the error; the caller already has it. Return `errors.New("user not found")` and let the caller annotate with the email only at the boundary it controls. |
| 18 | `cmd/auth.go` | 140 | low | `fmt.Printf("Email:  %s\n", creds.Email)` — `td auth status` prints the local user's own email to their terminal. | No action; user printing their own email on their own machine is not a leak. |
| 19 | `cmd/doctor.go` | 29 | low | `fmt.Printf("Auth config ............ OK (%s)\n", auth.Email)` — `td doctor` includes email in diagnostic output. | Low risk, but `td doctor` output is often pasted into bug reports / chat. Consider redacting to `user***@domain.com` to reduce accidental sharing. |
| 20 | `cmd/sync_init.go` | 55, 149 | low | `fmt.Printf("Authenticated as: %s\n", creds.Email)` / `fmt.Printf("Email:   %s\n", creds.Email)` — local CLI confirmation prints. | No action; user's own data on user's terminal. |
| 21 | `cmd/td-sync/admin.go` | 75, 120, 176 | low | Server admin CLI prints target user's email when granting/revoking admin or creating an API key. | No action; admin operator already has access to the user table. |

---

### Category: env-secret

| # | file | line | severity | detail | recommendation |
|---|------|------|----------|--------|----------------|
| 22 | `deploy/.env.example`, `deploy/envs/.env.dev.example`, `deploy/envs/.env.staging.example`, `deploy/envs/.env.prod.example` | — | none | All four tracked env files are templates: secret values (`AWS_ACCESS_KEY_ID=`, `AWS_SECRET_ACCESS_KEY=`) are blank; non-secret config is shown literally. Real `.env.{dev,staging,prod}` files are excluded by `.gitignore` line 28. | No action. Continue requiring the `.example` suffix and the `!deploy/envs/.env.*.example` re-include rule. |
| 23 | `.github/workflows/release.yml` | 31, 32, 52 | none | `GITHUB_TOKEN` and `HOMEBREW_TAP_TOKEN` are referenced via `${{ secrets.* }}` — never inlined. | No action. |
| 24 | `deploy/compose/docker-compose.prod.yml`, `…/docker-compose.staging.yml` | 17-18 | none | AWS keys passed via `${AWS_ACCESS_KEY_ID}` / `${AWS_SECRET_ACCESS_KEY}` shell substitution from the (gitignored) `.env.{prod,staging}` file. | No action. |
| 25 | `.github/workflows/deploy-docs.yml` | 13 | none | `id-token: write` is the OIDC permission for GitHub-issued tokens, not a secret value. | No action. |

---

### Category: unencrypted-storage

| # | file | line | severity | detail | recommendation |
|---|------|------|----------|--------|----------------|
| 26 | `internal/syncconfig/syncconfig.go` | 37-45, 115-126 | **high** | `AuthCredentials` (API key, email, user ID, device ID, expiry) is serialized as plaintext JSON to `~/.config/td/auth.json` with mode `0600`. An attacker with read access to the user's home directory recovers a server-side API key that grants full sync access for that user. | At minimum, document the file's sensitivity and mode in `cmd/auth.go`. Consider integrating with the OS keychain (macOS Keychain, libsecret, Windows DPAPI) and falling back to the plaintext file only when no keychain is available. This is a common CLI tradeoff but worth a tracked decision (see [docs/sync-server-ops-guide.md](docs/sync-server-ops-guide.md)). |
| 27 | `internal/serverdb/schema.go` | 8-14 (users), 90, 113 (auth_requests, auth_events) | medium | `users.email`, `auth_requests.email`, `auth_events.email` are stored as plaintext `TEXT NOT NULL`. Required for lookup (case-folded via `LOWER(email)` in `users.go:84`) and for audit, but means a DB exfil exposes all sync user emails. | (a) Document this in an operator-facing security note. (b) Ensure file-level encryption at rest (the Litestream replicas hit S3 — confirm bucket has SSE enabled). (c) For `auth_events`, add a retention job and consider hashing email after N days of operational utility have passed. |
| 28 | `internal/serverdb/schema.go` | 17-29 (api_keys) | none | `api_keys.key_hash` is stored hashed; the raw key is only shown once at creation time (`cmd/td-sync/admin.go:176`). Correct pattern. | No action. |
| 29 | passwordless auth | — | none | No `password` column exists anywhere in the schema; auth is by emailed verification code. The only legacy hits for "password" are in `pkg/monitor/clipboard_test.go` (an issue *titled* "Password reset" — fixture text, not a credential) and `internal/db/analytics.go:41` (a *deny list* of substrings the analytics tool refuses to emit). | No action. |

---

### Category: gitignore-gap

| # | file | line | severity | detail | recommendation |
|---|------|------|----------|--------|----------------|
| 30 | `.gitignore` | — | medium | `deploy/envs/.env.*` is covered, but the root pattern `.env` and unscoped `.env.*` are not. A contributor placing a `.env` in the repo root (a common docker-compose default) would not be protected. | Add a top-level `.env` and `.env.*` rule with a `!.env.example` re-include. |
| 31 | `.gitignore` | — | medium | No ignore rules for private keys / certificates: `*.pem`, `*.key`, `*.p12`, `*.pfx`, `*.crt`, `*.cer`. Currently no such files are tracked, but the gap allows accidental commit. | Append: `*.pem`, `*.key`, `*.p12`, `*.pfx`, `*.crt`, `*.cer`, and `id_rsa*`. |
| 32 | `.gitignore` | — | medium | No rules for `credentials.*` or `secrets.*` (and no rule covering local copies of the `auth.json` file used by the client). | Append: `credentials.*`, `secrets.*`, `auth.json` (paths outside `~/.config/td/` should still not appear in the repo). |
| 33 | `.gitignore` | — | low | No rules for common data-dump extensions: `*.sql`, `*.csv`, `*.xlsx`. Currently no such files tracked. The project ships a Litestream-replicated SQLite database, so a user dumping it for debugging could easily produce a `dump.sql` next to the repo. | Append: `*.sql`, `*.csv`, `*.xlsx` (with a re-include exception for any intentional migrations folder if one is added later). |
| 34 | tracked files | — | none | No currently-tracked file falls into the above sensitive categories (cross-checked with `git ls-files | grep -E '\.(env\|pem\|key\|p12\|pfx\|crt\|cert\|sql\|csv\|xlsx)$'` — only the four `.env.*.example` templates appear, and they are explicitly re-included). | No action. |

---

## Summary

### By category

| Category | Critical | High | Medium | Low | Total |
|---|---:|---:|---:|---:|---:|
| hardcoded-pii | 0 | 0 | 6 | 7 | 13 |
| pii-in-logs | 0 | 2 | 2 | 4 | 8 |
| env-secret | 0 | 0 | 0 | 0 | 4 (all clean) |
| unencrypted-storage | 0 | 1 | 1 | 0 | 4 (1 clean, 1 N/A) |
| gitignore-gap | 0 | 0 | 3 | 1 | 5 (1 clean) |
| **Total** | **0** | **3** | **12** | **12** | **34** |

### By severity

- **Critical:** 0
- **High:** 3 — items 14, 15 (email in operator logs), 26 (plaintext API key on disk)
- **Medium:** 12 — mostly realistic-looking test emails (auto-classified per task brief), `auth_events` retention, and `.gitignore` coverage gaps for `.env`/`*.pem`/`credentials.*`
- **Low:** 12 — local CLI prints of the user's own email, fake-domain test fixtures, RFC-1918 / TEST-NET IP literals

### Recommended next steps (prioritized)

1. **Stop logging plaintext email in operator slog output** (`internal/api/auth.go:240, 263`). Hash or replace with `user_id`. (#14, #15)
2. **Document the `~/.config/td/auth.json` sensitivity** and consider OS-keychain integration. (#26)
3. **Tighten `.gitignore`**: add `.env`, `.env.*`, `*.pem`, `*.key`, `credentials.*`, `secrets.*`, `*.sql`, `*.csv`, `*.xlsx` to defend against future accidental commits. (#30-33)
4. **Add retention/redaction for `auth_events.email`** to bound exposure if the server DB is compromised. (#16, #27)
5. **Trim email from `users.go:143` error string** to avoid leaking it through API error responses. (#17)

Nothing in this scan triggered the *critical* tier: no real-person PII, no committed secrets, no plaintext password storage. The repo's PII surface is narrow (email + API keys) and the existing defenses (passwordless auth, hashed `api_keys`, 0600 `auth.json`, env-var indirection in CI/Compose, gitignored deploy env files) cover the main pathways.
