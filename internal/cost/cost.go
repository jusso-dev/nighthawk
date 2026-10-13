// Package cost prices token usage. Amounts are USD per million tokens.
package cost

import (
	_ "embed"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

//go:embed prices.yaml
var embedded []byte

// Usage is one grouped token row.
type Usage struct {
	Key        string
	Model      string
	Input      int64
	Output     int64
	CacheRead  int64
	CacheWrite int64
}

// Line is a priced row. Unpriced is set when the model is not in the table.
type Line struct {
	Key        string  `json:"key"`
	Model      string  `json:"model"`
	Input      int64   `json:"input"`
	Output     int64   `json:"output"`
	CacheRead  int64   `json:"cache_read"`
	CacheWrite int64   `json:"cache_write"`
	USD        float64 `json:"usd"`
	AUD        float64 `json:"aud,omitempty"`
	Unpriced   bool    `json:"unpriced,omitempty"`
	Source     string  `json:"source,omitempty"`
}

type price struct {
	Model         string  `yaml:"model"`
	Input         float64 `yaml:"input"`
	Output        float64 `yaml:"output"`
	CacheRead     float64 `yaml:"cache_read"`
	CacheWrite    float64 `yaml:"cache_write"`
	EffectiveFrom string  `yaml:"effective_from"`
	Source        string  `yaml:"source"`
	from          time.Time
}

type file struct {
	Prices []price `yaml:"prices"`
}

// Table is a price list.
type Table struct{ rows []price }

// Embedded returns the built-in price list.
func Embedded() *Table {
	t, err := Parse(embedded)
	if err != nil {
		panic(err)
	}
	return t
}

// Load reads path. A missing file uses the embedded list.
func Load(path string) (*Table, error) {
	if path == "" {
		return Embedded(), nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Embedded(), nil
		}
		return nil, err
	}
	return Parse(b)
}

// Parse decodes a prices file.
func Parse(b []byte) (*Table, error) {
	var f file
	if err := yaml.Unmarshal(b, &f); err != nil {
		return nil, err
	}
	for i := range f.Prices {
		if f.Prices[i].EffectiveFrom == "" {
			continue
		}
		tm, err := time.Parse("2006-01-02", f.Prices[i].EffectiveFrom)
		if err != nil {
			return nil, err
		}
		f.Prices[i].from = tm
	}
	return &Table{rows: f.Prices}, nil
}

// Line prices one usage row. audRate 0 skips AUD.
func (t *Table) Line(u Usage, at time.Time, audRate float64) Line {
	line := Line{
		Key: u.Key, Model: u.Model, Input: u.Input, Output: u.Output,
		CacheRead: u.CacheRead, CacheWrite: u.CacheWrite,
	}
	p, ok := t.find(u.Model, at)
	if !ok {
		line.Unpriced = true
		return line
	}
	line.Source = p.Source
	line.USD = (float64(u.Input)*p.Input + float64(u.Output)*p.Output + float64(u.CacheRead)*p.CacheRead + float64(u.CacheWrite)*p.CacheWrite) / 1e6
	if audRate > 0 {
		line.AUD = line.USD * audRate
	}
	return line
}

func (t *Table) find(model string, at time.Time) (price, bool) {
	if t == nil || model == "" {
		return price{}, false
	}
	var best price
	ok := false
	for _, p := range t.rows {
		if !p.from.IsZero() && at.Before(p.from) {
			continue
		}
		if model != p.Model && !strings.HasPrefix(model, p.Model) {
			continue
		}
		if !ok || len(p.Model) > len(best.Model) {
			best = p
			ok = true
		}
	}
	return best, ok
}

// TaskID returns an explicit task, otherwise an agent name that starts with hsc-.
func TaskID(agent, explicit string) string {
	if explicit != "" {
		return explicit
	}
	if strings.HasPrefix(agent, "hsc-") {
		return agent
	}
	return ""
}
