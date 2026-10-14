package redact

// Hint holds lowercase literals. A rule runs only when one appears in the
// lowercased input, so large clean text skips the regex engine.
var builtin = []Rule{
	{Name: "aws", Re: `AKIA[0-9A-Z]{16}`, Repl: Mask, Hint: []string{"akia"}},
	{Name: "github", Re: `gh[pousr]_[A-Za-z0-9]{36,}`, Repl: Mask, Hint: []string{"ghp_", "gho_", "ghu_", "ghs_", "ghr_"}},
	{Name: "anthropic", Re: `sk-ant-[A-Za-z0-9_\-]{20,}`, Repl: Mask, Hint: []string{"sk-ant-"}},
	{Name: "openai", Re: `sk-[A-Za-z0-9_\-]{20,}`, Repl: Mask, Hint: []string{"sk-"}},
	{Name: "xai", Re: `xai-[A-Za-z0-9_\-]{20,}`, Repl: Mask, Hint: []string{"xai-"}},
	{Name: "slack", Re: `xox[baprs]-[A-Za-z0-9-]{10,}`, Repl: Mask, Hint: []string{"xox"}},
	{Name: "stripe", Re: `(?:sk|rk|pk)_(?:live|test)_[A-Za-z0-9]{16,}`, Repl: Mask, Hint: []string{"_live_", "_test_"}},
	{Name: "google", Re: `AIza[0-9A-Za-z_\-]{20,}`, Repl: Mask, Hint: []string{"aiza"}},
	{Name: "jwt", Re: `eyJ[A-Za-z0-9_\-]{8,}\.[A-Za-z0-9_\-]{8,}\.[A-Za-z0-9_\-]{8,}`, Repl: Mask, Hint: []string{"eyj"}},
	{Name: "pem", Re: `(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`, Repl: Mask, Hint: []string{"private key"}},
	{Name: "bearer", Re: `(?i)(authorization:\s*bearer\s+)[A-Za-z0-9._\-]+`, Repl: "${1}" + Mask, Hint: []string{"bearer"}},
	{Name: "urlpass", Re: `(?i)(://[^/\s:]+:)[^@\s/]+(@)`, Repl: "${1}" + Mask + "${2}", Hint: []string{"://"}},
	{Name: "password", Re: `(?i)(password=)[^&\s]+`, Repl: "${1}" + Mask, Hint: []string{"password="}},
}
