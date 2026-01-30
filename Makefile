.PHONY: help fmt test install install-dev tag release check-clean check-version

SHELL := /bin/sh

# Set VERSION on the command line, e.g.:
#   make release VERSION=v0.2.0
VERSION ?=

# A helpful dev version string (used by install-dev)
GIT_DESCRIBE := $(shell git describe --tags --always --dirty 2>/dev/null)

help:
	@printf "%s\n" \
		"Targets:" \
		"  make fmt                       # gofmt -w ." \
		"  make test                      # go test ./..." \
		"  make install                   # go install ." \
		"  make install-dev               # go install with -ldflags version from git" \
		"  make tag VERSION=vX.Y.Z        # create annotated git tag (requires clean tree)" \
		"  make release VERSION=vX.Y.Z    # tag + push (triggers GoReleaser via GitHub Actions)"

fmt:
	gofmt -w .

test:
	go test ./...

install:
	go install .

install-dev:
	@if [ -n "$(GIT_DESCRIBE)" ]; then \
		V="$(GIT_DESCRIBE)"; \
	else \
		V="dev"; \
	fi; \
	echo "Installing td with Version=$$V"; \
	go install -ldflags "-X main.Version=$$V" .

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