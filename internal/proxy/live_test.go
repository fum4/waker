package proxy

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fum4/waker/internal/app"
	"github.com/fum4/waker/internal/clock"
	"github.com/fum4/waker/internal/config"
	"github.com/fum4/waker/internal/pages"
	"github.com/fum4/waker/internal/testapp"
)

// liveProcess "starts" the app by serving testapp on the upstream address.
// With ready set, the app only comes up once ready is closed (a slow boot);
// with fail set, the start command fails.
type liveProcess struct {
	addr   string
	starts atomic.Int32
	mu     sync.Mutex
	srv    *http.Server
	ready  chan struct{}
	fail   bool
}

func (p *liveProcess) Start() (<-chan error, error) {
	p.starts.Add(1)
	if p.fail {
		return nil, errors.New("start failed")
	}
	if p.ready != nil {
		<-p.ready
	}
	ln, err := net.Listen("tcp", p.addr)
	if err != nil {
		return nil, err
	}
	srv := &http.Server{Handler: testapp.Handler()}
	p.mu.Lock()
	p.srv = srv
	p.mu.Unlock()
	go func() { _ = srv.Serve(ln) }()
	return make(chan error), nil
}

func (p *liveProcess) Healthy(ctx context.Context) bool {
	c, err := net.DialTimeout("tcp", p.addr, time.Second)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

func (p *liveProcess) Stop(context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.srv != nil {
		_ = p.srv.Close()
		p.srv = nil
	}
	return nil
}

type live struct {
	url  string
	addr string
	proc *liveProcess
	app  *app.App
}

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

func newLive(t *testing.T, mutate func(*config.App)) *live {
	t.Helper()
	return newLiveProc(t, mutate, func(*liveProcess) {})
}

func newLiveProc(t *testing.T, mutate func(*config.App), setup func(*liveProcess)) *live {
	t.Helper()
	upstream := freeAddr(t)
	cfg, err := config.Parse([]byte("apps: [{name: shop, listen: '127.0.0.1:1', upstream: 'http://" + upstream + "', start: [x], idle_ignore: [/poll]}]"))
	if err != nil {
		t.Fatal(err)
	}
	ac := cfg.Apps[0]
	if mutate != nil {
		mutate(&ac)
	}
	proc := &liveProcess{addr: upstream}
	setup(proc)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	a := app.New(ac, app.Deps{Clock: clock.Real{}, Process: proc, Log: log}, nil)
	pg, err := pages.New(ac)
	if err != nil {
		t.Fatal(err)
	}
	h, err := New(a, pg, log)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: h}
	go func() { _ = srv.Serve(LimitListener(ln, MaxConnections, pg.Bare())) }()
	// Let a running boot finish first: stopping under it would leave it polling
	// a dead upstream until boot_timeout.
	t.Cleanup(func() { srv.Close(); a.Wait(); _ = proc.Stop(context.Background()) })
	return &live{url: "http://" + ln.Addr().String(), addr: ln.Addr().String(), proc: proc, app: a}
}

func (l *live) do(t *testing.T, method, path string, body io.Reader, hdr ...string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(method, l.url+path, body)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Add(hdr[i], hdr[i+1])
	}
	resp, err := http.DefaultTransport.(*http.Transport).Clone().RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func (l *live) waitUp(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for l.app.State(context.Background()) != app.Up {
		if time.Now().After(deadline) {
			t.Fatal("not up in 5s")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// raw sends bytes on a fresh connection and returns everything read back.
func (l *live) raw(t *testing.T, req string) string {
	t.Helper()
	c, err := net.Dial("tcp", l.addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.WriteString(c, req); err != nil {
		t.Fatal(err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(c), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.Status + "\n" + string(b)
}

const htmlAccept = "text/html,application/xhtml+xml"

func contains(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("%q does not contain %q", got, want)
	}
}
