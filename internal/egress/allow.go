// Package egress is the local HTTP and CONNECT proxy.
package egress

import (
	"net"
	"strings"
)

// Allow is the host allowlist. Hosts may be exact names, *.suffix, or CIDRs.
type Allow struct {
	Hosts        []string
	AllowPrivate []string
	Resolve      func(host string) ([]net.IP, error)
}

// HostMatch reports whether host is listed. It does not resolve DNS.
func (a Allow) HostMatch(host string) bool {
	host = normHost(host)
	if host == "" {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		for _, h := range a.Hosts {
			if _, n, err := net.ParseCIDR(h); err == nil && n.Contains(ip) {
				return true
			}
			if normHost(h) == host {
				return true
			}
		}
		return false
	}
	for _, h := range a.Hosts {
		h = strings.ToLower(strings.TrimSpace(h))
		if strings.HasPrefix(h, "*.") {
			suf := h[1:]
			base := h[2:]
			if host == base || strings.HasSuffix(host, suf) {
				return true
			}
			continue
		}
		if _, _, err := net.ParseCIDR(h); err == nil {
			continue
		}
		if normHost(h) == host {
			return true
		}
	}
	return false
}

// Allows reports whether host may be contacted. Listed hosts still fail when
// every resolved address is private and not covered by AllowPrivate.
func (a Allow) Allows(host string, port int) bool {
	_ = port
	host = normHost(host)
	if !a.HostMatch(host) {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return a.ipOK(ip)
	}
	ips, err := a.lookup(host)
	if err != nil || len(ips) == 0 {
		return false
	}
	for _, ip := range ips {
		if !a.ipOK(ip) {
			return false
		}
	}
	return true
}

func (a Allow) lookup(host string) ([]net.IP, error) {
	if a.Resolve != nil {
		return a.Resolve(host)
	}
	names, err := net.LookupHost(host)
	if err != nil {
		return nil, err
	}
	var out []net.IP
	for _, n := range names {
		if ip := net.ParseIP(n); ip != nil {
			out = append(out, ip)
		}
	}
	return out, nil
}

func (a Allow) ipOK(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if !isPrivate(ip) {
		return true
	}
	for _, c := range a.AllowPrivate {
		c = strings.TrimSpace(c)
		if _, n, err := net.ParseCIDR(c); err == nil && n.Contains(ip) {
			return true
		}
		if net.ParseIP(c) != nil && net.ParseIP(c).Equal(ip) {
			return true
		}
	}
	return false
}

func isPrivate(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return true
	}
	return false
}

func normHost(h string) string {
	h = strings.TrimSpace(strings.ToLower(h))
	h = strings.TrimSuffix(h, ".")
	h = strings.Trim(h, "[]")
	return h
}
