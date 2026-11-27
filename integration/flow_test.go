package integration

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/0x3639/bridge-health/internal/collector"
	"github.com/0x3639/bridge-health/internal/wire"
)

const stream = "a82104c7-105c-4f86-88dd-6bec99fc2222"

type process struct {
	cmd     *exec.Cmd
	done    chan error
	stopped bool
}

func start(t *testing.T, bin, prefix string, args ...string) (*process, string) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	rd, wr, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdout, cmd.Stderr = wr, wr
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	wr.Close()
	p := &process{cmd: cmd, done: make(chan error, 1)}
	go func() { p.done <- cmd.Wait() }()
	t.Cleanup(func() { p.stop(t) })
	ready := make(chan string, 1)
	go func() {
		defer rd.Close()
		scanner := bufio.NewScanner(rd)
		found := false
		var last string
		for scanner.Scan() {
			last = scanner.Text()
			if !found && strings.Contains(last, prefix) {
				ready <- last
				found = true
			}
		}
		if !found {
			ready <- "startup failed: " + last
		}
	}()
	select {
	case line := <-ready:
		if at := strings.Index(line, prefix); at >= 0 {
			return p, strings.TrimSpace(line[at+len(prefix):])
		}
		t.Fatal(line)
	case <-time.After(10 * time.Second):
		t.Fatal("timed out starting", bin)
	}
	return nil, ""
}
func (p *process) stop(t *testing.T) {
	t.Helper()
	if p.stopped {
		return
	}
	p.stopped = true
	_ = p.cmd.Process.Signal(syscall.SIGTERM)
	select {
	case err := <-p.done:
		if err != nil {
			t.Errorf("child exit: %v", err)
		}
	case <-time.After(12 * time.Second):
		p.cmd.Process.Kill()
		<-p.done
		t.Error("child did not shut down")
	}
}
func run(t *testing.T, bin string, args ...string) []byte {
	t.Helper()
	out, err := exec.Command(bin, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v: %s", filepath.Base(bin), err, out)
	}
	return out
}
func client(t *testing.T, root string, cred *credential) *http.Client {
	t.Helper()
	b, err := os.ReadFile(root)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(b) {
		t.Fatal("empty CA")
	}
	cfg := &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots}
	if cred != nil {
		pair, err := tls.LoadX509KeyPair(cred.cert, cred.key)
		if err != nil {
			t.Fatal(err)
		}
		cfg.Certificates = []tls.Certificate{pair}
	}
	c := &http.Client{Transport: &http.Transport{TLSClientConfig: cfg}, Timeout: 3 * time.Second}
	t.Cleanup(c.CloseIdleConnections)
	return c
}
func post(t *testing.T, c *http.Client, url string, b []byte) int {
	t.Helper()
	r, err := c.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		return 0
	}
	defer r.Body.Close()
	io.Copy(io.Discard, r.Body)
	return r.StatusCode
}
func TestEnrolledOperatorFlow(t *testing.T) {
	bin := os.Getenv("BRIDGE_HEALTH_TEST_BIN")
	if bin == "" {
		t.Skip("run ./integration/test.sh with the snapshot checkout")
	}
	dir := t.TempDir()
	// Linux Unix socket pathname length is limited; avoid a long test workspace path.
	sockDir, err := os.MkdirTemp("", "bh-sock-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(sockDir) })
	socket := filepath.Join(sockDir, "health.sock")
	tlsCA := makeCA(t, dir, "server-ca")
	operatorCA := makeCA(t, dir, "client-ca")
	server := tlsCA.issue(t, dir, "collector", true)
	enrolled := operatorCA.issue(t, dir, "operator", false)
	renewed := operatorCA.issue(t, dir, "renewed", false)
	unlisted := operatorCA.issue(t, dir, "unlisted", false)
	foreign := tlsCA.issue(t, dir, "foreign", false)
	manifest := filepath.Join(dir, "enrollments.json")
	enroll := func(active ...credential) {
		entries := []collector.Enrollment{}
		if len(active) > 0 {
			var fps []string
			for _, cred := range active {
				fps = append(fps, cred.fp)
			}
			entries = append(entries, collector.Enrollment{Node: "pilot-01", Stream: stream, Certificates: fps})
		}
		b, _ := json.Marshal(map[string]any{"enrollments": entries})
		if err := os.WriteFile(manifest+".tmp", b, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(manifest+".tmp", manifest); err != nil {
			t.Fatal(err)
		}
	}
	enroll(enrolled)
	cbin := filepath.Join(bin, "bridge-health-collector")
	rbin := filepath.Join(bin, "bridge-health-reporter")
	common := []string{"-state", filepath.Join(dir, "collector"), "-enrollments", manifest}
	launch := func() (*process, string) {
		p, addr := start(t, cbin, "collector listening on ", append(append([]string{"serve"}, common...), "-listen", "127.0.0.1:0", "-cert", server.cert, "-key", server.key, "-client-ca", operatorCA.path)...)
		return p, "https://" + addr + wire.Path
	}
	status := func(extra ...string) collector.Status {
		b := run(t, cbin, append(append([]string{"status"}, common...), extra...)...)
		var rows []collector.Status
		if err := json.Unmarshal(b, &rows); err != nil || len(rows) != 1 {
			t.Fatalf("status %s: %v", b, err)
		}
		return rows[0]
	}
	capture := func(url string, cred credential) {
		run(t, rbin, "-once", "-socket", socket, "-collector", url, "-stream", stream, "-state", filepath.Join(dir, "reporter"), "-key", cred.key, "-cert", cred.cert, "-server-ca", tlsCA.path)
	}
	orch, _ := start(t, filepath.Join(bin, "orchestrator-socket"), "ready", socket)
	col, url := launch()
	if s := status(); s.Delivery != "never" {
		t.Fatal(s)
	}
	capture(url, enrolled)
	s := status()
	if s.Latest == nil || s.Node != "pilot-01" || s.Delivery != "fresh" {
		t.Fatal(s)
	}
	first := s.Latest.Report
	if first.Sequence != 1 || first.LocalRead != "ok" || first.LastSuccess == nil {
		t.Fatalf("unexpected first report: %+v", first)
	}
	var snap struct {
		Build struct{ Version string }
		Zenon struct {
			NodeSync    struct{ Value string }
			ReceiveHold *struct {
				Height   uint64
				Since    time.Time
				Attempts uint64
			}
		}
		Networks []struct {
			Wrap, Unwrap struct{ UnsignedObserved struct{ Value int } }
		}
	}
	if err := json.Unmarshal(first.Snapshot, &snap); err != nil {
		t.Fatal(err)
	}
	if snap.Build.Version != "integration-build" || snap.Zenon.NodeSync.Value != "synced" || snap.Zenon.ReceiveHold != nil || len(snap.Networks) != 1 || snap.Networks[0].Wrap.UnsignedObserved.Value != 3 || snap.Networks[0].Unwrap.UnsignedObserved.Value != 1 {
		t.Fatalf("wrong collector snapshot: %s", first.Snapshot)
	}
	b, _ := json.Marshal(first)
	valid := client(t, tlsCA.path, &enrolled)
	if sc := post(t, valid, url, b); sc != 204 {
		t.Fatalf("duplicate: %d", sc)
	}
	if !status().Latest.ReceivedAt.Equal(s.Latest.ReceivedAt) {
		t.Fatal("retry refreshed liveness")
	}
	altered := first
	altered.Stream = "a82104c7-105c-4f86-88dd-6bec99fc2223"
	other, _ := json.Marshal(altered)
	if sc := post(t, valid, url, other); sc != 403 {
		t.Fatalf("cross-stream: %d", sc)
	}
	if sc := post(t, client(t, tlsCA.path, &unlisted), url, b); sc != 403 {
		t.Fatalf("unenrolled leaf: %d", sc)
	}
	for _, bad := range []*http.Client{client(t, tlsCA.path, nil), client(t, tlsCA.path, &foreign), client(t, operatorCA.path, &enrolled)} {
		if sc := post(t, bad, url, b); sc != 0 {
			t.Fatalf("expected TLS rejection, got HTTP %d", sc)
		}
	}
	// Prove that the new key works during overlap before removing the old one.
	enroll(enrolled, renewed)
	if err := col.cmd.Process.Signal(syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	rotated := client(t, tlsCA.path, &renewed)
	deadline := time.Now().Add(3 * time.Second)
	for sc := post(t, rotated, url, b); sc != 204; sc = post(t, rotated, url, b) {
		if sc != 403 || time.Now().After(deadline) {
			t.Fatalf("overlap: %d", sc)
		}
		time.Sleep(20 * time.Millisecond)
	}

	orch.stop(t)
	capture(url, renewed)
	lost := status()
	if lost.Latest.Report.Sequence != 2 || lost.Latest.Report.LocalRead != "unreachable" || string(lost.Latest.Report.Snapshot) != "null" || lost.Latest.Report.LastSuccess == nil || !lost.Latest.Report.LastSuccess.Equal(*first.LastSuccess) {
		t.Fatalf("incorrect socket outage: %+v", lost)
	}
	if lost.HistoricalSuccess == nil || lost.HistoricalSuccess.Report.Sequence != 1 || lost.HistoricalSuccess.Report.LocalRead != "ok" {
		t.Fatal("last good snapshot missing as history")
	}
	if status("-stale-after", "1ns").Delivery != "stale" {
		t.Fatal("missed stale stream")
	}
	col.stop(t)
	col, url = launch()
	if sc := post(t, valid, url, b); sc != 409 {
		t.Fatalf("old sequence after collector restart: %d", sc)
	}
	enroll(renewed)
	if err := col.cmd.Process.Signal(syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	limit := time.Now().Add(2 * time.Second)
	for sc := post(t, valid, url, b); sc != 403; sc = post(t, valid, url, b) {
		if time.Now().After(limit) {
			t.Fatalf("revocation failed: %d", sc)
		}
		time.Sleep(20 * time.Millisecond)
	}
	heldOrch, _ := start(t, filepath.Join(bin, "orchestrator-socket"), "ready", socket, "hold")
	capture(url, renewed)
	heldStatus := status()
	heldOrch.stop(t)
	if heldStatus.Latest == nil || heldStatus.Latest.Report.Sequence != 3 || heldStatus.Latest.Report.LocalRead != "ok" {
		t.Fatalf("unexpected held report: %+v", heldStatus)
	}
	var heldSnap struct {
		Zenon struct {
			ReceiveHold *struct {
				Height   uint64
				Since    time.Time
				Attempts uint64
			}
		}
	}
	if err := json.Unmarshal(heldStatus.Latest.Report.Snapshot, &heldSnap); err != nil {
		t.Fatal(err)
	}
	if heldSnap.Zenon.ReceiveHold == nil || heldSnap.Zenon.ReceiveHold.Height != 16221 || heldSnap.Zenon.ReceiveHold.Attempts != 3 || heldSnap.Zenon.ReceiveHold.Since.IsZero() {
		t.Fatalf("wrong held snapshot: %s", heldStatus.Latest.Report.Snapshot)
	}
	t.Logf("accepted sequence %d: version %s, Zenon %s, 3 wraps, 1 unwrap; sequence %d: socket unreachable; sequence %d: receiveHold height %d", first.Sequence, snap.Build.Version, snap.Zenon.NodeSync.Value, lost.Latest.Report.Sequence, heldStatus.Latest.Report.Sequence, heldSnap.Zenon.ReceiveHold.Height)
}
