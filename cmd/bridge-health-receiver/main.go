package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/0x3639/bridge-health/internal/receiver"
)

func env(name, fallback string) string {
	if s, ok := os.LookupEnv(name); ok {
		return s
	}
	return fallback
}
func main() {
	var c receiver.Config
	flag.StringVar(&c.State, "state", "/data/receiver", "private receiver state directory")
	flag.StringVar(&c.Downloads, "downloads", "/downloads", "directory containing both Linux reporter binaries")
	flag.StringVar(&c.WebListen, "web-listen", ":8080", "HTTP listener behind the Coolify HTTPS proxy")
	flag.StringVar(&c.HeartbeatListen, "heartbeat-listen", ":8443", "direct mTLS listener")
	flag.IntVar(&c.HeartbeatPort, "heartbeat-port", 8443, "externally reachable mTLS port")
	flag.Parse()
	c.Origin = env("PUBLIC_URL", "https://bridgehealth.0x3639.com")
	c.StatusOrigin = env("STATUS_URL", c.Origin)
	c.User = env("ADMIN_USERNAME", "admin")
	c.Password = os.Getenv("ADMIN_PASSWORD")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := receiver.Run(ctx, c); err != nil {
		log.Fatal(err)
	}
}
