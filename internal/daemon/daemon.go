// Package daemon is `waker serve`: it builds every app from the config,
// binds each app's listener and the control socket, boots on-mode apps and
// runs the idle loop until its context ends.
package daemon

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/fum4/waker/internal/app"
	"github.com/fum4/waker/internal/boot"
	"github.com/fum4/waker/internal/clock"
	"github.com/fum4/waker/internal/config"
	"github.com/fum4/waker/internal/control"
	"github.com/fum4/waker/internal/idle"
	"github.com/fum4/waker/internal/pages"
	"github.com/fum4/waker/internal/proxy"
	"github.com/fum4/waker/internal/state"
)

// ShutdownTimeout bounds draining visitor connections on exit.
const ShutdownTimeout = 5 * time.Second

// Daemon is a running waker.
type Daemon struct {
	cfg   *config.Config
	log   *slog.Logger
	pages *pages.Pages
	sig   idle.Signal
	all   []*app.App

	mu        sync.Mutex
	overrides map[string]state.Override
	listening []*app.App // apps whose listener bound, in config order
	skipped   map[string]string
}

// New builds the apps; it binds nothing yet.
func New(cfg *config.Config, log *slog.Logger) (*Daemon, error) {
	pg, err := pages.New(cfg.Branding)
	if err != nil {
		return nil, err
	}
	d := &Daemon{cfg: cfg, log: log, pages: pg, sig: idle.NewSignal(), skipped: map[string]string{}}
	d.overrides = d.loadOverrides()
	for _, ac := range cfg.Apps {
		var o *state.Override
		if v, ok := d.overrides[ac.Name]; ok {
			o = &v
		}
		d.all = append(d.all, app.New(ac, app.Deps{
			Clock:   clock.Real{},
			Process: boot.New(ac),
			Notify:  d.sig.Notify,
			Persist: d.persist,
			Log:     log,
		}, o))
	}
	return d, nil
}

// Run serves until ctx ends, then drains connections and stops every app
// this waker started (its children would otherwise be orphaned).
func (d *Daemon) Run(ctx context.Context) error {
	servers := d.listen()
	if len(servers) == 0 {
		return errors.New("no app listener could bind")
	}
	ctl, err := control.Listen(d.cfg.ControlSocket)
	if err != nil {
		return err
	}
	ctlSrv := &http.Server{Handler: control.Handler(d, d.log), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = ctlSrv.Serve(ctl) }()
	servers = append(servers, ctlSrv)
	d.log.Info("waker serving", "apps", len(d.listening), "control", d.cfg.ControlSocket)

	for _, a := range d.listening {
		if a.Mode() == config.ModeOn {
			a.Wake(ctx)
		}
	}
	idle.Loop(ctx, d.sig, d.step)

	shutdown, cancel := context.WithTimeout(context.Background(), ShutdownTimeout)
	defer cancel()
	for _, s := range servers {
		_ = s.Shutdown(shutdown)
	}
	for _, a := range d.listening {
		a.Wait()
		if a.Started() {
			a.Stop("waker shutting down")
		}
	}
	return nil
}

func (d *Daemon) listen() []*http.Server {
	var servers []*http.Server
	for _, a := range d.all {
		ac := a.Config()
		ln, err := net.Listen("tcp", ac.Listen)
		if err != nil {
			d.log.Warn("listener skipped: the app stays unmanaged", "app", ac.Name, "err", err)
			d.skipped[ac.Name] = err.Error()
			continue
		}
		h, err := proxy.New(a, d.pages, d.log)
		if err != nil {
			ln.Close()
			d.skipped[ac.Name] = err.Error()
			continue
		}
		srv := &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
		go func() { _ = srv.Serve(proxy.LimitListener(ln, proxy.MaxConnections)) }()
		servers = append(servers, srv)
		d.listening = append(d.listening, a)
	}
	return servers
}

// step stops whatever is due and returns the time to the next deadline.
func (d *Daemon) step() time.Duration {
	next := idle.MaxSleep
	for _, a := range d.listening {
		func() {
			defer func() {
				if r := recover(); r != nil {
					d.log.Error("idle tick crashed", "app", a.Name(), "panic", r)
				}
			}()
			a.Tick()
		}()
		if left, ok := a.DeadlineIn(); ok {
			next = min(next, left)
		}
	}
	return next
}
