package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/fum4/waker/internal/app"
)

// ClientTimeout bounds one CLI call; a mode switch to on waits only for the
// boot to start, not to finish.
const ClientTimeout = 30 * time.Second

// Client talks to a running waker over its control socket.
type Client struct {
	path string
	hc   *http.Client
}

// NewClient returns a client for the socket at path.
func NewClient(path string) *Client {
	return &Client{path: path, hc: &http.Client{
		Timeout: ClientTimeout,
		Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", path)
		}},
	}}
}

// Status returns the policy and every listening app.s snapshot.
func (c *Client) Status(ctx context.Context) (Report, error) {
	var out Report
	return out, c.call(ctx, http.MethodGet, "/status", nil, &out)
}

// Health returns which apps got their listener.
func (c *Client) Health(ctx context.Context) (Health, error) {
	var out Health
	return out, c.call(ctx, http.MethodGet, "/health", nil, &out)
}

// SetMode sends a mode change or reset and returns the app's new snapshot.
func (c *Client) SetMode(ctx context.Context, req ModeRequest) (app.Snapshot, error) {
	var out app.Snapshot
	return out, c.call(ctx, http.MethodPost, "/mode", req, &out)
}

func (c *Client) call(ctx context.Context, method, path string, in, out any) error {
	if _, err := os.Stat(c.path); errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("the waker isn't running (%s missing): start it with `waker serve`", c.path)
	}
	var body bytes.Buffer
	if in != nil {
		if err := json.NewEncoder(&body).Encode(in); err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://waker"+path, &body)
	if err != nil {
		return err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("the waker doesn't answer on %s: %w", c.path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		if json.NewDecoder(resp.Body).Decode(&e) == nil && e.Error != "" {
			return errors.New(e.Error)
		}
		return fmt.Errorf("control %s %s: %s", method, path, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
