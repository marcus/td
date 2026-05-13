.PHONY: help fmt test test-race install install-dev build build-dev build-release tag release check-clean check-version install-hooks clean

SHELL := /bin/sh

# Set VERSION on the command line, e.g.:
#   make release VERSION=v0.2.0
VERSION ?=

# A helpful dev version string (used by install-dev)
GIT_DESCRIBE := $(shell git describe --tags --always --dirty 2>/dev/null)

# Parallelism for test runs. Defaults to the number of CPUs available.
JOBS ?= $(shell sysctl -n hw.ncpu 2>/dev/null || nproc 2>/dev/null || echo 4)

# Release link flags strip debug info and symbol tables for smaller, faster-linking binaries.
RELEASE_LDFLAGS := -s -w -X main.Version=$(GIT_DESCRIBE)
DEV_LDFLAGS     := -X main.Version=$(GIT_DESCRIBE)

# -trimpath removes filesystem path prefixes from the binary, making builds reproducible
# and avoiding embedding personal paths.
RELEASE_BUILD_FLAGS := -trimpath -ldflags "$(RELEASE_LDFLAGS)"
DEV_BUILD_FLAGS     := -ldflags "$(DEV_LDFLAGS)"

help:
	@printf "%s\n" \
		"Targets:" \
		"  make fmt                       # gofmt -w ." \
		"  make install-hooks             # install git pre-commit hook" \
		"  make test                      # go test ./... (parallel)" \
		"  make test-race                 # go test -race ./..." \
		"  make build                     # release build (-trimpath, stripped)" \
		"  make build-dev                 # dev build (uses Go build cache)" \
		"  make install                   # install release-flavored binary" \
		"  make install-dev               # install dev-flavored binary" \
		"  make clean                     # remove build artefacts" \
		"  make tag VERSION=vX.Y.Z        # create annotated git tag (requires clean tree)" \
		"  make release VERSION=vX.Y.Z    # tag + push (triggers GoReleaser via GitHub Actions)"

fmt:
	gofmt -w .

# Test target relies on Go's built-in build/test cache for speed.
# -p sets the package compile parallelism, -parallel controls in-package t.Parallel().
test:
	go test -p $(JOBS) -parallel $(JOBS) ./...

test-race:
	go test -race -p $(JOBS) -parallel $(JOBS) ./...

# Release build: stripped, trimpath'd, smaller binary that links faster.
build: build-release

build-release:
	@V="$(GIT_DESCRIBE)"; V=$${V:-dev}; \
	echo "Building td $$V (release)"; \
	go build -trimpath -ldflags "-s -w -X main.Version=$$V" -o td .

# Dev build: keep symbol table for debuggers, otherwise rely on Go's incremental cache.
build-dev:
	@V="$(GIT_DESCRIBE)"; V=$${V:-dev}; \
	echo "Building td $$V (dev)"; \
	go build -ldflags "-X main.Version=$$V" -o td .

install:
	@V="$(GIT_DESCRIBE)"; V=$${V:-dev}; \
	echo "Installing td $$V (release)"; \
	go install -trimpath -ldflags "-s -w -X main.Version=$$V" .

install-dev:
	@V="$(GIT_DESCRIBE)"; V=$${V:-dev}; \
	echo "Installing td $$V (dev)"; \
	go install -ldflags "-X main.Version=$$V" .

clean:
	rm -f td
	go clean

check-clean:
	@git diff --quiet && git diff --cached --quiet || (echo "Error: working tree is not clean" && exit 1)

check-version:
	@test -n "$(VERSION)" || (echo "Error: VERSION is required (e.g. VERSION=v0.2.0)" && exit 1)
	@echo "$(VERSION)" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+' || (echo "Error: VERSION should look like vX.Y.Z" && exit 1)

tag: check-clean check-version
	@git rev-parse -q --verify "refs/tags/$(VERSION)" >/dev/null && (echo "Error: tag $(VERSION) already exists" && exit 1) || true
	git tag -a "$(VERSION)" -m "$(VERSION)"
	repo=$$(git remote get-url origin 2>/dev/null || true); \
	if [ -n "$$repo" ]; then \
		echo "Created tag $(VERSION)"; \
	else \
		echo "Created tag $(VERSION) (no 'origin' remote found)"; \
	fi

release: tag
	@git remote get-url origin >/dev/null 2>&1 || (echo "Error: no 'origin' remote configured" && exit 1)
	git push origin "$(VERSION)"

install-hooks:
	@echo "Installing git pre-commit hook..."
	@ln -sf ../../scripts/pre-commit.sh .git/hooks/pre-commit
	@echo "Done. Hook installed at .git/hooks/pre-commit"
