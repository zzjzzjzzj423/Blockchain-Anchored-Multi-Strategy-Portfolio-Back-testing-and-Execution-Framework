SHELL := /bin/sh

PROJECT := project
LOCAL_GO := $(CURDIR)/.tools/go/bin/go
GO := $(if $(wildcard $(LOCAL_GO)),$(LOCAL_GO),go)
GOFMT := $(dir $(GO))gofmt
BIN_DIR := $(CURDIR)/bin
GO_ENV := GOCACHE=$(CURDIR)/.cache/go-build GOMODCACHE=$(CURDIR)/.cache/go-mod GOTELEMETRY=off GOTOOLCHAIN=local

.PHONY: help bootstrap doctor build test vet fmt fmt-check ci clean

help:
	@echo "MSBD5017 portfolio-proof"
	@echo "  make bootstrap  install the pinned local Go toolchain when needed"
	@echo "  make doctor     report tool availability and versions"
	@echo "  make build      build bin/project"
	@echo "  make test       run all Go tests"
	@echo "  make ci         run the complete M0 quality gate"

bootstrap:
	@./scripts/bootstrap-go.sh

doctor:
	@PATH="$(CURDIR)/.tools/go/bin:$$PATH" $(GO_ENV) $(GO) run ./cmd/project doctor

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

clean:
	@rm -rf "$(BIN_DIR)"
