#!/usr/bin/env python3
"""Install the Bridge Health reporter on a systemd Linux host."""
import argparse
import fcntl
import getpass
import grp
import hashlib
import json
import os
from pathlib import Path
import platform
import pwd
import re
import shutil
import stat
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request

BASE_URL = "@@BASE_URL@@"
CHECKSUMS = "@@CHECKSUMS@@"
SERVICE = "bridge-health-reporter"
CONFIG = Path("/etc") / SERVICE
UNIT = Path("/etc/systemd/system") / (SERVICE + ".service")
BINARY = Path("/usr/local/bin") / SERVICE
DEFAULT_SOCKET = "/run/orchestrator-health/health.sock"
TMPFILES = Path("/etc/tmpfiles.d/orchestrator-health.conf")


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


http = urllib.request.build_opener(NoRedirect())


def atomic_write(path, data, mode, gid=0):
    with tempfile.NamedTemporaryFile(dir=path.parent, delete=False) as out:
        tmp = Path(out.name)
        try:
            out.write(data)
            out.flush()
            os.fchown(out.fileno(), 0, gid)
            os.fchmod(out.fileno(), mode)
            os.fsync(out.fileno())
            os.replace(tmp, path)
            fd = os.open(path.parent, os.O_DIRECTORY)
            try:
                os.fsync(fd)
            finally:
                os.close(fd)
        finally:
            if tmp.exists():
                tmp.unlink()


def run(*args, **kwargs):
    return subprocess.run(args, check=True, **kwargs)


def service_group():
    try:
        grp.getgrnam(SERVICE)
    except KeyError:
        run("groupadd", "--system", SERVICE)
    gid = grp.getgrnam(SERVICE).gr_gid
    try:
        account = pwd.getpwnam(SERVICE)
    except KeyError:
        run("useradd", "--system", "--gid", SERVICE, "--home-dir", "/nonexistent",
            "--no-create-home", "--shell", "/usr/sbin/nologin", SERVICE)
        account = pwd.getpwnam(SERVICE)
    if account.pw_uid == 0 or gid == 0 or account.pw_gid != gid:
        raise ValueError("The reporter requires a dedicated non-root service account and group.")
    return gid


def invitation():
    if sys.stdin.isatty():
        value = getpass.getpass("Paste your private installation URL: ")
    else:
        value = sys.stdin.readline(512).strip()
    token = value[len(BASE_URL + "/install#"):] if value.startswith(BASE_URL + "/install#") else value
    if not re.fullmatch(r"[a-f0-9]{8}-(?:[a-f0-9]{4}-){3}[a-f0-9]{12}\.[A-Za-z0-9_-]{43}", token):
        raise ValueError("Open your complete invitation link and copy the installation command again.")
    return token


def download(arch):
    name = "bridge-health-reporter-linux-" + arch
    with http.open(BASE_URL + "/downloads/" + name, timeout=90) as response:
        binary = response.read(64 * 1024 * 1024 + 1)
    if hashlib.sha256(binary).hexdigest() != CHECKSUMS[arch]:
        raise ValueError("Reporter download failed its SHA-256 check. Fetch the installer again.")
    return binary


def credentials(folder, token, gid):
    key = folder / "reporter.key"
    if not key.exists():
        new = run("openssl", "genpkey", "-algorithm", "EC", "-pkeyopt",
                  "ec_paramgen_curve:P-256", stdout=subprocess.PIPE).stdout
        atomic_write(key, new, 0o640, gid)
    # The same node key can recover an enrollment if the HTTPS response was lost.
    csr = run("openssl", "req", "-new", "-key", str(key), "-subj", "/CN=bridge-health-reporter",
              stdout=subprocess.PIPE).stdout
    request = urllib.request.Request(BASE_URL + "/v1/enroll", data=csr, headers={
        "Authorization": "Bearer " + token, "Content-Type": "application/pkcs10"})
    with http.open(request, timeout=20) as response:
        bundle = json.loads(response.read(32768))
    if bundle["stream"] != folder.name or not re.fullmatch(
            r"https://[a-zA-Z0-9.:-]+/v1/heartbeats", bundle["collector"]):
        raise ValueError("Unexpected receiver enrollment response.")
    atomic_write(folder / "reporter.crt", bundle["certificate"].encode(), 0o640, gid)
    atomic_write(folder / "server-ca.crt", bundle["ca"].encode(), 0o640, gid)
    return bundle["collector"]


def unit_text(stream, socket, socket_gid, endpoint):
    config = CONFIG / stream
    group = "SupplementaryGroups=" + str(socket_gid) + "\n" if socket_gid else ""
    return f"""# Managed by Bridge Health: {stream}
[Unit]
Description=Bridge Health reporter
After=network-online.target
Wants=network-online.target

[Service]
User={SERVICE}
Group={SERVICE}
{group}StateDirectory={SERVICE}/{stream}
StateDirectoryMode=0700
UMask=0077
ExecStart={BINARY} -socket {socket} -collector {endpoint} -stream {stream} -state /var/lib/{SERVICE}/{stream} -cert {config}/reporter.crt -key {config}/reporter.key -server-ca {config}/server-ca.crt
Restart=on-failure
RestartSec=5
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
CapabilityBoundingSet=

[Install]
WantedBy=multi-user.target
"""


# Execute only this config edit as the signer account. The reporter must never
# gain access to the signer's data directory or receive the rest of its config.
SIGNER_CONFIG = r"""
import json, os, stat, sys, tempfile
from pathlib import Path

path = Path(sys.argv[1])
with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK), "rb") as f:
    st = os.fstat(f.fileno())
    if not stat.S_ISREG(st.st_mode) or st.st_uid != os.getuid() or st.st_size > 1024 * 1024:
        sys.exit("Expected the orchestrator user's regular config.json (at most 1 MiB).")
    original = f.read(1024 * 1024 + 1)
cfg = json.loads(original)
health = cfg.setdefault("HealthConfig", {})
current = health.get("SnapshotSocket") or ""
if current and current != sys.argv[2]:
    sys.exit("The orchestrator has a custom SnapshotSocket. Use --socket with that path.")
if sys.argv[3] == "check":
    print(health.get("Port") or 0)
if sys.argv[3] == "write" and not current:
    health["SnapshotSocket"] = sys.argv[2]
    data = (json.dumps(cfg, indent=2, allow_nan=False) + "\n").encode()
    if len(data) > 1024 * 1024:
        sys.exit("Updated config.json would exceed the orchestrator's 1 MiB limit.")
    with tempfile.NamedTemporaryFile(dir=path.parent, prefix="config.before-health.", delete=False) as backup:
        backup.write(original)
        backup.flush()
        os.fsync(backup.fileno())
    with tempfile.NamedTemporaryFile(dir=path.parent, prefix=".health-config.", delete=False) as out:
        try:
            out.write(data)
            out.flush()
            os.fsync(out.fileno())
            os.replace(out.name, path)
        finally:
            if os.path.exists(out.name):
                os.unlink(out.name)
    d = os.open(path.parent, os.O_DIRECTORY)
    try:
        os.fsync(d)
    finally:
        os.close(d)
"""


def signer_instance():
    output = run("systemctl", "show", "orchestrator.service", "-p", "MainPID", "-p",
                 "User", "-p", "InvocationID", stdout=subprocess.PIPE, text=True).stdout
    fields = dict(line.split("=", 1) for line in output.splitlines() if "=" in line)
    pid = int(fields.get("MainPID") or 0)
    invocation = fields.get("InvocationID", "")
    if pid <= 0 or not re.fullmatch("[0-9a-fA-F]{32}", invocation):
        raise ValueError("Start orchestrator.service before automatic setup, or use --socket for an externally managed socket.")
    user = fields.get("User") or "root"
    account = pwd.getpwuid(int(user)) if user.isdecimal() else pwd.getpwnam(user)
    env = Path("/proc", str(pid), "environ").read_bytes().split(b"\0")
    home = next((os.fsdecode(v[5:]) for v in env if v.startswith(b"HOME=") and v[5:]), account.pw_dir)
    if not os.path.isabs(home):
        raise ValueError("The orchestrator's HOME must be an absolute path.")
    return account, Path(home) / ".orchestrator/config.json", invocation, pid


def prepare_socket_dir(account, gid):
    directory = Path(DEFAULT_SOCKET).parent
    if directory.is_symlink() or (directory.exists() and (
            not directory.is_dir() or directory.stat().st_uid != account.pw_uid)):
        raise ValueError("The health socket directory must be owned by the orchestrator user.")
    if not TMPFILES.exists():
        rule = f"d {directory} 2750 {account.pw_uid} {gid} -\n"
        atomic_write(TMPFILES, rule.encode(), 0o644)
    run("systemd-tmpfiles", "--create", str(TMPFILES))
    info = directory.lstat()
    if (not stat.S_ISDIR(info.st_mode) or stat.S_IMODE(info.st_mode) != 0o2750
            or info.st_uid != account.pw_uid or info.st_gid != gid):
        raise ValueError(f"Check {TMPFILES}: {directory} needs mode 2750, owner {account.pw_name}, group {gid}.")


def wait_for_startup(invocation, pid, port):
    # This marker follows Zenon and *all* EVM startup scans, even when a short
    # scan didn't log a percentage. A previous service instance cannot match.
    print("Waiting for the running orchestrator to finish startup sync before enabling its socket.", flush=True)
    deadline = time.monotonic() + 12 * 3600
    while True:
        # Long-running nodes may have rotated the startup journal. The legacy
        # HTTP health listener is opened by the signer after its startup scans.
        if 0 < port < 65536 and shutil.which("ss"):
            listeners = subprocess.run(["ss", "-H", "-ltnp", "sport", "=", ":" + str(port)],
                                       stdout=subprocess.PIPE)
            if listeners.returncode == 0 and f"pid={pid},".encode() in listeners.stdout:
                return
        logs = subprocess.run(["journalctl", "-q", "--no-pager", "-n", "1", "-o", "cat",
                               "_SYSTEMD_INVOCATION_ID=" + invocation,
                               "--grep=Starting health rpc server on"], stdout=subprocess.PIPE, check=False)
        if logs.returncode == 0 and b"Starting health rpc server on" in logs.stdout:
            return
        if logs.returncode not in (0, 1) or time.monotonic() >= deadline:
            raise ValueError("Could not confirm startup sync in the current systemd journal. The orchestrator was left running.")
        if signer_instance()[2] != invocation:
            raise ValueError("The orchestrator restarted during installation. Rerun the installer.")
        time.sleep(10)


def enable_snapshot(gid):
    account, config, invocation, pid = signer_instance()
    edit = ("runuser", "-u", account.pw_name, "--", sys.executable, "-I", "-c",
            SIGNER_CONFIG, str(config), DEFAULT_SOCKET)
    port = int(run(*edit, "check", stdout=subprocess.PIPE).stdout)
    prepare_socket_dir(account, gid)
    wait_for_startup(invocation, pid, port)
    if signer_instance()[2] != invocation:
        raise ValueError("The orchestrator instance changed before setup. Rerun the installer.")
    # A graceful stop persists the in-memory config. Read the file again and
    # edit only after a successful stop. Even a failed stop can take the signer down.
    print("Startup sync finished. Stopping orchestrator.service to set HealthConfig.SnapshotSocket.", flush=True)
    try:
        run("systemctl", "stop", "orchestrator.service")
        run(*edit, "write")
    finally:
        run("systemctl", "start", "orchestrator.service")


def running_reporter_invocation():
    output = run("systemctl", "show", SERVICE, "-p", "ActiveState", "-p", "InvocationID",
                 stdout=subprocess.PIPE, text=True).stdout
    fields = dict(line.split("=", 1) for line in output.splitlines() if "=" in line)
    invocation = fields.get("InvocationID", "")
    if fields.get("ActiveState") == "active" and re.fullmatch("[0-9a-fA-F]{32}", invocation):
        return invocation
    return None


def verify_delivery():
    # The reporter emits this acknowledgement only after validating the
    # socket response as its own user and receiving a successful delivery.
    deadline = time.monotonic() + 120
    while time.monotonic() < deadline:
        invocation = running_reporter_invocation()
        if invocation:
            entry = subprocess.run(["journalctl", "-q", "--no-pager", "-n", "1", "-o", "cat",
                                    "_SYSTEMD_INVOCATION_ID=" + invocation,
                                    "--grep=heartbeat delivered .*local read ok"], stdout=subprocess.PIPE)
            if (entry.returncode == 0 and b"heartbeat delivered (local read ok)" in entry.stdout
                    and running_reporter_invocation() == invocation):
                return
        time.sleep(2)
    raise ValueError("Reporter started but no successful snapshot heartbeat was confirmed. "
                     "Check journalctl -u bridge-health-reporter -n 20.")


def socket_group_id(path, automatic):
    if automatic and Path(path).parent.is_symlink():
        raise ValueError("Automatic setup requires a direct socket path.")
    try:
        sock = os.stat(path, follow_symlinks=not automatic)
    except FileNotFoundError:
        return None
    if automatic and stat.S_ISLNK(sock.st_mode):
        raise ValueError("Automatic setup requires a direct socket path.")
    if not stat.S_ISSOCK(sock.st_mode):
        raise ValueError("The configured health socket path is not a Unix socket.")
    return sock.st_gid


def install(args):
    arch = {"x86_64": "amd64", "aarch64": "arm64", "arm64": "arm64"}.get(platform.machine())
    if platform.system() != "Linux" or not arch or not Path("/run/systemd/system").is_dir():
        raise ValueError("This installer needs Linux amd64 or arm64 with systemd.")
    if os.geteuid() != 0:
        raise ValueError("Run this installer with sudo.")
    automatic = args.socket is None
    socket = args.socket or DEFAULT_SOCKET
    tools = ["openssl", "systemctl", "groupadd", "useradd"]
    if automatic:
        tools.extend(["runuser", "systemd-tmpfiles", "journalctl"])
    for tool in tools:
        if not shutil.which(tool):
            raise ValueError("Install the missing command: " + tool)
    if not re.fullmatch(r"/[A-Za-z0-9_/.-]+", socket):
        raise ValueError("Use an absolute socket path containing letters, numbers, /, _, - or .")
    token = invitation()
    stream = token.split(".", 1)[0]
    os.umask(0o077)
    CONFIG.mkdir(mode=0o750, exist_ok=True)
    with (CONFIG / ".install.lock").open("a") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        if UNIT.exists():
            marker = UNIT.read_text().partition("\n")[0]
            if not marker.startswith("# Managed by Bridge Health: "):
                raise ValueError(f"An existing service owns {UNIT}. Review it before installing.")
            if marker != "# Managed by Bridge Health: " + stream and not args.replace:
                raise ValueError("A different stream is installed. Use --replace to switch this host to the new invitation.")
        sock_gid = socket_group_id(socket, automatic)
        binary = download(arch)
        gid = service_group()
        if args.socket_group:
            sock_gid = grp.getgrnam(args.socket_group).gr_gid
            if not sock_gid:
                raise ValueError("Choose a dedicated non-root socket group.")
        # Only root writes credentials and unit files. The service can read its own key.
        os.chown(CONFIG, 0, gid)
        os.chmod(CONFIG, 0o750)
        folder = CONFIG / stream
        folder.mkdir(mode=0o750, exist_ok=True)
        os.chown(folder, 0, gid)
        os.chmod(folder, 0o750)
        endpoint = credentials(folder, token, gid)
        if automatic:
            current_gid = socket_group_id(socket, automatic)
            if not args.socket_group:
                sock_gid = current_gid
            if current_gid is None:
                sock_gid = sock_gid or gid
                enable_snapshot(sock_gid)
        if not BINARY.parent.exists():
            BINARY.parent.mkdir(parents=True)
            BINARY.parent.chmod(0o755)
        atomic_write(BINARY, binary, 0o755)
        atomic_write(UNIT, unit_text(stream, socket, sock_gid, endpoint).encode(), 0o644)
        run("systemctl", "daemon-reload")
        run("systemctl", "enable", SERVICE)
        run("systemctl", "restart", SERVICE)
        run("systemctl", "is-active", "--quiet", SERVICE)
        if automatic:
            verify_delivery()
    if automatic:
        print("Reporter enrolled. The receiver acknowledged a valid health snapshot.")
    else:
        print("Reporter enrolled and started. Check delivery in the receiver dashboard.")
    if not automatic and not sock_gid:
        print(f"Grant {SERVICE} access to {socket} using a dedicated socket group. "
              "The reporter can report a socket outage until access is available.")
    print("Logs: journalctl -u bridge-health-reporter -n 20")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--socket", help="use an externally managed socket, skipping automatic orchestrator setup")
    parser.add_argument("--socket-group", help="dedicated health socket group, including before the socket exists")
    parser.add_argument("--replace", action="store_true", help="switch an existing managed service to a new stream")
    try:
        install(parser.parse_args())
    except urllib.error.HTTPError as exc:
        messages = {403: "Invitation expired, used by another key, or cancelled. Ask for a new link.",
                    429: "Receiver is busy. Run the same command again shortly."}
        sys.exit(messages.get(exc.code, "Receiver returned HTTP " + str(exc.code)))
    except (OSError, ValueError, KeyError, subprocess.CalledProcessError) as exc:
        sys.exit("Installation failed: " + str(exc))
