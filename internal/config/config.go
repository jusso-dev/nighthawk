package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Duration is a YAML time.Duration.
type Duration time.Duration

func (d Duration) MarshalYAML() (any, error) {
	return time.Duration(d).String(), nil
}

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return err
	}
	if s == "" {
		return nil
	}
	p, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("line %d: %w", value.Line, err)
	}
	*d = Duration(p)
	return nil
}

func (d Duration) D() time.Duration { return time.Duration(d) }

// SpendLimit is a budget matcher stored for callers that read config.
type SpendLimit struct {
	Scope string  `yaml:"scope"`
	USD   float64 `yaml:"usd"`
}

// Config is ~/.config/nighthawk/config.yaml.
type Config struct {
	ModeDefault     string   `yaml:"mode_default"`
	DecisionTimeout Duration `yaml:"decision_timeout"`
	PolicyFiles     []string `yaml:"policy_files"`
	PricesFile      string   `yaml:"prices_file"`
	CurrencyRateAUD float64  `yaml:"currency_rate_aud"`
	Egress          Egress   `yaml:"egress"`
	Alerts          Alerts   `yaml:"alerts"`
	OTEL            OTEL     `yaml:"otel"`
	Redact          Redact   `yaml:"redact"`
	Anchor          Anchor   `yaml:"anchor"`
	Judge           Judge    `yaml:"judge"`
	SIEM            SIEM     `yaml:"siem"`
	Web             Web      `yaml:"web"`
	MCPHTTP         MCPHTTP  `yaml:"mcp_http"`
	LinuxEgress     bool     `yaml:"linux_egress"`
}

type Egress struct {
	Allowlist    []string `yaml:"allowlist"`
	AllowPrivate []string `yaml:"allow_private"`
	Port         int      `yaml:"port"`
}

type Alerts struct {
	SlackWebhook  string `yaml:"slack_webhook"`
	WebhookURL    string `yaml:"webhook_url"`
	WebhookSecret string `yaml:"webhook_secret"`
}

type OTEL struct {
	Endpoint string `yaml:"endpoint"`
	Protocol string `yaml:"protocol"`
}

type Redact struct {
	Extra []string `yaml:"extra"`
}

type Anchor struct {
	KeyPath    string   `yaml:"key_path"`
	WebhookURL string   `yaml:"webhook_url"`
	File       string   `yaml:"file"`
	GitRepo    string   `yaml:"git_repo"`
	Interval   Duration `yaml:"interval"`
}

type Judge struct {
	Enabled  bool   `yaml:"enabled"`
	Endpoint string `yaml:"endpoint"`
	Model    string `yaml:"model"`
	APIKey   string `yaml:"api_key"`
}

type SIEM struct {
	Target     string `yaml:"target"`
	Format     string `yaml:"format"`
	SyslogAddr string `yaml:"syslog_addr"`
	WebhookURL string `yaml:"webhook_url"`
}

type Web struct {
	Addr string `yaml:"addr"`
}

type MCPHTTP struct {
	Addr string `yaml:"addr"`
}

// Default returns documented defaults. A missing file means these.
func Default() Config {
	return Config{
		ModeDefault:     "attended",
		DecisionTimeout: Duration(2 * time.Second),
		Egress: Egress{
			Port: 8765,
			Allowlist: []string{
				"api.anthropic.com",
				"api.openai.com",
				"api.x.ai",
				"claude.ai",
				"github.com",
				"api.github.com",
				"registry.npmjs.org",
				"pypi.org",
				"files.pythonhosted.org",
				"proxy.golang.org",
				"sum.golang.org",
			},
		},
		OTEL:   OTEL{Protocol: "http"},
		Anchor: Anchor{Interval: Duration(time.Hour)},
		Web:    Web{Addr: "127.0.0.1:8766"},
		SIEM:   SIEM{Format: "ndjson"},
	}
}

// Load reads path. A missing file returns defaults and a nil error.
// Unknown keys fail with a line number. env:NAME values resolve at load.
func Load(path string) (Config, error) {
	cfg := Default()
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return Config{}, err
	}
	dec := yaml.NewDecoder(strings.NewReader(string(b)))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, err
	}
	if cfg.ModeDefault == "" {
		cfg.ModeDefault = "attended"
	}
	if cfg.DecisionTimeout == 0 {
		cfg.DecisionTimeout = Duration(2 * time.Second)
	}
	if cfg.Egress.Port == 0 {
		cfg.Egress.Port = 8765
	}
	if cfg.Anchor.Interval == 0 {
		cfg.Anchor.Interval = Duration(time.Hour)
	}
	if cfg.Web.Addr == "" {
		cfg.Web.Addr = "127.0.0.1:8766"
	}
	if cfg.OTEL.Protocol == "" {
		cfg.OTEL.Protocol = "http"
	}
	cfg.resolveSecrets()
	return cfg, nil
}

func (c *Config) resolveSecrets() {
	c.Alerts.SlackWebhook = resolveEnv(c.Alerts.SlackWebhook)
	c.Alerts.WebhookURL = resolveEnv(c.Alerts.WebhookURL)
	c.Alerts.WebhookSecret = resolveEnv(c.Alerts.WebhookSecret)
	c.Anchor.WebhookURL = resolveEnv(c.Anchor.WebhookURL)
	c.Judge.APIKey = resolveEnv(c.Judge.APIKey)
	c.Judge.Endpoint = resolveEnv(c.Judge.Endpoint)
	c.SIEM.WebhookURL = resolveEnv(c.SIEM.WebhookURL)
	c.OTEL.Endpoint = resolveEnv(c.OTEL.Endpoint)
}

func resolveEnv(s string) string {
	if !strings.HasPrefix(s, "env:") {
		return s
	}
	return os.Getenv(strings.TrimPrefix(s, "env:"))
}

// RedactedCopy returns cfg with secret fields masked for config print.
func (c Config) RedactedCopy() Config {
	out := c
	out.Alerts.SlackWebhook = mask(c.Alerts.SlackWebhook)
	out.Alerts.WebhookURL = mask(c.Alerts.WebhookURL)
	out.Alerts.WebhookSecret = mask(c.Alerts.WebhookSecret)
	out.Anchor.WebhookURL = mask(c.Anchor.WebhookURL)
	out.Judge.APIKey = mask(c.Judge.APIKey)
	out.SIEM.WebhookURL = mask(c.SIEM.WebhookURL)
	return out
}

func mask(s string) string {
	if s == "" {
		return ""
	}
	return "[REDACTED]"
}

// PrintYAML marshals the redacted config.
func (c Config) PrintYAML() ([]byte, error) {
	return yaml.Marshal(c.RedactedCopy())
}
