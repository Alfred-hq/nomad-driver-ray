SHELL = bash
default: help

GIT_COMMIT := $(shell git rev-parse --short HEAD)
GIT_DIRTY := $(if $(shell git status --porcelain),+CHANGES)

GO_LDFLAGS := "-X github.com/ryadavDeqode/nomad-driver-ray/version.GitCommit=$(GIT_COMMIT)$(GIT_DIRTY)"

HELP_FORMAT="    \033[36m%-25s\033[0m %s\n"
.PHONY: help
help: ## Display this usage information
	@echo "Valid targets:"
	@grep -E '^[^ ]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		sort | \
		awk 'BEGIN {FS = ":.*?## "}; \
			{printf $(HELP_FORMAT), $$1, $$2}'
	@echo ""

pkg/%/nomad-driver-ray: GO_OUT ?= $@
pkg/windows_%/nomad-driver-ray: GO_OUT = $@.exe
pkg/%/nomad-driver-ray: ## Build nomad-driver-ray plugin for GOOS_GOARCH, e.g. pkg/linux_amd64/nomad
	@echo "==> Building $@ with tags $(GO_TAGS)..."
	@CGO_ENABLED=0 \
		GOOS=$(firstword $(subst _, ,$*)) \
		GOARCH=$(lastword $(subst _, ,$*)) \
		go build -trimpath -ldflags $(GO_LDFLAGS) -tags "$(GO_TAGS)" -o $(GO_OUT)

.PRECIOUS: pkg/%/nomad-driver-ray
pkg/%.zip: pkg/%/nomad-driver-ray ## Build and zip nomad-driver-ray plugin for GOOS_GOARCH, e.g. pkg/linux_amd64.zip
	@echo "==> Packaging for $@..."
	zip -j $@ $(dir $<)*

.PHONY: dev
dev: ## Build for the current development version
	@echo "==> Building nomad-driver-ray..."
	@CGO_ENABLED=0 \
		go build \
			-ldflags $(GO_LDFLAGS) \
			-o ./bin/nomad-driver-ray
	@echo "==> Done"

.PHONY: test
test: ## Run tests
	go test -v -race ./...

.PHONY: version
version:
ifneq (,$(wildcard version/version_ent.go))
	@$(CURDIR)/scripts/version.sh version/version.go version/version_ent.go
else
	@$(CURDIR)/scripts/version.sh version/version.go version/version.go
endif
