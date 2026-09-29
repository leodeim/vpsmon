package api

import (
	"fmt"
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

func TestDirectClientCannotEvadeRateLimitWithForwardedHeaders(t *testing.T) {
	mustSetProxies(t, "loopback")
	const clientIP = "203.0.113.77"
	loginMu.Lock()
	delete(loginAttempts, clientIP)
	loginMu.Unlock()
	t.Cleanup(func() {
		loginMu.Lock()
		delete(loginAttempts, clientIP)
		loginMu.Unlock()
	})

	for i := 0; i < 6; i++ {
		r := reqWith(clientIP+":1234", fmt.Sprintf("198.51.100.%d", i+1), "")
		if got, want := rateLimit(getIP(r)), i < 5; got != want {
			t.Fatalf("attempt %d allowed = %t, want %t", i+1, got, want)
		}
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

func TestGetIPRequiresLoopbackInCustomProxyList(t *testing.T) {
	mustSetProxies(t, "10.0.0.4")

	r := reqWith("10.0.0.4:5678", "198.51.100.7", "")
	if got := getIP(r); got != "198.51.100.7" {
		t.Fatalf("getIP from trusted proxy = %q, want %q", got, "198.51.100.7")
	}

	r = reqWith("127.0.0.1:5678", "198.51.100.7", "")
	if got := getIP(r); got != "127.0.0.1" {
		t.Fatalf("getIP from loopback not in proxy list = %q, want %q", got, "127.0.0.1")
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
