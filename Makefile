APP_NAME := progress_bar

GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
GOPATH := $(shell go env GOPATH)

.PHONY: all clean fmt vet test test-integration build install update

all: clean test build

fmt:
	@echo "Running go fmt"
	go fmt ./...

vet:
	@echo "Running go vet"
	go vet ./...

test: fmt vet
	@echo "Running go test"
	go test ./...

test-integration: fmt vet
	@echo "Running integration tests"
	go test -tags=integration ./...

build: clean
	@echo "Building $(APP_NAME) for $(GOOS)/$(GOARCH)"
	env GOOS=$(GOOS) GOARCH=$(GOARCH) go build -v -o "$(APP_NAME)" .

install: build
	@echo "Installing to $(GOPATH)/bin"
	install -m 0755 "$(APP_NAME)" "$(GOPATH)/bin/$(APP_NAME)"

update:
	@echo "Updating deps"
	go get -u -t ./...
	go mod tidy

clean:
	rm -f "$(APP_NAME)"