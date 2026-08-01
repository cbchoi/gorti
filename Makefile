.PHONY: verify fmt go-fmt-check lint go-lint typecheck test go-test go-test-coverage go-coverage-report determinism proto py-codegen py-test py-lint py-typecheck docs docs-serve docs-deps examples-check benchmark-check benchmark-dry-run clean ci-all help build build-dds test-dds rti-top

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
	@echo "  test-dds     go test under -tags=dds"
	@echo "  proto        regenerate gRPC bindings via buf"
	@echo "  py-codegen   regenerate Python gRPC bindings into pysdk/rti1516e/_generated/"
	@echo "  py-test      pytest pysdk/"
	@echo "  py-lint      ruff check pysdk/"
	@echo "  py-typecheck mypy --strict Python packages"
	@echo "  docs-deps    install MkDocs + Material + plugins (pip)"
	@echo "  docs         build the docs site into ./site/"
	@echo "  docs-serve   live-reload docs at http://127.0.0.1:8000/"
	@echo "  examples-check validate README + Windows/Linux entrypoints"
	@echo "  benchmark-check validate DEVStone-HLA workload, schema, runner, and comparator"
	@echo "  benchmark-dry-run inspect the tracked 5 warm-up + 30x2 experiment contract"
	@echo "  clean        remove generated artifacts"
	@echo "  ci-all       full CI gate including coverage"
	@echo ""
	@echo "DDS data-plane (experimental):"
	@echo "  - Default 'make build' produces a CGo-free + DDS-free rtid."
	@echo "  - 'make build-dds' produces rtid-dds, the DDS-capable variant."
	@echo "    The current adapter is experimental and may require Cyclone DDS:"
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
		cd $(PY_DIR) && mypy --strict rti1516e pyjevsim_bridge; \
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

# build creates the portable CGo-free deployment binaries.
build:
	mkdir -p bin
	go build -o bin/rtid ./rti/cmd/rtid
	go build -o bin/rti-top ./rti/cmd/rti-top

# rti-top builds just the TUI binary, for iterating on the TUI
# without recompiling rtid.
rti-top:
	mkdir -p bin
	go build -o bin/rti-top ./rti/cmd/rti-top

# build-dds compiles the experimental DDS-capable variant.
build-dds:
	mkdir -p bin
	go build -tags=dds -o bin/rtid-dds ./rti/cmd/rtid

# test-dds exercises the DDS-tagged unit tests.
test-dds:
	go test -race -tags=dds ./rti/internal/transport/dds/...

determinism:
	go test -tags=determinism -count=10 -race -run=Determinism $(GO_PKGS)

proto:
	@if ! command -v buf >/dev/null; then echo "ERROR: buf not installed"; exit 1; fi
	buf generate

# Python gRPC codegen emits into pysdk/rti1516e/_generated/ (gitignored).
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
	cd $(PY_DIR) && mypy --strict rti1516e pyjevsim_bridge

docs-deps:
	@if ! command -v pip >/dev/null; then echo "ERROR: pip not installed"; exit 1; fi
	pip install -r docs/requirements.txt

docs:
	@if ! command -v mkdocs >/dev/null; then echo "ERROR: mkdocs not installed (run: make docs-deps)"; exit 1; fi
	mkdocs build --strict

docs-serve:
	@if ! command -v mkdocs >/dev/null; then echo "ERROR: mkdocs not installed (run: make docs-deps)"; exit 1; fi
	mkdocs serve

examples-check:
	bash scripts/check-example-entrypoints.sh

benchmark-check:
	python3 -m unittest discover -s benchmark/common/tests -v
	python3 -m unittest discover -s benchmark/devstone/workload/tests -v
	python3 -m unittest discover -s benchmark/portico-gorti/tests -v
	python3 -m pytest verification/portico/tests -q

benchmark-dry-run:
	python3 benchmark/portico-gorti/orchestrator.py --dry-run

clean:
	rm -rf bin/ coverage.* pysdk/rti1516e/_generated/ cppsdk/_generated/ site/

ci-all: lint
	go test -race -coverprofile=coverage.out -covermode=atomic $(GO_PKGS)
	go tool cover -func=coverage.out
