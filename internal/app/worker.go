package app

import (
	"context"

	"github.com/fum4/waker/internal/config"
)

func (a *App) maintainWorker() {
	a.mu.Lock()
	now := a.d.Clock.Now()
	if a.modeLocked() != config.ModeOn || now.Before(a.workerCheckAt) {
		a.mu.Unlock()
		return
	}
	if retryAt := a.bootFailedAt.Add(BootRetryCooldown); !a.bootFailedAt.IsZero() && now.Before(retryAt) {
		a.workerCheckAt = retryAt
		a.mu.Unlock()
		return
	}
	a.workerCheckAt = now.Add(a.cfg.Health.Interval.D())
	if a.booting || a.workerChecking {
		a.mu.Unlock()
		return
	}
	a.workerChecking = true
	a.mu.Unlock()
	a.goBackground(func() {
		defer func() {
			a.mu.Lock()
			a.workerChecking = false
			a.mu.Unlock()
		}()
		if a.Mode() == config.ModeOn {
			a.Wake(context.Background())
		}
	})
}
