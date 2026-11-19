"""Regression tests for installation configuration and distributable content."""
import json
from pathlib import Path
import shutil
import tempfile
import unittest
from zipfile import ZipFile

from package import ENTRIES, build
from validate import ROOT, PRODUCTION_URL, validate


class PluginTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        for entry in ENTRIES:
            source, target = ROOT / entry, self.root / entry
            if source.is_dir():
                shutil.copytree(source, target, symlinks=True)
            else:
                shutil.copy2(source, target)

    def rewrite_json(self, path, change):
        target = self.root / path
        value = json.loads(target.read_text())
        change(value)
        target.write_text(json.dumps(value))

    def test_accepts_hosted_plugin(self):
        self.assertEqual(validate(self.root), "0.3.2")

    def test_rejects_credential_and_local_server_configuration(self):
        variants = [
            {"command": "reah-mcp"},
            {"type": "http", "url": PRODUCTION_URL, "headers": {"Authorization": "Bearer test"}},
            {"type": "http", "url": PRODUCTION_URL, "env": {"REAH_ENTITY_ID": "fixed"}},
            {"type": "http", "url": "https://platform.stg.reah.com/mcp"},
        ]
        for config in variants:
            with self.subTest(config=config):
                self.rewrite_json(".mcp.json", lambda value: value["mcpServers"].update(reah=config))
                with self.assertRaisesRegex(ValueError, "credential-free hosted production"):
                    validate(self.root)

    def test_rejects_stale_marketplace_version(self):
        self.rewrite_json(".claude-plugin/marketplace.json", lambda value: value["plugins"][0].update(version="0.2.0"))
        with self.assertRaisesRegex(ValueError, "marketplace version mismatch"):
            validate(self.root)

    def test_rejects_staging_in_any_packaged_connection(self):
        for path in [".mcp.json", "mcp.json", "examples/production.mcp.json"]:
            with self.subTest(path=path):
                original = (self.root / path).read_text()
                self.rewrite_json(path, lambda value: value["mcpServers"]["reah"].update(
                    url="https://platform.stg.reah.com/mcp"))
                with self.assertRaisesRegex(ValueError, "credential-free hosted production"):
                    validate(self.root)
                (self.root / path).write_text(original)

    def test_rejects_missing_skill_reference(self):
        path = self.root / "skills/reah-mcp/SKILL.md"
        path.write_text(path.read_text() + "\n[Missing](references/missing.md)\n")
        with self.assertRaisesRegex(ValueError, "missing reference"):
            validate(self.root)

    def test_rejects_unreachable_skill_reference(self):
        (self.root / "skills/reah-mcp/unlinked.md").write_text("Unlinked guidance")
        with self.assertRaisesRegex(ValueError, "unreachable skill documents"):
            validate(self.root)

    def test_rejects_skill_link_outside_its_package(self):
        path = self.root / "skills/reah-mcp/SKILL.md"
        path.write_text(path.read_text() + "\n[Outside](../../README.md)\n")
        with self.assertRaisesRegex(ValueError, "link leaves package"):
            validate(self.root)

    def test_rejects_missing_cursor_skill_discovery(self):
        self.rewrite_json(".cursor-plugin/plugin.json", lambda value: value.update(skills=[]))
        with self.assertRaisesRegex(ValueError, "undiscoverable skills"):
            validate(self.root)

    def test_rejects_broken_mirror(self):
        path = self.root / "plugins/reah/.mcp.json"
        path.unlink()
        path.symlink_to("../../plugin.json")
        with self.assertRaisesRegex(ValueError, "invalid plugin mirror"):
            validate(self.root)

    def test_rejects_legacy_tool_in_catalog(self):
        path = self.root / "docs/tools.md"
        path.write_text(path.read_text().replace("`reah_get_invoice`", "`reah_query`"))
        with self.assertRaisesRegex(ValueError, "legacy tool in reference"):
            validate(self.root)

    def test_archive_resolves_mirror_and_excludes_local_files(self):
        (self.root / ".env").write_text("TEST_SECRET=not-for-distribution")
        (self.root / "bin").mkdir()
        (self.root / "bin/reah-mcp").write_text("obsolete local binary")
        archive_path = build(self.root)
        with ZipFile(archive_path) as archive:
            names = archive.namelist()
            self.assertNotIn(".env", names)
            self.assertFalse(any(name.startswith("bin/") for name in names))
            self.assertFalse(any("reah-browser" in name for name in names))
            for path in [".mcp.json", "README.md", "docs/tools.md", "examples/production.mcp.json",
                         ".codex-plugin/plugin.json", "skills/reah-mcp/SKILL.md"]:
                self.assertEqual(archive.read(path), archive.read(f"plugins/reah/{path}"))
            extracted = self.root / "extracted"
            archive.extractall(extracted)
            config = json.loads((extracted / "plugins/reah/.mcp.json").read_text())
            self.assertEqual(config["mcpServers"]["reah"]["url"], PRODUCTION_URL)
            self.assertTrue((extracted / "plugins/reah/docs/data-contract.md").is_file())

    def test_archive_rejects_external_symlink(self):
        (self.root / "docs/external.md").symlink_to(ROOT / "README.md")
        with self.assertRaisesRegex(ValueError, "archive link leaves repository"):
            build(self.root)

    def test_archive_rejects_cyclic_symlink(self):
        (self.root / "examples/cycle").symlink_to(self.root / "examples", target_is_directory=True)
        with self.assertRaisesRegex(ValueError, "cyclic archive link"):
            build(self.root)


if __name__ == "__main__":
    unittest.main()
