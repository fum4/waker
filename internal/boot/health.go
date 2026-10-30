package boot

import (
	"context"
	"net/http"
	"os/exec"
	"strings"

	"github.com/fum4/waker/internal/config"
)

// healthProbe decides whether an app is up: GET <upstream><path> answering
// below 400 (redirects are not followed and count as up), or the health
// command exiting 0. Anything else — refused, timed out, not HTTP — is down.
type healthProbe struct {
	url     string
	command []string
	app     config.App
	client  *http.Client
}

func newHealthProbe(app config.App) *healthProbe {
	h := &healthProbe{app: app, command: app.Health.Command}
	if app.Health.Path != "" {
		h.url = strings.TrimRight(app.Upstream, "/") + app.Health.Path
		h.client = &http.Client{
			Timeout: app.Health.Timeout.D(),
			// Probes go straight to the app, never through an env proxy.
			Transport:     &http.Transport{Proxy: nil, DisableKeepAlives: true},
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
	}
	return h
}

func (h *healthProbe) check(ctx context.Context) bool {
	if h.client == nil {
		ctx, cancel := context.WithTimeout(ctx, h.app.Health.Timeout.D())
		defer cancel()
		cmd := exec.CommandContext(ctx, h.command[0], h.command[1:]...)
		cmd.Dir = h.app.Cwd
		return cmd.Run() == nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.url, nil)
	if err != nil {
		return false
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode < 400
}
