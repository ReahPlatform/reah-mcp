#!/usr/bin/env python3
"""Validate marketplace manifests and links against the actual server catalog."""
import json
from pathlib import Path
import re
from urllib.parse import unquote, urlsplit

root = Path(__file__).resolve().parent.parent


def read_json(path):
    return json.loads((root / path).read_text())


manifests = ["plugin.json", ".claude-plugin/plugin.json", ".codex-plugin/plugin.json", ".cursor-plugin/plugin.json"]
version = read_json("plugin.json")["version"]
assert re.fullmatch(r"(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)", version), "invalid release version"
for path in manifests:
    manifest = read_json(path)
    assert manifest["name"] == "reah", path
    assert manifest["version"] == version, f"plugin version mismatch: {path}"
    assert manifest["repository"] == "https://github.com/ReahPlatform/reah-mcp", path

mcp = read_json(".mcp.json")
assert mcp["mcpServers"] == read_json("mcp.json")["mcpServers"]
assert mcp["mcpServers"]["reah"] == {"command": "reah-mcp"}
claude_marketplace = read_json(".claude-plugin/marketplace.json")["plugins"][0]
assert claude_marketplace["source"] == "./"
assert claude_marketplace["version"] == version, "marketplace version mismatch"
market = read_json(".agents/plugins/marketplace.json")
mirror = root / market["plugins"][0]["source"]["path"]
for path in [".codex-plugin", ".mcp.json", "skills", "README.md"]:
    assert (mirror / path).resolve(strict=True) == (root / path).resolve(), path
packaged_skills = {path.parent.resolve() for path in (root / "skills").glob("*/SKILL.md")}
for path in [".codex-plugin/plugin.json", ".cursor-plugin/plugin.json"]:
    skills = read_json(path)["skills"]
    discovered_skills = set()
    for skill in [skills] if isinstance(skills, str) else skills:
        directory = root / skill
        assert directory.is_dir(), skill
        if (directory / "SKILL.md").is_file():
            discovered_skills.add(directory.resolve())
        else:
            discovered_skills.update(path.parent.resolve() for path in directory.glob("*/SKILL.md"))
    assert discovered_skills == packaged_skills, f"undiscoverable or extra skills: {path}"

for directory in packaged_skills:
    documents = {path.resolve() for path in directory.rglob("*.md")}
    links = {}
    for document in documents:
        links[document] = set()
        for destination in re.findall(r"\[[^\]]+\]\(([^\s)]+)\)", document.read_text()):
            url = urlsplit(destination)
            if url.scheme or url.netloc or not url.path:
                continue
            target = (document.parent / unquote(url.path)).resolve()
            assert target.is_relative_to(directory), f"skill link leaves package: {document}: {destination}"
            assert target.exists(), f"missing skill reference: {document}: {destination}"
            if target in documents:
                links[document].add(target)
    reachable = set()
    pending = [directory / "SKILL.md"]
    while pending:
        document = pending.pop()
        if document not in reachable:
            reachable.add(document)
            pending.extend(links[document] - reachable)
    assert documents <= reachable, f"unreachable skill documents: {documents - reachable}"

source = (root / "internal/server/tools.go").read_text()
tools = set(re.findall(r'\{"(reah_[a-z_]+)",', source))
tools.update(re.findall(r'tool\("(reah_[a-z_]+)"', source))
skill = (root / "skills/reah-mcp/SKILL.md").read_text()
declared = re.search(r"  tools: (.+)", skill).group(1).split(",")
assert set(declared) == tools, f"skill tool drift: {set(declared) ^ tools}"
assert len(tools) == 19, f"unexpected tool count: {len(tools)}"
assert skill.startswith("---\nname: reah-mcp\ndescription:")
assert "TODO" not in skill
assert (root / "docs/upstream.json").is_file()
assert not (root / "internal/schema/source").exists(), "raw upstream mutation schemas must not be packaged"
for path in root.rglob("*.json"):
    if ".git" not in path.parts:
        json.loads(path.read_text())
print(f"Validated 4 plugin manifests, 2 marketplaces, plugin mirror, {len(packaged_skills)} skills and {len(tools)} read-only tools.")
