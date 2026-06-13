.PHONY: build run test fmt vet

build:
	go build -o bin/portato ./cmd/portato

run:
	go run ./cmd/portato

test:
	go test ./...

fmt:
	gofmt -w .

vet:
	go vet ./...
