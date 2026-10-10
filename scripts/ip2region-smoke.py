#!/usr/bin/env python3
"""Extract the image's pinned xdb and execute the official reader integration tests."""
import os
from pathlib import Path
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[1]
IMAGE = os.environ.get("CCH_TEST_IMAGE", "cch-geoip:local")

with tempfile.TemporaryDirectory(prefix="cch-ip2region-test-") as tmp:
    container = subprocess.check_output(["docker", "create", IMAGE], text=True).strip()
    try:
        subprocess.run(["docker", "cp", f"{container}:/usr/share/ip2region/v3.18.0/.", tmp], check=True)
    finally:
        subprocess.run(["docker", "rm", container], check=True, stdout=subprocess.DEVNULL)
    env = {**os.environ, "IP2REGION_TEST_DATA_DIR": tmp}
    subprocess.run(["go", "test", "-race", "-cover", "./internal/ip2region"], cwd=ROOT, env=env, check=True)
print("PASS: bundled xdb IPv4/IPv6/concurrent lookup/corruption fallback")
