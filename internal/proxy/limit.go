package proxy

import (
	"net"
	"sync"
	"time"
)

// MaxConnections caps concurrent visitor connections per app.
const MaxConnections = 64

// busyResponse is written raw to a connection over the cap, before any
// request is read, so a flood can't pile up goroutines.
const busyResponse = "HTTP/1.1 503 Service Unavailable\r\n" +
	"Content-Type: application/json\r\nContent-Length: 16\r\n" +
	"Retry-After: 10\r\nConnection: close\r\n\r\n" +
	`{"state":"busy"}`

// busyBareResponse is busyResponse for an app with pages: bare: status and
// headers only, like its other answers.
const busyBareResponse = "HTTP/1.1 503 Service Unavailable\r\n" +
	"Content-Length: 0\r\nCache-Control: no-store\r\nX-Robots-Tag: noindex\r\n" +
	"Retry-After: 10\r\nConnection: close\r\n\r\n"

// LimitListener accepts at most limit connections at once; the rest get
// the busy answer (bodiless when bare) and are closed.
func LimitListener(l net.Listener, limit int, bare bool) net.Listener {
	busy := busyResponse
	if bare {
		busy = busyBareResponse
	}
	return &limitListener{Listener: l, sem: make(chan struct{}, limit), busy: []byte(busy)}
}

type limitListener struct {
	net.Listener
	sem  chan struct{}
	busy []byte
}

func (l *limitListener) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		select {
		case l.sem <- struct{}{}:
			return &limitConn{Conn: c, release: func() { <-l.sem }}, nil
		default:
			go func() {
				_ = c.SetWriteDeadline(time.Now().Add(time.Second))
				_, _ = c.Write(l.busy)
				c.Close()
			}()
		}
	}
}

type limitConn struct {
	net.Conn
	once    sync.Once
	release func()
}

func (c *limitConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(c.release)
	return err
}
