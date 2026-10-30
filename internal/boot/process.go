// Package boot runs an app's start and stop commands and probes its health.
//
// The start command runs without a shell in its own process group. It may
// stay in the foreground (the app itself; it is stopped by signalling the
// group) or exit 0 after handing off to a service manager (then `stop` is
// the only way down). A non-zero exit before the app is healthy fails the boot.
package boot

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/fum4/waker/internal/config"
)

// groupPoll is how often a stopping process group is checked for exit.
const groupPoll = 20 * time.Millisecond

// Process supervises one app's commands. It is safe for concurrent use.
type Process struct {
	app    config.App
	health *healthProbe

	mu     sync.Mutex
	cmd    *exec.Cmd
	exited chan struct{} // closed when cmd has been reaped
}

// New returns a supervisor for app.
func New(app config.App) *Process {
	return &Process{app: app, health: newHealthProbe(app)}
}

// Start launches the start command. The returned channel receives the
// command's exit result exactly once (nil for exit 0) and is then closed.
func (p *Process) Start() (<-chan error, error) {
	cmd := p.command(p.app.Start)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", p.app.Start[0], err)
	}
	result := make(chan error, 1)
	exited := make(chan struct{})
	p.mu.Lock()
	p.cmd, p.exited = cmd, exited
	p.mu.Unlock()
	go func() {
		err := cmd.Wait()
		close(exited)
		result <- err
		close(result)
	}()
	return result, nil
}

// PID returns the start command's process id while it runs, else 0.
func (p *Process) PID() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd == nil || isClosed(p.exited) {
		return 0
	}
	return p.cmd.Process.Pid
}

// Healthy probes the app once.
func (p *Process) Healthy(ctx context.Context) bool { return p.health.check(ctx) }

// Stop runs the stop command if one is configured, then signals whatever is
// left of the started process group: SIGTERM, and SIGKILL after stop_timeout.
func (p *Process) Stop(ctx context.Context) error {
	var err error
	if len(p.app.Stop) > 0 {
		err = p.Run(ctx, p.app.Stop, p.app.StopTimeout.D())
	}
	p.terminate()
	return err
}

// Run executes argv in the app's cwd and env, killing its process group
// when timeout passes.
func (p *Process) Run(ctx context.Context, argv []string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := p.command(argv)
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done
		return fmt.Errorf("%s: %w after %s", argv[0], ctx.Err(), timeout)
	}
}

func (p *Process) terminate() {
	p.mu.Lock()
	cmd, exited := p.cmd, p.exited
	p.cmd = nil
	p.mu.Unlock()
	if cmd == nil {
		return
	}
	pgid := cmd.Process.Pid
	if err := syscall.Kill(-pgid, syscall.SIGTERM); errors.Is(err, syscall.ESRCH) {
		return // the whole group is already gone
	}
	// Grace for the whole group, not just its leader: children orphaned by a
	// leader that already exited get the same stop_timeout before SIGKILL.
	deadline := time.Now().Add(p.app.StopTimeout.D())
	for time.Now().Before(deadline) && syscall.Kill(-pgid, 0) == nil {
		select {
		case <-exited:
		case <-time.After(groupPoll):
		}
	}
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
	<-exited
}

// command builds argv with the app's cwd and env in a fresh process group,
// so a stop reaches every child it spawned.
func (p *Process) command(argv []string) *exec.Cmd {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = p.app.Cwd
	cmd.Env = os.Environ()
	keys := make([]string, 0, len(p.app.Env))
	for k := range p.app.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		cmd.Env = append(cmd.Env, k+"="+p.app.Env[k])
	}
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return cmd
}

func isClosed(ch chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}
