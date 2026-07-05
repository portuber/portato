package fdpass

import "os"

// Header precedes each listener fd on the transfer socket, telling the
// receiver which tunnel the following fd belongs to and its type (so the daemon
// can sanity-check it is a local/dynamic tunnel that owns a local listener).
type Header struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// Offer is one listener the sender hands to the receiver: the tunnel name and
// type (carried in the Header) plus a dup'd *os.File of its local listener.
// The File is consumed (closed) by Send.
type Offer struct {
	Name string
	Type string
	File *os.File
}
