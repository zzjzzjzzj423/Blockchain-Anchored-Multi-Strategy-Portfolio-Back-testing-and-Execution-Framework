SHELL := /bin/sh

PROJECT := project
LOCAL_GO := $(CURDIR)/.tools/go/bin/go
GO := $(if $(wildcard $(LOCAL_GO)),$(LOCAL_GO),go)
GOFMT := $(dir $(GO))gofmt
BIN_DIR := $(CURDIR)/bin
GO_ENV := GOCACHE=$(CURDIR)/.cache/go-build GOMODCACHE=$(CURDIR)/.cache/go-mod GOTELEMETRY=off GOTOOLCHAIN=local
CODEX_APP_BIN := /Applications/ChatGPT.app/Contents/Resources/codex-cli/CodexCLI.app/Contents/MacOS/codex
CODEX_BIN ?= $(if $(wildcard $(CODEX_APP_BIN)),$(CODEX_APP_BIN),codex)
FOUNDRY_BIN := $(CURDIR)/.tools/foundry-v1.9.0/bin
FOUNDRY_ENV := FOUNDRY_DIR=$(CURDIR)/.cache/foundry SVM_HOME=$(CURDIR)/.cache/svm PATH=$(FOUNDRY_BIN):$$PATH

.PHONY: help bootstrap foundry-bootstrap doctor build test vet fmt fmt-check ci public-evidence data data-validate execution strategies wasm m4 agent-search agent-workflow dashboard m5-freeze m5 m6 contract-test contract-coverage m7 replay mutation m8 report deliverables demo m9 clean

help:
	@echo "MSBD5017 portfolio-proof"
	@echo "  make bootstrap  install the pinned local Go toolchain when needed"
	@echo "  make doctor     report tool availability and versions"
	@echo "  make build      build bin/project"
	@echo "  make test       run all Go tests"
	@echo "  make ci         run the complete M0 quality gate"
	@echo "  make public-evidence  check reports and files available in a fresh clone"
	@echo "  make data       download, verify, and normalize the M1 snapshot"
	@echo "  make data-validate  rebuild M1 outputs from the local raw snapshot"
	@echo "  make execution  run M2 accounting acceptance scenarios"
	@echo "  make strategies run M3 strategies, no-look-ahead, and parallel checks"
	@echo "  make wasm       build three portable WASI strategy modules"
	@echo "  make m4         run 20-candidate walk-forward selection and freeze parameters"
	@echo "  make agent-search  run a development-only Codex search"
	@echo "  make agent-workflow  run the full agent research, execution, replay and report pipeline"
	@echo "  make dashboard  build a read-only interactive evidence viewer"
	@echo "  make m5-freeze  freeze the development-only regime threshold"
	@echo "  make m5         run the one-time formal 2025 final holdout"
	@echo "  make m6         build and verify the canonical evidence bundle 10 times"
	@echo "  make contract-test run the pinned Foundry contract suite and gas report"
	@echo "  make contract-coverage report Solidity line, branch, and function coverage"
	@echo "  make m7         deploy to deterministic Anvil and verify the frozen M6 proof"
	@echo "  make replay     run 10 clean M8 replays and all QA gates"
	@echo "  make mutation   rerun the seed-5017 100-byte mutation suite"
	@echo "  make m8         run the complete M8 replay and QA acceptance gate"
	@echo "  make report     regenerate traceable final SVG figures"
	@echo "  make deliverables regenerate the D1-D3 SHA-256 manifest"
	@echo "  make demo       run the read-only end-to-end demonstration rehearsal"
	@echo "  make m9         regenerate final deliverables and run the demonstration"

bootstrap:
	@./scripts/bootstrap-go.sh

foundry-bootstrap:
	@./scripts/bootstrap-foundry.sh

doctor:
	@PATH="$(CURDIR)/.tools/go/bin:$(FOUNDRY_BIN):$$PATH" $(GO_ENV) $(GO) run ./cmd/project doctor

build:
	@mkdir -p "$(BIN_DIR)"
	@$(GO_ENV) CGO_ENABLED=0 $(GO) build -trimpath -o "$(BIN_DIR)/$(PROJECT)" ./cmd/project

test:
	@$(GO_ENV) $(GO) test -count=1 ./...

vet:
	@$(GO_ENV) $(GO) vet ./...

fmt:
	@$(GOFMT) -w $$(find . -type f -name '*.go' -not -path './.tools/*')

fmt-check:
	@unformatted="$$( $(GOFMT) -l $$(find . -type f -name '*.go' -not -path './.tools/*') )"; \
	if [ -n "$$unformatted" ]; then \
		echo "Go files require formatting:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi

ci: fmt-check vet test build

public-evidence:
	@python3 ./scripts/verify-public-evidence.py

data:
	@PATH="$(CURDIR)/.tools/go/bin:$$PATH" $(GO_ENV) $(GO) run ./cmd/project data build

data-validate:
	@PATH="$(CURDIR)/.tools/go/bin:$$PATH" $(GO_ENV) $(GO) run ./cmd/project data validate

execution:
	@PATH="$(CURDIR)/.tools/go/bin:$$PATH" $(GO_ENV) $(GO) run ./cmd/project execution validate

strategies: wasm
	@PATH="$(CURDIR)/.tools/go/bin:$$PATH" $(GO_ENV) $(GO) run ./cmd/project strategies validate

wasm:
	@mkdir -p "$(BIN_DIR)/strategies"
	@for strategy in buyhold trend meanreversion; do \
		$(GO_ENV) GOOS=wasip1 GOARCH=wasm $(GO) build -trimpath -buildvcs=false -ldflags=-buildid= -o "$(BIN_DIR)/strategies/$$strategy.wasm" "./cmd/wasm/$$strategy"; \
	done

m4:
	@PATH="$(CURDIR)/.tools/go/bin:$$PATH" $(GO_ENV) $(GO) run ./cmd/project experiment select --budget 20

agent-search:
	@PATH="$(CURDIR)/.tools/go/bin:$$PATH" $(GO_ENV) $(GO) run ./cmd/project experiment agent-search --budget 3 --codex-bin "$(CODEX_BIN)"

agent-workflow:
	@python3 scripts/agent_workflow.py --budget 20 --codex-bin "$(CODEX_BIN)"

dashboard:
	@python3 ./scripts/build-dashboard.py

m5-freeze:
	@PATH="$(CURDIR)/.tools/go/bin:$$PATH" $(GO_ENV) $(GO) run ./cmd/project experiment freeze-final

m5:
	@PATH="$(CURDIR)/.tools/go/bin:$$PATH" $(GO_ENV) $(GO) run ./cmd/project experiment final

m6: wasm
	@PATH="$(CURDIR)/.tools/go/bin:$$PATH" $(GO_ENV) $(GO) run ./cmd/project evidence build --rebuilds 10

contract-test: foundry-bootstrap
	@$(FOUNDRY_ENV) forge fmt --root contracts --check
	@$(FOUNDRY_ENV) forge test --root contracts --gas-report

contract-coverage: foundry-bootstrap
	@$(FOUNDRY_ENV) forge coverage --root contracts --report summary

m7: contract-test
	@PATH="$(CURDIR)/.tools/go/bin:$$PATH" $(GO_ENV) $(GO) run ./cmd/project chain deploy --root .

replay:
	@PATH="$(CURDIR)/.tools/go/bin:$$PATH" $(GO_ENV) $(GO) run ./cmd/project replay --root . --replays 10 --mutations 100 --seed 5017 artifacts/098376de7f855b9e

mutation:
	@PATH="$(CURDIR)/.tools/go/bin:$$PATH" $(GO_ENV) $(GO) run ./cmd/project mutate-test --count 100 --seed 5017 artifacts/098376de7f855b9e

m8: ci contract-test replay

report:
	@python3 ./scripts/generate-report-charts.py

deliverables:
	@python3 ./scripts/build-deliverables.py

demo:
	@./scripts/demo.sh

m9: report deliverables demo

clean:
	@rm -rf "$(BIN_DIR)"
