.PHONY: build run test fmt vet cross build-all cover install-service snapshot

build:
	go build -o bin/portato ./cmd/portato

run:
	go run ./cmd/portato

test:
	go test ./...

# cover runs the tests with a coverage profile and prints the total.
cover:
	go test -coverprofile=cover.out ./...
	go tool cover -func=cover.out | tail -1

fmt:
	gofmt -w .

vet:
	go vet ./...

# build-all cross-compiles the binary for the MVP target matrix (SPEC §15).
# cross is a back-compat alias.
build-all:
	@for os in darwin linux; do \
	  for arch in amd64 arm64; do \
	    echo "==> $$os/$$arch"; \
	    GOOS=$$os GOARCH=$$arch go build -o bin/portato-$$os-$$arch ./cmd/portato || exit 1; \
	  done; \
	done

cross: build-all

# snapshot builds the full cross-platform release matrix locally via goreleaser
# (darwin/linux × amd64/arm64), writing archives + checksums.txt to dist/.
# No publish. Requires goreleaser: go install github.com/goreleaser/goreleaser/v2@latest
snapshot:
	goreleaser release --snapshot --clean

# install-service builds the local binary and registers autostart (Phase 6).
install-service: build
	./bin/portato install
