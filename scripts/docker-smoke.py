#!/usr/bin/env python3
"""Verify final-image UID, MMDB mounts, health, writable cache and CA bundle."""
import json
import http.client
import os
from pathlib import Path
import subprocess
import tempfile
import time
import uuid

ROOT = Path(__file__).resolve().parents[1]
IMAGE = os.environ.get("CCH_TEST_IMAGE", "cch-geoip:local")


def run(*args):
    return subprocess.check_output(args, text=True).strip()


def main():
    name = "cch-geoip-test-" + uuid.uuid4().hex[:12]
    volume = name + "-cache"
    created = False
    with tempfile.TemporaryDirectory(prefix="cch-geoip-docker-") as tmp:
        os.chmod(tmp, 0o755)
        subprocess.run([ROOT / "bin/fixtures", tmp], check=True)
        for path in Path(tmp).glob("*.mmdb"):
            path.chmod(0o644)
        run("docker", "volume", "create", volume)
        try:
            run("docker", "run", "-d", "--name", name, "--read-only",
                "--cap-drop=ALL", "--security-opt=no-new-privileges:true",
                "-p", "127.0.0.1::8080", "-v", f"{tmp}:/usr/share/GeoIP:ro",
                "-v", f"{volume}:/var/lib/cch-geoip", IMAGE)
            created = True
            inspected = json.loads(run("docker", "inspect", name))[0]
            assert inspected["Config"]["User"] == "65532:65532"
            port = inspected["NetworkSettings"]["Ports"]["8080/tcp"][0]["HostPort"]
            deadline = time.monotonic() + 10
            while True:
                try:
                    connection = http.client.HTTPConnection("127.0.0.1", int(port), timeout=2)
                    try:
                        connection.request("GET", "/v1/ip2location/8.8.8.8?lang=zh-CN")
                        response = connection.getresponse()
                        assert response.status == 200
                        data = json.loads(response.read())
                    finally:
                        connection.close()
                    break
                except OSError:
                    if time.monotonic() > deadline:
                        raise RuntimeError(run("docker", "logs", name))
                    time.sleep(.05)
            assert data["location"]["country"]["name"] == "美国"
            assert data["timezone"]["id"] == "America/Los_Angeles"
            assert data["timezone"]["current_time"]
            assert data["connection"]["asn"] == 15169
            assert data["_meta"]["ip2region"]["state"] == "ready"
            assert data["_meta"]["ip2region"]["version"] == "v3.18.0"
            # Replace only the synthetic test MMDB with a CN record. The image's
            # actual bundled xdb must supply the province/city/ISP.
            subprocess.run([ROOT / "bin/fixtures", tmp, "Wrong MMDB City", "CN"], check=True)
            for path in Path(tmp).glob("*.mmdb"):
                path.chmod(0o644)
            run("docker", "kill", "--signal=HUP", name)
            connection = http.client.HTTPConnection("127.0.0.1", int(port), timeout=2)
            try:
                # Wait for SIGHUP to finish loading the newly replaced files.
                deadline = time.monotonic() + 5
                while True:
                    connection.request("GET", "/v1/ip2location/223.104.143.182?lang=zh-CN")
                    response = connection.getresponse()
                    cn = json.loads(response.read())
                    if cn["location"]["city"] == "长春市":
                        break
                    assert time.monotonic() < deadline, "ip2region locality not loaded"
                    time.sleep(.05)
                assert cn["location"]["region"]["name"] == "吉林省"
                assert cn["connection"]["isp"] == "移动"
                assert cn["connection"]["organization"] == "Example ASN"
                assert cn["carrier"]["name"] == "中国移动"
                assert not cn["_meta"]["coordinates_available"]
                assert cn["location"]["coordinates"]["latitude"] == 0
            finally:
                connection.close()
            subprocess.run(["docker", "exec", name, "/cch-geoip", "healthcheck"], check=True)
            subprocess.run(["docker", "run", "--rm", "--read-only", "--cap-drop=ALL",
                            "--security-opt=no-new-privileges:true",
                            "-v", f"{volume}:/var/lib/cch-geoip",
                            "-v", f"{ROOT}/bin/threat.test:/runtime-test:ro",
                            "-e", "CCH_TEST_CACHE_DIR=/var/lib/cch-geoip",
                            "--entrypoint", "/runtime-test", IMAGE,
                            "-test.run=TestRuntimeCachePermissions", "-test.v"], check=True)
            subprocess.run(["docker", "run", "--rm", "--read-only", "--cap-drop=ALL",
                            "--security-opt=no-new-privileges:true",
                            "-v", f"{volume}:/var/lib/cch-geoip",
                            "-v", f"{ROOT}/bin/onlinegeo.test:/runtime-test:ro",
                            "-e", "CCH_TEST_CACHE_DIR=/var/lib/cch-geoip",
                            "--entrypoint", "/runtime-test", IMAGE,
                            "-test.run=TestRuntimeOnlineStatePermissions", "-test.v"], check=True)
            run("docker", "stop", "-t", "10", name)
            assert json.loads(run("docker", "inspect", name))[0]["State"]["ExitCode"] == 0
        finally:
            if created:
                run("docker", "rm", "-f", name)
            run("docker", "volume", "rm", volume)
    print("PASS: scratch/non-root/read-only-rootfs/local MMDB/tzdata/health/cache permissions/system CA/SIGTERM")


if __name__ == "__main__":
    main()
