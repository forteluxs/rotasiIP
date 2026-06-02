#!/usr/bin/env python3
"""
Filter proxy lists to EU/Americas IPs only, then fetch additional EU/US proxies.
"""

import json
import re
import sys
import time
import urllib.request
import urllib.error
from pathlib import Path

BASE_DIR = Path(__file__).parent.parent

EU_CC = {
    "AL","AD","AT","BY","BE","BA","BG","HR","CY","CZ","DK","EE",
    "FI","FR","DE","GR","HU","IS","IE","IT","LV","LI","LT","LU",
    "MK","MT","MD","MC","ME","NL","NO","PL","PT","RO","RS","SK",
    "SI","ES","SE","CH","UA","GB","RU",
}
AMERICAS_CC = {
    "US","CA","MX","BR","AR","CL","CO","PE","VE","UY","EC","PY",
    "BO","CR","CU","DO","HN","GT","PA","SV","NI","JM","TT","HT",
    "BB","GY","SR","BZ",
}
ALLOWED_CC = EU_CC | AMERICAS_CC

IP_RE = re.compile(r"(\d{1,3}(?:\.\d{1,3}){3})")


def extract_ip(line: str) -> str | None:
    m = IP_RE.search(line.strip())
    return m.group(1) if m else None


def extract_proxy(line: str) -> str | None:
    line = line.strip()
    if not line or line.startswith("#"):
        return None
    if re.match(r"(https?|socks[45a]*)://", line):
        return line
    if re.match(r"\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}:\d+", line):
        return line
    return None


def fetch_url(url: str, data: bytes | None = None, headers: dict | None = None, timeout: int = 20) -> bytes | None:
    req = urllib.request.Request(url, data=data, headers=headers or {"User-Agent": "mubeng/1.0"})
    if data:
        req.add_header("Content-Type", "application/json")
    for attempt in range(5):
        try:
            with urllib.request.urlopen(req, timeout=timeout) as resp:
                return resp.read()
        except urllib.error.HTTPError as e:
            if e.code == 429:
                wait = 2 ** attempt * 3
                print(f"  [rate-limit] 429 on {url[:60]} — waiting {wait}s", file=sys.stderr)
                time.sleep(wait)
            elif e.code == 404:
                return None
            else:
                print(f"  [warn] HTTP {e.code} on {url[:60]}", file=sys.stderr)
                return None
        except Exception as e:
            print(f"  [warn] {e} on {url[:60]}", file=sys.stderr)
            if attempt < 4:
                time.sleep(2)
    return None


def geolocate_batch(ips: list[str]) -> dict[str, str]:
    payload = json.dumps([{"query": ip, "fields": "query,countryCode"} for ip in ips]).encode()
    data = fetch_url("http://ip-api.com/batch?fields=query,countryCode", data=payload)
    if not data:
        return {}
    try:
        items = json.loads(data)
        return {item["query"]: item.get("countryCode", "") for item in items}
    except Exception:
        return {}


def geolocate_all(ips: list[str]) -> dict[str, str]:
    result: dict[str, str] = {}
    batches = [ips[i:i+100] for i in range(0, len(ips), 100)]
    print(f"  Geolocating {len(ips)} unique IPs in {len(batches)} batches...")
    for idx, batch in enumerate(batches, 1):
        cc_map = geolocate_batch(batch)
        result.update(cc_map)
        if idx % 5 == 0 or idx == len(batches):
            resolved = len([v for v in result.values() if v])
            print(f"  [{idx}/{len(batches)}] {resolved} IPs resolved")
        time.sleep(1.5)   # ip-api free: 45 req/min → ~1.33s min gap
    return result


def filter_proxies(lines: list[str], ip_cc: dict[str, str]) -> list[str]:
    kept = []
    for line in lines:
        ip = extract_ip(line)
        if ip and ip_cc.get(ip, "") in ALLOWED_CC:
            kept.append(line.rstrip("\n"))
    return kept


def fetch_proxyscrape(countries: list[str], protocol: str) -> list[str]:
    """Fetch proxies from proxyscrape API filtered by country and protocol."""
    cc_str = ",".join(c.lower() for c in countries)
    url = (
        f"https://api.proxyscrape.com/v4/free-proxy-list/get"
        f"?request=display_proxies&country={cc_str}&protocol={protocol}"
        f"&format=text&timeout=10000"
    )
    data = fetch_url(url, timeout=30)
    if not data:
        return []
    proxies = []
    for line in data.decode("utf-8", errors="ignore").splitlines():
        line = line.strip()
        if not line:
            continue
        # proxyscrape returns plain IP:PORT, prefix with scheme
        if re.match(r"\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}:\d+", line):
            line = f"{protocol}://{line}"
        if extract_proxy(line):
            proxies.append(line)
    return proxies


def dedup(lines: list[str]) -> list[str]:
    seen: set[str] = set()
    out: list[str] = []
    for line in lines:
        line = line.strip()
        if not line:
            continue
        key = re.sub(r"^(https?|socks[45a]*)://", "", line).strip("/")
        if key not in seen:
            seen.add(key)
            out.append(line)
    return out


def main():
    raw_dir = BASE_DIR / "raw_lists"

    # ── 1. Read all existing proxy files ──────────────────────────────────────
    print("\n[1] Reading all proxy files...")
    file_lines: dict[str, list[str]] = {}
    all_proxy_lines: list[str] = []

    for path in sorted(raw_dir.glob("*.txt")):
        lines = path.read_text(errors="ignore").splitlines()
        file_lines[str(path)] = lines
        all_proxy_lines.extend(l for l in lines if extract_proxy(l))

    for fname in ["all_proxies.txt", "proxies.txt", "proxies_http.txt"]:
        path = BASE_DIR / fname
        if path.exists():
            lines = path.read_text(errors="ignore").splitlines()
            file_lines[str(path)] = lines
            all_proxy_lines.extend(l for l in lines if extract_proxy(l))

    unique_ips = list({ip for l in all_proxy_lines if (ip := extract_ip(l))})
    print(f"  {len(all_proxy_lines)} proxy entries, {len(unique_ips)} unique IPs")

    # ── 2. Geolocate ──────────────────────────────────────────────────────────
    print("\n[2] Geolocating IPs via ip-api.com...")
    ip_cc = geolocate_all(unique_ips)
    eu_n = sum(1 for cc in ip_cc.values() if cc in EU_CC)
    am_n = sum(1 for cc in ip_cc.values() if cc in AMERICAS_CC)
    print(f"  EU: {eu_n}  Americas: {am_n}  (from {len([v for v in ip_cc.values() if v])} resolved)")

    # ── 3. Filter existing raw files ──────────────────────────────────────────
    print("\n[3] Filtering raw lists to EU/Americas only...")
    for path_str, lines in file_lines.items():
        path = Path(path_str)
        if path.parent != raw_dir:
            continue
        filtered = filter_proxies(lines, ip_cc)
        original = len([l for l in lines if extract_proxy(l)])
        path.write_text("\n".join(filtered) + ("\n" if filtered else ""))
        print(f"  {path.name}: {original} → {len(filtered)}")

    # ── 4. Fetch additional EU/Americas proxies from proxyscrape ──────────────
    EU_COUNTRIES = [
        "DE","FR","GB","NL","BE","CH","AT","ES","IT","PT","IE","LU",
        "SE","NO","DK","FI","EE","LV","LT","IS",
        "PL","CZ","SK","HU","RO","BG","HR","RS","SI","UA","GR","RU",
    ]
    AM_COUNTRIES = ["US","CA","BR","MX","AR","CO","CL"]

    ALL_COUNTRIES = EU_COUNTRIES + AM_COUNTRIES

    print(f"\n[4] Fetching extra EU/Americas proxies from proxyscrape...")

    print("  Fetching EU HTTP proxies...", end=" ", flush=True)
    eu_http = fetch_proxyscrape(EU_COUNTRIES, "http")
    print(f"{len(eu_http)} entries")

    time.sleep(2)
    print("  Fetching Americas HTTP proxies...", end=" ", flush=True)
    am_http = fetch_proxyscrape(AM_COUNTRIES, "http")
    print(f"{len(am_http)} entries")

    time.sleep(2)
    print("  Fetching EU SOCKS5 proxies...", end=" ", flush=True)
    eu_socks5 = fetch_proxyscrape(EU_COUNTRIES, "socks5")
    print(f"{len(eu_socks5)} entries")

    time.sleep(2)
    print("  Fetching Americas SOCKS5 proxies...", end=" ", flush=True)
    am_socks5 = fetch_proxyscrape(AM_COUNTRIES, "socks5")
    print(f"{len(am_socks5)} entries")

    time.sleep(2)
    print("  Fetching EU SOCKS4 proxies...", end=" ", flush=True)
    eu_socks4 = fetch_proxyscrape(EU_COUNTRIES, "socks4")
    print(f"{len(eu_socks4)} entries")

    time.sleep(2)
    print("  Fetching Americas SOCKS4 proxies...", end=" ", flush=True)
    am_socks4 = fetch_proxyscrape(AM_COUNTRIES, "socks4")
    print(f"{len(am_socks4)} entries")

    extra_http = eu_http + am_http
    extra_socks5 = eu_socks5 + am_socks5
    extra_socks4 = eu_socks4 + am_socks4
    print(f"  Total extra: {len(extra_http)} http, {len(extra_socks5)} socks5, {len(extra_socks4)} socks4")

    # ── 5. Rebuild raw_lists files ─────────────────────────────────────────────
    print("\n[5] Merging and writing raw list files...")

    def read_file(p: Path) -> list[str]:
        return p.read_text(errors="ignore").splitlines() if p.exists() else []

    # mmpx_http: all http
    mmpx_http_path = raw_dir / "mmpx_http.txt"
    mmpx_http = dedup(read_file(mmpx_http_path) + extra_http)
    mmpx_http_path.write_text("\n".join(mmpx_http) + "\n")
    print(f"  mmpx_http.txt: {len(mmpx_http)}")

    # mmpx_socks5: socks5 + socks4
    mmpx_s5_path = raw_dir / "mmpx_socks5.txt"
    mmpx_s5 = dedup(read_file(mmpx_s5_path) + extra_socks5)
    mmpx_s5_path.write_text("\n".join(mmpx_s5) + "\n")
    print(f"  mmpx_socks5.txt: {len(mmpx_s5)}")

    # speedx_http: http
    sx_http_path = raw_dir / "speedx_http.txt"
    sx_http = dedup(read_file(sx_http_path) + extra_http)
    sx_http_path.write_text("\n".join(sx_http) + "\n")
    print(f"  speedx_http.txt: {len(sx_http)}")

    # speedx_socks5: socks5
    sx_s5_path = raw_dir / "speedx_socks5.txt"
    sx_s5 = dedup(read_file(sx_s5_path) + extra_socks5 + extra_socks4)
    sx_s5_path.write_text("\n".join(sx_s5) + "\n")
    print(f"  speedx_socks5.txt: {len(sx_s5)}")

    # ── 6. Rebuild top-level proxy files ─────────────────────────────────────
    print("\n[6] Rebuilding all_proxies.txt, proxies.txt, proxies_http.txt...")

    all_combined: list[str] = []
    for path in sorted(raw_dir.glob("*.txt")):
        for line in path.read_text(errors="ignore").splitlines():
            p = extract_proxy(line.strip())
            if p:
                if not re.match(r"(https?|socks[45a]*)://", p):
                    p = "http://" + p
                all_combined.append(p)

    all_combined = dedup(all_combined)
    (BASE_DIR / "all_proxies.txt").write_text("\n".join(all_combined) + "\n")
    print(f"  all_proxies.txt: {len(all_combined)}")

    http_only = [p for p in all_combined if p.startswith("http://")]
    http_bare = dedup([re.sub(r"^http://", "", p) for p in http_only])
    (BASE_DIR / "proxies.txt").write_text("\n".join(http_bare) + "\n")
    (BASE_DIR / "proxies_http.txt").write_text("\n".join(f"http://{b}" for b in http_bare) + "\n")
    print(f"  proxies.txt / proxies_http.txt: {len(http_bare)} HTTP entries")

    # ── 7. Summary ────────────────────────────────────────────────────────────
    print("\n[Done] EU/Americas proxy audit complete.\n")

    # Country breakdown from geolocated existing IPs
    cc_count: dict[str, int] = {}
    for cc in ip_cc.values():
        if cc in ALLOWED_CC:
            cc_count[cc] = cc_count.get(cc, 0) + 1
    top = sorted(cc_count.items(), key=lambda x: -x[1])[:20]
    print("Top countries in original dataset (EU/Americas only):")
    for cc, n in top:
        tag = "(EU)" if cc in EU_CC else "(AM)"
        print(f"  {cc} {tag}: {n}")


if __name__ == "__main__":
    main()
