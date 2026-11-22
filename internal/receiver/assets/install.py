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
import urllib.error
import urllib.request

BASE_URL = "@@BASE_URL@@"
CHECKSUMS = "@@CHECKSUMS@@"
SERVICE = "bridge-health-reporter"
CONFIG = Path("/etc") / SERVICE
UNIT = Path("/etc/systemd/system") / (SERVICE + ".service")
BINARY = Path("/usr/local/bin") / SERVICE
DEFAULT_SOCKET = "/run/orchestrator-health/health.sock"


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


def install(args):
    arch = {"x86_64": "amd64", "aarch64": "arm64", "arm64": "arm64"}.get(platform.machine())
    if platform.system() != "Linux" or not arch or not Path("/run/systemd/system").is_dir():
        raise ValueError("This installer needs Linux amd64 or arm64 with systemd.")
    if os.geteuid() != 0:
        raise ValueError("Run this installer with sudo.")
    for tool in ("openssl", "systemctl", "groupadd", "useradd"):
        if not shutil.which(tool):
            raise ValueError("Install the missing command: " + tool)
    if not re.fullmatch(r"/[A-Za-z0-9_/.-]+", args.socket):
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
        sock_gid = None
        if Path(args.socket).exists():
            sock = os.stat(args.socket)
            if not stat.S_ISSOCK(sock.st_mode):
                raise ValueError("The configured health socket path is not a Unix socket.")
            sock_gid = sock.st_gid
        if args.socket_group:
            sock_gid = grp.getgrnam(args.socket_group).gr_gid
            if not sock_gid:
                raise ValueError("Choose a dedicated non-root socket group.")
        binary = download(arch)
        gid = service_group()
        # Only root writes credentials and unit files. The service can read its own key.
        os.chown(CONFIG, 0, gid)
        os.chmod(CONFIG, 0o750)
        folder = CONFIG / stream
        folder.mkdir(mode=0o750, exist_ok=True)
        os.chown(folder, 0, gid)
        os.chmod(folder, 0o750)
        endpoint = credentials(folder, token, gid)
        if not BINARY.parent.exists():
            BINARY.parent.mkdir(parents=True)
            BINARY.parent.chmod(0o755)
        atomic_write(BINARY, binary, 0o755)
        atomic_write(UNIT, unit_text(stream, args.socket, sock_gid, endpoint).encode(), 0o644)
        run("systemctl", "daemon-reload")
        run("systemctl", "enable", SERVICE)
        run("systemctl", "restart", SERVICE)
        run("systemctl", "is-active", "--quiet", SERVICE)
    print("Reporter enrolled and started. Check delivery in the receiver dashboard.")
    if sock_gid is None or sock_gid == 0:
        print(f"Grant {SERVICE} access to {args.socket} using a dedicated socket group. "
              "The reporter can report a socket outage until access is available.")
    print("Logs: journalctl -u bridge-health-reporter -n 20")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--socket", default=DEFAULT_SOCKET)
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
