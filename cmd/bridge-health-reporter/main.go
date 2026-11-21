package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/0x3639/bridge-health/internal/localstate"
	"github.com/0x3639/bridge-health/internal/reporter"
	"github.com/0x3639/bridge-health/internal/transport"
	"github.com/0x3639/bridge-health/internal/wire"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
func run() error {
	socket := flag.String("socket", "/run/orchestrator-health/health.sock", "orchestrator Unix socket")
	endpoint := flag.String("collector", "", "HTTPS heartbeat URL")
	stream := flag.String("stream", "", "assigned stream UUID")
	dir := flag.String("state", "", "private sequence state directory")
	cert := flag.String("cert", "", "reporter certificate chain")
	key := flag.String("key", "", "reporter private key")
	ca := flag.String("server-ca", "", "collector CA certificates")
	once := flag.Bool("once", false, "send one heartbeat and exit")
	flag.Parse()
	if flag.NArg() != 0 {
		return errors.New("unexpected arguments")
	}
	lock, err := localstate.Open(*dir)
	if err != nil {
		return err
	}
	defer lock.Close()
	cfg, err := transport.TLS(*cert, *key, *ca, false)
	if err != nil {
		return err
	}
	contracts, err := wire.Compile()
	if err != nil {
		return err
	}
	r, err := reporter.New(*socket, *endpoint, *stream, *dir, cfg, contracts)
	if err != nil {
		return err
	}
	defer r.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	for {
		started := time.Now()
		outcome, err := r.Once(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			log.Printf("delivery failed: %v", err)
		} else {
			log.Printf("heartbeat delivered (local read %s)", outcome)
		}
		if *once {
			return err
		}
		timer := time.NewTimer(time.Until(r.NextSend(started)))
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}
