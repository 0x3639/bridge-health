package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/0x3639/bridge-health/internal/collector"
	"github.com/0x3639/bridge-health/internal/localstate"
	"github.com/0x3639/bridge-health/internal/transport"
	"github.com/0x3639/bridge-health/internal/wire"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
func run() error {
	mode := "serve"
	args := os.Args[1:]
	if len(args) > 0 && (args[0] == "status" || args[0] == "serve") {
		mode, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet(mode, flag.ContinueOnError)
	registry := fs.String("enrollments", "", "enrollment manifest")
	dir := fs.String("state", "", "private state directory")
	addr := fs.String("listen", "127.0.0.1:8443", "TLS listen address")
	cert := fs.String("cert", "", "server certificate chain")
	key := fs.String("key", "", "server private key")
	ca := fs.String("client-ca", "", "reporter CA certificates")
	stale := fs.Duration("stale-after", 3*time.Minute, "status freshness threshold")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || *dir == "" || *registry == "" || *stale <= 0 {
		return errors.New("provide -state and -enrollments")
	}
	reg, err := collector.Load(*registry)
	if err != nil {
		return err
	}
	if mode == "status" {
		if _, err := os.Stat(*dir); err != nil {
			return err
		}
		rows, err := collector.Statuses(*dir, reg, *stale, time.Now())
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(rows)
	}
	lock, err := localstate.Open(*dir)
	if err != nil {
		return err
	}
	defer lock.Close()
	contracts, err := wire.Compile()
	if err != nil {
		return err
	}
	cfg, err := transport.TLS(*cert, *key, *ca, true)
	if err != nil {
		return err
	}
	handler := collector.New(*dir, contracts, reg)
	server := &http.Server{Handler: handler, TLSConfig: cfg, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8 << 10}
	l, err := net.Listen("tcp", *addr)
	if err != nil {
		return err
	}
	defer l.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	reload := make(chan os.Signal, 1)
	signal.Notify(reload, syscall.SIGHUP)
	defer signal.Stop(reload)
	done := make(chan error, 1)
	go func() { done <- server.ServeTLS(collector.Limit(l), "", "") }()
	log.Printf("collector listening on %s", l.Addr())
	for {
		select {
		case <-ctx.Done():
			timeout, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			return server.Shutdown(timeout)
		case <-reload:
			next, err := collector.Load(*registry)
			if err != nil {
				log.Printf("enrollment reload failed: %v", err)
				continue
			}
			handler.Reload(next)
			log.Printf("loaded %d enrollments", len(next.Entries))
		case err := <-done:
			return fmt.Errorf("serve: %w", err)
		}
	}
}
