#!/usr/bin/env python3
"""Build a credential-free plugin archive with portable, resolved mirror files."""
from pathlib import Path
from zipfile import ZIP_DEFLATED, ZipFile

from validate import ROOT, require, validate

ENTRIES = (
    ".agents", ".claude-plugin", ".codex-plugin", ".cursor-plugin",
    ".mcp.json", "mcp.json", "plugin.json", "README.md",
    "docs", "examples", "skills", "plugins",
)


def package_files(root, path, ancestors=()):
    resolved = path.resolve(strict=True)
    require(resolved.is_relative_to(root), f"archive link leaves repository: {path}")
    require(resolved not in ancestors, f"cyclic archive link: {path}")
    if path.is_dir():
        for child in sorted(path.iterdir()):
            yield from package_files(root, child, (*ancestors, resolved))
    else:
        yield path


def build(root=ROOT):
    root = Path(root).resolve()
    files = [path for entry in ENTRIES for path in package_files(root, root / entry)]
    version = validate(root)
    output = root / "dist" / f"reah-{version}.zip"
    output.parent.mkdir(exist_ok=True)
    with ZipFile(output, "w", compression=ZIP_DEFLATED) as archive:
        for path in files:
            archive.writestr(path.relative_to(root).as_posix(), path.read_bytes())
    return output


if __name__ == "__main__":
    print(f"Built {build()}")
