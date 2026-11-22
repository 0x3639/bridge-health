// Package receiver runs the managed collector and its Coolify-facing web UI.
package receiver

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"time"

	"github.com/0x3639/bridge-health/internal/collector"
	"github.com/0x3639/bridge-health/internal/localstate"
	"github.com/0x3639/bridge-health/internal/wire"
)

type Config struct {
	State, Downloads, Origin, StatusOrigin, User, Password, WebListen, HeartbeatListen string
	HeartbeatPort                                                                      int
}

var hostname = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]*$`)

func validOrigin(origin string) (*url.URL, bool) {
	u, err := url.Parse(origin)
	return u, err == nil && u.Scheme == "https" && hostname.MatchString(u.Hostname()) && u.User == nil && origin == "https://"+u.Host
}

func Run(ctx context.Context, c Config) error {
	u, ok := validOrigin(c.Origin)
	if !ok {
		return errors.New("PUBLIC_URL must be an HTTPS origin with a DNS hostname and no trailing slash")
	}
	if _, ok := validOrigin(c.StatusOrigin); !ok {
		return errors.New("STATUS_URL must be an HTTPS origin with a DNS hostname and no trailing slash")
	}
	if c.User == "" || len(c.Password) < 16 {
		return errors.New("set ADMIN_USERNAME and an ADMIN_PASSWORD of at least 16 characters")
	}
	if c.HeartbeatPort < 1 || c.HeartbeatPort > 65535 {
		return errors.New("invalid public heartbeat port")
	}
	endpoint := "https://" + net.JoinHostPort(u.Hostname(), strconv.Itoa(c.HeartbeatPort)) + wire.Path
	lock, err := localstate.Open(c.State)
	if err != nil {
		return err
	}
	defer lock.Close()
	f, err := openFleet(c.State)
	if err != nil {
		return err
	}
	tlsConfig, err := f.ca.serverTLS(u.Hostname())
	if err != nil {
		return err
	}
	ui, err := newWeb(f, c.Origin, c.StatusOrigin, endpoint, c.Downloads, c.User, c.Password)
	if err != nil {
		return err
	}
	server := func(h http.Handler) *http.Server {
		return &http.Server{Handler: h, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 2 * time.Minute, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8 << 10}
	}
	webServer, heartbeat := server(ui.handler()), server(f.collector)
	defer webServer.Close()
	defer heartbeat.Close()
	heartbeat.TLSConfig = tlsConfig
	heartbeat.WriteTimeout = 10 * time.Second
	webListener, err := net.Listen("tcp", c.WebListen)
	if err != nil {
		return err
	}
	defer webListener.Close()
	tlsListener, err := net.Listen("tcp", c.HeartbeatListen)
	if err != nil {
		return err
	}
	defer tlsListener.Close()
	done := make(chan error, 2)
	go func() { done <- webServer.Serve(webListener) }()
	go func() { done <- heartbeat.ServeTLS(collector.Limit(tlsListener), "", "") }()
	log.Printf("receiver web listening on %s, mTLS on %s", webListener.Addr(), tlsListener.Addr())
	select {
	case <-ctx.Done():
	case err = <-done:
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	return errors.Join(err, webServer.Shutdown(shutdown), heartbeat.Shutdown(shutdown))
}
