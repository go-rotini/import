# The development tools (linters, the license and vulnerability checkers and the mutation
# tester) are declared in tools.mod rather than go.mod, so a module that requires this one
# never sees them in its own dependency graph. Run them through TOOL, and upgrade them with
# `make tools-upgrade`, never `go mod tidy -modfile=tools.mod`.
TOOL := go tool -modfile=tools.mod

# The framework releases each importer is checked against: the oldest it builds with
# (go.mod's requirement) through the newest.
COBRA_VERSIONS := v1.6.0 v1.7.0 v1.8.0 v1.9.1 v1.10.2
KONG_VERSIONS := v0.9.0 v1.0.0 v1.2.0 v1.4.0 v1.8.0 v1.12.0 v1.16.1
URFAVE_VERSIONS := v3.1.0 v3.3.0 v3.6.0 v3.10.0 v3.12.0 v3.14.0

.PHONY: all clean lint test test-cobra-versions test-kong-versions test-urfave-versions test-fuzz test-mutation test-race tools-upgrade vuln vuln-tools

all: clean lint test test-cobra-versions test-kong-versions test-urfave-versions test-fuzz test-mutation test-race

clean:
	@rm -rf *.out test_mutation.json

lint:
	@gofmt_unformatted=$$(gofmt -l . 2>/dev/null | grep -v '\(^\|/\)testdata/' || true); \
	test -z "$$gofmt_unformatted" || (echo "files not formatted:" && echo "$$gofmt_unformatted" && exit 1)
	@go vet ./...
	@go mod verify
	@go mod verify -modfile=tools.mod
	@$(TOOL) golangci-lint run ./...
	@$(TOOL) go-licenses check ./...
	@$(MAKE) --no-print-directory vuln
	@$(MAKE) --no-print-directory vuln-tools

vuln:
	@$(TOOL) govulncheck -test ./...

# Scans each tool in tools.mod as a built binary: exactly the code that runs.
vuln-tools:
	@bin=$$(mktemp -d); trap 'rm -rf "$$bin"' EXIT; \
	for pkg in $$(go list -modfile=tools.mod tool); do \
		name=$$(echo "$$pkg" | tr / _); \
		echo "→ $$pkg"; \
		go build -modfile=tools.mod -o "$$bin/$$name" "$$pkg" || exit 1; \
		$(TOOL) govulncheck -mode=binary "$$bin/$$name" || exit 1; \
	done

# Upgrades every tool in tools.mod to its latest release, rebuilding tools.mod from scratch
# so it holds exactly what today's tools require. On failure the previous files are restored.
tools-upgrade:
	@tools=$$(go list -modfile=tools.mod tool) || exit 1; \
	backup=$$(mktemp -d); cp tools.mod tools.sum "$$backup"/; \
	restore() { cp "$$backup"/tools.mod "$$backup"/tools.sum .; rm -rf "$$backup"; echo "tools-upgrade failed; tools.mod and tools.sum restored"; exit 1; }; \
	{ grep -E '^module ' go.mod; echo; grep -E '^go ' go.mod; } > tools.mod; \
	rm -f tools.sum; \
	go get -modfile=tools.mod -tool $$(for t in $$tools; do printf '%s@latest ' "$$t"; done) || restore; \
	go mod verify -modfile=tools.mod || restore; \
	rm -rf "$$backup"
	@$(MAKE) --no-print-directory vuln-tools
	@echo "tools upgraded; review: git diff tools.mod"

test:
	@go test -v -count=1 -coverprofile=test.out ./...
	@go tool cover -func=test.out | tail -1

# Runs an importer's tests against each release of its framework, through a temporary
# modfile so go.mod keeps the oldest. $(1) is the framework's module, $(2) its releases and
# $(3) the importer's package.
define test-versions
	@for v in $(2); do \
		dir=$$(mktemp -d); \
		cp go.mod "$$dir/versions.mod"; cp go.sum "$$dir/versions.sum"; \
		echo "→ $(1) $$v"; \
		go get -modfile="$$dir/versions.mod" $(1)@$$v || exit 1; \
		go test -modfile="$$dir/versions.mod" -count=1 ./$(3)/... || exit 1; \
		rm -rf "$$dir"; \
	done
endef

test-cobra-versions:
	$(call test-versions,github.com/spf13/cobra,$(COBRA_VERSIONS),cobra)

test-kong-versions:
	$(call test-versions,github.com/alecthomas/kong,$(KONG_VERSIONS),kong)

test-urfave-versions:
	$(call test-versions,github.com/urfave/cli/v3,$(URFAVE_VERSIONS),urfave)

test-fuzz:
	@go test -fuzz=^FuzzRead$$ -fuzztime=60s -run=^$$ .
	@go test -fuzz=^FuzzParseUse$$ -fuzztime=60s -run=^$$ ./internal/walk
	@go test -fuzz=^FuzzDefaultValue$$ -fuzztime=60s -run=^$$ ./kong
	@go test -fuzz=^FuzzFromURFave$$ -fuzztime=60s -run=^$$ ./urfave

test-mutation:
	@$(TOOL) gremlins unleash --config .gremlins.yaml

test-race:
	@go test -race -count=1 -coverprofile=test_race.out ./...
	@go tool cover -func=test_race.out | tail -1
