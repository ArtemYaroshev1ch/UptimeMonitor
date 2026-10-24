# Developer tasks for Uptime Monitor. Run `make help` for the list.
#
# Portability: recipes are POSIX sh. On Linux/macOS that is the default. On
# native Windows, GNU Make uses sh.exe when it finds it in PATH (Git for
# Windows puts it in "C:\Program Files\Git\bin"); otherwise it silently falls
# back to cmd.exe, so we check for that up front.
ifeq ($(shell echo $$0),$$0)
$(error This Makefile needs a POSIX shell, but make is using cmd.exe. Add "C:\Program Files\Git\bin" to PATH or run make from Git Bash)
endif

ifeq ($(OS),Windows_NT)
# Make starts simple recipe lines (no shell metacharacters) directly, without
# sh, so coreutils (echo, date, rm, mkdir, awk) must be in PATH. Git for
# Windows keeps them in usr\bin, which is not in PATH outside Git Bash. The
# redirect forces this command through sh, which knows where /usr/bin is.
POSIX_BIN := $(shell cygpath -w /usr/bin 2>/dev/null)
export PATH := $(POSIX_BIN);$(PATH)
endif

.DEFAULT_GOAL := help

GO ?= go

# Executable suffix: ".exe" on Windows, empty elsewhere.
GOEXE := $(shell $(GO) env GOEXE)

BIN_DIR   := bin
TOOLS_DIR := $(BIN_DIR)/tools

# Binaries under cmd/. worker and migrate are added in later phases.
CMDS := api

# Build metadata injected into package main of every binary via -ldflags.
# Release tags are vX.Y.Z; the version itself is SemVer without the "v".
# Override from the command line, e.g. `make build VERSION=1.2.3`.
VERSION  := $(patsubst v%,%,$(or $(shell git describe --tags --match "v[0-9]*" --dirty 2>/dev/null),0.0.0-dev))
COMMIT   := $(or $(shell git rev-parse --short=12 HEAD 2>/dev/null),unknown)
BUILT_AT := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS  := -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.builtAt=$(BUILT_AT)

# The race detector needs cgo, i.e. a C compiler. Native Windows usually has
# none (CGO_ENABLED=0), so -race is used only where it works; CI always has it.
ifeq ($(shell $(GO) env CGO_ENABLED),1)
RACE := -race
endif

# golangci-lint is pinned and built with the local Go toolchain, so the linter
# always supports the Go version from go.mod. The version is part of the file
# name: bumping it triggers a reinstall. Keep in sync with ci.yml.
GOLANGCI_LINT_VERSION := v2.15.0
GOLANGCI_LINT := $(TOOLS_DIR)/golangci-lint-$(GOLANGCI_LINT_VERSION)$(GOEXE)

# Exports variables from .env (if present) into the recipe's shell. The file
# is sourced by sh, so it must contain plain KEY=value lines.
LOAD_ENV := if [ -f .env ]; then set -a; . ./.env; set +a; fi;

##@ Development

.PHONY: help
help: ## Show this help
	@awk 'BEGIN { FS = ":.*## " } \
		/^##@/ { printf "\n%s\n", substr($$0, 5) } \
		/^[a-zA-Z0-9_-]+:.*## / { printf "  %-14s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

# The binary is built and started instead of `go run`: its path stays the same
# between runs (Windows Firewall asks once per executable path) and signals
# such as Ctrl+C reach the server process directly.
.PHONY: run-api
run-api: build ## Build and run the API (loads .env if present)
	@$(LOAD_ENV) ./$(BIN_DIR)/api$(GOEXE)

.PHONY: build
build: ## Build binaries into bin/
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/ $(addprefix ./cmd/,$(CMDS))

.PHONY: clean
clean: ## Remove built binaries and coverage reports
	rm -f $(addprefix $(BIN_DIR)/,$(addsuffix $(GOEXE),$(CMDS))) coverage.out coverage.html

##@ Quality

.PHONY: test
test: ## Run unit tests (-race when cgo is available)
ifndef RACE
	@echo "note: cgo is disabled (no C compiler), running tests without -race"
endif
	$(GO) test $(RACE) ./...

.PHONY: cover
cover: ## Run unit tests with coverage, write coverage.html
	$(GO) test $(RACE) -covermode=atomic -coverprofile=coverage.out ./...
	$(GO) tool cover -func=coverage.out | tail -n 1
	$(GO) tool cover -html=coverage.out -o coverage.html

.PHONY: lint
lint: $(GOLANGCI_LINT) ## Run golangci-lint
	$(GOLANGCI_LINT) run ./...

.PHONY: fmt
fmt: $(GOLANGCI_LINT) ## Format code (gofmt + goimports)
	$(GOLANGCI_LINT) fmt ./...

.PHONY: tidy
tidy: ## Tidy and verify go.mod/go.sum
	$(GO) mod tidy
	$(GO) mod verify

.PHONY: tools
tools: $(GOLANGCI_LINT) ## Install pinned dev tools into bin/tools

$(GOLANGCI_LINT):
	@echo "Installing golangci-lint $(GOLANGCI_LINT_VERSION) (first run takes a minute)"
	@mkdir -p $(TOOLS_DIR)/tmp
	GOBIN="$(abspath $(TOOLS_DIR)/tmp)" $(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	@mv "$(TOOLS_DIR)/tmp/golangci-lint$(GOEXE)" "$@"
	@rm -rf "$(TOOLS_DIR)/tmp"
