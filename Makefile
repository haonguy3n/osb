# osb build + docs. Doc comments follow godoc convention (the Go-native,
# tooling-parseable style); `make docs` renders them to Markdown and `make
# docs-serve` browses them like Doxygen/pkgsite HTML.

BIN := osb
PKG := ./cmd/osb

.PHONY: build test test-full docs docs-serve clean

## build: compile the osb binary
build:
	go build -o $(BIN) $(PKG)

## test: run the unit tests
test:
	go test ./...

## test-full: run the suite matrix from test-suites.yaml (docs/testing.md);
## suites needing Docker/KVM skip automatically when the host lacks them
test-full:
	go run ./cmd/testsuite

## docs: render godoc doc comments to Markdown under docs/api/
docs:
	@mkdir -p docs/api
	go run github.com/princjef/gomarkdoc/cmd/gomarkdoc@latest \
		--output 'docs/api/{{.Dir}}.md' ./...
	@echo "API docs written to docs/api/"

## docs-serve: browse the docs locally (pkgsite, like a Doxygen HTML site)
docs-serve:
	@echo "Serving on http://localhost:6060 — Ctrl-C to stop"
	go run golang.org/x/pkgsite/cmd/pkgsite@latest -http=:6060 .

## clean: remove build artifacts
clean:
	rm -f $(BIN)
	rm -rf docs/api
