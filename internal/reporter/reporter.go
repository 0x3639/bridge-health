package reporter

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/0x3639/bridge-health/internal/localstate"
	"github.com/0x3639/bridge-health/internal/wire"
)

type position struct {
	Stream      string     `json:"streamId"`
	Sequence    uint64     `json:"sequence"`
	LastSuccess *time.Time `json:"lastLocalSuccessAt"`
}
type Reporter struct {
	contracts      *wire.Contracts
	local, remote  *http.Client
	cursor         position
	retryAt        time.Time
	path, endpoint string
}

func New(socket, endpoint, stream, dir string, cfg *tls.Config, contracts *wire.Contracts) (*Reporter, error) {
	if !filepath.IsAbs(socket) || !wire.StreamID(stream) {
		return nil, errors.New("absolute socket path and lowercase stream UUID required")
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Path != wire.Path || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("collector URL must use HTTPS and /v1/heartbeats")
	}
	noRedirect := func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	dialer := new(net.Dialer)
	r := &Reporter{
		contracts: contracts, endpoint: endpoint, path: filepath.Join(dir, "sequence.json"), cursor: position{Stream: stream},
		local: &http.Client{Timeout: 2 * time.Second, CheckRedirect: noRedirect, Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", socket)
		}, DisableCompression: true}},
		remote: &http.Client{Timeout: 5 * time.Second, CheckRedirect: noRedirect, Transport: &http.Transport{TLSClientConfig: cfg, DisableCompression: true, IdleConnTimeout: 30 * time.Second}},
	}
	b, err := os.ReadFile(r.path)
	if err == nil {
		var p position
		if err = wire.StrictJSON(b, &p); err != nil {
			return nil, err
		}
		if p.Stream != stream || p.Sequence == 0 || p.Sequence > wire.MaxSequence {
			return nil, errors.New("invalid sequence state or mismatched stream")
		}
		r.cursor = p
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return r, nil
}

func (r *Reporter) Close() { r.remote.CloseIdleConnections(); r.local.CloseIdleConnections() }
func readFailure(err error) string {
	var n net.Error
	if errors.As(err, &n) && n.Timeout() {
		return "timeout"
	}
	return "unreachable"
}
func (r *Reporter) read(ctx context.Context) (json.RawMessage, string) {
	const call = `{"method":"getHealthSnapshot","params":[]}`
	req, err := http.NewRequestWithContext(ctx, "POST", "http://localhost/", bytes.NewBufferString(call))
	if err != nil {
		return nil, "invalidResponse"
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.local.Do(req)
	if err != nil {
		return nil, readFailure(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, "unsupported"
	}
	if resp.StatusCode != 200 {
		return nil, "invalidResponse"
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, wire.MaxBody+1))
	if err != nil {
		return nil, readFailure(err)
	}
	if len(b) > wire.MaxBody {
		return nil, "invalidResponse"
	}
	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  *string         `json:"error"`
	}
	if wire.StrictJSON(b, &envelope) != nil || envelope.Error == nil || *envelope.Error != "" || r.contracts.Snapshot(envelope.Result) != nil {
		return nil, "invalidResponse"
	}
	return envelope.Result, "ok"
}

// Once reserves a durable sequence before the first POST. A transient failure
// retries the exact bytes once. The next tick always takes a fresh observation.
func (r *Reporter) Once(ctx context.Context) (string, error) {
	snapshot, outcome := r.read(ctx)
	if ctx.Err() != nil {
		return outcome, ctx.Err()
	}
	if r.cursor.Sequence >= wire.MaxSequence {
		return outcome, errors.New("stream sequence exhausted")
	}
	now := time.Now().UTC()
	report := wire.Report{Version: 1, Stream: r.cursor.Stream, Sequence: r.cursor.Sequence + 1, SentAt: now, LocalRead: outcome, LastSuccess: r.cursor.LastSuccess, Snapshot: snapshot}
	if outcome == "ok" {
		report.LastSuccess = &now
	}
	body, err := json.Marshal(report)
	if err != nil {
		return outcome, err
	}
	if _, err = r.contracts.Report(body); err != nil {
		if outcome != "ok" {
			return outcome, err
		}
		// A stale snapshot or one too large for the envelope is a failed local read.
		outcome = "invalidResponse"
		report.LocalRead, report.Snapshot, report.LastSuccess = outcome, nil, r.cursor.LastSuccess
		body, err = json.Marshal(report)
		if err != nil {
			return outcome, err
		}
		if _, err = r.contracts.Report(body); err != nil {
			return outcome, err
		}
	}
	next := position{Stream: report.Stream, Sequence: report.Sequence, LastSuccess: report.LastSuccess}
	if err = localstate.Write(r.path, next); err != nil {
		return outcome, err
	}
	r.cursor = next
	retry, err := r.post(ctx, body)
	if !retry {
		return outcome, err
	}
	delay := time.Duration(500+rand.IntN(1001)) * time.Millisecond
	if wait := time.Until(r.retryAt); wait > delay {
		delay = wait
	}
	if delay > 5*time.Second {
		return outcome, err
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return outcome, ctx.Err()
	case <-timer.C:
		_, err = r.post(ctx, body)
		return outcome, err
	}
}
func (r *Reporter) post(ctx context.Context, body []byte) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", r.endpoint, bytes.NewReader(body))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.remote.Do(req)
	if err != nil {
		return true, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
	if resp.StatusCode == 204 {
		return false, nil
	}
	temporary := resp.StatusCode == 429 || resp.StatusCode >= 500
	if temporary {
		value := resp.Header.Get("Retry-After")
		if seconds, err := strconv.ParseUint(value, 10, 31); err == nil {
			r.retryAt = time.Now().Add(time.Duration(seconds) * time.Second)
		} else if date, err := http.ParseTime(value); err == nil {
			r.retryAt = date
		}
	}
	return temporary, fmt.Errorf("collector returned HTTP %d", resp.StatusCode)
}

// NextSend spaces reports across the minute and respects the collector's backoff.
func (r *Reporter) NextSend(previous time.Time) time.Time {
	next := previous.Add(time.Minute + time.Duration(rand.IntN(10001)-5000)*time.Millisecond)
	if r.retryAt.After(next) {
		return r.retryAt
	}
	return next
}
