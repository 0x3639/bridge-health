# Bridge health

One service runs on each operator host and one runs on the monitoring host. The reporter reads `getHealthSnapshot` through the orchestrator’s Unix socket roughly once a minute and sends a v1 heartbeat to the collector over mutual TLS. The collector authenticates the reporter, checks the payload and ordering, and stores the result. The reporter needs access to the socket, its telemetry key and its own state directory. Operators can keep incoming health ports closed.

## Deploy the managed receiver on Coolify

The managed receiver includes the collector, a public Vue status page, password-protected administration, a certificate authority and installers for Linux amd64 and arm64. It builds both reporter binaries from the same checkout as the receiver.

1. Point the DNS A records for `bridgestatus.0x3639.com` and `bridgehealth.0x3639.com` at the Coolify server. Add an AAAA record only if IPv6 reaches the same server. With Cloudflare, use **DNS only** so operators can reach the receiver's mutual TLS listener directly.
2. Create a Git-based **Docker Compose** application in Coolify using the branch containing these changes. Set **Base Directory** to `/` and **Docker Compose Location** to `/compose.coolify.yaml`.
3. In the application's runtime environment, set `ADMIN_PASSWORD` to a random password of at least 16 characters. For example, generate one with `openssl rand -hex 24`. The username defaults to `admin`. Set `ADMIN_USERNAME` to change it.
4. On the existing **receiver** service, assign both HTTPS domains to container port **8080**. In the Domains field enter `https://bridgehealth.0x3639.com:8080,https://bridgestatus.0x3639.com:8080`. Keep the existing receiver volume and resource when upgrading. Allow TCP 80 and 443 to the Coolify proxy and TCP **8443** directly to the receiver. Keep port 8080 internal.
5. Deploy one replica. Open [Bridge status](https://bridgestatus.0x3639.com/) for the public table. To sign in, go directly to [receiver administration](https://bridgestatus.0x3639.com/admin) and use the existing Coolify credentials. A request to `https://bridgehealth.0x3639.com/healthz` returns HTTP 204 when the web service is available.

| Connection | Destination | Authentication |
| --- | --- | --- |
| Visitors and administrators | `https://bridgestatus.0x3639.com` | The root and `/api/status` are public. Management requires signing in at `/admin`. |
| Installer and first enrollment | `https://bridgehealth.0x3639.com` | Coolify serves public HTTPS. Admin forms require a login session. Enrollment requires the private invitation and a signed CSR. |
| Enrolled reporter | `https://bridgehealth.0x3639.com:8443/v1/heartbeats` | The receiver terminates mutual TLS and checks the active certificate fingerprint for the assigned stream on every request. |

The Compose volume `receiver-data` holds the private CA, invitations, enrollments and heartbeat history under `/data/receiver`. Preserve it across redeployments. Back up the entire volume together while the receiver is stopped, protect the backup like a signing key, and keep the volume attached to only one receiver. Restarting the container ends browser sessions. To change the administrator password, update it in Coolify and redeploy.

[Coolify Docker Compose setup](https://coolify.io/docs/applications/builds/docker-compose) · [Coolify domain configuration](https://coolify.io/docs/core/networking/domains)

## Public dashboard

The public page lists enrolled, unrevoked reporters under their assigned pillar names. It refreshes every 30 seconds. A fresh, successful socket read appears as **Online**. A fresh heartbeat carrying a local read failure appears as **Unavailable**, with the reason. **Stale** means three minutes have elapsed since receipt. **Awaiting report** marks an enrolled node with no receipt. Each public update projects the latest report. Raw snapshots, stream identifiers, invitation history and certificates stay in administration.

- **State** shows the orchestrator bridge state when known, with the process phase as a fallback.
- **BNB, ETH and Supernova** show observed unsigned wraps / unwraps for EVM chain IDs 56, 1 and 73405. A partial count has an asterisk. Missing measurements display a dash. Hover over a pair for each observation's quality and time.
- **Producer** defaults to the address for an exact, case-insensitive name match in the [explorer’s pillar directory](https://github.com/0x3639/explorer.zenon.info/blob/0abfad6f822b28f285d7584153f28b4e77686976/src/lib/constants.ts). Other names start blank. In **Overview**, open a node, fill in **Public listing → Producer address** and save. Clearing it removes the explorer links.
- **Resp. time** is the reporter’s elapsed time to read and validate the local snapshot. Reporters built before this release omit that measurement. **Last checked** is the receiver's UTC receipt time.

`STATUS_URL` configures the additional HTTPS origin allowed for administration. `PUBLIC_URL` remains the installer and heartbeat hostname. The included Compose file configures both. When running the binary directly, `STATUS_URL` defaults to `PUBLIC_URL`.

The dashboard and admin screens use Vue 3 and the [Zenon design system](https://github.com/digitalSloth/zenon-design-system). The Go binary embeds the compiled UI, runtime and fonts. Coolify builds from the checked-in assets with the existing Dockerfile. After editing the Vue components or styles, rebuild and include the updated assets:

```sh
cd frontend
npm ci
npm run build
```

## Invite and install a node

1. In **Invitations**, enter the node name and select **Generate link**. Copy the link before leaving the page and send it privately to the operator. Each link is valid for 24 hours and enrolls one node key. Opening the page or previewing it in chat leaves the invitation available.
2. The operator opens the link, copies the installation command, and runs it on the orchestrator host with sudo. The host needs Linux amd64 or arm64, systemd, Python 3.7+, curl, OpenSSL, the standard user/group administration tools, and outbound HTTPS to the two receiver ports. The installer verifies the reporter binary's SHA-256 digest, generates a P-256 key on the operator host, enrolls its CSR, and enables the dedicated non-root `bridge-health-reporter` systemd service. Nothing needs to listen publicly on the node.
3. In **Overview**, look for the node to move from **Awaiting report** to **Fresh**. Open the node to inspect the last heartbeat, socket read, stream ID and certificate expiry. A fresh report with `unreachable` means delivery is working and the reporter needs access to the orchestrator socket. The service samples immediately and then about once a minute.

### Socket selection and private installation

The invitation command configures the standard socket on an operator host running a snapshot-capable `orchestrator.service`. When `/run/orchestrator-health/health.sock` is missing, the installer discovers the service user and config, provisions a private setgid directory with systemd-tmpfiles for this boot and future boots, and waits for startup sync to finish. It checks the signer's own legacy health listener when `ss` is installed, or its current systemd invocation's startup journal. It stops the service once, reads the final saved config, backs up the original to an owner-only `config.before-health.*` beside it, sets `HealthConfig.SnapshotSocket`, and starts the orchestrator. Existing socket installations skip this service interruption. Run the command in a persistent terminal session for a long initial sync.

The installer verifies the new reporter's first acknowledged heartbeat with a valid snapshot. If the service is running but verification times out, use `journalctl -u bridge-health-reporter -n 20` to see its delivery or local read result.

For a custom service, container, or externally managed socket, pass `--socket /absolute/path` explicitly. This retains the reporter-only install flow. The reporter needs directory traversal and read/write access to that socket. The installer detects an existing socket's non-root GID, or accepts its dedicated group with `--socket-group`. Use a socket outside private home or temporary directories used by systemd sandboxing.

The copied command contains the invitation and may be saved in shell history. For a hidden invitation prompt or a custom socket, download the installer on the node and run it directly. Paste the complete private URL when prompted.

```sh
curl -fsS https://bridgehealth.0x3639.com/install.py -o bridge-health-install.py
sudo python3 -I bridge-health-install.py --socket /run/orchestrator-health/health.sock
systemctl status bridge-health-reporter
journalctl -u bridge-health-reporter -n 20
```

### Cancel, revoke and replace

- **Cancel link** disables an unused invitation. To replace an expired, lost or cancelled link, generate another for the same node.
- **Revoke access** stops an enrolled reporter's heartbeats immediately. Its last stored heartbeat remains available in the dashboard.
- A retry during the invitation window recovers the same certificate only for the original key. If installation was interrupted, run the original command again on the same host.
- Client certificates last up to one year. To replace a certificate or recover from reporter state loss, revoke its access and create a fresh invitation. On an already installed node, run `sudo python3 -I bridge-health-install.py --replace` and paste the new URL. Each new stream has its own key and sequence directory. The installer preserves the old files.
- To stop reporting on the node, run `sudo systemctl disable --now bridge-health-reporter`. For receiver-side revocation, use **Revoke access**.

The managed receiver starts its own fleet. Existing manually enrolled collectors and reporters can continue using the commands below. To move an operator, use a new managed invitation and stop its old reporter after verifying delivery.

## Try the complete flow

The integration test runs the two command-line programs and the **actual Unix socket server and snapshot store** from orchestrator commit `aea5eb03fdb97b77d936d37633e99aab62792c08` on `codex/heartbeat-v1`. It seeds the store with known observations, creates temporary server and operator certificate authorities, generates a reporter key and signed certificate request, and enrolls the issued certificate against one stream. It uses loopback and disposable state. It needs Linux, a Go toolchain with the orchestrator's dependencies, and a checkout of that commit. All Bridge Health binaries build independently from this repository.

```sh
go build -o bin/ ./cmd/...
go test -race ./...
python3 integration/installer_test.py
./integration/test.sh /path/to/orchestrator
```

The test checks that the collector receives the version, sync state and both queue counts. It stops the socket, restarts the reporter, verifies a fresh failure report and preserves the previous snapshot as history. It also exercises duplicate delivery, certificate rotation and revocation, cross-stream posting, TLS rejection, and replay after a collector restart. The script compares both schemas and fixtures with the orchestrator checkout before building.

## Manual certificate enrollment

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

## Run the manually provisioned services

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

## Rotate and revoke manually provisioned credentials

Add the replacement certificate fingerprint to the existing stream for a short overlap. Atomically replace `enrollments.json` and run `docker compose -f deploy/collector.compose.yaml kill -s HUP collector`. Confirm the collector logged the reload. Invalid manifests leave the last valid registry active. Install the new reporter credential, restart only the reporter, and verify the next sequence arrives. Remove the old fingerprint and reload again. To revoke immediately, remove the affected fingerprint and reload. To retire a stream, remove its enrollment entirely.

## Pilot cutover

During the pilot, keep the existing scraper running and compare its results with authenticated heartbeats. Check a normal report, a stopped socket and a silent reporter. Switch alerts for each operator after verifying freshness, and monitor the collector itself independently. Once the replacement alerts are live, bind the old orchestrator health port to loopback, remove its public proxy and firewall exposure, and verify from outside that heartbeats continue and the old health endpoint is closed.
