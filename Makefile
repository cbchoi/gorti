.PHONY: verify fmt go-fmt-check lint go-lint typecheck test go-test go-test-coverage go-coverage-report determinism proto py-codegen py-test py-lint py-typecheck docs docs-serve docs-deps clean ci-all help build build-dds test-dds rti-top

GO_PKGS := \
	./examples/... \
	./rti/... \
	./tests/... \
	./verification/gorti-go/... \
	./verification/gorti-go-fair/...
GO_ROOTS := $(GO_PKGS:/...=)
PY_DIR := pysdk

help:
	@echo "Targets:"
	@echo "  verify       fmt + lint + test + determinism (run before opening any PR)"
	@echo "  fmt          gofmt + goimports + black + ruff --fix"
	@echo "  lint         golangci-lint + ruff + buf lint"
	@echo "  typecheck    mypy --strict (Python)"
	@echo "  test         go test + pytest"
	@echo "  determinism  10x determinism harness on core packages"
	@echo "  build        compile bin/rtid + bin/rti-top (CGo-free, DDS-free)"
	@echo "  rti-top      compile bin/rti-top only (live-federation TUI)"
	@echo "  build-dds    compile bin/rtid-dds (DDS-capable; requires libcyclonedds-dev)"
	@echo "  test-dds     go test under -tags=dds (M19 Phase 1a stub-contract tests)"
	@echo "  proto        regenerate gRPC bindings via buf"
	@echo "  py-codegen   regenerate Python gRPC bindings into pysdk/rti1516e/_generated/"
	@echo "  py-test      pytest pysdk/ (M4)"
	@echo "  py-lint      ruff check pysdk/"
	@echo "  py-typecheck mypy --strict pysdk/"
	@echo "  docs-deps    install MkDocs + Material + plugins (pip)"
	@echo "  docs         build the docs site into ./site/"
	@echo "  docs-serve   live-reload docs at http://127.0.0.1:8000/"
	@echo "  clean        remove generated artifacts"
	@echo "  ci-all       full CI gate including coverage"
	@echo ""
	@echo "DDS data-plane (M19 — docs/m19-dds-adapter.md):"
	@echo "  - Default 'make build' produces a CGo-free + DDS-free rtid."
	@echo "  - 'make build-dds' produces rtid-dds, the DDS-capable variant."
	@echo "    Phase 1a: stubs return errors.ErrUnsupported (no Cyclone DDS"
	@echo "    runtime needed yet — the package compiles without the C library)."
	@echo "    Phase 1b: requires Cyclone DDS C library + headers:"
	@echo "      Linux:  apt-get install libcyclonedds-dev"
	@echo "      macOS:  brew install cyclonedds"

verify: fmt lint test determinism

fmt:
	git ls-files '*.go' | xargs gofmt -w -s
	@if command -v goimports >/dev/null; then git ls-files '*.go' | xargs goimports -w; fi
	@if command -v black >/dev/null && [ -d "$(PY_DIR)" ]; then black $(PY_DIR); fi
	@if command -v ruff   >/dev/null && [ -d "$(PY_DIR)" ]; then ruff check --fix $(PY_DIR); fi

go-fmt-check:
	@OUT="$$(find $(GO_ROOTS) -type f -name '*.go' -exec gofmt -l -s {} +)" || exit $$?; \
	if [ -n "$$OUT" ]; then \
		echo "::error ::files need gofmt:"; \
		echo "$$OUT"; \
		exit 1; \
	fi

lint: go-lint
	@if command -v ruff >/dev/null && [ -d "$(PY_DIR)" ]; then ruff check $(PY_DIR); fi
	@if command -v buf  >/dev/null; then buf lint; fi

go-lint:
	@if ! command -v golangci-lint >/dev/null; then echo "ERROR: golangci-lint not installed"; exit 1; fi
	golangci-lint run --config .golangci.yml $(GO_PKGS)

typecheck:
	@if command -v mypy >/dev/null && [ -d "$(PY_DIR)" ]; then \
		cd $(PY_DIR) && mypy --strict .; \
	else \
		echo "mypy or $(PY_DIR) not present; skipping"; \
	fi

test: go-test
	@if command -v pytest >/dev/null && [ -d "$(PY_DIR)" ]; then \
		cd $(PY_DIR) && pytest --maxfail=1; \
	else \
		echo "pytest or $(PY_DIR) not present; skipping"; \
	fi

go-test:
	go test -race -timeout 60s $(GO_PKGS)

go-test-coverage:
	go test -race -timeout 60s -coverprofile=coverage.out -covermode=atomic $(GO_PKGS)

go-coverage-report:
	go tool cover -func=coverage.out

# build is the default deployment artifact set — CGo-free, DDS-free.
# bin/rtid is identical to every cut-2 release; the M19 work does not
# change its dependency surface or output bytes. bin/rti-top is the
# live-federation TUI (read-only AdminService client; see
# docs/rtid-tui.md and rti/cmd/rti-top/README.md).
build:
	mkdir -p bin
	go build -o bin/rtid ./rti/cmd/rtid
	go build -o bin/rti-top ./rti/cmd/rti-top

# rti-top builds just the TUI binary, for iterating on the TUI
# without recompiling rtid.
rti-top:
	mkdir -p bin
	go build -o bin/rti-top ./rti/cmd/rti-top

# build-dds compiles the DDS-capable rtid variant. M19 Phase 1a — see
# docs/m19-dds-adapter.md §3.5 PINNED. The dds-tagged build links the
# rti/internal/transport/dds/ package; in Phase 1a every primitive
# returns errors.ErrUnsupported so the build succeeds even without
# Cyclone DDS installed. Phase 1b adds the cgo_dds.go interop, which
# DOES require libcyclonedds-dev (apt) or cyclonedds (brew).
build-dds:
	mkdir -p bin
	go build -tags=dds -o bin/rtid-dds ./rti/cmd/rtid

# test-dds exercises the dds-tagged unit tests — Phase 1a stub-contract
# tests under rti/internal/transport/dds/. Phase 1b adds end-to-end
# tests under -tags=dds_e2e that require a running Cyclone DDS install.
test-dds:
	go test -race -tags=dds ./rti/internal/transport/dds/...

determinism:
	go test -tags=determinism -count=10 -race -run=Determinism $(GO_PKGS)

proto:
	@if ! command -v buf >/dev/null; then echo "ERROR: buf not installed"; exit 1; fi
	buf generate

# Python gRPC codegen — emits into pysdk/rti1516e/_generated/ (gitignored).
# TASK-062 wires this; until then the target is informational. Re-runnable.
py-codegen:
	@if ! command -v python3 >/dev/null; then echo "ERROR: python3 not installed"; exit 1; fi
	@mkdir -p $(PY_DIR)/rti1516e/_generated/rti/v1
	python3 -m grpc_tools.protoc \
		-I proto \
		--python_out=$(PY_DIR)/rti1516e/_generated \
		--grpc_python_out=$(PY_DIR)/rti1516e/_generated \
		--pyi_out=$(PY_DIR)/rti1516e/_generated \
		proto/rti/v1/*.proto
	@touch $(PY_DIR)/rti1516e/_generated/__init__.py

py-test:
	@if ! command -v pytest >/dev/null; then echo "ERROR: pytest not installed (pip install -e 'pysdk[dev]')"; exit 1; fi
	cd $(PY_DIR) && pytest --maxfail=5 -q

py-lint:
	@if ! command -v ruff >/dev/null; then echo "ERROR: ruff not installed"; exit 1; fi
	ruff check $(PY_DIR)

py-typecheck:
	@if ! command -v mypy >/dev/null; then echo "ERROR: mypy not installed"; exit 1; fi
	cd $(PY_DIR) && mypy --strict .

docs-deps:
	@if ! command -v pip >/dev/null; then echo "ERROR: pip not installed"; exit 1; fi
	pip install -r docs/requirements.txt

docs:
	@if ! command -v mkdocs >/dev/null; then echo "ERROR: mkdocs not installed (run: make docs-deps)"; exit 1; fi
	mkdocs build --strict

docs-serve:
	@if ! command -v mkdocs >/dev/null; then echo "ERROR: mkdocs not installed (run: make docs-deps)"; exit 1; fi
	mkdocs serve

clean:
	rm -rf bin/ coverage.* rti/internal/genproto/ pysdk/rti1516e/_generated/ site/

ci-all: lint
	go test -race -coverprofile=coverage.out -covermode=atomic $(GO_PKGS)
	go tool cover -func=coverage.out
