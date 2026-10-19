#!/usr/bin/env python3
"""Validate marketplace manifests and links against the actual server catalog."""
import json
from pathlib import Path
import re

root = Path(__file__).resolve().parent.parent


def read_json(path):
    return json.loads((root / path).read_text())


manifests = ["plugin.json", ".claude-plugin/plugin.json", ".codex-plugin/plugin.json", ".cursor-plugin/plugin.json"]
for path in manifests:
    manifest = read_json(path)
    assert manifest["name"] == "reah", path
    assert manifest["version"] == "0.1.0", path
    assert manifest["repository"] == "https://github.com/ReahPlatform/reah-mcp", path

mcp = read_json(".mcp.json")
assert mcp["mcpServers"] == read_json("mcp.json")["mcpServers"]
assert mcp["mcpServers"]["reah"] == {"command": "reah-mcp"}
assert read_json(".claude-plugin/marketplace.json")["plugins"][0]["source"] == "./"
market = read_json(".agents/plugins/marketplace.json")
mirror = root / market["plugins"][0]["source"]["path"]
for path in [".codex-plugin", ".mcp.json", "skills", "README.md"]:
    assert (mirror / path).resolve(strict=True) == (root / path).resolve(), path
for path in [".codex-plugin/plugin.json", ".cursor-plugin/plugin.json"]:
    skills = read_json(path)["skills"]
    for skill in [skills] if isinstance(skills, str) else skills:
        assert (root / skill).is_dir(), skill

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
print(f"Validated 4 plugin manifests, 2 marketplaces, plugin mirror and {len(tools)} skill tools.")
