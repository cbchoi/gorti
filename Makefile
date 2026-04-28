.PHONY: verify fmt lint typecheck test determinism proto clean ci-all help

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

clean:
	rm -rf bin/ coverage.* rti/internal/genproto/ pysdk/rti1516e/_generated/

ci-all: lint
	go test -race -coverprofile=coverage.out -covermode=atomic $(GO_PKGS)
	go tool cover -func=coverage.out
