# portato Linux/systemd E2E (Docker)

Verifies the Linux DoD items of Phase 6: lingering [116], reboot survival [115]
(approximated by `docker restart`), uninstall-after-restart [117], and the
live-traffic + auto-reconnect MVP E2E [119]. launchd parts are macOS-only. The
`check` run also exercises Phase 22 socket activation: it stops the service,
leaving only `portato.socket` holding the IPC socket, and confirms `portato list`
is served (the connection socket-activates the daemon).

## Build (from repo root)

    make cross
    cp bin/portato-linux-arm64 e2e/systemd-docker/portato
    docker build -t portato-test e2e/systemd-docker

## Run + automated checks

    docker run -d --name portato-test --privileged --cgroupns=host portato-test
    sleep 6
    docker exec portato-test /e2e/e2e.sh check      # -> block of PASS, exit 0

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
