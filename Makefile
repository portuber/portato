.PHONY: build run test fmt vet cross build-all cover install-service snapshot e2e-handoff stop reload

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

# stop terminates the running daemon via the CLI (Phase 27).
stop:
	./bin/portato stop

# reload makes the running daemon re-read config.yaml via the CLI (Phase 28).
reload:
	./bin/portato reload

# e2e-handoff runs the Phase 16 black-box hand-off E2E: builds the real binary,
# spins up an in-process SSH server (internal/sshtest), and asserts the local
# port never refuses a connection across the standalone->daemon transition, plus
# the close+rebind fallback. Slower than unit tests, hence a separate target.
e2e-handoff:
	go test -tags e2e ./internal/tui/... -run TestHandoffE2E -v -count=1
