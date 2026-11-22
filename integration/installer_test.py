"""Exercise the installed files and service commands without changing the test host."""
import hashlib
import io
import json
import os
from pathlib import Path
import ssl
import subprocess
import tempfile
import unittest
from types import ModuleType, SimpleNamespace
from unittest.mock import patch

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


if __name__ == "__main__":
    unittest.main()
