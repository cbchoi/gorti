.PHONY: verify fmt lint typecheck test determinism proto py-codegen py-test py-lint py-typecheck clean ci-all help

GO_PKGS := ./...
PY_DIR := pysdk

help:
	@echo "Targets:"
	@echo "  verify       fmt + lint + test + determinism (run before opening any PR)"
	@echo "  fmt          gofmt + goimports + black + ruff --fix"
	@echo "  lint         golangci-lint + ruff + buf lint"
	@echo "  typecheck    mypy --strict (Python)"
	@echo "  test         go test + pytest"
	@echo "  determinism  10x determinism harness on core packages"
	@echo "  proto        regenerate gRPC bindings via buf"
	@echo "  py-codegen   regenerate Python gRPC bindings into pysdk/rti1516e/_generated/"
	@echo "  py-test      pytest pysdk/ (M4)"
	@echo "  py-lint      ruff check pysdk/"
	@echo "  py-typecheck mypy --strict pysdk/"
	@echo "  clean        remove generated artifacts"
	@echo "  ci-all       full CI gate including coverage"

verify: fmt lint test determinism

fmt:
	gofmt -w -s .
	@if command -v goimports >/dev/null; then goimports -w .; fi
	@if command -v black >/dev/null && [ -d "$(PY_DIR)" ]; then black $(PY_DIR); fi
	@if command -v ruff   >/dev/null && [ -d "$(PY_DIR)" ]; then ruff check --fix $(PY_DIR); fi

lint:
	@if ! command -v golangci-lint >/dev/null; then echo "ERROR: golangci-lint not installed"; exit 1; fi
	golangci-lint run --config .golangci.yml ./...
	@if command -v ruff >/dev/null && [ -d "$(PY_DIR)" ]; then ruff check $(PY_DIR); fi
	@if command -v buf  >/dev/null; then buf lint; fi

typecheck:
	@if command -v mypy >/dev/null && [ -d "$(PY_DIR)" ]; then \
		cd $(PY_DIR) && mypy --strict .; \
	else \
		echo "mypy or $(PY_DIR) not present; skipping"; \
	fi

test:
	go test -race -timeout 60s $(GO_PKGS)
	@if command -v pytest >/dev/null && [ -d "$(PY_DIR)" ]; then \
		cd $(PY_DIR) && pytest --maxfail=1; \
	else \
		echo "pytest or $(PY_DIR) not present; skipping"; \
	fi

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

clean:
	rm -rf bin/ coverage.* rti/internal/genproto/ pysdk/rti1516e/_generated/

ci-all: lint
	go test -race -coverprofile=coverage.out -covermode=atomic $(GO_PKGS)
	go tool cover -func=coverage.out
