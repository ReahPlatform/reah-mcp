"""Build standalone clients without Node, npm, private Go modules, or CGO."""
import argparse
import base64
import hashlib
import json
from datetime import datetime, timedelta, timezone
import os
from pathlib import Path
import re
import shutil
import subprocess
import tarfile
import tempfile
import zipfile

from build_enclave import build as build_enclave

root = Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser()
parser.add_argument("--version", required=True)
args = parser.parse_args()
if not re.fullmatch(r"v[0-9]+\.[0-9]+\.[0-9]+(?:-[a-zA-Z0-9.-]+)?", args.version):
    parser.error("use a Go release version such as v0.1.0")
output = root / "dist/go" / args.version
output.mkdir(parents=True, exist_ok=True)
checksums = []
manifest_url = os.environ.get("REAH_MCP_UPDATE_URL", "")
public_key = os.environ.get("REAH_MCP_UPDATE_PUBLIC_KEY", "")
if bool(manifest_url) != bool(public_key):
    parser.error("configure both REAH_MCP_UPDATE_URL and REAH_MCP_UPDATE_PUBLIC_KEY")
if manifest_url:
    if manifest_url != "https://github.com/ReahPlatform/reah-mcp/releases/latest/download/update.json":
        parser.error("the update channel must use this repository's GitHub Releases")
    try:
        key = base64.b64decode(public_key, validate=True)
    except ValueError:
        parser.error("invalid base64 update public key")
    if len(key) != 32:
        parser.error("the Ed25519 update public key must contain 32 bytes")
    if not re.fullmatch(r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)", args.version):
        parser.error("the automatic update channel requires a stable version")
flags = f"-s -w -X main.version={args.version}"
if manifest_url:
    flags += f" -X main.updateManifestURL={manifest_url} -X main.updatePublicKey={public_key}"
published = datetime.now(timezone.utc)
manifest = {
    "schema": 1, "version": args.version,
    "publishedAt": published.isoformat().replace("+00:00", "Z"),
    "expiresAt": (published + timedelta(days=30)).isoformat().replace("+00:00", "Z"),
    "targets": {},
}
build_enclave(root)
for platform in ("darwin", "linux", "windows"):
    for arch in ("amd64", "arm64"):
        name = "reah-mcp" + (".exe" if platform == "windows" else "")
        stem = f"reah-mcp_{args.version}_{platform}_{arch}"
        with tempfile.TemporaryDirectory() as work:
            binary = Path(work) / name
            env = {**os.environ, "GOOS": platform, "GOARCH": arch, "CGO_ENABLED": "0"}
            tags = ["-tags", "secureenclave"] if platform == "darwin" else []
            subprocess.run(["go", "build", *tags, "-trimpath", "-buildvcs=false", "-ldflags",
                            flags, "-o", str(binary),
                            "./cmd/reah-mcp"], cwd=root, env=env, check=True)
            if platform == "windows":
                archive = output / f"{stem}.zip"
                with zipfile.ZipFile(archive, "w", zipfile.ZIP_DEFLATED) as package:
                    package.write(binary, name)
                    package.write(root / "docs/local-client.md", "local-client.md")
            else:
                archive = output / f"{stem}.tar.gz"
                with tarfile.open(archive, "w:gz") as package:
                    package.add(binary, arcname=name)
                    package.add(root / "docs/local-client.md", arcname="local-client.md")
            raw = output / (stem + (".exe" if platform == "windows" else ""))
            shutil.copyfile(binary, raw)
            digest = hashlib.sha256(raw.read_bytes()).hexdigest()
            manifest["targets"][f"{platform}_{arch}"] = {
                "url": f"https://github.com/ReahPlatform/reah-mcp/releases/download/{args.version}/{raw.name}",
                "sha256": digest, "size": raw.stat().st_size,
            }
            checksums.append(f"{digest}  {raw.name}\n")
            checksums.append(f"{hashlib.sha256(archive.read_bytes()).hexdigest()}  {archive.name}\n")
(output / "update-manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
(output / "SHA256SUMS").write_text("".join(checksums))
print(f"Built six standalone clients, update assets, and SHA256SUMS in {output}")
