# -----------------------------------------------------------------------------
# global

.DEFAULT_GOAL := help

# -----------------------------------------------------------------------------
# go

GO_BUILDTAGS = osusergo,netgo,static
GO_LDFLAGS = -s -w
ifeq ($(shell go env GOOS),linux)
GO_LDFLAGS += "-extldflags=-static"
endif
GO_FLAGS ?= -tags='${GO_BUILDTAGS}' -ldflags='${GO_LDFLAGS}'

GOEXPERIMENT := simd,runtimesecret,mapsplitgroup
export GOEXPERIMENT

TOOLS_DIR = ${CURDIR}/hack/tools
TOOLS_BIN = ${TOOLS_DIR}/bin
TOOLS = $(shell go -C ${TOOLS_DIR} list tool)

GO_TEST ?= ${TOOLS_BIN}/gotestsum --
GO_TEST_PACKAGES = $(shell go list -f='{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}' ./... | grep -v tests)
GO_TEST_FLAGS ?= -race -count=1
GO_TEST_FUNC ?= .
GO_COVERAGE_JUNITFILE_DIR ?= _test_results
GO_BENCH_FLAGS ?= -benchmem
GO_BENCH_FUNC ?= .
GO_LINT_FLAGS ?=

# -----------------------------------------------------------------------------
# defines

define install_tool
for t in ${TOOLS}; do \
	if [ -n '$1' ] && [ $$(basename $${t%%/v[0-9]*}) = '$1' ]; then \
		echo "Install $$t ..." >&2; \
		GOBIN=${TOOLS_BIN} CGO_ENABLED=0 go install -C ${TOOLS_DIR} -v -mod=readonly ${GO_FLAGS} "$${t}"; \
	fi \
done
endef

# -----------------------------------------------------------------------------
# target

##@ test, bench, coverage

.PHONY: test
test: hack/tools/bin/gotestsum
test:  ## Runs package test including race condition.
	${GO_TEST} ${GO_TEST_FLAGS} -run=${GO_TEST_FUNC} $(strip ${GO_FLAGS}) ${GO_TEST_PACKAGES}

.PHONY: test/integration
test/integration: hack/tools/bin/gotestsum
test/integration:  ## Runs integration test.
	MCP_LSP_INTEGRATION=1 ${GO_TEST} ${GO_TEST_FLAGS} -run=${GO_TEST_FUNC} $(strip ${GO_FLAGS}) ./tests/...

.PHONY: coverage
coverage: GO_TEST=${TOOLS_BIN}/gotestsum --junitfile=${GO_COVERAGE_JUNITFILE_DIR}/tests.$(@F).xml --
coverage: hack/tools/bin/gotestsum
coverage:  ## Takes packages test coverage.
	@mkdir -p ${GO_COVERAGE_JUNITFILE_DIR}
	MCP_LSP_INTEGRATION=1 ${GO_TEST} ${GO_TEST_FLAGS} -cover -covermode=atomic -coverpkg=./... -coverprofile=coverage.out $(strip ${GO_FLAGS}) ./...


##@ fmt, lint

.PHONY: fmt
fmt: hack/tools/bin/goimports-rereviser hack/tools/bin/gofumpt
fmt:  ## Run goimports-rereviser and gofumpt.
	@${TOOLS_BIN}/goimports-rereviser -project-name=github.com/zchee/mcp-lsp -skip-blanked -use-cache -cache-fast-skip -format -rm-unused -set-alias -recursive .
	@${TOOLS_BIN}/gofumpt -extra -w .

.PHONY: lint
lint: lint/golangci-lint  ## Run all linters.

.PHONY: lint/golangci-lint
lint/golangci-lint: hack/tools/bin/golangci-lint .golangci.yaml
lint/golangci-lint:  ## Run golangci-lint.
	@${TOOLS_BIN}/golangci-lint run $(strip ${GO_LINT_FLAGS}) ./...


##@ tools

hack/tools/bin/%: ${TOOLS_DIR}/go.mod ${TOOLS_DIR}/go.sum
hack/tools/bin/%:  ## Install an individual dependency tool.
	@$(call install_tool,$(@F))

.PHONY: tools
tools: ${TOOLS_DIR}/go.mod ${TOOLS_DIR}/go.sum
tools:  ## Install tools.
	@GOBIN=${TOOLS_BIN} go install -C ${TOOLS_DIR} -v -mod=readonly ${GO_FLAGS} tool

##@ clean

.PHONY: clean
clean:  ## Cleanups binaries and extra files in the package.
	@rm -rf *.out *.test *.prof trace.txt ${TOOLS_BIN} ${GO_COVERAGE_JUNITFILE_DIR}


##@ miscellaneous

.PHONY: todo
todo:  ## Print the all of (TODO|BUG|XXX|FIXME|NOTE) in packages.
	@grep -E '(TODO|BUG|XXX|FIXME)(\(.+\):|:)' $(shell find . -type f -name '*.go' -and -not -iwholename '*vendor*')

.PHONY: nolint
nolint:  ## Print the all of //nolint:... pragma in packages.
	@grep -E -C 3 '//nolint.+' $(shell find . -type f -name '*.go' -and -not -iwholename '*vendor*' -and -not -iwholename '*internal*')

.PHONY: env/%
env/%: ## Print the value of MAKEFILE_VARIABLE. Use `make env/GO_FLAGS` or etc.
	@echo $($*)


##@ help

.PHONY: help
help:  ## Show this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[33m<target>\033[0m\n"} /^[a-zA-Z_0-9\/%_-]+:.*?##/ { printf "  \033[1;32m%-20s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)
