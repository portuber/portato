.PHONY: build run test fmt vet lint cover build-all cross snapshot install-service stop reload e2e-handoff e2e-proxyjump e2e-sshconfig e2e-docker third-party-licenses optimize-assets release release-patch release-minor release-major

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

# lint runs two golangci-lint passes:
#   1. .golangci.yml — predeclared (no builtin shadowing, e.g. max/min/len) and
#      gocyclo (cyclomatic complexity < 15) on production code; _test.go is
#      exempt from gocyclo (legit table-driven tests run high). The local gate
#      for the codefactor.io issue classes (Phase 33).
#   2. .golangci-tests.yml — gocyclo at codefactor.io's ~18 threshold on test
#      files too, so a too-complex test is caught locally before CodeFactor
#      flags it on the public repo (Phase 46 regression). Looser than pass 1
#      for production, so it adds catches only on _test.go.
# Requires golangci-lint v1.x: go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.64.8
lint:
	golangci-lint run ./...
	golangci-lint run ./... -c .golangci-tests.yml

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

# e2e-proxyjump runs the Phase 43 black-box ProxyJump E2E: builds the real
# binary, spins up two in-process SSH servers (bastion + target), and asserts a
# -L forward carries traffic through the chain (edge -> target) plus self-heals
# when the bastion drops. Real binary + real SSH servers + real traffic.
e2e-proxyjump:
	go test -tags e2e ./internal/daemon/... -run TestProxyJumpE2E -v -count=1

# e2e-sshconfig runs the Phase 44 black-box ~/.ssh/config E2E: builds the real
# binary, writes a temp ~/.ssh/config whose alias carries a ProxyJump, and
# asserts a `ssh: <alias>` (no `jump:`) tuber resolves + dials through the
# chain and a -L forward carries traffic, plus self-heals when the bastion
# drops. Real binary + real SSH servers + real traffic; runs on macOS.
e2e-sshconfig:
	go test -tags e2e ./internal/daemon/... -run TestSSHConfigE2E -v -count=1

# e2e-docker runs a real-Linux/systemd E2E case in Docker — the only way to
# verify against real OpenSSH + systemd on Linux without a native host (the dev
# is on macOS; the make e2e-* targets above cover the dial logic in-process on
# the host). E2E_CASE selects the e2e.sh case (check|jump|sshconfig); default
# check. It cross-builds the linux binary if missing, (re)builds the image, and
# recreates the container fresh each run so image changes always take effect.
# Heavy (Docker + a privileged container); NOT part of CI. The container is
# removed at the end by default; set E2E_KEEP=1 to leave it running and iterate
# with `docker exec portato-test /e2e/e2e.sh <case>` between cases (then
# `docker rm -f` it yourself).
E2E_DOCKER_IMG ?= portato-test
E2E_DOCKER_CTR ?= portato-test
E2E_LINUX_BIN  ?= bin/portato-linux-arm64
E2E_CASE       ?= check
E2E_KEEP       ?=
e2e-docker:
	@test -f $(E2E_LINUX_BIN) || $(MAKE) cross
	cp $(E2E_LINUX_BIN) e2e/systemd-docker/portato
	docker build -t $(E2E_DOCKER_IMG) e2e/systemd-docker
	-docker rm -f $(E2E_DOCKER_CTR) >/dev/null 2>&1
	docker run -d --name $(E2E_DOCKER_CTR) --privileged --cgroupns=host $(E2E_DOCKER_IMG) >/dev/null
	@sleep 6
	docker exec $(E2E_DOCKER_CTR) /e2e/e2e.sh $(E2E_CASE)
	@if [ -z "$(E2E_KEEP)" ]; then \
		docker rm -f $(E2E_DOCKER_CTR) >/dev/null; \
		echo "container removed (set E2E_KEEP=1 to leave it running for docker-exec iteration)"; \
	fi

# third-party-licenses regenerates the bundled THIRD_PARTY_LICENSES.txt: each
# runtime dependency's license text under a module-path header. Packed into
# release archives and the deb/rpm packages (Phase 32). Needs network for the
# first go-licenses install.
third-party-licenses:
	go install github.com/google/go-licenses@latest
	go-licenses save ./cmd/portato --save_path third_party --force
	@{ \
	  echo "Third-party licenses bundled with Portato"; \
	  echo "(permissive: MIT / Apache-2.0 / BSD). Each block below is one"; \
	  echo "dependency's license text, under its module path. Generated by"; \
	  echo "'make third-party-licenses' via github.com/google/go-licenses."; \
	  find third_party -type f | sort | while read f; do \
	    mod=$$(echo "$$f" | sed 's|^third_party/||'); \
	    printf '\n\n================================================================================\n%s\n================================================================================\n\n' "$$mod"; \
	    cat "$$f"; \
	  done; \
	} > THIRD_PARTY_LICENSES.txt
	@rm -rf third_party

# optimize-assets compresses the landing demo assets in place: gifsicle on
# hero.gif (palette trim + lossy frame diff) and pngquant on the still PNGs
# (8-bit palette, q70-85 — near-lossless for UI screenshots). Run after a vhs
# re-record (see demo.tape). Requires: brew install gifsicle pngquant.
optimize-assets:
	gifsicle -O3 --lossy=80 --colors 128 docs/landing/assets/hero.gif -o docs/landing/assets/hero.gif
	pngquant --quality=70-85 --force --ext .png -- docs/landing/assets/*.png

# release cuts and pushes a release tag, which triggers the GitHub Actions
# 'release' workflow (goreleaser). Tags are strictly vX.Y.Z (no pre-releases;
# see docs/VERSIONING.md). Pick the version one of two ways:
#   make release-patch | release-minor | release-major  -- bump from the latest tag
#   make release VERSION=vX.Y.Z                        -- explicit override
# Before touching the remote it prints latest tag, the computed new tag, the
# HEAD commit, the commits that will land on main, and asks y/N. On y it runs
# `git push origin main`, creates an annotated tag, and pushes it.
release-patch:
	@$(MAKE) --no-print-directory release LEVEL=patch

release-minor:
	@$(MAKE) --no-print-directory release LEVEL=minor

release-major:
	@$(MAKE) --no-print-directory release LEVEL=major

release:
	@set -e; \
	latest=$$(git describe --tags --abbrev=0 2>/dev/null) || { echo "no tags found; use: make release VERSION=vX.Y.Z"; exit 1; }; \
	base=$${latest#v}; \
	major=$${base%%.*}; rest=$${base#*.}; minor=$${rest%%.*}; patch=$${rest##*.}; \
	if [ -n "$(VERSION)" ]; then \
		newtag=v$$(printf '%s' "$(VERSION)" | sed 's/^v//'); \
	elif [ -n "$(LEVEL)" ]; then \
		case "$(LEVEL)" in \
			patch) patch=$$((patch+1)); newtag=v$$major.$$minor.$$patch ;; \
			minor) minor=$$((minor+1)); newtag=v$$major.$$minor.0 ;; \
			major) major=$$((major+1)); newtag=v$$major.0.0 ;; \
			*) echo "bad LEVEL=$(LEVEL)"; exit 1 ;; \
		esac; \
	else \
		echo "usage: make release-{patch,minor,major}  |  make release VERSION=vX.Y.Z"; exit 1; \
	fi; \
	case "$$newtag" in v[0-9]*.[0-9]*.[0-9]*) ;; *) echo "invalid tag '$$newtag' (want vX.Y.Z)"; exit 1;; esac; \
	(git rev-parse -q --verify "refs/tags/$$newtag" >/dev/null) && { echo "$$newtag already exists"; exit 1; } || true; \
	echo "latest tag : $$latest"; \
	echo "new tag    : $$newtag"; \
	echo "main HEAD  : $$(git rev-parse --short HEAD) - $$(git log -1 --format='%s')"; \
	if git rev-parse -q --verify origin/main >/dev/null 2>&1; then \
		echo "commits to push to main:"; \
		pending=$$(git log origin/main..HEAD --oneline); \
		if [ -n "$$pending" ]; then printf '%s\n' "$$pending" | sed 's/^/    /'; else echo "    (none; main is up to date)"; fi; \
	else echo "commits to push to main: (origin/main unknown)"; fi; \
	echo "Pushing will trigger the 'release' GitHub Actions workflow."; \
	printf "Push main + tag %s? [y/N] " "$$newtag"; read ans; \
	case "$$ans" in y|Y|yes|YES) ;; *) echo "aborted"; exit 1;; esac; \
	git push origin main && git tag -a "$$newtag" -m "Release $$newtag" && git push origin "$$newtag"
