"""Embed a universal macOS CryptoKit helper; never compile on the user's first login."""
from pathlib import Path
import platform
import subprocess
import tempfile


def build(root):
    if platform.system() != "Darwin":
        raise RuntimeError("macOS Secure Enclave builds require macOS and Xcode command line tools")
    source = root / "native/secure-enclave/main.swift"
    output = root / "internal/client/enclave-helper.bin"
    with tempfile.TemporaryDirectory(prefix="reah-enclave-build-") as work:
        binaries = []
        for arch in ("arm64", "x86_64"):
            binary = Path(work) / arch
            subprocess.run([
                "/usr/bin/xcrun", "swiftc", "-O", "-whole-module-optimization",
                "-target", f"{arch}-apple-macos12.0", str(source), "-o", str(binary),
            ], check=True)
            binaries.append(str(binary))
        subprocess.run(["/usr/bin/lipo", "-create", *binaries, "-output", str(output)], check=True)
        # A stable ad-hoc code signature supports local executable integrity. No
        # Apple distribution identity or private entitlement is required here.
        subprocess.run(["/usr/bin/codesign", "--force", "--sign", "-", str(output)], check=True)


if __name__ == "__main__":
    build(Path(__file__).resolve().parents[1])
