.PHONY: help fmt test test-dev-install install install-local install-worktree \
	use-homebrew install-status tag release check-clean check-version \
	check-main check-pushed check-changelog check-ci install-hooks

SHELL := /bin/sh

# Set VERSION on the command line, e.g.:
#   make release VERSION=v0.2.0
VERSION ?=

# A helpful dev version string (used by install)
GIT_DESCRIBE := $(shell git describe --tags --always --dirty 2>/dev/null)

help:
	@printf "%s\n" \
		"Targets:" \
		"  make fmt                       # gofmt -w ." \
		"  make install-hooks             # install git pre-commit hook" \
		"  make test                      # full tests with release-safe environment" \
		"  make test-dev-install          # test install switching in a fake prefix" \
		"  make install-local             # activate the canonical main checkout" \
		"  make install-worktree          # activate the current branch/worktree" \
		"  make install-status            # show the active install and shell resolution" \
		"  make use-homebrew              # restore the installed Homebrew release" \
		"  make install                   # unmanaged go install into GOBIN" \
		"  make tag VERSION=vX.Y.Z        # create annotated git tag (requires clean tree)" \
		"  make release VERSION=vX.Y.Z    # verify changelog+CI+pushed main, test, tag + push"

fmt:
	gofmt -w .

test:
	env -u TD_FEATURE_SYNC_AUTOSYNC -u TD_FEATURE_SYNC_CLI GOWORK=off go test ./...

test-dev-install:
	./scripts/test-dev-install.sh

# Unmanaged Go install into GOBIN. Does not touch Homebrew links, and does not
# decide which td wins PATH precedence. Use install-local for that.
install:
	@V="$(GIT_DESCRIBE)"; V=$${V:-dev}; \
	echo "Installing td $$V"; \
	go install -ldflags "-X main.Version=$$V" .

# Managed machine-wide development installs and Homebrew switching.
install-local:
	./scripts/dev-install.sh install-local

install-worktree:
	./scripts/dev-install.sh install-worktree

use-homebrew:
	./scripts/dev-install.sh use-homebrew

install-status:
	./scripts/dev-install.sh status

check-clean:
	@test -z "$$(git status --porcelain)" || (echo "Error: working tree is not clean" && exit 1)

check-version:
	@test -n "$(VERSION)" || (echo "Error: VERSION is required (e.g. VERSION=v0.2.0)" && exit 1)
	@echo "$(VERSION)" | grep -Eq '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$$' || (echo "Error: VERSION should look like vX.Y.Z without leading zeroes" && exit 1)

check-main:
	@test "$$(git branch --show-current)" = "main" || (echo "Error: releases must be cut from main" && exit 1)

check-pushed:
	@git remote get-url origin >/dev/null 2>&1 || (echo "Error: no 'origin' remote configured" && exit 1)
	@test "$$(git rev-parse HEAD)" = "$$(git rev-parse origin/main)" || (echo "Error: HEAD must match origin/main; push main before releasing" && exit 1)

check-changelog:
	@grep -Fq "## [$(VERSION)] - " CHANGELOG.md || (echo "Error: CHANGELOG.md has no $(VERSION) release entry" && exit 1)

# Go CI (currently tests only; lint isn't wired in yet, see go-ci.yml) must be
# green on the commit being released. Fails closed if red, still running, or
# hasn't started. Skips with a warning if gh can't resolve a GitHub repo here
# (e.g. no origin, or origin isn't github.com/marcus/td).
check-ci:
	@if ! command -v gh >/dev/null 2>&1 || ! gh repo view >/dev/null 2>&1; then \
		echo "Warning: gh unavailable or origin is not a resolvable GitHub repo; skipping automated Go CI status check" >&2; \
		exit 0; \
	fi; \
	head=$$(git rev-parse origin/main); \
	runs=$$(gh run list --workflow=go-ci.yml --branch main --limit 20 --json headSha,status,conclusion -q "[.[] | select(.headSha == \"$$head\")]" 2>/dev/null || echo '[]'); \
	count=$$(echo "$$runs" | jq 'length'); \
	if [ "$$count" = 0 ]; then \
		echo "Error: no Go CI run found for $$head yet; wait for it to start" && exit 1; \
	fi; \
	status=$$(echo "$$runs" | jq -r '.[0].status'); \
	conclusion=$$(echo "$$runs" | jq -r '.[0].conclusion'); \
	if [ "$$status" != completed ]; then \
		echo "Error: Go CI is still $$status on $$head; wait for it to finish" && exit 1; \
	fi; \
	if [ "$$conclusion" != success ]; then \
		echo "Error: Go CI is $$conclusion on $$head; fix it before releasing" && exit 1; \
	fi

tag: check-clean check-version check-main
	@git rev-parse -q --verify "refs/tags/$(VERSION)" >/dev/null && (echo "Error: tag $(VERSION) already exists" && exit 1) || true
	git tag -a "$(VERSION)" -m "Release $(VERSION)"
	repo=$$(git remote get-url origin 2>/dev/null || true); \
	if [ -n "$$repo" ]; then \
		echo "Created tag $(VERSION)"; \
	else \
		echo "Created tag $(VERSION) (no 'origin' remote found)"; \
	fi

release: check-clean check-version check-main check-pushed check-changelog check-ci
	$(MAKE) test
	$(MAKE) tag VERSION="$(VERSION)"
	git push origin "$(VERSION)"

install-hooks:
	@echo "Installing git pre-commit hook..."
	@ln -sf ../../scripts/pre-commit.sh .git/hooks/pre-commit
	@echo "Done. Hook installed at .git/hooks/pre-commit"
