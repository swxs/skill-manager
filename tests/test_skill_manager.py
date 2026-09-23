"""skill-manager 行为测试。全部指向临时目录，不碰真实 ~/.agents。"""

from __future__ import annotations

import contextlib
import io
import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "scripts"))

import skill_manager


class SkillManagerTests(unittest.TestCase):
    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.root = Path(self._tmp.name)
        self.home = self.root / "home"
        self.home.mkdir()
        self.library = self.root / "library"
        self._old_home = os.environ.get("SKILL_MANAGER_HOME")
        os.environ["SKILL_MANAGER_HOME"] = str(self.home)

    def tearDown(self) -> None:
        if self._old_home is None:
            os.environ.pop("SKILL_MANAGER_HOME", None)
        else:
            os.environ["SKILL_MANAGER_HOME"] = self._old_home
        self._tmp.cleanup()

    def write_skill(self, directory: Path) -> None:
        directory.mkdir(parents=True, exist_ok=True)
        (directory / "SKILL.md").write_text(
            f"---\nname: {directory.name}\n---\n",
            encoding="utf-8",
        )

    def run_cli(self, *args: str) -> tuple[int, str]:
        buf = io.StringIO()
        with contextlib.redirect_stdout(buf):
            code = skill_manager.main(["--library", str(self.library), *args])
        return code, buf.getvalue()

    def test_parse_jsonc_keeps_comment_markers_inside_strings(self) -> None:
        text = """
        // 行注释
        [
          "keep // inside",
          "keep, comma",
          /* 块
             注释 */
          "tail",
        ]
        """
        self.assertEqual(
            skill_manager.parse_jsonc(text),
            ["keep // inside", "keep, comma", "tail"],
        )

    def test_strip_trailing_commas_keeps_commas_inside_strings(self) -> None:
        self.assertEqual(
            skill_manager.strip_trailing_commas('["a,b", {"k": "x,",},]\n'),
            '["a,b", {"k": "x,"}]\n',
        )

    def test_classify_source_single_pack_and_mixed(self) -> None:
        single = self.root / "single"
        self.write_skill(single)
        pack = self.root / "pack"
        self.write_skill(pack / "alpha")
        self.write_skill(pack / "beta")
        mixed = self.root / "mixed"
        self.write_skill(mixed)
        self.write_skill(mixed / "child")
        self.assertEqual(skill_manager.classify_source(single), "single")
        self.assertEqual(skill_manager.classify_source(pack), "pack")
        self.assertEqual(skill_manager.classify_source(mixed), "mixed")

    def test_classify_source_rejects_missing_skill_and_skips_hidden(self) -> None:
        empty = self.root / "empty"
        empty.mkdir()
        hidden = self.root / "hidden"
        self.write_skill(hidden / ".secret")
        visible = self.root / "visible"
        self.write_skill(visible / "shown")
        self.write_skill(visible / ".secret")
        with self.assertRaises(ValueError) as empty_err:
            skill_manager.classify_source(empty)
        with self.assertRaises(ValueError) as hidden_err:
            skill_manager.classify_source(hidden)
        self.assertIn("没有 SKILL.md", str(empty_err.exception))
        self.assertIn("没有 SKILL.md", str(hidden_err.exception))
        self.assertEqual(skill_manager.classify_source(visible), "pack")
        self.assertEqual(list(skill_manager.direct_members(visible)), ["shown"])

    def test_direct_members_and_inspect_pack_sort_by_casefold(self) -> None:
        pack = self.root / "sorted"
        for name in ("b", "A", "c"):
            self.write_skill(pack / name)
        self.assertEqual(list(skill_manager.direct_members(pack)), ["A", "b", "c"])
        info = skill_manager.inspect_pack(pack)
        self.assertEqual(info["skills"], ["A", "b", "c"])

    def test_resolve_selection_reports_missing_pack_and_skill(self) -> None:
        self.write_skill(self.library / "demo" / "one")
        missing_pack, pack_problems = skill_manager.resolve_selection(
            self.library, ["nope"]
        )
        missing_skill, skill_problems = skill_manager.resolve_selection(
            self.library, ["demo:missing"]
        )
        self.assertEqual(missing_pack, {})
        self.assertEqual(pack_problems, ["! 库中没有包 nope"])
        self.assertEqual(missing_skill, {})
        self.assertEqual(skill_problems, ["! 包 demo 里没有技能 missing"])

    def test_resolve_selection_rejects_splitting_a_mixed_pack(self) -> None:
        mixed = self.library / "mix"
        self.write_skill(mixed)
        self.write_skill(mixed / "child")
        desired, problems = skill_manager.resolve_selection(self.library, ["mix:child"])
        self.assertEqual(desired, {})
        self.assertEqual(problems, ["! 混合包不能单拆 mix:child"])

    def test_resolve_selection_skips_same_name_with_different_target(self) -> None:
        self.write_skill(self.library / "pack-a" / "shared")
        self.write_skill(self.library / "pack-b" / "shared")
        desired, problems = skill_manager.resolve_selection(
            self.library, ["pack-a", "pack-b"]
        )
        self.assertEqual(list(desired), ["shared"])
        self.assertEqual(
            problems,
            ["! 同名跳过 pack-b:shared（已占用 shared）"],
        )

    def test_resolve_selection_expands_a_pure_pack(self) -> None:
        self.write_skill(self.library / "demo" / "one")
        self.write_skill(self.library / "demo" / "two")
        desired, problems = skill_manager.resolve_selection(self.library, ["demo"])
        self.assertEqual(problems, [])
        self.assertEqual(list(desired), ["one", "two"])

    def test_global_config_path_follows_skill_manager_home(self) -> None:
        self.assertEqual(skill_manager.global_config_path(), self.home / "skills.json")

    def test_install_add_link_status_remove_and_lf_output(self) -> None:
        source = self.root / "sources" / "demo"
        self.write_skill(source / "one")
        self.write_skill(source / "two")
        for relative in (
            ".git/config",
            "__pycache__/x.pyc",
            ".venv/pyvenv.cfg",
            "node_modules/pkg/index.js",
        ):
            path = source / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text("noise", encoding="utf-8")
        (source / ".DS_Store").write_text("noise", encoding="utf-8")
        (source / ".skill-lock.json").write_text("{}\n", encoding="utf-8")

        code, _output = self.run_cli("install", str(source))
        self.assertEqual(code, 0)
        installed = self.library / "demo"
        self.assertTrue((installed / "one" / "SKILL.md").is_file())
        self.assertTrue((installed / "two" / "SKILL.md").is_file())
        for name in (".git", "__pycache__", ".venv", "node_modules", ".DS_Store", ".skill-lock.json"):
            self.assertFalse((installed / name).exists(), name)
        lock_bytes = (self.library / ".skill-lock.json").read_bytes()
        self.assertNotIn(b"\r", lock_bytes)
        self.assertTrue(lock_bytes.endswith(b"\n"))

        repo = self.root / "repo"
        repo.mkdir()
        subprocess.run(["git", "init", "-q"], cwd=repo, check=True, capture_output=True)
        code, _output = self.run_cli("add", "--root", str(repo), "demo")
        self.assertEqual(code, 0)
        selection = repo / ".agents" / "skills.json"
        selection_bytes = selection.read_bytes()
        self.assertNotIn(b"\r", selection_bytes)
        self.assertEqual(skill_manager.parse_jsonc(selection.read_text(encoding="utf-8")), ["demo"])

        code, _output = self.run_cli("link", "--root", str(repo))
        self.assertEqual(code, 0)
        self.assertIsNotNone(skill_manager.link_target(repo / ".agents" / "skills" / "one"))
        self.assertIsNotNone(skill_manager.link_target(repo / ".agents" / "skills" / "two"))
        exclude = repo / ".git" / "info" / "exclude"
        exclude_bytes = exclude.read_bytes()
        self.assertNotIn(b"\r", exclude_bytes)
        self.assertIn(b".agents/skills/one", exclude_bytes)
        self.assertIn(b".agents/skills/two", exclude_bytes)

        code, output = self.run_cli("status", "--root", str(repo))
        self.assertEqual(code, 0)
        self.assertIn("已与配置一致", output)

        code, _output = self.run_cli("remove", "demo")
        self.assertEqual(code, 0)
        self.assertFalse(installed.exists())
        lock = json.loads((self.library / ".skill-lock.json").read_text(encoding="utf-8"))
        self.assertEqual(lock["packs"], {})

    def test_add_global_writes_skills_json_under_home(self) -> None:
        source = self.root / "sources" / "demo"
        self.write_skill(source / "one")
        self.assertEqual(self.run_cli("install", str(source))[0], 0)
        code, _output = self.run_cli("add", "--global", "demo")
        self.assertEqual(code, 0)
        path = self.home / "skills.json"
        self.assertTrue(path.is_file())
        self.assertFalse((self.home / "skills" / "skill-manager" / "skills.json").exists())
        self.assertNotIn(b"\r", path.read_bytes())
        self.assertEqual(skill_manager.parse_jsonc(path.read_text(encoding="utf-8")), ["demo"])


if __name__ == "__main__":
    unittest.main()
