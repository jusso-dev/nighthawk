# Linux egress

macOS has no supported way to force every agent through the proxy without a Network Extension. That is out of scope. On Linux, root can install an nftables redirect for one cgroup.

The ruleset is `table inet nighthawk`. It matches `socket cgroupv2` and redirects TCP and UDP to the local proxy port 8765.

`internal/egress/linux.Table` returns that text on every OS so it can be reviewed without root. `Apply` and `Disable` run `nft` only when the process is root on Linux. `Disable` deletes `table inet nighthawk`. Tests that install the table are built with `linux && linux_root` and are not part of the default `go test` run.

Set `linux_egress: true` in config only on a host where this redirect is intentional. The proxy still applies the allowlist. The redirect does not intercept traffic that never enters that cgroup.
