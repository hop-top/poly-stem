.PHONY: 12fcc-record 12fcc-grade 12fcc-badge help setup test test-go test-go-root test-go-sqlite test-ts test-py test-rs test-php \
	test-parity test-parity-go test-parity-ts test-parity-py test-parity-rs test-parity-php \
	build build-go build-ts build-py build-rs build-php \
	package package-go package-ts package-py package-rs package-php \
	lint lint-go lint-whitespace fmt fmt-go test-all lint-all fmt-all check clean

PYTHON ?= python3
PNPM ?= pnpm
COMPOSER ?= composer
UV ?= uv
CARGO ?= cargo
GOCACHE ?= /tmp/stem-go-build

help: ## Show available targets
	@awk 'BEGIN {FS = ":.*## "; printf "Targets:\n"} /^[a-zA-Z0-9_-]+:.*## / {printf "  %-18s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

setup: ## Install local dependencies (Go only for v0.1.0)
	cd go && env -u GOROOT GOCACHE=$(GOCACHE) go mod download

check: lint test build ## Run lint, all tests, and all builds

# --- Cross-language fan-out ---------------------------------------------------

test-all: test ## Alias: run every language's tests + parity
lint-all: lint ## Alias: run every language's linters
fmt-all:  fmt  ## Alias: run every language's formatters

test: test-go test-ts test-py test-rs test-php test-parity ## Run all language tests and parity checks

lint: lint-go lint-whitespace ## Run available linters (Go only for v0.1.0)

fmt: fmt-go ## Run available formatters (Go only for v0.1.0)

# --- Go (real targets) --------------------------------------------------------

test-go: test-go-root test-go-sqlite ## Run Go tests across stem + sqlite submodules with -race

test-go-root: ## Run Go tests for the root stem module (covers kitadapter)
	cd go && env -u GOROOT GOCACHE=$(GOCACHE) go test -race ./...

test-go-sqlite: ## Run Go tests for the stem/sqlite submodule
	cd go/sqlite && env -u GOROOT GOCACHE=$(GOCACHE) go test -race ./...

build-go: ## Verify Go packages compile
	cd go && env -u GOROOT GOCACHE=$(GOCACHE) go test ./... -run '^$$'

package-go: ## Validate Go module package list
	cd go && env -u GOROOT GOCACHE=$(GOCACHE) go list ./...

lint-go: ## Run go vet
	cd go && env -u GOROOT GOCACHE=$(GOCACHE) go vet ./...

fmt-go: ## Run gofmt -s -w
	cd go && gofmt -s -w .

# --- Other-language SDK tests -------------------------------------------------

test-ts: ## Run TypeScript SDK tests (vitest)
	cd ts && $(PNPM) install --frozen-lockfile && $(PNPM) test

test-py: ## Run Python SDK tests (pytest via uv)
	cd py && $(UV) sync && $(UV) run pytest

test-rs: ## Run Rust SDK tests (workspace)
	cd rs && $(CARGO) test --workspace

test-php: ## Run PHP SDK tests (phpunit)
	cd php && $(COMPOSER) install --no-interaction --prefer-dist && ./vendor/bin/phpunit

test-parity: ## Cross-SDK envelope parity — all 5 SDKs round-trip to byte-identical canonical JSON
	$(MAKE) -s _parity-build
	$(PYTHON) tools/parity/runner.py

test-parity-go: ## Run parity harness restricted to the Go SDK
	$(PYTHON) tools/parity/runner.py go

test-parity-ts: ## Run parity harness restricted to the TypeScript SDK (builds ts/dist first)
	cd ts && $(PNPM) install --ignore-scripts && $(PNPM) build
	$(PYTHON) tools/parity/runner.py ts

test-parity-py: ## Run parity harness restricted to the Python SDK
	$(PYTHON) tools/parity/runner.py py

test-parity-rs: ## Run parity harness restricted to the Rust SDK
	$(PYTHON) tools/parity/runner.py rs

test-parity-php: ## Run parity harness restricted to the PHP SDK (installs composer deps if missing)
	@if [ ! -f php/vendor/autoload.php ]; then cd php && $(COMPOSER) install --no-interaction --no-progress; fi
	$(PYTHON) tools/parity/runner.py php

# Internal: ensure cross-SDK prerequisites are present before running
# the full parity harness. Mirrors what each per-SDK target does.
_parity-build:
	cd ts && $(PNPM) install --ignore-scripts && $(PNPM) build
	@if [ ! -f php/vendor/autoload.php ]; then cd php && $(COMPOSER) install --no-interaction --no-progress; fi

build: build-go build-ts build-py build-rs build-php ## Build all language packages

build-ts: ## Compile TypeScript SDK to ts/dist via tsc
	cd ts && $(PNPM) install --frozen-lockfile && $(PNPM) run build

build-py: ## Build Python wheel + sdist into py/dist via uv
	cd py && $(UV) sync && $(UV) build

build-rs: ## Build Rust workspace release artifacts under rs/target/release
	cd rs && $(CARGO) build --workspace --release

build-php: ## Install PHP deps and validate composer manifest
	cd php && $(COMPOSER) install --no-interaction --prefer-dist && $(COMPOSER) validate --strict

package: package-go package-ts package-py package-rs package-php ## Package all language artifacts

package-ts: ## Produce TypeScript .tgz tarball via pnpm pack
	cd ts && $(PNPM) install --frozen-lockfile && $(PNPM) pack

package-py: ## Produce Python wheel + sdist into py/dist (alias for build-py)
	cd py && $(UV) sync && $(UV) build

package-rs: ## Produce hop-top-stem .crate under rs/target/package (examples set publish=false)
	cd rs && $(CARGO) package -p hop-top-stem --allow-dirty --no-verify

package-php: ## Validate composer manifest (PHP packaging = source tree)
	cd php && $(COMPOSER) validate --strict

# --- Conformance grading (12fcc service tier) --------------------------------

12fcc-record: ## Re-record conformance cassettes from real binary runs (KIT_BIN=<kit with conformance>)
	@./scripts/12fcc-record.sh

12fcc-grade: ## Grade recorded cassettes (KIT_BIN=<kit with conformance>; STRICT=1 to fail on fail verdicts)
	@./scripts/12fcc-grade.sh

12fcc-badge: ## Regenerate .12fc.json from verify leaves + graded verdicts (KIT_BIN=<kit with conformance>)
	@./scripts/12fcc-badge.sh

# --- Shared ------------------------------------------------------------------

lint-whitespace: ## Check whitespace and conflict-marker issues in the git diff
	git diff --check

clean: ## Remove generated local build artifacts
	rm -rf go/bin go/coverage.out
