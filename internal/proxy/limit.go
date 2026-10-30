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

// LimitListener accepts at most limit connections at once; the rest get
// busyResponse and are closed.
func LimitListener(l net.Listener, limit int) net.Listener {
	return &limitListener{Listener: l, sem: make(chan struct{}, limit)}
}

type limitListener struct {
	net.Listener
	sem chan struct{}
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
				_, _ = c.Write([]byte(busyResponse))
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
