# The development tools live in the repository root's tools.mod, shared with the root module.
# Each is resolved to its built binary outside any workspace (go refuses -modfile in workspace
# mode) and then run in this module's own mode, so a go.work that supplies the root module
# during development still applies to the code it checks.
tool = $(shell GOWORK=off go tool -modfile=../tools.mod -n $(1))

# The Cobra releases the importer is checked against: the oldest it builds with (go.mod's
# requirement) through the newest.
COBRA_VERSIONS := v1.6.0 v1.7.0 v1.8.0 v1.9.1 v1.10.2

.PHONY: all clean lint test test-cobra-versions test-fuzz test-mutation test-race vuln

all: clean lint test test-cobra-versions test-fuzz test-mutation test-race

clean:
	@rm -rf *.out test_mutation.json

lint:
	@gofmt_unformatted=$$(gofmt -l . 2>/dev/null | grep -v '^testdata/' || true); \
	test -z "$$gofmt_unformatted" || (echo "files not formatted:" && echo "$$gofmt_unformatted" && exit 1)
	@go vet ./...
	@go mod verify
	@$(call tool,golangci-lint) run ./...
	@$(call tool,go-licenses) check ./...
	@$(MAKE) --no-print-directory vuln

vuln:
	@$(call tool,govulncheck) -test ./...

test:
	@go test -v -count=1 -coverprofile=test.out ./...
	@go tool cover -func=test.out | tail -1

# Runs the tests against each Cobra release in COBRA_VERSIONS, through a temporary modfile so
# go.mod keeps the oldest. The root module is taken from this checkout.
test-cobra-versions:
	@for v in $(COBRA_VERSIONS); do \
		dir=$$(mktemp -d); \
		cp go.mod "$$dir/cobra.mod"; cp go.sum "$$dir/cobra.sum"; \
		echo "→ cobra $$v"; \
		GOWORK=off go mod edit -modfile="$$dir/cobra.mod" -replace=github.com/go-rotini/import=.. || exit 1; \
		GOWORK=off go get -modfile="$$dir/cobra.mod" github.com/spf13/cobra@$$v || exit 1; \
		GOWORK=off go test -modfile="$$dir/cobra.mod" -count=1 ./... || exit 1; \
		rm -rf "$$dir"; \
	done

test-fuzz:
	@go test -fuzz=^FuzzParseUse$$ -fuzztime=60s -run=^$$ .

test-mutation:
	@$(call tool,gremlins) unleash --config ../.gremlins.yaml

test-race:
	@go test -race -count=1 -coverprofile=test_race.out ./...
	@go tool cover -func=test_race.out | tail -1
