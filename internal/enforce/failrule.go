package enforce

import "github.com/jusso-dev/nighthawk/internal/policy"

// Fallback is ADR 0004. Unattended denies. Attended allows and warns.
func Fallback(mode policy.Mode, err error) policy.Decision {
	if mode == policy.Unattended {
		return policy.OnError(mode, err)
	}
	return policy.Decision{Action: policy.Allow, RuleID: "nh.failopen", Reason: "daemon unreachable"}
}
