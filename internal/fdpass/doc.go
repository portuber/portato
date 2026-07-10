// Package fdpass transfers live net.Listeners between processes over a unix
// domain socket using SCM_RIGHTS, for the Phase 16 standalone->daemon hand-off.
//
// The standalone TUI, on its way out, hands its already-bound local TCP
// listeners to the spawned daemon so the local ports never go down during the
// transition. A dedicated SOCK_SEQPACKET ("unixpacket") transfer socket carries
// one message per listener: a JSON [Header] as the payload (which tuber the fd
// belongs to) and the listener fd as SCM_RIGHTS ancillary data. The daemon
// reconstructs each into a net.Listener via net.FileListener and adopts it,
// skipping the net.Listen bind for that tuber.
//
// The transfer socket is used instead of exec fd inheritance on purpose: the
// hand-off spawns the daemon under Setsid, and inherited fds fight FD_CLOEXEC;
// SCM_RIGHTS hands the fd over explicitly after the child is alive.
//
// SCM_RIGHTS is a unix facility; Send/Recv return an error off unix (Windows is
// Phase 17), so the daemon and TUI build on every platform and the hand-off
// degrades to the Phase 5 close+rebind path there.
package fdpass
