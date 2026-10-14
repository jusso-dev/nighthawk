// Package linux builds the nftables egress redirect. Apply runs only as root on Linux.
package linux

import "fmt"

// Table is the inet nighthawk ruleset. cgroup is a cgroup v2 path.
func Table(cgroup string) string {
	return fmt.Sprintf(`table inet nighthawk {
	chain output {
		type filter hook output priority 0; policy accept;
		socket cgroupv2 level 2 "%s" meta l4proto { tcp, udp } redirect to :8765
	}
}
`, cgroup)
}
