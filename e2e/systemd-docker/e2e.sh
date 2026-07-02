#!/usr/bin/env bash
# portato Linux/systemd E2E. Run INSIDE the container as root.
#   e2e.sh check     -> install + [116] lingering + [119] live-traffic + auto-reconnect
#   e2e.sh status    -> is portato active? (run after `docker restart`)
#   e2e.sh uninstall -> portato uninstall as appuser
set -u
APP=appuser
APP_UID=$(id -u "$APP")
RT="/run/user/$APP_UID"

as_app() {
  runuser -u "$APP" -- env \
    XDG_RUNTIME_DIR="$RT" \
    DBUS_SESSION_BUS_ADDRESS="unix:path=$RT/bus" \
    HOME="/home/$APP" "$@"
}
pass(){ echo "PASS: $*"; }
fail(){ echo "FAIL: $*"; RC=1; }
RC=0

case "${1:-}" in
check)
  echo "== waiting for user manager bus =="
  loginctl enable-linger "$APP" 2>/dev/null || true
  for i in $(seq 1 40); do [ -S "$RT/bus" ] && break; sleep 0.5; done
  [ -S "$RT/bus" ] && pass "user manager bus up" || fail "no user manager bus at $RT/bus"

  nc -z 127.0.0.1 22    && pass "sshd:22 up"    || fail "sshd:22 not reachable"
  nc -z 127.0.0.1 28080 && pass "echo:28080 up" || fail "echo:28080 not reachable"

  echo "== portato install =="
  as_app portato install
  [ "$(as_app systemctl --user is-active portato 2>/dev/null)" = active ] \
    && pass "portato.service active" || fail "portato.service not active"

  echo "== [116] lingering =="
  loginctl show-user "$APP" -p Linger | grep -q 'Linger=yes' && pass "Linger=yes" || fail "Linger not enabled"

  echo "== [119] live-traffic =="
  as_app portato enable echo
  c=no; for i in $(seq 1 40); do as_app portato list | grep -qi connected && { c=yes; break; }; sleep 0.5; done
  [ "$c" = yes ] && pass "tunnel Connected" || fail "tunnel not connected"
  nc -w2 -z 127.0.0.1 18080 && pass "nc -z 127.0.0.1 18080 (forward works)" || fail "forward 18080 unreachable"
  as_app portato disable echo
  sleep 1
  if nc -w2 -z 127.0.0.1 18080; then fail "18080 still open after disable"; else pass "18080 closed after disable"; fi

  echo "== [119] auto-reconnect =="
  as_app portato enable echo
  for i in $(seq 1 40); do as_app portato list | grep -qi connected && break; sleep 0.5; done
  pkill -KILL -f 'sshd: appuser' 2>/dev/null || true
  r=no; for i in $(seq 1 60); do as_app portato list | grep -qi connected && { r=yes; break; }; sleep 0.5; done
  [ "$r" = yes ] && pass "auto-reconnect after sshd drop" || fail "no auto-reconnect"
  as_app portato list
  echo "== [Phase 22] socket activation =="
  # Stop the daemon so the only thing holding the IPC socket is portato.socket
  # (which install enabled). The service unit is NOT running for this probe.
  as_app systemctl --user stop portato.service 2>/dev/null || true
  as_app systemctl --user start portato.socket
  pre=$(as_app systemctl --user is-active portato 2>/dev/null || true)
  # The first 'portato list' is what socket-ACTIVATES the stopped service, so
  # its discovery probe races the daemon's cold start (it times out while the
  # daemon is still coming up). Retry briefly until the activated daemon Serve()s.
  ok=no
  for i in $(seq 1 20); do
    if as_app portato list >/tmp/plist.$$ 2>&1; then ok=yes; break; fi
    sleep 0.5
  done
  if [ "$ok" = yes ]; then
    pass "portato list served via socket activation (pre-list state=$pre)"
  else
    fail "portato list via socket activation (pre-list state=$pre): $(cat /tmp/plist.$$)"
  fi
  rm -f /tmp/plist.$$
  post=$(as_app systemctl --user is-active portato 2>/dev/null || true)
  [ "$post" = active ] && pass "portato.service socket-activated (active after list)" \
    || fail "portato.service not active after socket-activated list (state=$post)"
  as_app portato list
  echo "== summary: exit $RC =="
  exit $RC
  ;;
status)
  echo "portato is-active: $(as_app systemctl --user is-active portato 2>&1)"
  echo "--- portato list ---"
  as_app portato list 2>&1 || true
  ;;
uninstall)
  as_app portato uninstall
  ;;
*)
  echo "usage: e2e.sh check|status|uninstall" >&2; exit 2 ;;
esac
