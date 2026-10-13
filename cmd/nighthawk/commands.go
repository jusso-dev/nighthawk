package main

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jusso-dev/nighthawk/internal/alert"
	"github.com/jusso-dev/nighthawk/internal/audit"
	"github.com/jusso-dev/nighthawk/internal/config"
	"github.com/jusso-dev/nighthawk/internal/cost"
	"github.com/jusso-dev/nighthawk/internal/daemon"
	"github.com/jusso-dev/nighthawk/internal/event"
	"github.com/jusso-dev/nighthawk/internal/herdr"
	"github.com/jusso-dev/nighthawk/internal/ipc"
	"github.com/jusso-dev/nighthawk/internal/judge"
	"github.com/jusso-dev/nighthawk/internal/policy"
	"github.com/jusso-dev/nighthawk/internal/redact"
	"github.com/jusso-dev/nighthawk/internal/store"
)

func cmdDaemon(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("daemon", flag.ContinueOnError)
	fs.SetOutput(errOut)
	dir := stateDir(fs)
	_ = fs.Bool("foreground", false, "run in the foreground")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg, err := loadCfg()
	if err != nil {
		fmt.Fprintln(errOut, "nighthawk: config:", err)
		return 1
	}
	state := resolveState(*dir)
	opt := daemon.Options{StateDir: state, Config: cfg, ListenEgress: true}
	if cfg.Judge.Enabled && cfg.Judge.Endpoint != "" {
		j := &judge.Client{Endpoint: cfg.Judge.Endpoint, Model: cfg.Judge.Model, APIKey: cfg.Judge.APIKey}
		opt.Judge = func(ctx context.Context, ev event.Event, d policy.Decision) policy.Decision {
			raw, _ := json.Marshal(ev.Attrs)
			v, err := j.Judge(ctx, redact.String(string(raw)))
			if err != nil || ctx.Err() != nil {
				return policy.OnError(config.ReadMode(state), err)
			}
			out := judge.Tighten(d, v)
			out.Reason = redact.String(out.Reason)
			return out
		}
	}
	sender := &alert.Sender{
		SlackURL: cfg.Alerts.SlackWebhook, WebhookURL: cfg.Alerts.WebhookURL, Secret: cfg.Alerts.WebhookSecret,
		Notify: func(title, body string) error {
			notifyDesktop(title, body)
			return (herdr.CLI{}).Notify(context.Background(), title, body)
		},
	}
	opt.Alert = func(rule, pane, body string) {
		_ = sender.Send(context.Background(), rule, pane, body)
	}
	err = daemon.Run(context.Background(), opt)
	if err == nil {
		return 0
	}
	if err == daemon.ErrAlreadyRunning {
		fmt.Fprintln(out, "nighthawk: daemon already running")
		return 0
	}
	fmt.Fprintln(errOut, "nighthawk: daemon stopped:", err)
	return 1
}

func cmdMode(args []string, out, errOut io.Writer) int {
	if len(args) < 1 {
		return usage(errOut, "usage: nighthawk mode <get|set> [attended|unattended]")
	}
	state := config.StateDir()
	ctx, cancel := callCtx()
	defer cancel()
	switch args[0] {
	case "get":
		resp, err := daemonCall(ctx, state, ipc.Request{Op: "mode.get"})
		if err != nil {
			fmt.Fprintln(out, config.ReadMode(state))
			return 0
		}
		fmt.Fprintln(out, resp.Mode)
		return 0
	case "set":
		if len(args) < 2 {
			return usage(errOut, "usage: nighthawk mode set <attended|unattended>")
		}
		mode := policy.Mode(args[1])
		if mode != policy.Attended && mode != policy.Unattended {
			return usage(errOut, "usage: nighthawk mode set <attended|unattended>")
		}
		resp, err := daemonCall(ctx, state, ipc.Request{Op: "mode.set", Mode: mode})
		if err != nil {
			if err := config.WriteMode(state, mode); err != nil {
				fmt.Fprintln(errOut, "nighthawk: mode:", err)
				return 1
			}
			fmt.Fprintln(out, mode)
			return 0
		}
		if !resp.OK {
			fmt.Fprintln(errOut, "nighthawk: mode:", resp.Error)
			return 1
		}
		fmt.Fprintln(out, resp.Mode)
		return 0
	default:
		return usage(errOut, "usage: nighthawk mode <get|set>")
	}
}

func cmdConfig(args []string, out, errOut io.Writer) int {
	if len(args) < 1 {
		return usage(errOut, "usage: nighthawk config <check|print>")
	}
	cfg, err := loadCfg()
	if err != nil {
		fmt.Fprintln(errOut, err.Error())
		return 1
	}
	switch args[0] {
	case "check":
		fmt.Fprintln(out, "ok")
		return 0
	case "print":
		b, err := cfg.PrintYAML()
		if err != nil {
			fmt.Fprintln(errOut, "nighthawk: config")
			return 1
		}
		_, _ = out.Write(b)
		return 0
	default:
		return usage(errOut, "usage: nighthawk config <check|print>")
	}
}

func cmdAudit(args []string, out, errOut io.Writer) int {
	if len(args) < 1 {
		return usage(errOut, "usage: nighthawk audit <verify|head|anchor>")
	}
	st, err := store.Open(config.DBPath(config.StateDir()))
	if err != nil {
		fmt.Fprintln(errOut, "nighthawk: store")
		return 1
	}
	defer st.Close()
	au := audit.NewSQLStore(st.DB())
	switch args[0] {
	case "head":
		rec, err := au.Head(st.DB())
		if err != nil {
			fmt.Fprintln(out, "seq=0")
			return 0
		}
		fmt.Fprintf(out, "seq=%d\nhash=%s\n", rec.Seq, rec.Hash)
		return 0
	case "verify":
		var from uint64
		fs := flag.NewFlagSet("verify", flag.ContinueOnError)
		fs.SetOutput(errOut)
		fromFlag := fs.Uint64("from", 0, "first seq")
		anchors := fs.Bool("anchors", false, "also check stored anchors")
		fs.Usage = func() {
			fmt.Fprintln(errOut, "usage: nighthawk audit verify [--from SEQ] [--anchors]")
			fmt.Fprintln(errOut, "exit: 0 ok, 1 read error, 2 usage, 3 chain or anchor broken")
			fs.PrintDefaults()
		}
		if err := fs.Parse(args[1:]); err != nil {
			return 2
		}
		from = *fromFlag
		if seq, err := au.VerifyDB(from); err != nil {
			if errors.Is(err, audit.ErrBroken) {
				fmt.Fprintf(errOut, "nighthawk: audit chain broken at seq %d\n", seq)
				return 3
			}
			fmt.Fprintln(errOut, "nighthawk: audit read:", err)
			return 1
		}
		if *anchors {
			list, err := st.ListAnchors(context.Background())
			if err != nil {
				fmt.Fprintln(errOut, "nighthawk: anchors")
				return 1
			}
			pub := anchorPub()
			for _, a := range list {
				h, err := au.HashAt(st.DB(), a.Seq)
				if err != nil || h != a.Hash || (pub != nil && !audit.CheckAnchor(pub, a.Seq, a.Hash, a.Sig)) {
					fmt.Fprintf(errOut, "nighthawk: anchor mismatch at seq %d\n", a.Seq)
					return 3
				}
			}
		}
		fmt.Fprintln(out, "ok")
		return 0
	case "anchor":
		return cmdAnchor(args[1:], out, errOut, st, au)
	default:
		return usage(errOut, "usage: nighthawk audit <verify|head|anchor>")
	}
}

func cmdAnchor(args []string, out, errOut io.Writer, st *store.Store, au *audit.SQLStore) int {
	fs := flag.NewFlagSet("anchor", flag.ContinueOnError)
	fs.SetOutput(errOut)
	keyPath := fs.String("key", "", "ed25519 private key file, outside the state dir")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *keyPath == "" {
		return usage(errOut, "usage: nighthawk audit anchor --key PATH")
	}
	if !config.OutsideDir(config.StateDir(), *keyPath) {
		fmt.Fprintln(errOut, "nighthawk: anchor key must live outside the state dir")
		return 1
	}
	raw, err := os.ReadFile(*keyPath)
	if err != nil {
		fmt.Fprintln(errOut, "nighthawk: key")
		return 1
	}
	priv, err := parseKey(raw)
	if err != nil {
		fmt.Fprintln(errOut, "nighthawk: key")
		return 1
	}
	rec, err := au.Head(st.DB())
	if err != nil {
		fmt.Fprintln(errOut, "nighthawk: empty audit")
		return 1
	}
	sig := audit.SignAnchor(priv, rec.Seq, rec.Hash)
	if err := st.PutAnchor(context.Background(), store.Anchor{Seq: rec.Seq, Hash: rec.Hash, Target: "file", TS: recTime(), Sig: sig}); err != nil {
		fmt.Fprintln(errOut, "nighthawk: anchor")
		return 1
	}
	fmt.Fprintf(out, "seq=%d\n", rec.Seq)
	return 0
}

func parseKey(raw []byte) (ed25519.PrivateKey, error) {
	return audit.ParsePrivateKey(raw)
}

func anchorPub() ed25519.PublicKey {
	cfg, err := loadCfg()
	if err != nil || cfg.Anchor.KeyPath == "" {
		return nil
	}
	raw, err := os.ReadFile(cfg.Anchor.KeyPath)
	if err != nil {
		return nil
	}
	priv, err := parseKey(raw)
	if err != nil {
		return nil
	}
	pub, _ := priv.Public().(ed25519.PublicKey)
	return pub
}

func recTime() time.Time { return time.Now() }

func cmdHerdrEvent(args []string, errOut io.Writer) int {
	if len(args) < 1 {
		return 0
	}
	ctx, cancel := callCtx()
	defer cancel()
	_, _ = daemonCall(ctx, config.StateDir(), ipc.Request{Op: "herdr.event", Kind: args[0], Pane: os.Getenv("HERDR_PANE_ID")})
	return 0
}

func cmdSpend(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("spend", flag.ContinueOnError)
	fs.SetOutput(errOut)
	by := fs.String("by", "model", "agent|session|task|model")
	since := fs.String("since", "24h", "window")
	asJSON := fs.Bool("json", false, "json output")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	ctx, cancel := callCtx()
	defer cancel()
	resp, err := daemonCall(ctx, config.StateDir(), ipc.Request{Op: "spend", By: *by, Since: *since})
	if callFailed(errOut, resp, err) {
		return 1
	}
	if *asJSON {
		_, _ = out.Write(append(resp.Data, '\n'))
		return 0
	}
	var lines []cost.Line
	_ = json.Unmarshal(resp.Data, &lines)
	if len(lines) == 0 {
		fmt.Fprintln(out, "no usage")
		return 0
	}
	for _, l := range lines {
		price := strconv.FormatFloat(l.USD, 'f', 4, 64)
		if l.Unpriced {
			price = "unpriced"
		}
		fmt.Fprintf(out, "%s\t%s\t%s\n", l.Key, l.Model, price)
	}
	return 0
}

func cmdSkills(out, errOut io.Writer) int {
	return printOp(out, errOut, ipc.Request{Op: "skills.list"})
}

func cmdDoctor(out, errOut io.Writer) int {
	return printOp(out, errOut, ipc.Request{Op: "doctor"})
}

func cmdEnv(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("env", flag.ContinueOnError)
	fs.SetOutput(errOut)
	pane := fs.String("pane", os.Getenv("HERDR_PANE_ID"), "pane id")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *pane == "" {
		return usage(errOut, "usage: nighthawk env --pane ID")
	}
	return printOp(out, errOut, ipc.Request{Op: "env", Pane: *pane})
}

func cmdStatus(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(errOut)
	asJSON := fs.Bool("json", false, "json")
	hosts := fs.String("hosts", os.Getenv("NIGHTHAWK_CLUSTER_HOSTS"), "comma-separated ssh hosts")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*hosts) == "" {
		_ = asJSON
		return printOp(out, errOut, ipc.Request{Op: "status"})
	}
	return cmdStatusCluster(out, errOut, *hosts)
}

func cmdResolve(op string, args []string, out, errOut io.Writer) int {
	if len(args) < 1 {
		return usage(errOut, "usage: nighthawk "+op+" <approval-id>")
	}
	status := "denied"
	if op == "approve" {
		status = "approved"
	}
	ctx, cancel := callCtx()
	defer cancel()
	resp, err := daemonCall(ctx, config.StateDir(), ipc.Request{Op: "approval.resolve", ID: args[0], Kind: status, Who: "cli"})
	if callFailed(errOut, resp, err) {
		return 1
	}
	fmt.Fprintln(out, status)
	return 0
}

func cmdPolicy(args []string, in io.Reader, out, errOut io.Writer) int {
	if len(args) < 1 {
		return usage(errOut, "usage: nighthawk policy <test|explain>")
	}
	cfg, err := loadCfg()
	if err != nil {
		fmt.Fprintln(errOut, "nighthawk: config")
		return 1
	}
	eng, err := policy.NewReloader(cfg.PolicyFiles)
	if err != nil {
		fmt.Fprintln(errOut, "nighthawk: policy")
		return 1
	}
	switch args[0] {
	case "explain":
		if len(args) < 2 {
			return usage(errOut, "usage: nighthawk policy explain <rule>")
		}
		rule, ok := eng.Engine().Explain(args[1])
		if !ok {
			fmt.Fprintln(errOut, "nighthawk: no such rule")
			return 1
		}
		return printJSON(out, rule)
	case "test":
		body, err := io.ReadAll(io.LimitReader(in, 1<<20))
		if err != nil {
			return 1
		}
		var ev event.Event
		if err := json.Unmarshal(body, &ev); err != nil {
			fmt.Fprintln(errOut, "nighthawk: event json")
			return 1
		}
		mode := config.ReadMode(config.StateDir())
		dec, err := eng.Engine().Evaluate(ev, mode)
		if err != nil {
			dec = policy.OnError(mode, err)
		}
		return printJSON(out, dec)
	default:
		return usage(errOut, "usage: nighthawk policy <test|explain>")
	}
}

func printOp(out, errOut io.Writer, req ipc.Request) int {
	ctx, cancel := callCtx()
	defer cancel()
	resp, err := daemonCall(ctx, config.StateDir(), req)
	if callFailed(errOut, resp, err) {
		return 1
	}
	if len(resp.Data) == 0 {
		fmt.Fprintln(out, "ok")
		return 0
	}
	_, _ = out.Write(append(resp.Data, '\n'))
	return 0
}

func ipcRequestSubmit(ev event.Event) ipc.Request {
	return ipc.Request{Op: "submit", Event: &ev}
}
