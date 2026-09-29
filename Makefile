VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE    ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

MODULE   = github.com/rlrghb/olkcli
LDFLAGS  = -s -w \
	-X $(MODULE)/internal/cmd.Version=$(VERSION) \
	-X $(MODULE)/internal/cmd.Commit=$(COMMIT) \
	-X $(MODULE)/internal/cmd.Date=$(DATE)

BINARY   = ./bin/olk

# Storage namespace of `make build` output. A development binary keeps its own
# config directory and credential-store entries, so it never touches the
# tokens of an installed olk. `make install` always builds with the release
# namespace, olk.
NAMESPACE ?= olk-dev

.PHONY: build test lint install clean version

build:
	go build -ldflags '$(LDFLAGS) -X $(MODULE)/internal/config.Namespace=$(NAMESPACE)' \
		-o $(BINARY) ./cmd/olk

test:
	go test -race -count=1 ./...

lint:
	golangci-lint run ./...

install:
	go build -ldflags '$(LDFLAGS)' -o $(shell go env GOPATH)/bin/olk ./cmd/olk

clean:
	rm -rf ./bin

version:
	@echo "Version: $(VERSION)"
	@echo "Commit:  $(COMMIT)"
	@echo "Date:    $(DATE)"
