package redact

var builtin = []Rule{
	{Name: "aws", Re: `AKIA[0-9A-Z]{16}`, Repl: Mask},
	{Name: "github", Re: `gh[pousr]_[A-Za-z0-9]{36,}`, Repl: Mask},
	{Name: "anthropic", Re: `sk-ant-[A-Za-z0-9_\-]{20,}`, Repl: Mask},
	{Name: "openai", Re: `sk-[A-Za-z0-9_\-]{20,}`, Repl: Mask},
	{Name: "xai", Re: `xai-[A-Za-z0-9_\-]{20,}`, Repl: Mask},
	{Name: "slack", Re: `xox[baprs]-[A-Za-z0-9-]{10,}`, Repl: Mask},
	{Name: "stripe", Re: `(?:sk|rk|pk)_(?:live|test)_[A-Za-z0-9]{16,}`, Repl: Mask},
	{Name: "google", Re: `AIza[0-9A-Za-z_\-]{20,}`, Repl: Mask},
	{Name: "jwt", Re: `eyJ[A-Za-z0-9_\-]{8,}\.[A-Za-z0-9_\-]{8,}\.[A-Za-z0-9_\-]{8,}`, Repl: Mask},
	{Name: "pem", Re: `(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`, Repl: Mask},
	{Name: "bearer", Re: `(?i)(authorization:\s*bearer\s+)[A-Za-z0-9._\-]+`, Repl: "${1}" + Mask},
	{Name: "urlpass", Re: `(?i)(://[^/\s:]+:)[^@\s/]+(@)`, Repl: "${1}" + Mask + "${2}"},
	{Name: "password", Re: `(?i)(password=)[^&\s]+`, Repl: "${1}" + Mask},
}
