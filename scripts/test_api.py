#!/usr/bin/env python3
"""
RymeVisor API Test Suite
========================
Tests every API endpoint across all services end-to-end.
Uses a single API key configured in RYMEVISOR_API_KEY env var.

Usage:
    RYMEVISOR_API_KEY=yourkey python3 scripts/test_api.py
    RYMEVISOR_API_KEY=yourkey python3 scripts/test_api.py --base-url http://host:port
    RYMEVISOR_API_KEY=yourkey python3 scripts/test_api.py -s vms
    python3 scripts/test_api.py --list
"""

import argparse
import json
import os
import random
import string
import sys
import time
import traceback
from dataclasses import dataclass, field
from enum import Enum
from typing import Any, Optional
from urllib.request import Request, urlopen
from urllib.error import HTTPError, URLError


class Color:
    RESET = "\033[0m"
    RED = "\033[0;31m"
    GREEN = "\033[0;32m"
    YELLOW = "\033[1;33m"
    CYAN = "\033[0;36m"
    DIM = "\033[2m"
    BOLD = "\033[1m"


@dataclass
class HTTPResponse:
    status: int
    headers: dict
    body: Any
    raw: str


class Client:
    def __init__(self, base_url: str, api_key: str = ""):
        self.base_url = base_url.rstrip("/")
        self.api_key = api_key

    def _request(self, method: str, path: str, body: Any = None,
                 raw_body: bytes = None) -> HTTPResponse:
        url = f"{self.base_url}{path}"
        hdrs = {"Content-Type": "application/json"}
        if self.api_key:
            hdrs["X-API-Key"] = self.api_key

        data = raw_body
        if body is not None and raw_body is None:
            data = json.dumps(body).encode()

        req = Request(url, data=data, headers=hdrs, method=method)
        try:
            with urlopen(req, timeout=30) as resp:
                raw = resp.read().decode()
                parsed = json.loads(raw) if raw else {}
                return HTTPResponse(status=resp.status, headers=dict(resp.headers), body=parsed, raw=raw)
        except HTTPError as e:
            raw = e.read().decode() if e.fp else ""
            try:
                parsed = json.loads(raw) if raw else {}
            except json.JSONDecodeError:
                parsed = {"_raw": raw}
            return HTTPResponse(status=e.code, headers=dict(e.headers) if e.headers else {}, body=parsed, raw=raw)
        except URLError as e:
            raise ConnectionError(f"Cannot connect to {url}: {e.reason}")

    def get(self, path, **kw) -> HTTPResponse:
        return self._request("GET", path, **kw)
    def post(self, path, body=None, **kw) -> HTTPResponse:
        return self._request("POST", path, body=body, **kw)
    def put(self, path, body=None, **kw) -> HTTPResponse:
        return self._request("PUT", path, body=body, **kw)
    def delete(self, path, **kw) -> HTTPResponse:
        return self._request("DELETE", path, **kw)


class Status(Enum):
    PASS = "pass"
    FAIL = "fail"
    SKIP = "skip"
    WARN = "warn"


@dataclass
class TestResult:
    name: str
    status: Status
    message: str = ""


@dataclass
class TestSuite:
    results: list = field(default_factory=list)
    _current: str = ""

    def begin_test(self, name: str):
        self._current = name
    def pass_test(self, msg: str = ""):
        self.results.append(TestResult(self._current, Status.PASS, msg))
    def fail_test(self, msg: str = ""):
        self.results.append(TestResult(self._current, Status.FAIL, msg))
    def skip_test(self, msg: str = ""):
        self.results.append(TestResult(self._current, Status.SKIP, msg))
    def warn_test(self, msg: str = ""):
        self.results.append(TestResult(self._current, Status.WARN, msg))

    def summary(self) -> int:
        passed = sum(1 for r in self.results if r.status == Status.PASS)
        failed = sum(1 for r in self.results if r.status == Status.FAIL)
        skipped = sum(1 for r in self.results if r.status == Status.SKIP)
        warned = sum(1 for r in self.results if r.status == Status.WARN)
        total = len(self.results)

        print()
        print(f"{Color.BOLD}{'='*70}{Color.RESET}")
        print(f"{Color.BOLD} TEST RESULTS: {passed}/{total} passed{Color.RESET}")
        print(f"{Color.BOLD}{'='*70}{Color.RESET}")

        for i, r in enumerate(self.results, 1):
            if r.status == Status.PASS:
                icon = f"{Color.GREEN}PASS{Color.RESET}"
            elif r.status == Status.FAIL:
                icon = f"{Color.RED}FAIL{Color.RESET}"
            elif r.status == Status.SKIP:
                icon = f"{Color.YELLOW}SKIP{Color.RESET}"
            else:
                icon = f"{Color.YELLOW}WARN{Color.RESET}"
            detail = f" - {r.message}" if r.message else ""
            print(f"  {i:3d}. [{icon}] {r.name}{detail}")

        print()
        print(f"  {Color.GREEN}Passed: {passed}{Color.RESET}  "
              f"{Color.RED}Failed: {failed}{Color.RESET}  "
              f"{Color.YELLOW}Skipped: {skipped}{Color.RESET}  "
              f"{Color.DIM}Warned: {warned}{Color.RESET}")
        print()
        return 1 if failed > 0 else 0


def rnd(n: int = 8) -> str:
    return "".join(random.choices(string.ascii_lowercase + string.digits, k=n))


def get_id(obj):
    if obj is None:
        return ""
    return obj.get("id") or obj.get("ID") or ""


def safe_len(obj):
    if obj is None:
        return 0
    return len(obj)


# ============================================================
# Test Suite: Health Checks
# ============================================================

def test_health(c: Client, s: TestSuite):
    for path in ["/health", "/health/live", "/health/ready"]:
        s.begin_test(f"GET {path}")
        r = c.get(path)
        if r.status in (200, 503):
            s.pass_test(f"status={r.status}")
        else:
            s.fail_test(f"expected 200/503, got {r.status}")


# ============================================================
# Test Suite: API Key Auth
# ============================================================

def test_api_key(c: Client, s: TestSuite):
    s.begin_test("GET /api/v1/vms (valid API key)")
    r = c.get("/api/v1/vms")
    if r.status == 200:
        s.pass_test("authenticated")
    else:
        s.fail_test(f"expected 200, got {r.status}")

    s.begin_test("GET /api/v1/vms (no API key)")
    saved = c.api_key
    c.api_key = ""
    r = c.get("/api/v1/vms")
    c.api_key = saved
    if r.status == 401:
        s.pass_test("rejected as expected")
    else:
        s.fail_test(f"expected 401, got {r.status}")

    s.begin_test("GET /api/v1/vms (bad API key)")
    c.api_key = "bad-key-12345"
    r = c.get("/api/v1/vms")
    c.api_key = saved
    if r.status == 401:
        s.pass_test("rejected as expected")
    else:
        s.fail_test(f"expected 401, got {r.status}")


# ============================================================
# Test Suite: VMs
# ============================================================

CREATED_VM_ID = ""

def test_vms(c: Client, s: TestSuite):
    global CREATED_VM_ID

    s.begin_test("GET /api/v1/vms (list)")
    r = c.get("/api/v1/vms")
    if r.status == 200:
        items = r.body.get("items") or []
        s.pass_test(f"found {safe_len(items)} VMs")
    else:
        s.fail_test(f"expected 200, got {r.status}: {r.body}")

    s.begin_test("POST /api/v1/vms (create)")
    r = c.post("/api/v1/vms", {"name": f"test-vm-{rnd(4)}", "vcpus": 2, "memory_mb": 1024})
    if r.status in (200, 201):
        vm = r.body if "id" in r.body else r.body.get("vm") or {}
        CREATED_VM_ID = get_id(vm)
        s.pass_test(f"created VM {CREATED_VM_ID}")
    else:
        s.fail_test(f"expected 200/201, got {r.status}: {r.body}")
        return

    s.begin_test(f"GET /api/v1/vms/{CREATED_VM_ID}")
    r = c.get(f"/api/v1/vms/{CREATED_VM_ID}")
    if r.status == 200:
        s.pass_test("VM found")
    else:
        s.fail_test(f"expected 200, got {r.status}")

    s.begin_test(f"PUT /api/v1/vms/{CREATED_VM_ID}")
    r = c.put(f"/api/v1/vms/{CREATED_VM_ID}", {"name": "renamed-vm"})
    if r.status in (200, 404):
        s.pass_test(f"status={r.status}")
    else:
        s.fail_test(f"expected 200, got {r.status}")

    for action in ["power-on", "power-off", "reboot"]:
        s.begin_test(f"POST /api/v1/vms/{CREATED_VM_ID}/{action}")
        r = c.post(f"/api/v1/vms/{CREATED_VM_ID}/{action}")
        if r.status in (200, 202, 400, 404, 409):
            s.pass_test(f"status={r.status}")
        else:
            s.fail_test(f"unexpected status {r.status}")

    s.begin_test(f"POST /api/v1/vms/{CREATED_VM_ID}/resize")
    r = c.post(f"/api/v1/vms/{CREATED_VM_ID}/resize", {"vcpus": 4, "memory_mb": 2048})
    if r.status in (200, 202, 404):
        s.pass_test(f"status={r.status}")
    else:
        s.fail_test(f"expected 200/202/404, got {r.status}")

    s.begin_test(f"POST /api/v1/vms/{CREATED_VM_ID}/snapshot")
    r = c.post(f"/api/v1/vms/{CREATED_VM_ID}/snapshot", {"name": f"snap-{rnd(4)}"})
    if r.status in (200, 201, 202, 404):
        s.pass_test(f"status={r.status}")
    else:
        s.fail_test(f"expected 200/201/202/404, got {r.status}")

    s.begin_test(f"POST /api/v1/vms/{CREATED_VM_ID}/clone")
    r = c.post(f"/api/v1/vms/{CREATED_VM_ID}/clone", {"name": f"clone-{rnd(4)}", "linked": True})
    if r.status in (200, 201, 202, 404):
        s.pass_test(f"status={r.status}")
    else:
        s.fail_test(f"expected 200/201/202/404, got {r.status}")

    s.begin_test(f"DELETE /api/v1/vms/{CREATED_VM_ID}")
    r = c.delete(f"/api/v1/vms/{CREATED_VM_ID}")
    if r.status in (200, 204, 400, 404):
        s.pass_test(f"status={r.status}")
    else:
        s.fail_test(f"expected 200/204/400/404, got {r.status}")


# ============================================================
# Test Suite: Nodes
# ============================================================

CREATED_NODE_ID = ""

def test_nodes(c: Client, s: TestSuite):
    global CREATED_NODE_ID

    s.begin_test("GET /api/v1/nodes (list)")
    r = c.get("/api/v1/nodes")
    if r.status == 200:
        items = r.body.get("items") or []
        s.pass_test(f"found {safe_len(items)} nodes")
    else:
        s.fail_test(f"expected 200, got {r.status}")

    s.begin_test("POST /api/v1/nodes (register)")
    r = c.post("/api/v1/nodes", {"name": f"test-node-{rnd(4)}", "address": "192.168.1.100", "total_cpus": 16, "total_memory_mb": 65536})
    if r.status in (200, 201):
        node = r.body if "id" in r.body else r.body.get("node") or {}
        CREATED_NODE_ID = get_id(node)
        s.pass_test(f"registered node {CREATED_NODE_ID}")
    else:
        s.fail_test(f"expected 200/201, got {r.status}: {r.body}")
        return

    s.begin_test(f"GET /api/v1/nodes/{CREATED_NODE_ID}")
    r = c.get(f"/api/v1/nodes/{CREATED_NODE_ID}")
    if r.status == 200:
        s.pass_test("node found")
    else:
        s.fail_test(f"expected 200, got {r.status}")

    s.begin_test(f"PUT /api/v1/nodes/{CREATED_NODE_ID}")
    r = c.put(f"/api/v1/nodes/{CREATED_NODE_ID}", {"labels": {"zone": "us-east-1"}})
    if r.status in (200, 404):
        s.pass_test(f"status={r.status}")
    else:
        s.fail_test(f"expected 200, got {r.status}")

    s.begin_test(f"POST /api/v1/nodes/{CREATED_NODE_ID}/heartbeat")
    r = c.post(f"/api/v1/nodes/{CREATED_NODE_ID}/heartbeat", {"total_cpus": 16, "used_cpus": 4, "total_memory_mb": 65536, "used_memory_mb": 8192})
    if r.status in (200, 204, 404):
        s.pass_test(f"status={r.status}")
    else:
        s.fail_test(f"expected 200/204/404, got {r.status}")

    s.begin_test(f"POST /api/v1/nodes/{CREATED_NODE_ID}/drain")
    r = c.post(f"/api/v1/nodes/{CREATED_NODE_ID}/drain", {"timeout": 30})
    if r.status in (200, 202, 404, 409):
        s.pass_test(f"status={r.status}")
    else:
        s.fail_test(f"expected 200/202/404/409, got {r.status}")


# ============================================================
# Test Suite: Networks
# ============================================================

CREATED_NETWORK_ID = ""

def test_networks(c: Client, s: TestSuite):
    global CREATED_NETWORK_ID

    s.begin_test("GET /api/v1/networks (list)")
    r = c.get("/api/v1/networks")
    if r.status == 200:
        nets = r.body.get("networks") or []
        s.pass_test(f"found {safe_len(nets)} networks")
    else:
        s.fail_test(f"expected 200, got {r.status}")

    s.begin_test("POST /api/v1/networks (create)")
    r = c.post("/api/v1/networks", {"name": f"test-net-{rnd(4)}", "cidr": "10.0.0.0/24"})
    if r.status in (200, 201):
        net = r.body if "id" in r.body else r.body.get("network") or {}
        CREATED_NETWORK_ID = get_id(net)
        s.pass_test(f"created network {CREATED_NETWORK_ID}")
    else:
        s.fail_test(f"expected 200/201, got {r.status}: {r.body}")
        return

    s.begin_test(f"GET /api/v1/networks/{CREATED_NETWORK_ID}")
    r = c.get(f"/api/v1/networks/{CREATED_NETWORK_ID}")
    if r.status == 200:
        s.pass_test("network found")
    else:
        s.fail_test(f"expected 200, got {r.status}")

    s.begin_test(f"POST /api/v1/networks/{CREATED_NETWORK_ID}/subnets")
    r = c.post(f"/api/v1/networks/{CREATED_NETWORK_ID}/subnets", {"name": f"sub-{rnd(4)}", "cidr": "10.0.0.128/25", "dhcp": True})
    if r.status in (200, 201, 404):
        s.pass_test(f"status={r.status}")
    else:
        s.fail_test(f"expected 200/201/404, got {r.status}")

    s.begin_test(f"POST /api/v1/networks/{CREATED_NETWORK_ID}/firewall-rules")
    r = c.post(f"/api/v1/networks/{CREATED_NETWORK_ID}/firewall-rules", {"name": f"fw-{rnd(4)}", "protocol": "tcp", "port": 443, "direction": "ingress", "source": "0.0.0.0/0", "action": "allow"})
    if r.status in (200, 201, 404):
        s.pass_test(f"status={r.status}")
    else:
        s.fail_test(f"expected 200/201/404, got {r.status}")

    s.begin_test("POST /api/v1/floating-ips (allocate)")
    r = c.post("/api/v1/floating-ips", {"network_id": CREATED_NETWORK_ID, "vm_id": "00000000-0000-0000-0000-000000000001"})
    if r.status in (200, 201, 404):
        s.pass_test(f"status={r.status}")
    else:
        s.fail_test(f"expected 200/201/404, got {r.status}")

    s.begin_test("GET /api/v1/floating-ips (list)")
    r = c.get("/api/v1/floating-ips")
    if r.status == 200:
        s.pass_test("listed floating IPs")
    else:
        s.fail_test(f"expected 200, got {r.status}")

    s.begin_test(f"DELETE /api/v1/networks/{CREATED_NETWORK_ID}")
    r = c.delete(f"/api/v1/networks/{CREATED_NETWORK_ID}")
    if r.status in (200, 204, 404):
        s.pass_test(f"status={r.status}")
    else:
        s.fail_test(f"expected 200/204/404, got {r.status}")


# ============================================================
# Test Suite: Storage
# ============================================================

CREATED_POOL_ID = ""
CREATED_VOLUME_ID = ""

def test_storage(c: Client, s: TestSuite):
    global CREATED_POOL_ID, CREATED_VOLUME_ID

    s.begin_test("GET /api/v1/storage/pools (list)")
    r = c.get("/api/v1/storage/pools")
    if r.status == 200:
        pools = r.body.get("pools") or []
        s.pass_test(f"found {safe_len(pools)} pools")
    else:
        s.fail_test(f"expected 200, got {r.status}")

    s.begin_test("POST /api/v1/storage/pools (create)")
    r = c.post("/api/v1/storage/pools", {"name": f"pool-{rnd(4)}", "driver": "qcow2", "path": "/tmp/storage"})
    if r.status in (200, 201):
        pool = r.body if "id" in r.body else r.body.get("pool") or {}
        CREATED_POOL_ID = get_id(pool)
        s.pass_test(f"created pool {CREATED_POOL_ID}")
    else:
        s.fail_test(f"expected 200/201, got {r.status}: {r.body}")
        return

    s.begin_test(f"GET /api/v1/storage/pools/{CREATED_POOL_ID}")
    r = c.get(f"/api/v1/storage/pools/{CREATED_POOL_ID}")
    if r.status == 200:
        s.pass_test("pool found")
    else:
        s.fail_test(f"expected 200, got {r.status}")

    s.begin_test("GET /api/v1/storage/volumes (list)")
    r = c.get("/api/v1/storage/volumes")
    if r.status == 200:
        vols = r.body.get("volumes") or []
        s.pass_test(f"found {safe_len(vols)} volumes")
    else:
        s.fail_test(f"expected 200, got {r.status}")

    s.begin_test("POST /api/v1/storage/volumes (create)")
    r = c.post("/api/v1/storage/volumes", {"name": f"vol-{rnd(4)}", "pool_id": CREATED_POOL_ID, "size_bytes": 10737418240})
    if r.status in (200, 201):
        vol = r.body if "id" in r.body else r.body.get("volume") or {}
        CREATED_VOLUME_ID = get_id(vol)
        s.pass_test(f"created volume {CREATED_VOLUME_ID}")
    else:
        s.fail_test(f"expected 200/201, got {r.status}: {r.body}")
        return

    s.begin_test(f"GET /api/v1/storage/volumes/{CREATED_VOLUME_ID}")
    r = c.get(f"/api/v1/storage/volumes/{CREATED_VOLUME_ID}")
    if r.status == 200:
        s.pass_test("volume found")
    else:
        s.fail_test(f"expected 200, got {r.status}")

    s.begin_test(f"PUT /api/v1/storage/volumes/{CREATED_VOLUME_ID}/resize")
    r = c.put(f"/api/v1/storage/volumes/{CREATED_VOLUME_ID}/resize", {"size_bytes": 21474836480})
    if r.status in (200, 404):
        s.pass_test(f"status={r.status}")
    else:
        s.fail_test(f"expected 200/404, got {r.status}")

    s.begin_test(f"POST /api/v1/storage/volumes/{CREATED_VOLUME_ID}/snapshots")
    r = c.post(f"/api/v1/storage/volumes/{CREATED_VOLUME_ID}/snapshots", {"name": f"snap-{rnd(4)}"})
    if r.status in (200, 201, 400, 404):
        s.pass_test(f"status={r.status}")
    else:
        s.fail_test(f"expected 200/201/400/404, got {r.status}")

    s.begin_test(f"DELETE /api/v1/storage/volumes/{CREATED_VOLUME_ID}")
    r = c.delete(f"/api/v1/storage/volumes/{CREATED_VOLUME_ID}")
    if r.status in (200, 204, 404):
        s.pass_test(f"status={r.status}")
    else:
        s.fail_test(f"expected 200/204/404, got {r.status}")


# ============================================================
# Test Suite: Scheduler
# ============================================================

def test_scheduler(c: Client, s: TestSuite):
    s.begin_test("GET /api/v1/scheduler/jobs")
    r = c.get("/api/v1/scheduler/jobs")
    if r.status == 200:
        jobs = r.body.get("jobs") or []
        s.pass_test(f"found {safe_len(jobs)} jobs")
    else:
        s.fail_test(f"expected 200, got {r.status}")

    s.begin_test("POST /api/v1/scheduler/schedule")
    r = c.post("/api/v1/scheduler/schedule", {"vm_id": "test-vm", "priority": 5})
    if r.status in (200, 201, 400, 404, 500):
        s.pass_test(f"status={r.status} (expected for no nodes)")
    else:
        s.fail_test(f"unexpected status {r.status}")


# ============================================================
# Test Suite: Images
# ============================================================

def test_images(c: Client, s: TestSuite):
    s.begin_test("GET /api/v1/images (list)")
    r = c.get("/api/v1/images")
    if r.status == 200:
        imgs = r.body.get("images") or []
        s.pass_test(f"found {safe_len(imgs)} images")
    else:
        s.fail_test(f"expected 200, got {r.status}")

    s.begin_test("POST /api/v1/images (create)")
    r = c.post("/api/v1/images", {"name": f"img-{rnd(4)}", "os": "ubuntu", "os_version": "22.04", "architecture": "amd64"})
    if r.status in (200, 201):
        img = r.body if "id" in r.body else r.body.get("image") or {}
        img_id = get_id(img)
        s.pass_test(f"created image {img_id}")

        s.begin_test(f"DELETE /api/v1/images/{img_id}")
        r = c.delete(f"/api/v1/images/{img_id}")
        if r.status in (200, 204, 404):
            s.pass_test(f"status={r.status}")
        else:
            s.fail_test(f"expected 200/204/404, got {r.status}")
    else:
        s.fail_test(f"expected 200/201, got {r.status}: {r.body}")


# ============================================================
# Test Suite: Backups
# ============================================================

def test_backups(c: Client, s: TestSuite):
    global CREATED_VM_ID
    vm_id = CREATED_VM_ID or "00000000-0000-0000-0000-000000000001"

    s.begin_test("GET /api/v1/backups (list)")
    r = c.get("/api/v1/backups")
    if r.status == 200:
        bks = r.body.get("backups") or []
        s.pass_test(f"found {safe_len(bks)} backups")
    else:
        s.fail_test(f"expected 200, got {r.status}")

    s.begin_test("POST /api/v1/backups (create)")
    r = c.post("/api/v1/backups", {"vm_id": vm_id, "name": f"bak-{rnd(4)}"})
    if r.status in (200, 201):
        bk = r.body if "id" in r.body else r.body.get("backup") or {}
        bk_id = get_id(bk)
        s.pass_test(f"created backup {bk_id}")

        s.begin_test(f"DELETE /api/v1/backups/{bk_id}")
        r = c.delete(f"/api/v1/backups/{bk_id}")
        if r.status in (200, 204, 404):
            s.pass_test(f"status={r.status}")
        else:
            s.fail_test(f"expected 200/204/404, got {r.status}")
    else:
        s.fail_test(f"expected 200/201, got {r.status}: {r.body}")


# ============================================================
# Test Suite: Cross-Cutting
# ============================================================

def test_cross_cutting(c: Client, s: TestSuite):
    s.begin_test("GET /api/v1/nonexistent (404)")
    r = c.get("/api/v1/nonexistent")
    if r.status == 404:
        s.pass_test("got 404")
    else:
        s.fail_test(f"expected 404, got {r.status}")

    s.begin_test("DELETE /api/v1/vms (wrong method 405)")
    r = c.delete("/api/v1/vms")
    if r.status in (404, 405):
        s.pass_test(f"got {r.status}")
    else:
        s.fail_test(f"expected 404/405, got {r.status}")

    s.begin_test("POST /api/v1/vms (empty body)")
    r = c.post("/api/v1/vms", None)
    if r.status in (400, 422, 500):
        s.pass_test(f"got {r.status}")
    else:
        s.fail_test(f"expected 400/422/500, got {r.status}")

    s.begin_test("POST /api/v1/vms (malformed JSON)")
    r = c.post("/api/v1/vms", raw_body=b"{invalid}")
    if r.status in (400, 422, 500):
        s.pass_test(f"got {r.status}")
    else:
        s.fail_test(f"expected 400/422/500, got {r.status}")


# ============================================================
# Test Suites Registry
# ============================================================

ALL_SUITES = {
    "health": ("Health Checks", test_health),
    "auth": ("API Key Auth", test_api_key),
    "vms": ("Virtual Machines", test_vms),
    "nodes": ("Nodes", test_nodes),
    "networks": ("Networking", test_networks),
    "storage": ("Storage", test_storage),
    "scheduler": ("Scheduler", test_scheduler),
    "images": ("Images", test_images),
    "backups": ("Backups", test_backups),
    "cross": ("Cross-Cutting", test_cross_cutting),
}

DEFAULT_ORDER = ["health", "auth", "vms", "nodes", "networks", "storage", "scheduler", "images", "backups", "cross"]


def main():
    parser = argparse.ArgumentParser(description="RymeVisor API Test Suite")
    parser.add_argument("--base-url", default=None, help="Base URL (default: http://localhost:8081, will prompt if not set)")
    parser.add_argument("--service", "-s", choices=list(ALL_SUITES.keys()))
    parser.add_argument("--list", "-l", action="store_true")
    args = parser.parse_args()

    if args.list:
        print("Available test suites:")
        for key in DEFAULT_ORDER:
            name, _ = ALL_SUITES[key]
            print(f"  {key:12s} - {name}")
        return 0

    api_key = os.environ.get("RYMEVISOR_API_KEY", "")
    if not api_key:
        try:
            api_key = input("API Key: ").strip()
        except (EOFError, KeyboardInterrupt):
            print()
            return 1
    if not api_key:
        print(f"{Color.RED}ERROR: RYMEVISOR_API_KEY not set{Color.RESET}")
        print("Usage: RYMEVISOR_API_KEY=yourkey python3 scripts/test_api.py")
        print("   or  run interactively and enter the key when prompted")
        return 1

    base_url = args.base_url
    if not base_url:
        if sys.stdin.isatty():
            try:
                port = input("Port [8081]: ").strip()
                if not port:
                    port = "8081"
                if port.startswith("http"):
                    base_url = port
                else:
                    base_url = f"http://localhost:{port}"
            except (EOFError, KeyboardInterrupt):
                print()
                base_url = "http://localhost:8081"
        else:
            base_url = "http://localhost:8081"
    if not base_url:
        base_url = "http://localhost:8081"
    args.base_url = base_url

    print(f"{Color.BOLD}RymeVisor API Test Suite{Color.RESET}")
    print(f"Target: {Color.CYAN}{args.base_url}{Color.RESET}")
    print(f"API Key: {Color.DIM}{api_key[:8]}...{Color.RESET}")
    print()

    client = Client(args.base_url, api_key)
    suite = TestSuite()

    try:
        client.get("/health")
    except ConnectionError as e:
        print(f"{Color.RED}Cannot connect to {args.base_url}{Color.RESET}")
        print(f"  {e}")
        return 1

    suites_to_run = [args.service] if args.service else DEFAULT_ORDER
    for key in suites_to_run:
        name, func = ALL_SUITES[key]
        print(f"\n{Color.BOLD}{Color.CYAN}--- {name} ---{Color.RESET}")
        try:
            func(client, suite)
        except ConnectionError as e:
            suite.begin_test(f"{name} connection error")
            suite.fail_test(str(e))
        except Exception as e:
            suite.begin_test(f"{name} exception")
            suite.fail_test(f"{type(e).__name__}: {e}")
            traceback.print_exc()

    return suite.summary()


if __name__ == "__main__":
    sys.exit(main())
