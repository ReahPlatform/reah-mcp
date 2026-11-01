"""Build standalone clients without Node, npm, private Go modules, or CGO."""
import argparse
import hashlib
import os
from pathlib import Path
import re
import subprocess
import tarfile
import tempfile
import zipfile

root = Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser()
parser.add_argument("--version", required=True)
args = parser.parse_args()
if not re.fullmatch(r"v[0-9]+\.[0-9]+\.[0-9]+(?:-[a-zA-Z0-9.-]+)?", args.version):
    parser.error("use a Go release version such as v0.1.0")
output = root / "dist/go" / args.version
output.mkdir(parents=True, exist_ok=True)
checksums = []
for platform in ("darwin", "linux", "windows"):
    for arch in ("amd64", "arm64"):
        name = "reah-mcp" + (".exe" if platform == "windows" else "")
        stem = f"reah-mcp_{args.version}_{platform}_{arch}"
        with tempfile.TemporaryDirectory() as work:
            binary = Path(work) / name
            env = {**os.environ, "GOOS": platform, "GOARCH": arch, "CGO_ENABLED": "0"}
            subprocess.run(["go", "build", "-trimpath", "-buildvcs=false", "-ldflags",
                            f"-s -w -X main.version={args.version}", "-o", str(binary),
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
            checksums.append(f"{hashlib.sha256(archive.read_bytes()).hexdigest()}  {archive.name}\n")
(output / "SHA256SUMS").write_text("".join(checksums))
print(f"Built six standalone clients and SHA256SUMS in {output}")
