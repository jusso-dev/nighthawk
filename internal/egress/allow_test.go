package egress

import (
	"net"
	"testing"
)

func TestHostMatch(t *testing.T) {
	a := Allow{Hosts: []string{"github.com", "*.anthropic.com", "10.0.0.0/8"}}
	if !a.HostMatch("GitHub.com") || !a.HostMatch("api.anthropic.com") || !a.HostMatch("anthropic.com") {
		t.Fatal("listed")
	}
	if a.HostMatch("evil.example") || a.HostMatch("notanthropic.com") {
		t.Fatal("unlisted")
	}
	if !a.HostMatch("10.1.2.3") {
		t.Fatal("cidr")
	}
}

func TestPrivateRejected(t *testing.T) {
	a := Allow{
		Hosts:   []string{"github.com", "127.0.0.1"},
		Resolve: func(string) ([]net.IP, error) { return []net.IP{net.ParseIP("10.1.1.1")}, nil },
	}
	if a.Allows("github.com", 443) {
		t.Fatal("rebinding")
	}
	a.AllowPrivate = []string{"10.1.1.1/32"}
	if !a.Allows("github.com", 443) {
		t.Fatal("explicit private")
	}
	if a.Allows("127.0.0.1", 80) {
		t.Fatal("loopback")
	}
	a.AllowPrivate = append(a.AllowPrivate, "127.0.0.1/32")
	if !a.Allows("127.0.0.1", 80) {
		t.Fatal("loopback allowed")
	}
}
