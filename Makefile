.PHONY: build run test fmt vet cross install-service

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

# cross compiles the binary for the MVP target matrix (SPEC §15).
cross:
	@for os in darwin linux; do \
	  for arch in amd64 arm64; do \
	    echo "==> $$os/$$arch"; \
	    GOOS=$$os GOARCH=$$arch go build -o bin/portato-$$os-$$arch ./cmd/portato || exit 1; \
	  done; \
	done

# install-service builds the local binary and registers autostart (Phase 6).
install-service: build
	./bin/portato install
