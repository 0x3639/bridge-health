package collector

import (
	"net"
	"sync"

	"golang.org/x/time/rate"
)

// Edge limits apply to TCP arrivals before any TLS handshake or request parsing.
type edge struct {
	net.Listener
	slots  chan struct{}
	starts *rate.Limiter
}

func Limit(l net.Listener) net.Listener {
	return &edge{Listener: l, slots: make(chan struct{}, 128), starts: rate.NewLimiter(64, 128)}
}
func (l *edge) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		if l.starts.Allow() {
			select {
			case l.slots <- struct{}{}:
				return &limitedConn{Conn: conn, release: func() { <-l.slots }}, nil
			default:
			}
		}
		conn.Close()
	}
}

type limitedConn struct {
	net.Conn
	once    sync.Once
	release func()
}

func (c *limitedConn) Close() error { err := c.Conn.Close(); c.once.Do(c.release); return err }
