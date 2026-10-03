BIN := osb

.PHONY: build test clean

build:
	go build -o $(BIN) ./cmd/osb

test:
	go vet ./...
	go test ./...

clean:
	rm -f $(BIN)
