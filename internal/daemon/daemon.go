// Package daemon is the Nighthawk composition root.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/jusso-dev/nighthawk/internal/audit"
	"github.com/jusso-dev/nighthawk/internal/config"
	"github.com/jusso-dev/nighthawk/internal/cost"
	"github.com/jusso-dev/nighthawk/internal/detect"
	"github.com/jusso-dev/nighthawk/internal/egress"
	"github.com/jusso-dev/nighthawk/internal/enforce"
	"github.com/jusso-dev/nighthawk/internal/event"
	"github.com/jusso-dev/nighthawk/internal/herdr"
	"github.com/jusso-dev/nighthawk/internal/ipc"
	"github.com/jusso-dev/nighthawk/internal/policy"
	"github.com/jusso-dev/nighthawk/internal/sessions"
	"github.com/jusso-dev/nighthawk/internal/store"
	"golang.org/x/sys/unix"
)

// ErrAlreadyRunning is returned when another daemon holds the lock.
var ErrAlreadyRunning = errors.New("daemon already running")

// Options configures one daemon process.
type Options struct {
	StateDir     string
	Config       config.Config
	ListenEgress bool
	// Judge may only tighten a decision. Nil disables the judge.
	Judge func(ctx context.Context, e event.Event, d policy.Decision) policy.Decision
	// Alert is called for any decision other than allow. body is a rule id.
	Alert func(rule, pane, body string)
}

// Server handles IPC.
type Server struct {
	opt      Options
	st       *store.Store
	au       *audit.SQLStore
	pol      *policy.Reloader
	mode     policy.Mode
	mu       sync.Mutex
	evalMu   sync.Mutex
	loops    *detect.Loop
	sessions *sessions.Map
	dedupe   *detect.Deduper
	acts     *enforce.Actions
	prices   *cost.Table
	allow    egress.Allow
	tokens   map[string]string
	host     string
	run      context.Context
	subWake  chan struct{}
	trMu     sync.Mutex
	trs      map[string]*transcriptTail
}

// Run holds the flock, serves the socket, and returns when ctx ends.
// A second process returns ErrAlreadyRunning.
func Run(ctx context.Context, opt Options) error {
	if opt.StateDir == "" {
		opt.StateDir = config.StateDir()
	}
	if err := os.MkdirAll(opt.StateDir, 0o700); err != nil {
		return err
	}
	lock, err := lockFile(filepath.Join(opt.StateDir, "daemon.lock"))
	if err != nil {
		return err
	}
	defer lock.Close()
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	s, err := openServer(opt)
	if err != nil {
		return err
	}
	defer s.st.Close()
	pidPath := filepath.Join(opt.StateDir, "daemon.pid")
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o600); err != nil {
		return err
	}
	defer os.Remove(pidPath)
	if opt.ListenEgress && opt.Config.Egress.Port > 0 {
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", opt.Config.Egress.Port))
		if err != nil {
			return err
		}
		go s.proxy().Serve(ctx, ln)
	}
	s.run = ctx
	go s.follow(ctx)
	go s.poll(ctx)
	go s.anchorLoop(ctx)
	go s.sideLoops(ctx)
	return ipc.Serve(ctx, config.SocketPath(opt.StateDir), s)
}

func lockFile(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if err != nil {
		f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, ErrAlreadyRunning
		}
		return nil, err
	}
	return f, nil
}

func openServer(opt Options) (*Server, error) {
	if opt.Config.ModeDefault == "" {
		opt.Config = config.Default()
	}
	st, err := store.Open(config.DBPath(opt.StateDir))
	if err != nil {
		return nil, err
	}
	pol, err := policy.NewReloader(opt.Config.PolicyFiles)
	if err != nil {
		st.Close()
		return nil, err
	}
	prices, err := cost.Load(opt.Config.PricesFile)
	if err != nil {
		st.Close()
		return nil, err
	}
	host, _ := os.Hostname()
	s := &Server{
		opt: opt, st: st, au: audit.NewSQLStore(st.DB()), pol: pol,
		mode: config.ReadMode(opt.StateDir), loops: detect.NewLoop(),
		sessions: sessions.NewMap(), dedupe: detect.NewDeduper(),
		prices: prices, host: host, tokens: map[string]string{},
		subWake: make(chan struct{}, 1),
		allow:   egress.Allow{Hosts: opt.Config.Egress.Allowlist, AllowPrivate: opt.Config.Egress.AllowPrivate},
	}
	if s.mode != policy.Unattended {
		s.mode = policy.Attended
	}
	s.acts = &enforce.Actions{H: herdr.CLI{}, Audit: s.auditRaw}
	return s, nil
}

func (s *Server) poll(ctx context.Context) {
	if len(s.opt.Config.PolicyFiles) == 0 {
		return
	}
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.pol.Poll()
		}
	}
}

func (s *Server) currentMode() policy.Mode {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mode
}

func (s *Server) proxy() *egress.Proxy {
	return &egress.Proxy{
		Allow: s.allow,
		Pane:  s.paneForToken,
		Decide: func(ctx context.Context, pane, host string, port int) (policy.Decision, error) {
			d, _ := s.decide(ctx, event.Event{
				Kind: event.KindNetwork, Source: event.SourceEgress, PaneID: pane, Pre: true,
				Attrs: map[string]string{"host": host, "port": strconv.Itoa(port)},
			})
			return d, nil
		},
		OnEvent: func(ev event.Event) { _, _ = s.decide(context.Background(), ev) },
	}
}
