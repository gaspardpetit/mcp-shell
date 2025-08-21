.PHONY: fmt build test all

fmt:
	go fmt ./...

build:
	go build ./...

test:
	go test ./...

all: fmt build test
