#!/usr/bin/env python3
"""Validate the hosted plugin without credentials or a running MCP service."""
import json
from pathlib import Path
import re
from urllib.parse import unquote, urlsplit

ROOT = Path(__file__).resolve().parent.parent
STAGING_URL = "https://platform.stg.reah.com/mcp"
MANIFESTS = (
    "plugin.json", ".claude-plugin/plugin.json",
    ".codex-plugin/plugin.json", ".cursor-plugin/plugin.json",
)
MIRROR_ENTRIES = (".codex-plugin", ".mcp.json", "skills", "README.md", "docs", "examples")


def require(condition, message):
    if not condition:
        raise ValueError(message)


def read_json(root, path):
    return json.loads((root / path).read_text())


def check_links(documents, boundary):
    links = {}
    for document in documents:
        links[document] = set()
        for destination in re.findall(r"\[[^\]]+\]\(([^\s)]+)\)", document.read_text()):
            url = urlsplit(destination)
            if url.scheme or url.netloc or not url.path:
                continue
            target = (document.parent / unquote(url.path)).resolve()
            require(target.is_relative_to(boundary), f"link leaves package: {document}: {destination}")
            require(target.exists(), f"missing reference: {document}: {destination}")
            if target in documents:
                links[document].add(target)
    return links


def validate(root=ROOT):
    root = Path(root).resolve()
    version = read_json(root, "plugin.json")["version"]
    require(re.fullmatch(r"(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)", version), "invalid release version")
    for path in MANIFESTS:
        manifest = read_json(root, path)
        require(manifest["name"] == "reah", f"invalid plugin name: {path}")
        require(manifest["version"] == version, f"plugin version mismatch: {path}")
        require(manifest["repository"] == "https://github.com/ReahPlatform/reah-mcp", f"invalid repository: {path}")

    expected = {"reah": {"type": "http", "url": STAGING_URL}}
    for path in (".mcp.json", "mcp.json", "examples/staging.mcp.json"):
        require(read_json(root, path)["mcpServers"] == expected,
                f"expected credential-free hosted staging connection: {path}")
    require(read_json(root, ".codex-plugin/plugin.json")["mcpServers"] == "./.mcp.json",
            "Codex must load the packaged MCP configuration")
    claude = read_json(root, ".claude-plugin/marketplace.json")["plugins"][0]
    require(claude["name"] == "reah" and claude["source"] == "./", "invalid Claude marketplace source")
    require(claude["version"] == version, "marketplace version mismatch")
    market = read_json(root, ".agents/plugins/marketplace.json")["plugins"][0]
    require(market["name"] == "reah" and market["source"] == {"source": "local", "path": "./plugins/reah"},
            "invalid Codex marketplace source")
    mirror = root / "plugins/reah"
    for path in MIRROR_ENTRIES:
        require((mirror / path).resolve(strict=True) == (root / path).resolve(), f"invalid plugin mirror: {path}")

    skills = {path.parent.resolve() for path in (root / "skills").glob("*/SKILL.md")}
    require({path.name for path in skills} == {"reah-mcp", "reah-browser"}, "missing or unexpected skills")
    for path in (".codex-plugin/plugin.json", ".cursor-plugin/plugin.json"):
        entries = read_json(root, path)["skills"]
        discovered = set()
        for entry in [entries] if isinstance(entries, str) else entries:
            directory = (root / entry).resolve()
            require(directory.is_relative_to(root) and directory.is_dir(), f"invalid skill path: {entry}")
            if (directory / "SKILL.md").is_file():
                discovered.add(directory)
            else:
                discovered.update(p.parent.resolve() for p in directory.glob("*/SKILL.md"))
        require(discovered == skills, f"undiscoverable skills: {path}")

    for directory in skills:
        documents = {path.resolve() for path in directory.rglob("*.md")}
        links = check_links(documents, directory)
        entry = directory / "SKILL.md"
        require(entry.read_text().startswith(f"---\nname: {directory.name}\ndescription:"), f"invalid skill frontmatter: {entry}")
        reachable, pending = set(), [entry]
        while pending:
            document = pending.pop()
            if document not in reachable:
                reachable.add(document)
                pending.extend(links[document] - reachable)
        require(documents <= reachable, f"unreachable skill documents: {documents - reachable}")

    check_links({root / "README.md", *(root / "docs").glob("*.md")}, root)
    tools = re.findall(r"^\| `(reah_[a-z_]+)` \|", (root / "docs/tools.md").read_text(), re.MULTILINE)
    require(len(tools) == len(set(tools)) == 21, "tool reference must contain 21 unique tools")
    require({"reah_catalog", "reah_list_entities"} <= set(tools), "missing discovery tools")
    require(not {"reah_query", "reah_describe_type", "reah_list_statements"} & set(tools), "legacy tool in reference")
    for path in root.rglob("*.json"):
        if ".git" not in path.parts and "dist" not in path.parts:
            json.loads(path.read_text())
    return version


if __name__ == "__main__":
    print(f"Validated Reah {validate()}: hosted staging configurations, manifests, mirrors, skill links, and tool reference.")
