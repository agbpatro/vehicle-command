VERSION           = $(shell go version)
LINTER			  = golangci-lint run -v $(LINTER_FLAGS) --exclude-use-default=false --enable gofmt,golint --timeout $(LINTER_DEADLINE)
LINTER_DEADLINE	  = 30s
LINTER_FLAGS ?=
all: build

set-version:
	if TAG=$$(git describe --tags --abbrev=0); then echo "$${TAG}" | sed 's/v//' > pkg/account/version.txt; fi

format: set-version
	go fmt ./...

test: 
	go test -cover ./...
	go vet ./...

build: set-version test
	go build ./...

install: test
	go install ./cmd/...

linters: 
	$(LINTER)

.PHONY: install build test format set-version linters
