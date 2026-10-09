#!/usr/bin/env python3
"""Exercise a real service process, generated MMDBs, reload and shutdown."""
import json
import os
from pathlib import Path
import signal
import socket
import subprocess
import tempfile
import time
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[1]


def main():
    with tempfile.TemporaryDirectory(prefix="cch-geoip-smoke-") as tmp:
        subprocess.run([ROOT / "bin/fixtures", tmp, "Before"], check=True)
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            port = sock.getsockname()[1]
        env = {k: v for k, v in os.environ.items()
               if not k.startswith(("GEOIP_", "THREAT_", "ABUSEIPDB_"))}
        env.update(GEOIP_LISTEN=f"127.0.0.1:{port}",
                   GEOIP_CITY_DB=f"{tmp}/GeoLite2-City.mmdb",
                   GEOIP_ASN_DB=f"{tmp}/GeoLite2-ASN.mmdb",
                   GEOIP_API_TOKEN="synthetic-smoke-token",
                   GEOIP_RELOAD_INTERVAL="100ms")
        with open(f"{tmp}/service.log", "w+") as log:
            process = subprocess.Popen([ROOT / "bin/cch-geoip"], env=env, stdout=log, stderr=log)
            def get(path, auth=True):
                headers = {"Authorization": "Bearer synthetic-smoke-token"} if auth else {}
                req = urllib.request.Request(f"http://127.0.0.1:{port}{path}", headers=headers)
                with urllib.request.urlopen(req, timeout=2) as response:
                    return json.load(response)

            def wait_for(predicate):
                deadline = time.monotonic() + 10
                while time.monotonic() < deadline:
                    if process.poll() is not None:
                        log.seek(0)
                        raise RuntimeError(log.read())
                    try:
                        if predicate():
                            return
                    except (OSError, urllib.error.URLError):
                        pass
                    time.sleep(.05)
                raise AssertionError("service condition timed out")

            try:
                wait_for(lambda: get("/readyz")["ready"])
                data = get("/v1/ip2location/8.8.8.8?lang=zh-CN")
                assert data["location"]["country"]["name"] == "美国"
                assert data["connection"]["asn"] == 15169
                assert data["location"]["city"] == "Before"
                assert data["_meta"]["threat_sources"]["abuseipdb"]["state"] == "disabled"
                try:
                    get("/v1/ip2location/8.8.8.8", auth=False)
                    raise AssertionError("authentication bypass")
                except urllib.error.HTTPError as error:
                    assert error.code == 401
                subprocess.run([ROOT / "bin/fixtures", tmp, "After"], check=True)
                process.send_signal(signal.SIGHUP)
                wait_for(lambda: get("/v1/ip2location/8.8.8.8")["location"]["city"] == "After")
                bad = Path(tmp) / "bad.mmdb"
                bad.write_bytes(b"corrupt")
                bad.replace(Path(tmp) / "GeoLite2-City.mmdb")
                process.send_signal(signal.SIGHUP)
                wait_for(lambda: not get("/readyz")["reload_healthy"])
                assert get("/v1/ip2location/8.8.8.8")["location"]["city"] == "After"
                subprocess.run([ROOT / "bin/cch-geoip", "healthcheck", f"http://127.0.0.1:{port}/healthz"], check=True)
                process.terminate()
                assert process.wait(timeout=10) == 0
            finally:
                if process.poll() is None:
                    process.terminate()
                    process.wait(timeout=10)
    print("PASS: process HTTP/auth/localization/MMDB hot reload/corruption fallback/healthcheck/SIGTERM")


if __name__ == "__main__":
    main()
