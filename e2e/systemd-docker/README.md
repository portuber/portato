# portato Linux/systemd E2E (Docker)

Verifies the Linux DoD items of Phase 6: lingering [116], reboot survival [115]
(approximated by `docker restart`), uninstall-after-restart [117], and the
live-traffic + auto-reconnect MVP E2E [119]. launchd parts are macOS-only. The
`check` run also exercises Phase 22 socket activation: it stops the service,
leaving only `portato.socket` holding the IPC socket, and confirms `portato list`
is served (the connection socket-activates the daemon).

The `jump` run is the Phase 43 ProxyJump real-Linux proof: the image runs TWO
sshd instances — a target on `:22` and a bastion on `:2222` (with its own host
key) — and a `jump:` tuber (`echo-via-bastion`) reaches the target through the
bastion. Both authorize the one appuser key (the shared-identity model). This is
the only way to verify ProxyJump against real OpenSSH on Linux without a native
Linux host (the dev is on macOS; `make e2e-proxyjump` covers the dial logic with
in-process SSH servers, this run covers real OpenSSH + systemd).

## Build (from repo root)

    make cross
    cp bin/portato-linux-arm64 e2e/systemd-docker/portato
    docker build -t portato-test e2e/systemd-docker

## Run + automated checks

    docker run -d --name portato-test --privileged --cgroupns=host portato-test
    sleep 6
    docker exec portato-test /e2e/e2e.sh check      # -> block of PASS, exit 0
    docker exec portato-test /e2e/e2e.sh jump       # Phase 43: two-hop forward + reconnect

## [115] reboot survival

    docker restart portato-test && sleep 6
    docker exec portato-test /e2e/e2e.sh status      # expect: portato is-active: active

## [117] uninstall + reboot

    docker exec portato-test /e2e/e2e.sh uninstall
    docker restart portato-test && sleep 6
    docker exec portato-test /e2e/e2e.sh status      # expect: inactive / not loaded

## Cleanup

    docker rm -f portato-test

## Troubleshooting

- systemd not starting: must use `--privileged --cgroupns=host` (cgroups required).
- `Failed to connect to bus` from `systemctl --user`: handled by e2e.sh; if it
  still happens, ensure linger (`docker exec portato-test loginctl enable-linger appuser`).
- Intel Mac / amd64 host: `cp bin/portato-linux-amd64 e2e/systemd-docker/portato`
  and `docker build --platform linux/amd64 -t portato-test e2e/systemd-docker`.
