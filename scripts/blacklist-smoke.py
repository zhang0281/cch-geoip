#!/usr/bin/env python3
"""Verify process wiring of local-only AbuseIPDB with fresh and stale snapshots."""
import base64
from datetime import datetime, timedelta, timezone
import json
import http.client
import os
from pathlib import Path
import socket
import subprocess
import tempfile
import time

ROOT = Path(__file__).resolve().parents[1]


def run_case(age_hours):
    with tempfile.TemporaryDirectory(prefix="cch-blacklist-smoke-") as tmp:
        subprocess.run([ROOT / "bin/fixtures", tmp], check=True)
        now = datetime.now(timezone.utc)
        generated = now - timedelta(hours=age_hours)
        payload = {"meta": {"generatedAt": generated.isoformat()}, "data": [
            {"ipAddress": "8.8.8.8", "abuseConfidenceScore": 95,
             "lastReportedAt": (generated - timedelta(hours=1)).isoformat()}]}
        snapshot = {"Attempt": now.isoformat(), "Updated": generated.isoformat(),
                    "Data": base64.b64encode(json.dumps(payload).encode()).decode()}
        (Path(tmp) / "abuseipdb_blacklist.json").write_text(json.dumps(snapshot))
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            port = sock.getsockname()[1]
        env = {k: v for k, v in os.environ.items()
               if not k.startswith(("GEOIP_", "THREAT_", "ABUSEIPDB_"))}
        env.update(GEOIP_LISTEN=f"127.0.0.1:{port}",
                   GEOIP_CITY_DB=f"{tmp}/GeoLite2-City.mmdb",
                   GEOIP_ASN_DB=f"{tmp}/GeoLite2-ASN.mmdb", THREAT_CACHE_DIR=tmp,
                   ABUSEIPDB_ENABLED="true", ABUSEIPDB_API_KEY="synthetic-key",
                   ABUSEIPDB_CHECK_ENABLED="false")
        with open(f"{tmp}/service.log", "w+") as log:
            process = subprocess.Popen([ROOT / "bin/cch-geoip"], env=env, stdout=log, stderr=log)
            try:
                verify_response(process, port, age_hours)
                process.terminate()
                assert process.wait(timeout=10) == 0
            finally:
                if process.poll() is None:
                    process.terminate()
                    process.wait(timeout=10)


def verify_response(process, port, age_hours):
    deadline = time.monotonic() + 10
    while True:
        assert process.poll() is None, "service exited"
        try:
            data = query(port, "8.8.8.8")
            break
        except (OSError, http.client.HTTPException):
            assert time.monotonic() < deadline, "service not ready"
            time.sleep(.05)
    sources = data["_meta"]["threat_sources"]
    if age_hours == 0:
        assert data["threat"]["score"] == .95 and data["threat"]["is_threat"]
        assert data["threat"]["blocklists"][0]["name"] == "AbuseIPDB BLACKLIST"
        assert data["threat"]["blocklists"][0]["listed_at"] is None
        assert sources["abuseipdb_blacklist"]["last_reported_at"]
        assert sources["abuseipdb_check"]["state"] == "skipped"
    else:
        assert data["threat"]["score"] == 0 and not data["threat"]["is_threat"]
        assert sources["abuseipdb_blacklist"]["state"] == "stale"
        assert sources["abuseipdb"]["state"] == "unavailable"
    missing = query(port, "1.1.1.1")
    assert missing["_meta"]["threat_sources"]["abuseipdb_check"]["state"] == "disabled"
    assert missing["_meta"]["threat_sources"]["abuseipdb"]["state"] == "unavailable"


def query(port, ip):
    connection = http.client.HTTPConnection("127.0.0.1", port, timeout=2)
    try:
        connection.request("GET", f"/v1/ip2location/{ip}")
        response = connection.getresponse()
        assert response.status == 200
        return json.loads(response.read())
    finally:
        connection.close()


if __name__ == "__main__":
    run_case(0)
    run_case(96)
    print("PASS: process BLACKLIST restore/local priority/CCH threat mapping/stale exclusion/local-only unknown")
