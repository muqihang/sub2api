.PHONY: test build

test:
	go test ./...

build:
	CGO_ENABLED=0 go build -trimpath -o nexus-agent-guard .
