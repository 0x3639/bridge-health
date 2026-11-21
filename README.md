# Bridge health

One service runs on each operator host and one runs on the monitoring host. The reporter reads `getHealthSnapshot` through the orchestrator’s Unix socket roughly once a minute and sends a v1 heartbeat to the collector over mutual TLS. The collector authenticates the reporter, checks the payload and ordering, and stores the result. The reporter needs access to the socket, its telemetry key and its own state directory. Operators can keep incoming health ports closed.

## Try the complete flow

The integration test runs the two command-line programs and the **actual Unix socket server and snapshot store** from orchestrator commit `aea5eb03fdb97b77d936d37633e99aab62792c08` on `codex/heartbeat-v1`. It seeds the store with known observations, creates temporary server and operator certificate authorities, generates a reporter key and signed certificate request, and enrolls the issued certificate against one stream. It uses loopback and disposable state. It needs Linux, a Go toolchain with the orchestrator's dependencies, and a checkout of that commit. The two production services build independently from this repository.

```sh
go build -o bin/ ./cmd/...
go test -race ./...
./integration/test.sh /path/to/orchestrator
```

The test checks that the collector receives the version, sync state and both queue counts. It stops the socket, restarts the reporter, verifies a fresh failure report and preserves the previous snapshot as history. It also exercises duplicate delivery, certificate rotation and revocation, cross-stream posting, TLS rejection, and replay after a collector restart. The script compares both schemas and fixtures with the orchestrator checkout before building.

## Enroll an operator

1. **Operator host.** Create the key and CSR under the reporter’s dedicated account, in a directory accessible only to that account.

   ```sh
   umask 077
   openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out reporter.key
   openssl req -new -key reporter.key -subj '/CN=bridge-health-reporter' -out reporter.csr
   openssl req -in reporter.csr -outform DER | openssl dgst -sha256
   ```

   Send `reporter.csr` to the monitoring administrator. Confirm the CSR fingerprint through the operator contact already on file. The private key stays on the host.

2. **Monitoring administrator.** Assign a stable node identifier and a new lowercase stream UUID. Verify the CSR signature and fingerprint and issue a short-lived client certificate from the telemetry CA. Keep the CA signing key on the issuing system. For example, using a PEM CA on that system:

   ```sh
   openssl req -in reporter.csr -verify -noout
   openssl req -in reporter.csr -outform DER | openssl dgst -sha256
   printf '%s\n' 'basicConstraints=critical,CA:FALSE' 'keyUsage=critical,digitalSignature' 'extendedKeyUsage=clientAuth' > reporter.ext
   openssl x509 -req -in reporter.csr -CA client-ca.crt -CAkey client-ca.key \
     -set_serial "0x$(openssl rand -hex 16)" -days 30 -sha256 -extfile reporter.ext -out reporter.crt
   openssl x509 -in reporter.crt -outform DER | openssl dgst -sha256
   ```

3. **Collector host.** Register the issued leaf certificate’s SHA-256 fingerprint (64 lowercase hex digits), node and stream in `enrollments.json`. The sample fingerprint and stream below are illustrative. The registry is an administrator-managed file. Display name and expected EVM chain IDs are optional registry metadata.

   ```json
   {
     "enrollments": [{
       "nodeId": "pilot-01",
       "displayName": "Pilot operator",
       "expectedEvmChainIds": [1],
       "streamId": "e71664d3-a7ba-4029-8e17-95a5d0b85537",
       "certificateSHA256": ["0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"]
     }]
   }
   ```

4. Return the certificate (with any intermediate certificates), collector HTTPS URL, assigned stream UUID and server CA bundle through the authenticated operator channel. The reporter verifies the server name against its certificate. Use the name present in the certificate's subject alternative names.

## Run the services

The Compose files run each service as its own non-root account, with a read-only root filesystem. Set the listed environment variables in the corresponding host's shell or Compose environment file. Use absolute host paths. Pre-create each state directory with mode `0700` and ownership matching the service UID. Give the service read access to its credential files and restrict each private key to that service.

| Host | Variables | Contents |
| --- | --- | --- |
| Collector | `COLLECTOR_CONFIG`, `COLLECTOR_STATE`, `COLLECTOR_UID`, `COLLECTOR_GID` | The config directory contains `enrollments.json`, `server.crt`, `server.key`, and `client-ca.crt`. |
| Reporter | `REPORTER_CONFIG`, `REPORTER_STATE`, `REPORTER_UID`, `REPORTER_GID`, `SOCKET_GID`, `STREAM_ID`, `COLLECTOR_URL` | The config directory contains `reporter.crt`, `reporter.key`, and `server-ca.crt`. The collector URL ends in `/v1/heartbeats`. |

On the monitoring host, choose the interface that accepts operators’ connections with `COLLECTOR_BIND_IP`. The default is loopback for testing. The collector terminates TLS itself. For a load balancer use TCP passthrough.

```sh
docker compose -f deploy/collector.compose.yaml up -d --build
```

On the operator host, configure the orchestrator's `HealthConfig.SnapshotSocket` as `/run/orchestrator-health/health.sock`. Mount its parent directory so replacing a socket on orchestrator restart works. Supply the socket group's numeric GID. `SOCKET_DIR` can override the host directory.

```sh
docker compose -f deploy/reporter.compose.yaml up -d --build
```

Read the operator status on the monitoring host using the collector's local command. Its full snapshot contains per-operator version and queue details.

```sh
docker compose -f deploy/collector.compose.yaml run --rm --no-deps collector status \
  -state /state -enrollments /etc/bridge-health/enrollments.json
```

- `delivery` is `never`, `fresh` or `stale`, based on collector receipt. The default stale threshold is three minutes. Change it with `-stale-after`.
- A fresh heartbeat with `localRead: "unreachable"` means the reporter delivered a message but could not reach the socket. The current snapshot is null. `historicalSuccess`, when available, retains the last successfully collected snapshot and its original times.
- On successful reads, the snapshot exposes the provider’s sync claim, scan progress, and work queues with their own observation times and quality. An independently corroborated sync verdict needs trusted chain references. A partial or unknown zero cannot establish that a signer is idle.

## Delivery and storage

The reporter applies a two-second socket deadline and a five-second send deadline. It samples immediately at startup, then on a one-minute interval with up to five seconds of jitter. Before posting it saves the sequence number and last successful local read by atomic rename and directory sync. A temporary delivery failure permits one jittered retry of the same bytes. A long `Retry-After` defers the next sample. Gaps in the sequence are valid.

The collector serves only `POST /v1/heartbeats`. It verifies the client chain and checks the active leaf fingerprint on every request, including reused TLS connections. An enrolled leaf can update only its assigned stream. Before TLS, the listener limits concurrent connections and handshake starts. After authentication it applies per-stream and global limits, a 64 KiB body cap, both pinned schemas, duplicate checks, and time checks. Keep service clocks synchronized. New messages can be up to two minutes old or 30 seconds ahead of collector time.

| HTTP result | Meaning |
| --- | --- |
| 204 | Durable acceptance or byte-identical retry of the current sequence. A retry leaves the receipt time intact. |
| 400 / 413 / 415 | Invalid payload or time, excessive size, or unsupported content type. |
| 403 | Valid TLS credential with inactive or mismatched enrollment. Missing or untrusted client certificates fail during TLS. |
| 409 | Older sequence, conflicting retry, or stream already recorded under a different node. |
| 429 / 503 | Rate limit or temporarily unavailable storage. |

Run one collector per state directory on a local filesystem with working file locks, atomic rename and fsync. Each stream file retains its latest report, up to 30 prior receipts, and the last good snapshot across a longer socket outage. The replay watermark and history are committed together. Both programs refuse concurrent writers to the same state directory. Include ordering state in backups. After reporter state loss or a collector rollback to an older backup, retire affected stream IDs and enroll fresh streams before resuming ingestion.

## Rotate and revoke

Add the replacement certificate fingerprint to the existing stream for a short overlap. Atomically replace `enrollments.json` and run `docker compose -f deploy/collector.compose.yaml kill -s HUP collector`. Confirm the collector logged the reload. Invalid manifests leave the last valid registry active. Install the new reporter credential, restart only the reporter, and verify the next sequence arrives. Remove the old fingerprint and reload again. To revoke immediately, remove the affected fingerprint and reload. To retire a stream, remove its enrollment entirely.

## Pilot cutover

During the pilot, keep the existing scraper running and compare its results with authenticated heartbeats. Check a normal report, a stopped socket and a silent reporter. Switch alerts for each operator after verifying freshness, and monitor the collector itself independently. Once the replacement alerts are live, bind the old orchestrator health port to loopback, remove its public proxy and firewall exposure, and verify from outside that heartbeats continue and the old health endpoint is closed.
