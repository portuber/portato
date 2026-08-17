#!/usr/bin/env bash
# portato Linux/systemd E2E. Run INSIDE the container as root.
#   e2e.sh check     -> install + [116] lingering + [119] live-traffic + auto-reconnect
#   e2e.sh jump      -> [Phase 43] two-hop proxyjump forward + auto-reconnect
#   e2e.sh sshconfig -> [Phase 44] ~/.ssh/config alias (ProxyJump) forward + auto-reconnect
#   e2e.sh import    -> [Phase 48] import forwards from ~/.ssh/config + marker flow
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
jump)
  echo "== [Phase 43 proxyjump] waiting for user manager bus =="
  loginctl enable-linger "$APP" 2>/dev/null || true
  for i in $(seq 1 40); do [ -S "$RT/bus" ] && break; sleep 0.5; done
  [ -S "$RT/bus" ] && pass "user manager bus up" || fail "no user manager bus at $RT/bus"

  nc -z 127.0.0.1 22   && pass "target sshd:22 up"    || fail "target sshd:22 not reachable"
  nc -z 127.0.0.1 2222 && pass "bastion sshd:2222 up" || fail "bastion sshd:2222 not reachable"
  nc -z 127.0.0.1 28080 && pass "echo:28080 up" || fail "echo:28080 not reachable"

  echo "== [Phase 43] portato install (start the daemon) =="
  as_app portato install
  for i in $(seq 1 40); do [ "$(as_app systemctl --user is-active portato 2>/dev/null)" = active ] && break; sleep 0.5; done
  [ "$(as_app systemctl --user is-active portato 2>/dev/null)" = active ] && pass "portato.service active" || fail "portato.service not active"

  echo "== [Phase 43] jump tunnel connects through the bastion =="
  as_app portato enable echo-via-bastion
  # list prints one tabwriter row per tuber (NAME ... STATUS); match the row
  # where echo-via-bastion is connected. (There are now two tubers, so a bare
  # 'grep connected' could match the wrong one.)
  c=no; for i in $(seq 1 40); do as_app portato list 2>/dev/null | awk '/echo-via-bastion/ && /connected/ {f=1} END{exit !f}' && { c=yes; break; }; sleep 0.5; done
  [ "$c" = yes ] && pass "echo-via-bastion Connected through the bastion" || fail "echo-via-bastion not connected"

  nc -w2 -z 127.0.0.1 19090 && pass "nc -z 127.0.0.1 19090 (jump forward works)" || fail "jump forward 19090 unreachable"

  echo "== [Phase 43] auto-reconnect after the bastion drops =="
  # Kill the active SSH sessions (per-connection privsep procs) on BOTH sshd;
  # the listeners stay up (Restart=always), so portato rebuilds the chain.
  pkill -KILL -f 'sshd: appuser' 2>/dev/null || true
  r=no; for i in $(seq 1 60); do as_app portato list 2>/dev/null | awk '/echo-via-bastion/ && /connected/ {f=1} END{exit !f}' && { r=yes; break; }; sleep 0.5; done
  [ "$r" = yes ] && pass "auto-reconnect after bastion drop" || fail "no auto-reconnect through the bastion"
  nc -w2 -z 127.0.0.1 19090 && pass "jump forward works after reconnect" || fail "jump forward 19090 unreachable after reconnect"

  as_app portato disable echo-via-bastion
  sleep 1
  if nc -w2 -z 127.0.0.1 19090; then fail "19090 still open after disable"; else pass "19090 closed after disable"; fi
  as_app portato list
  echo "== summary: exit $RC =="
  exit $RC
  ;;
sshconfig)
  echo "== [Phase 44 ssh-config] waiting for user manager bus =="
  loginctl enable-linger "$APP" 2>/dev/null || true
  for i in $(seq 1 40); do [ -S "$RT/bus" ] && break; sleep 0.5; done
  [ -S "$RT/bus" ] && pass "user manager bus up" || fail "no user manager bus at $RT/bus"

  nc -z 127.0.0.1 22   && pass "target sshd:22 up"    || fail "target sshd:22 not reachable"
  nc -z 127.0.0.1 2222 && pass "bastion sshd:2222 up" || fail "bastion sshd:2222 not reachable"
  nc -z 127.0.0.1 28080 && pass "echo:28080 up" || fail "echo:28080 not reachable"

  echo "== [Phase 44] portato install (start the daemon) =="
  as_app portato install
  for i in $(seq 1 40); do [ "$(as_app systemctl --user is-active portato 2>/dev/null)" = active ] && break; sleep 0.5; done
  [ "$(as_app systemctl --user is-active portato 2>/dev/null)" = active ] && pass "portato.service active" || fail "portato.service not active"

  echo "== [Phase 44] ssh-config alias tunnel connects (ProxyJump from ~/.ssh/config) =="
  as_app portato enable echo-via-alias
  c=no; for i in $(seq 1 40); do as_app portato list 2>/dev/null | awk '/echo-via-alias/ && /connected/ {f=1} END{exit !f}' && { c=yes; break; }; sleep 0.5; done
  [ "$c" = yes ] && pass "echo-via-alias Connected via the ssh-config alias" || fail "echo-via-alias not connected"

  nc -w2 -z 127.0.0.1 19091 && pass "nc -z 127.0.0.1 19091 (alias forward works)" || fail "alias forward 19091 unreachable"

  echo "== [Phase 44] auto-reconnect after the bastion drops =="
  pkill -KILL -f 'sshd: appuser' 2>/dev/null || true
  r=no; for i in $(seq 1 60); do as_app portato list 2>/dev/null | awk '/echo-via-alias/ && /connected/ {f=1} END{exit !f}' && { r=yes; break; }; sleep 0.5; done
  [ "$r" = yes ] && pass "auto-reconnect after bastion drop" || fail "no auto-reconnect through the alias"
  nc -w2 -z 127.0.0.1 19091 && pass "alias forward works after reconnect" || fail "alias forward 19091 unreachable after reconnect"

  as_app portato list
  echo "== summary: exit $RC =="
  exit $RC
  ;;
import)
  # Phase 48: `portato import` against the REAL ~/.ssh/config (import-alias
  # carries one Local/Remote/Dynamic forward; the trailing Host * forward at
  # 19999 must be skipped), the imported tubers run against the real sshd,
  # and the daemon-first marker flow ends the case.
  echo "== [Phase 48 import] waiting for user manager bus =="
  loginctl enable-linger "$APP" 2>/dev/null || true
  for i in $(seq 1 40); do [ -S "$RT/bus" ] && break; sleep 0.5; done
  [ -S "$RT/bus" ] && pass "user manager bus up" || fail "no user manager bus at $RT/bus"

  nc -z 127.0.0.1 22    && pass "target sshd:22 up"    || fail "target sshd:22 not reachable"
  nc -z 127.0.0.1 2222  && pass "bastion sshd:2222 up" || fail "bastion sshd:2222 not reachable"
  nc -z 127.0.0.1 28080 && pass "echo:28080 up" || fail "echo:28080 not reachable"

  SSHMD5=$(md5sum "/home/$APP/.ssh/config" | cut -d' ' -f1)
  CFG="/home/$APP/.config/portato/config.yaml"
  CFGMD5=$(md5sum "$CFG" | cut -d' ' -f1)

  echo "== [Phase 48] import --dry-run --all lists candidates, Host * skipped =="
  dry=$(as_app portato import --dry-run --all 2>&1)
  echo "$dry"
  for n in 19092 19093 19094; do echo "$dry" | grep -q "import-alias-$n" && pass "candidate import-alias-$n" || fail "candidate import-alias-$n missing"; done
  echo "$dry" | grep -q 19999 && fail "Host * forward leaked into candidates" || pass "Host * forward skipped"
  [ "$(md5sum "/home/$APP/.ssh/config" | cut -d' ' -f1)" = "$SSHMD5" ] && pass "~/.ssh/config untouched (dry run)" || fail "~/.ssh/config changed by dry run"
  [ "$(md5sum "$CFG" | cut -d' ' -f1)" = "$CFGMD5" ] && pass "config.yaml untouched (dry run)" || fail "config.yaml changed by dry run"

  echo "== [Phase 48] import --all --yes creates disabled tubers =="
  imp=$(as_app portato import --all --yes 2>&1)
  echo "$imp"
  for n in 19092 19093 19094; do echo "$imp" | grep -q "imported import-alias-$n" && pass "imported import-alias-$n" || fail "import-alias-$n not imported"; done
  [ "$(md5sum "/home/$APP/.ssh/config" | cut -d' ' -f1)" = "$SSHMD5" ] && pass "~/.ssh/config untouched (import)" || fail "~/.ssh/config changed by import"

  echo "== [Phase 48] second import is a dedup no-op =="
  again=$(as_app portato import --all --yes 2>&1)
  echo "$again"
  echo "$again" | grep -q "nothing new to import" && pass "re-import deduped" || fail "re-import not deduped"

  echo "== [Phase 48] portato install; imported tubers listed off =="
  as_app portato install
  for i in $(seq 1 40); do [ "$(as_app systemctl --user is-active portato 2>/dev/null)" = active ] && break; sleep 0.5; done
  [ "$(as_app systemctl --user is-active portato 2>/dev/null)" = active ] && pass "portato.service active" || fail "portato.service not active"
  as_app portato reload >/dev/null 2>&1 || true
  off=no; for i in $(seq 1 40); do as_app portato list 2>/dev/null | awk '/import-alias-19092/ && /off|Off/ {f=1} END{exit !f}' && { off=yes; break; }; sleep 0.5; done
  [ "$off" = yes ] && pass "imported tubers listed off" || fail "imported tubers not listed off"

  echo "== [Phase 48] local forward (19092) carries traffic via real sshd =="
  as_app portato enable import-alias-19092
  c=no; for i in $(seq 1 40); do as_app portato list 2>/dev/null | awk '/import-alias-19092/ && /connected/ {f=1} END{exit !f}' && { c=yes; break; }; sleep 0.5; done
  [ "$c" = yes ] && pass "import-alias-19092 connected" || fail "import-alias-19092 not connected"
  nc -w2 -z 127.0.0.1 19092 && pass "nc -z 127.0.0.1 19092 (local forward works)" || fail "local forward 19092 unreachable"

  echo "== [Phase 48] dynamic forward (19093) listens =="
  as_app portato enable import-alias-19093
  d=no; for i in $(seq 1 40); do as_app portato list 2>/dev/null | awk '/import-alias-19093/ && /connected/ {f=1} END{exit !f}' && { d=yes; break; }; sleep 0.5; done
  [ "$d" = yes ] && pass "import-alias-19093 connected" || fail "import-alias-19093 not connected"
  nc -w2 -z 127.0.0.1 19093 && pass "nc -z 127.0.0.1 19093 (socks listener up)" || fail "socks listener 19093 unreachable"

  echo "== [Phase 48] remote forward (19094) listens on the sshd side =="
  if nc -w2 -z 127.0.0.1 19094 2>/dev/null; then fail "19094 open before enable (expected closed)"; else pass "19094 closed before enable"; fi
  as_app portato enable import-alias-19094
  r=no; for i in $(seq 1 40); do as_app portato list 2>/dev/null | awk '/import-alias-19094/ && /connected/ {f=1} END{exit !f}' && { r=yes; break; }; sleep 0.5; done
  [ "$r" = yes ] && pass "import-alias-19094 connected" || fail "import-alias-19094 not connected"
  # The bare-port RemoteForward imports as 127.0.0.1:19094 (loopback, no
  # GatewayPorts needed) — real OpenSSH binds it on the server side.
  nc -w2 -z 127.0.0.1 19094 && pass "nc -z 127.0.0.1 19094 (remote forward bound by sshd)" || fail "remote forward 19094 unreachable"

  echo "== [Phase 48] disable closes the local port =="
  as_app portato disable import-alias-19092
  sleep 1
  if nc -w2 -z 127.0.0.1 19092; then fail "19092 still open after disable"; else pass "19092 closed after disable"; fi
  as_app portato disable import-alias-19093
  as_app portato disable import-alias-19094

  echo "== [Phase 48] daemon-first bootstrap does not consume the offer =="
  as_app systemctl --user stop portato 2>/dev/null || true
  cp "$CFG" "$CFG.import-e2e-bak"
  rm -f "$CFG" "/home/$APP/.local/state/portato/fresh_install" "/home/$APP/.local/state/portato/import_offered"
  as_app systemctl --user start portato
  for i in $(seq 1 40); do [ "$(as_app systemctl --user is-active portato 2>/dev/null)" = active ] && break; sleep 0.5; done
  [ -f "/home/$APP/.local/state/portato/fresh_install" ] && pass "fresh_install marker written by daemon bootstrap" || fail "fresh_install marker missing"
  [ -f "/home/$APP/.local/state/portato/import_offered" ] && fail "daemon consumed the import offer" || pass "import_offered NOT written by the daemon"
  boot=no; for i in $(seq 1 40); do [ -f "$CFG" ] && { boot=yes; break; }; sleep 0.5; done
  [ "$boot" = yes ] && pass "daemon bootstrapped config.yaml" || fail "daemon did not bootstrap config.yaml"

  echo "== [Phase 48] restore the imported config =="
  as_app systemctl --user stop portato 2>/dev/null || true
  # cp/mv ran as root, so restore ownership before appuser's daemon reads it.
  mv "$CFG.import-e2e-bak" "$CFG"
  chown "$APP:$APP" "$CFG"
  chmod 600 "$CFG"
  as_app systemctl --user start portato
  for i in $(seq 1 40); do [ "$(as_app systemctl --user is-active portato 2>/dev/null)" = active ] && break; sleep 0.5; done
  # active != IPC-ready: retry list until the daemon actually serves.
  for i in $(seq 1 40); do as_app portato list >/dev/null 2>&1 && break; sleep 0.5; done
  as_app portato list
  echo "== summary: exit $RC =="
  exit $RC
  ;;
check)
  echo "== waiting for user manager bus =="
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
  echo "usage: e2e.sh check|jump|sshconfig|import|status|uninstall" >&2; exit 2 ;;
esac
