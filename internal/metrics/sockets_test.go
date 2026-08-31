package metrics

import (
	"strings"
	"testing"
)

func TestParseProcNetTCP(t *testing.T) {
	data := `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 00000000:0016 00000000:0000 0A 00000000:00000000 00:00000000 00000000 0 0 1
   1: 0100007F:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000 0 0 2
   2: 0200000A:20FB 00000000:0000 0A 00000000:00000000 00:00000000 00000000 0 0 3
   3: 0100007F:C001 0100007F:01BB 01 00000000:00000000 00:00000000 00000000 0 0 4
`

	got := parseProcNet(strings.NewReader(data), "tcp", 4, "0A")
	if len(got) != 3 {
		t.Fatalf("got %d sockets, want 3", len(got))
	}
	assertSocket(t, got[0], "tcp", "0.0.0.0", 22, "all interfaces")
	assertSocket(t, got[1], "tcp", "127.0.0.1", 8080, "loopback")
	assertSocket(t, got[2], "tcp", "10.0.0.2", 8443, "interface")
}

func TestParseProcNetIPv6(t *testing.T) {
	data := `  sl  local_address                         rem_address                          st
   0: 00000000000000000000000000000000:01BB 00000000000000000000000000000000:0000 0A
   1: 00000000000000000000000001000000:14E9 00000000000000000000000000000000:0000 0A
`

	got := parseProcNet(strings.NewReader(data), "tcp6", 16, "0A")
	if len(got) != 2 {
		t.Fatalf("got %d sockets, want 2", len(got))
	}
	assertSocket(t, got[0], "tcp6", "::", 443, "all interfaces")
	assertSocket(t, got[1], "tcp6", "::1", 5353, "loopback")
}

func TestParseProcNetUDPOnlyIncludesBoundSockets(t *testing.T) {
	data := `  sl  local_address rem_address   st
   0: 00000000:0035 00000000:0000 07
   1: 0100007F:CAFE 08080808:0035 01
   2: not-an-address 00000000:0000 07
`

	got := parseProcNet(strings.NewReader(data), "udp", 4, "07")
	if len(got) != 1 {
		t.Fatalf("got %d sockets, want 1", len(got))
	}
	assertSocket(t, got[0], "udp", "0.0.0.0", 53, "all interfaces")
}

func assertSocket(t *testing.T, got ListeningSocket, protocol, address string, port uint16, scope string) {
	t.Helper()
	if got.Protocol != protocol || got.Address != address || got.Port != port || got.Scope != scope {
		t.Errorf("got %+v, want protocol=%q address=%q port=%d scope=%q", got, protocol, address, port, scope)
	}
}
