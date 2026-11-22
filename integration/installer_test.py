"""Exercise the installed files and service commands without changing the test host."""
import hashlib
import io
import json
import os
from pathlib import Path
import ssl
import stat
import subprocess
import sys
import tempfile
import unittest
from types import ModuleType, SimpleNamespace
from unittest.mock import Mock, patch

SOURCE = Path(__file__).resolve().parents[1] / "internal/receiver/assets/install.py"
installer = ModuleType("bridge_installer")
exec(compile(SOURCE.read_bytes(), str(SOURCE), "exec"), installer.__dict__)


class InstallerTest(unittest.TestCase):
    def test_install_and_retry(self):
        stream = "11d4eeee-8972-4101-82bb-9397366ee437"
        token = stream + "." + "a" * 43
        origin = "https://bridgehealth.0x3639.com"
        binary = b"test reporter binary"
        commands = []
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            unit, config, executable = root / "reporter.service", root / "etc", root / "bin/reporter"
            ca_key, ca_crt = root / "ca.key", root / "ca.crt"
            subprocess.run(["openssl", "req", "-new", "-newkey", "rsa:2048", "-x509", "-nodes",
                            "-subj", "/CN=Test CA", "-keyout", str(ca_key), "-out", str(ca_crt)],
                           check=True, capture_output=True)

            def open_url(req, timeout):
                if isinstance(req, str):
                    self.assertEqual(req, origin + "/downloads/bridge-health-reporter-linux-arm64")
                    return io.BytesIO(binary)
                self.assertEqual(req.full_url, origin + "/v1/enroll")
                self.assertEqual(req.get_header("Authorization"), "Bearer " + token)
                cert = subprocess.run(["openssl", "x509", "-req", "-CA", str(ca_crt), "-CAkey",
                                       str(ca_key), "-set_serial", "1"], input=req.data,
                                      check=True, capture_output=True).stdout.decode()
                return io.BytesIO(json.dumps(dict(stream=stream, ca=ca_crt.read_text(),
                                                  collector=origin + ":8443/v1/heartbeats",
                                                  certificate=cert)).encode())

            def command(*args, **kwargs):
                if args[0] == "openssl":
                    return subprocess.run(args, check=True, **kwargs)
                commands.append(args)

            args = SimpleNamespace(socket=str(root / "health.sock"), socket_group=None, replace=False)
            gid = os.getgid()
            with patch.multiple(installer, UNIT=unit, CONFIG=config, BINARY=executable, BASE_URL=origin,
                                CHECKSUMS={"arm64": hashlib.sha256(binary).hexdigest()}, run=command,
                                service_group=lambda: gid), \
                 patch.object(installer.http, "open", open_url), \
                 patch.object(installer.platform, "machine", return_value="aarch64"), \
                 patch.object(installer.platform, "system", return_value="Linux"), \
                 patch.object(Path, "is_dir", return_value=True), \
                 patch.object(installer.shutil, "which", return_value="/usr/bin/test"), \
                 patch.object(os, "geteuid", return_value=0), \
                 patch.object(os, "chown"), patch.object(os, "fchown"), patch.object(os, "umask"), \
                 patch("builtins.print"):
                def install(value):
                    with patch("sys.stdin", io.StringIO(value + "\n")):
                        installer.install(args)

                install(origin + "/install#" + token)
                key = config / stream / "reporter.key"
                before = key.read_bytes()
                install(token)
                self.assertEqual(key.read_bytes(), before)
                self.assertEqual(key.stat().st_mode & 0o777, 0o640)
                self.assertEqual(executable.read_bytes(), binary)
                self.assertEqual(executable.stat().st_mode & 0o777, 0o755)
                self.assertIn("StateDirectory=bridge-health-reporter/" + stream, unit.read_text())
                self.assertIn("-collector " + origin + ":8443/v1/heartbeats", unit.read_text())
                self.assertIn(("systemctl", "enable", "bridge-health-reporter"), commands)
                self.assertIn(("systemctl", "restart", "bridge-health-reporter"), commands)
                context = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
                context.load_cert_chain(config / stream / "reporter.crt", key)
                context.load_verify_locations(config / stream / "server-ca.crt")
                with self.assertRaisesRegex(ValueError, "different stream"):
                    install("22" + token[2:])
                with patch.object(installer, "CHECKSUMS", {"arm64": "0" * 64}):
                    with self.assertRaisesRegex(ValueError, "SHA-256"):
                        install(token)

                # The invitation page passes no socket arguments. Exercise first
                # setup and a later run using the socket already created.
                args.socket = None
                socket_path = root / "health.sock"
                real_stat = os.stat
                def socket_stat(path, *a, **kw):
                    info = real_stat(path, *a, **kw)
                    if Path(path) == socket_path:
                        return SimpleNamespace(st_mode=stat.S_IFSOCK | 0o660, st_gid=gid)
                    return info
                with patch.object(os, "stat", socket_stat), \
                     patch.object(installer, "DEFAULT_SOCKET", str(socket_path)), \
                     patch.object(installer, "enable_snapshot", side_effect=lambda _: socket_path.touch()) as enable, \
                     patch.object(installer, "verify_delivery") as verify:
                    install(token)
                    install(token)
                    enable.assert_called_once_with(gid)
                    self.assertEqual(verify.call_count, 2)

    def test_exact_backup_and_retry(self):
        with tempfile.TemporaryDirectory() as tmp:
            cfg = Path(tmp) / "config.json"
            original = b'{"HealthConfig":{"Port":55000},"large":123456789123456789,"secret":"unchanged"}'
            cfg.write_bytes(original)
            edit = [sys.executable, "-I", "-c", installer.SIGNER_CONFIG, str(cfg), installer.DEFAULT_SOCKET]
            self.assertEqual(subprocess.check_output(edit + ["check"]).strip(), b"55000")
            subprocess.run(edit + ["write"], check=True)
            subprocess.run(edit + ["write"], check=True)
            restored = json.loads(cfg.read_bytes())
            self.assertEqual(restored["HealthConfig"].pop("SnapshotSocket"), installer.DEFAULT_SOCKET)
            self.assertEqual(restored, json.loads(original))
            backup, = Path(tmp).glob("config.before-health.*")
            self.assertEqual(backup.read_bytes(), original)
            for f in (cfg, backup):
                self.assertEqual(f.stat().st_mode & 0o777, 0o600)
            other = edit[:]
            other[-1] = "/run/custom.sock"
            self.assertNotEqual(subprocess.run(other + ["write"], capture_output=True).returncode, 0)
            self.assertEqual(json.loads(cfg.read_text())["HealthConfig"]["SnapshotSocket"], installer.DEFAULT_SOCKET)

    def test_shutdown_config_is_read_after_stop_and_restart_even_on_failure(self):
        account = SimpleNamespace(pw_name="signer")
        for fail in (False, True):
            steps = []
            def command(*args, **kw):
                if args[0] == "runuser":
                    self.assertEqual(args[2], "signer")
                    steps.append(args[-1])
                    if fail and args[-1] == "write":
                        raise OSError("write failed")
                    return SimpleNamespace(stdout=b"55000")
                steps.append(args[1])
            with patch.object(installer, "signer_instance", return_value=(account, "/cfg", "active", 42)), \
                 patch.object(installer, "prepare_socket_dir", side_effect=lambda *_: steps.append("directory")), \
                 patch.object(installer, "wait_for_startup", side_effect=lambda *_: steps.append("ready")), \
                 patch.object(installer, "run", command), patch("builtins.print"):
                if fail:
                    with self.assertRaises(OSError):
                        installer.enable_snapshot(99)
                else:
                    installer.enable_snapshot(99)
            self.assertEqual(steps, ["check", "directory", "ready", "stop", "write", "start"])

    def test_tmpfiles_rule_recreates_private_runtime_dir(self):
        account = SimpleNamespace(pw_uid=os.getuid(), pw_name="signer")
        gid = os.getgid()
        with tempfile.TemporaryDirectory() as tmp:
            directory = Path(tmp) / "health"
            rule = Path(tmp) / "health.conf"
            lstat = Path.lstat
            def linux_stat(path):
                info = lstat(path)
                if path == directory and sys.platform != "linux":
                    # macOS temp volumes clear setgid on directories.
                    return SimpleNamespace(st_mode=info.st_mode | stat.S_ISGID,
                                           st_uid=info.st_uid, st_gid=info.st_gid)
                return info
            def tmpfiles(*args):
                self.assertEqual(args, ("systemd-tmpfiles", "--create", str(rule)))
                self.assertEqual(rule.read_text(), f"d {directory} 2750 {account.pw_uid} {gid} -\n")
                directory.mkdir(exist_ok=True)
                os.chown(directory, -1, gid)
                directory.chmod(0o2750)
            with patch.object(installer, "DEFAULT_SOCKET", str(directory / "health.sock")), \
                 patch.object(installer, "TMPFILES", rule), patch.object(installer, "run", tmpfiles), \
                 patch.object(os, "fchown"), patch.object(Path, "lstat", linux_stat):
                installer.prepare_socket_dir(account, gid)
                directory.rmdir()
                installer.prepare_socket_dir(account, gid)
                self.assertEqual(directory.lstat().st_mode & 0o7777, 0o2750)

    def test_waits_for_current_invocation_and_for_delivery(self):
        invocation = "f" * 32
        empty = SimpleNamespace(stdout=b"", returncode=1)
        ready = SimpleNamespace(stdout=b"Starting health rpc server on localhost", returncode=0)
        delivered = SimpleNamespace(stdout=b"heartbeat delivered (local read ok)", returncode=0)
        socket_failure = SimpleNamespace(stdout=b"heartbeat delivered (local read unreachable)", returncode=0)
        journal = Mock(side_effect=[empty, ready, socket_failure, delivered])
        with patch.object(installer.subprocess, "run", journal), \
             patch.object(installer.shutil, "which", return_value=None), \
             patch.object(installer, "signer_instance", return_value=(None, None, invocation, 42)), \
             patch.object(installer, "run", return_value=SimpleNamespace(stdout=invocation)), \
             patch.object(installer.time, "sleep") as sleep, patch("builtins.print"):
            installer.wait_for_startup(invocation, 42, 55000)
            installer.verify_delivery()
        self.assertEqual(sleep.call_count, 2)
        for call in journal.call_args_list:
            self.assertIn("_SYSTEMD_INVOCATION_ID=" + invocation, call[0][0])


if __name__ == "__main__":
    unittest.main()
