package api

import (
	"net/http"
	"testing"
)

func mustSetProxies(t *testing.T, spec string) {
	t.Helper()
	if err := SetTrustedProxies(spec); err != nil {
		t.Fatalf("SetTrustedProxies(%q) = %v", spec, err)
	}
	t.Cleanup(func() {
		_ = SetTrustedProxies("loopback")
	})
}

func reqWith(remoteAddr, xff, xrip string) *http.Request {
	r, _ := http.NewRequest(http.MethodPost, "/login", nil)
	r.RemoteAddr = remoteAddr
	if xff != "" {
		r.Header.Set("X-Forwarded-For", xff)
	}
	if xrip != "" {
		r.Header.Set("X-Real-IP", xrip)
	}
	return r
}

func TestGetIPIgnoresSpoofedHeadersFromUntrustedPeer(t *testing.T) {
	mustSetProxies(t, "loopback")

	r := reqWith("203.0.113.10:1234", "1.2.3.4", "5.6.7.8")
	if got := getIP(r); got != "203.0.113.10" {
		t.Fatalf("getIP = %q, want %q", got, "203.0.113.10")
	}
}

func TestGetIPTrustsHeadersFromLoopback(t *testing.T) {
	mustSetProxies(t, "loopback")

	r := reqWith("127.0.0.1:5678", "203.0.113.10", "")
	if got := getIP(r); got != "203.0.113.10" {
		t.Fatalf("getIP = %q, want %q", got, "203.0.113.10")
	}

	r = reqWith("[::1]:5678", "", "198.51.100.7")
	if got := getIP(r); got != "198.51.100.7" {
		t.Fatalf("getIP = %q, want %q", got, "198.51.100.7")
	}
}

func TestGetIPWalksForwardedChainFromTrustedEdge(t *testing.T) {
	mustSetProxies(t, "loopback, 10.0.0.0/8")

	r := reqWith("127.0.0.1:5678", "192.0.2.1, 203.0.113.10, 10.0.0.4", "")
	if got := getIP(r); got != "203.0.113.10" {
		t.Fatalf("getIP = %q, want %q", got, "203.0.113.10")
	}
}

func TestGetIPSkipsInvalidXFFEntries(t *testing.T) {
	mustSetProxies(t, "*")

	r := reqWith("10.0.0.1:1234", "garbage, 203.0.113.9", "")
	if got := getIP(r); got != "203.0.113.9" {
		t.Fatalf("getIP = %q, want %q", got, "203.0.113.9")
	}

	r = reqWith("10.0.0.1:1234", "garbage", "also-bad")
	if got := getIP(r); got != "10.0.0.1" {
		t.Fatalf("getIP = %q, want %q", got, "10.0.0.1")
	}
}

func TestSetTrustedProxiesRejectsBadInput(t *testing.T) {
	mustSetProxies(t, "loopback")

	if err := SetTrustedProxies("not-an-ip"); err == nil {
		t.Fatal("expected error for invalid IP")
	}
	if err := SetTrustedProxies("10.0.0.0/99"); err == nil {
		t.Fatal("expected error for invalid CIDR")
	}
}
