// Integration fixture. Build from the pinned orchestrator checkout so it links
// its production Unix socket implementation and its real snapshot store.
package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"orchestrator/common/config"
	"orchestrator/health"
	"orchestrator/health/snapshot"
)

func main() {
	s := snapshot.New("integration-build", "aea5eb0")
	defer s.Close()
	s.AddNetwork(2, 1)
	s.SetNetworksKnown()
	s.SetPhase("running")
	s.UpdateZenon(func(z *snapshot.Zenon) { z.NodeSync = snapshot.SyncResult(true, true) })
	s.UpdateNetwork(1, func(n *snapshot.Network) {
		n.NodeSync = snapshot.SyncResult(true, true)
		n.HeadHeight = snapshot.Measure(uint64(555))
		n.Wrap.UnsignedObserved = snapshot.Measure(3)
		n.Unwrap.UnsignedObserved = snapshot.Measure(1)
	})
	cfg := config.HealthRpcConfig{CachedResponseDelay: 25, ResponsesPerSecond: 10, Burst: 10, PerClientResponsesPerSecond: 10, PerClientBurst: 10}
	server, err := health.ListenSnapshot(os.Args[1], s, cfg)
	if err != nil {
		panic(err)
	}
	defer server.Close()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	fmt.Println("ready")
	<-stop
}
