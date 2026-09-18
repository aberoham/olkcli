VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE    ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

MODULE   = github.com/rlrghb/olkcli
LDFLAGS  = -s -w \
	-X $(MODULE)/internal/cmd.Version=$(VERSION) \
	-X $(MODULE)/internal/cmd.Commit=$(COMMIT) \
	-X $(MODULE)/internal/cmd.Date=$(DATE)

BINARY   = ./bin/olk

# A locally built binary that reads the keychain must keep a stable code
# signature; an unsigned rebuild is a new application to macOS and triggers an
# access prompt for every stored account. Override for a different identity.
SIGN_IDENTITY   ?= teams-cli-signer
SIGN_IDENTIFIER ?= com.rlrghb.olk

.PHONY: build build-signed test lint install clean version

build:
	go build -ldflags '$(LDFLAGS)' -o $(BINARY) ./cmd/olk

build-signed: build
	codesign --force -s $(SIGN_IDENTITY) --identifier $(SIGN_IDENTIFIER) $(BINARY)

test:
	go test -race -count=1 ./...

lint:
	golangci-lint run ./...

install: build
	cp $(BINARY) $(GOPATH)/bin/olk

clean:
	rm -rf ./bin

version:
	@echo "Version: $(VERSION)"
	@echo "Commit:  $(COMMIT)"
	@echo "Date:    $(DATE)"
