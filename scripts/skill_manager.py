"""Manage the local skill library and per-repo skill links.

Disk under ~/.agents/skill-library is the source of truth. .skill-lock.json
is an index rewritten from that tree. Repo .agents/skills.json selects what
gets linked into .agents/skills/.
"""

from __future__ import annotations

import argparse
import json
import os
import shutil
import subprocess
import sys
from pathlib import Path

LOCK_NAME = ".skill-lock.json"
CONFIG_NAME = "skills.json"
SKILLS_DIR_NAME = "skills"
EXCLUDE_BEGIN = "# begin skill-manager"
EXCLUDE_END = "# end skill-manager"
INSTALL_IGNORE = (
    ".git",
    "__pycache__",
    ".venv",
    "node_modules",
    ".DS_Store",
    LOCK_NAME,
)
DEFAULT_LIBRARY = Path.home() / ".agents" / "skill-library"


def configure_stdio() -> None:
    for stream in (sys.stdout, sys.stderr):
        reconfigure = getattr(stream, "reconfigure", None)
        if reconfigure is not None:
            reconfigure(encoding="utf-8", errors="replace")


def parse_jsonc(text: str) -> object:
    out: list[str] = []
    i = 0
    n = len(text)
    in_str = False
    esc = False
    while i < n:
        c = text[i]
        if in_str:
            out.append(c)
            if esc:
                esc = False
            elif c == "\\":
                esc = True
            elif c == '"':
                in_str = False
            i += 1
            continue
        if c == '"':
            in_str = True
            out.append(c)
            i += 1
            continue
        if c == "/" and i + 1 < n and text[i + 1] == "/":
            i += 2
            while i < n and text[i] not in "\r\n":
                i += 1
            continue
        if c == "/" and i + 1 < n and text[i + 1] == "*":
            i += 2
            while i + 1 < n and not (text[i] == "*" and text[i + 1] == "/"):
                i += 1
            i = min(i + 2, n)
            continue
        out.append(c)
        i += 1
    return json.loads(strip_trailing_commas("".join(out)))


def strip_trailing_commas(text: str) -> str:
    out: list[str] = []
    i = 0
    n = len(text)
    in_str = False
    esc = False
    while i < n:
        c = text[i]
        if in_str:
            out.append(c)
            if esc:
                esc = False
            elif c == "\\":
                esc = True
            elif c == '"':
                in_str = False
            i += 1
            continue
        if c == '"':
            in_str = True
            out.append(c)
            i += 1
            continue
        if c == ",":
            j = i + 1
            while j < n and text[j] in " \t\r\n":
                j += 1
            if j < n and text[j] in "}]":
                i += 1
                continue
        out.append(c)
        i += 1
    return "".join(out)


def skill_files(root: Path) -> list[Path]:
    found: list[Path] = []
    if not root.is_dir():
        return found
    for path in root.rglob("SKILL.md"):
        if not path.is_file():
            continue
        rel_parts = path.relative_to(root).parts
        if any(part.startswith(".") for part in rel_parts):
            continue
        found.append(path)
    return found


def classify_source(src: Path) -> str:
    files = skill_files(src)
    if not files:
        raise ValueError(f"没有 SKILL.md: {src}")
    root_has = (src / "SKILL.md").is_file()
    others = [path for path in files if path.parent != src]
    if root_has and not others:
        return "single"
    if root_has and others:
        return "mixed"
    return "pack"


def direct_members(pack: Path) -> dict[str, Path]:
    members: dict[str, Path] = {}
    if not pack.is_dir():
        return members
    for child in pack.iterdir():
        if not child.is_dir() or child.name.startswith("."):
            continue
        if (child / "SKILL.md").is_file():
            members[child.name] = child
    return dict(sorted(members.items(), key=lambda item: item[0].casefold()))


def mixed_skill_names(pack: Path) -> list[str]:
    names: list[str] = []
    seen: set[str] = set()
    for path in skill_files(pack):
        name = pack.name if path.parent == pack else path.parent.name
        if name.casefold() in seen:
            name = path.parent.relative_to(pack).as_posix()
        if name.casefold() in seen:
            continue
        seen.add(name.casefold())
        names.append(name)
    return names


def inspect_pack(pack: Path) -> dict[str, object]:
    root_has = (pack / "SKILL.md").is_file()
    descendants = [path for path in skill_files(pack) if path.parent != pack]
    if root_has and descendants:
        return {
            "kind": "mixed",
            "skills": mixed_skill_names(pack),
            "members": {pack.name: pack},
        }
    if root_has:
        return {"kind": "single", "skills": [pack.name], "members": {pack.name: pack}}
    members = direct_members(pack)
    kind = "single" if len(members) == 1 and pack.name in members else "pack"
    return {"kind": kind, "skills": list(members), "members": members}


def lock_path(library: Path) -> Path:
    return library / LOCK_NAME


def read_lock(library: Path) -> dict[str, object]:
    path = lock_path(library)
    if not path.is_file():
        return {"version": 1, "packs": {}}
    data = json.loads(path.read_text(encoding="utf-8-sig"))
    if not isinstance(data, dict):
        return {"version": 1, "packs": {}}
    packs = data.get("packs")
    if not isinstance(packs, dict):
        packs = {}
    return {"version": 1, "packs": packs}


def pack_dirs(library: Path) -> list[Path]:
    if not library.is_dir():
        return []
    return sorted(
        (
            entry
            for entry in library.iterdir()
            if entry.is_dir() and not entry.name.startswith(".") and link_target(entry) is None
        ),
        key=lambda entry: entry.name.casefold(),
    )


def write_lock(library: Path, source_overrides: dict[str, str] | None = None) -> None:
    library.mkdir(parents=True, exist_ok=True)
    previous = read_lock(library).get("packs", {})
    if not isinstance(previous, dict):
        previous = {}
    overrides = source_overrides or {}
    packs: dict[str, object] = {}
    for pack in pack_dirs(library):
        info = inspect_pack(pack)
        old = previous.get(pack.name)
        source = overrides.get(pack.name, "")
        if not source and isinstance(old, dict) and isinstance(old.get("source"), str):
            source = old["source"]
        packs[pack.name] = {
            "kind": info["kind"],
            "source": source,
            "skills": info["skills"],
        }
    payload = {"version": 1, "packs": packs}
    with lock_path(library).open("w", encoding="utf-8", newline="\n") as handle:
        handle.write(json.dumps(payload, ensure_ascii=False, indent=2) + "\n")


def link_target(path: Path) -> Path | None:
    try:
        raw = os.readlink(path)
    except OSError:
        return None
    if raw.startswith("\\\\?\\"):
        raw = raw[4:]
    return Path(raw)


def same_path(left: Path, right: Path) -> bool:
    return os.path.normcase(str(left.resolve())) == os.path.normcase(str(right.resolve()))


def create_link(link: Path, target: Path) -> str:
    link.parent.mkdir(parents=True, exist_ok=True)
    try:
        os.symlink(target, link, target_is_directory=True)
        return "symlink"
    except OSError:
        result = subprocess.run(
            ["cmd", "/c", "mklink", "/J", str(link), str(target)],
            capture_output=True,
            text=True,
            encoding="utf-8",
            errors="replace",
        )
        if result.returncode != 0 or link_target(link) is None:
            detail = (result.stdout or "") + (result.stderr or "")
            raise RuntimeError(detail.strip() or "创建目录联接失败")
        return "junction"


def remove_link(path: Path) -> None:
    if path.is_symlink():
        path.unlink()
        return
    os.rmdir(path)


def valid_name(name: str) -> bool:
    if not name or name in (".", "..") or name.startswith("."):
        return False
    return not any(sep in name for sep in ("/", "\\", ":"))


def copy_install(src: Path, dest: Path) -> None:
    shutil.copytree(
        src,
        dest,
        ignore=shutil.ignore_patterns(*INSTALL_IGNORE),
        dirs_exist_ok=False,
    )


def install_pack(library: Path, src: Path) -> int:
    src = src.expanduser().resolve()
    if not src.is_dir():
        print(f"目录不存在: {src}")
        return 2
    library_resolved = library.resolve()
    if src == library_resolved or library_resolved in src.parents:
        print("不能把库目录本身或库内目录再装进库")
        return 2
    try:
        kind = classify_source(src)
    except ValueError as exc:
        print(str(exc))
        return 2
    pack_name = src.name
    if not valid_name(pack_name):
        print(f"非法包名: {pack_name}")
        return 2
    dest = library / pack_name
    if dest.exists() or link_target(dest) is not None:
        print(f"库里已有包: {pack_name}")
        return 1
    library.mkdir(parents=True, exist_ok=True)
    try:
        if kind == "single":
            dest.mkdir()
            copy_install(src, dest / pack_name)
        else:
            copy_install(src, dest)
    except Exception:
        if dest.exists() and link_target(dest) is None:
            shutil.rmtree(dest)
        raise
    write_lock(library, {pack_name: str(src)})
    info = inspect_pack(dest)
    print(f"已安装 {pack_name} ({info['kind']})")
    for skill in info["skills"]:
        print(f"  - {skill}")
    print(f"lock: {lock_path(library)}")
    return 0


def remove_pack(library: Path, pack_name: str) -> int:
    if not valid_name(pack_name):
        print(f"非法包名: {pack_name}")
        return 2
    dest = library / pack_name
    if link_target(dest) is not None:
        print(f"拒绝删除链接: {dest}")
        return 2
    if not dest.is_dir():
        print(f"库里没有包: {pack_name}")
        return 1
    resolved = dest.resolve()
    library_resolved = library.resolve()
    if library_resolved not in resolved.parents:
        print(f"拒绝删除库以外的路径: {dest}")
        return 2
    shutil.rmtree(dest)
    write_lock(library)
    print(f"已从库中删除 {pack_name}")
    print("仓库里的链接要再跑 link 才会去掉")
    return 0


def agent_home() -> Path:
    override = os.environ.get("SKILL_MANAGER_HOME")
    if override:
        return Path(override)
    return Path.home() / ".agents"


def global_config_path() -> Path:
    return agent_home() / CONFIG_NAME


def global_skills_dir() -> Path:
    return agent_home() / "skills"


def load_config_file(path: Path) -> list[str]:
    data = parse_jsonc(path.read_text(encoding="utf-8-sig"))
    if isinstance(data, dict):
        data = data.get("skills", [])
    if not isinstance(data, list) or not all(isinstance(item, str) for item in data):
        raise ValueError(f"{path} 必须是字符串数组")
    return [item.strip() for item in data if item.strip()]


def load_selection(root: Path) -> list[str] | None:
    path = root / ".agents" / CONFIG_NAME
    if not path.is_file():
        return None
    return load_config_file(path)


def global_coverage(library: Path) -> tuple[set[str], list[str]]:
    path = global_config_path()
    if not path.is_file():
        return set(), []
    desired, problems = resolve_selection(library, load_config_file(path))
    return set(desired), problems


def leading_comment(text: str) -> str:
    kept: list[str] = []
    for line in text.splitlines():
        stripped = line.strip()
        if not stripped or stripped.startswith("//"):
            kept.append(line)
            continue
        break
    return "\n".join(kept).rstrip()


def write_selection(path: Path, entries: list[str], created_comment: str) -> None:
    comment = ""
    if path.is_file():
        comment = leading_comment(path.read_text(encoding="utf-8-sig"))
    if not comment:
        comment = created_comment
    body = "[\n" + "".join(f"  {json.dumps(item, ensure_ascii=False)},\n" for item in entries) + "]\n"
    path.parent.mkdir(parents=True, exist_ok=True)
    text = f"{comment}\n{body}" if comment else body
    with path.open("w", encoding="utf-8", newline="\n") as handle:
        handle.write(text)


def validate_entry(library: Path, entry: str) -> list[str]:
    _desired, problems = resolve_selection(library, [entry])
    return problems


def resolve_selection(
    library: Path, entries: list[str]
) -> tuple[dict[str, Path], list[str]]:
    desired: dict[str, Path] = {}
    problems: list[str] = []
    for entry in entries:
        if ":" in entry:
            pack_name, skill_name = entry.split(":", 1)
            pack_name, skill_name = pack_name.strip(), skill_name.strip()
            single = False
        else:
            pack_name, skill_name = entry.strip(), ""
            single = False
        if not valid_name(pack_name) or (skill_name and not valid_name(skill_name)):
            problems.append(f"! 非法配置 {entry}")
            continue
        pack = library / pack_name
        if not pack.is_dir():
            problems.append(f"! 库中没有包 {pack_name}")
            continue
        info = inspect_pack(pack)
        members: dict[str, Path] = info["members"]  # type: ignore[assignment]
        if info["kind"] == "mixed":
            if skill_name:
                problems.append(f"! 混合包不能单拆 {entry}")
                continue
            links = {pack_name: pack}
        elif skill_name:
            target = members.get(skill_name)
            if target is None:
                problems.append(f"! 包 {pack_name} 里没有技能 {skill_name}")
                continue
            links = {skill_name: target}
            single = True
        else:
            links = dict(members)
            if not links:
                problems.append(f"! 包 {pack_name} 里没有可链接的技能")
                continue
        for link_name, target in links.items():
            current = desired.get(link_name)
            if current is not None and not same_path(current, target):
                label = entry if single or skill_name or info["kind"] == "mixed" else f"{pack_name}:{link_name}"
                problems.append(f"! 同名跳过 {label}（已占用 {link_name}）")
                continue
            desired[link_name] = target
    return desired, problems


def git_exclude_file(root: Path) -> Path | None:
    result = subprocess.run(
        ["git", "-C", str(root), "rev-parse", "--git-path", "info/exclude"],
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
    )
    if result.returncode != 0:
        return None
    path = Path(result.stdout.strip())
    if not path.is_absolute():
        path = root / path
    return path


def update_exclude(root: Path, rel_paths: list[str]) -> None:
    exclude = git_exclude_file(root)
    if exclude is None:
        return
    exclude.parent.mkdir(parents=True, exist_ok=True)
    existing = exclude.read_text(encoding="utf-8") if exclude.is_file() else ""
    begin = existing.find(EXCLUDE_BEGIN)
    end = existing.find(EXCLUDE_END)
    if begin != -1 and end != -1 and end > begin:
        existing = existing[:begin] + existing[end + len(EXCLUDE_END) :]
    existing = existing.strip()
    block = ""
    if rel_paths:
        block = "\n".join([EXCLUDE_BEGIN, *rel_paths, EXCLUDE_END])
    parts = [part for part in (existing, block) if part]
    text = "\n\n".join(parts)
    with exclude.open("w", encoding="utf-8", newline="\n") as handle:
        handle.write((text + "\n") if text else "")


def skills_dir(root: Path) -> Path:
    return root / ".agents" / SKILLS_DIR_NAME


def apply_links(
    root: Path,
    desired: dict[str, Path],
    problems: list[str],
    write: bool,
    covered: set[str] | None = None,
    folder: Path | None = None,
    manage_exclude: bool = True,
) -> int:
    root = root.resolve()
    folder = folder or skills_dir(root)
    covered = covered or set()
    created: list[str] = []
    updated: list[str] = []
    removed: list[str] = []
    unchanged: list[str] = []
    owned: list[str] = []
    skipped: list[str] = []
    seen_covered: set[str] = set()
    linked: dict[str, Path] = {}

    if folder.is_dir():
        for entry in sorted(folder.iterdir(), key=lambda item: item.name.casefold()):
            if entry.name.startswith("."):
                continue
            current = link_target(entry)
            if entry.name in covered:
                seen_covered.add(entry.name)
                if current is None:
                    problems.append(
                        f"! 冲突 {entry.name}: global 已覆盖，但 .agents/skills/{entry.name} 是真实目录，未改动"
                    )
                else:
                    if write:
                        remove_link(entry)
                    removed.append(f"{entry.name}（global 已覆盖）")
                continue
            wanted = desired.get(entry.name)
            if current is None:
                if wanted is not None:
                    problems.append(f"! 冲突 {entry.name}: .agents/skills/{entry.name} 已是真实目录，未改动")
                else:
                    owned.append(entry.name)
                continue
            if wanted is None:
                if write:
                    remove_link(entry)
                removed.append(entry.name)
                continue
            if same_path(entry, wanted):
                unchanged.append(entry.name)
                linked[entry.name] = wanted
                continue
            if write:
                remove_link(entry)
                kind = create_link(entry, wanted)
                updated.append(f"{entry.name} ({kind})")
            else:
                updated.append(entry.name)
            linked[entry.name] = wanted

    for name, target in desired.items():
        if name in linked or any(item.startswith(f"! 冲突 {name}:") for item in problems):
            continue
        dest = folder / name
        if dest.exists() and link_target(dest) is None:
            problems.append(f"! 冲突 {name}: .agents/skills/{name} 已是真实目录，未改动")
            continue
        if write:
            kind = create_link(dest, target)
            created.append(f"{name} ({kind})")
        else:
            created.append(name)
        linked[name] = target

    for name in sorted(covered, key=str.casefold):
        if name not in seen_covered:
            skipped.append(name)

    if write and manage_exclude:
        rel_paths = [f".agents/skills/{name}" for name in sorted(linked, key=str.casefold)]
        update_exclude(root, rel_paths)

    if write:
        if created or updated or removed:
            print("已按配置同步")
        elif problems:
            print("未完全对齐")
        else:
            print("已与配置一致")
    else:
        if created or updated or removed or problems:
            print("未对齐")
        else:
            print("已与配置一致")
    prefix = "  " if write else "  将"
    for line in created:
        print(f"{prefix}+ {line}" if write else f"  + 缺少 {line}")
    for line in updated:
        print(f"  ~ {'更新 ' if write else '指向不同 '}{line}")
    for line in removed:
        print(f"  - {'移除 ' if write else '多余 '}{line}")
    for line in unchanged:
        print(f"  = 未变化 {line}")
    for line in problems:
        print(f"  {line}" if line.startswith("!") else f"  ! {line}")
    for line in owned:
        print(f"  · 仓库自有目录，未改动 {line}")
    for line in skipped:
        print(f"  · 跳过 {line}（global 已覆盖）")
    if not any((created, updated, removed, unchanged, problems, owned, skipped)):
        print("  （配置的列表为空，未链入任何技能）")
    return 1 if problems else 0


def cmd_list(library: Path, pack_name: str | None) -> int:
    if not library.is_dir():
        print(f"skill 库不存在: {library}")
        return 2
    if not pack_name:
        packs = pack_dirs(library)
        if not packs:
            print("（空）")
            return 0
        for pack in packs:
            info = inspect_pack(pack)
            skills = info["skills"]
            assert isinstance(skills, list)
            print(f"{pack.name}  {info['kind']}  {len(skills)}")
        return 0
    if not valid_name(pack_name):
        print(f"非法包名: {pack_name}")
        return 2
    pack = library / pack_name
    if not pack.is_dir() or link_target(pack) is not None:
        print(f"库里没有包: {pack_name}")
        return 1
    info = inspect_pack(pack)
    skills = info["skills"]
    assert isinstance(skills, list)
    print(f"{pack.name}  {info['kind']}")
    if not skills:
        print("  （无技能）")
        return 0
    for skill in skills:
        print(f"  {skill}")
    return 0


def print_library(library: Path) -> None:
    print(f"库: {library}")
    packs = pack_dirs(library)
    if not packs:
        print("  （空）")
        return
    for pack in packs:
        info = inspect_pack(pack)
        skills = info["skills"]
        assert isinstance(skills, list)
        preview = ", ".join(str(item) for item in skills[:8])
        extra = "" if len(skills) <= 8 else f" +{len(skills) - 8}"
        print(f"  {pack.name}  {info['kind']}  {preview}{extra}")


def workspace_plan(
    library: Path, entries: list[str]
) -> tuple[dict[str, Path], list[str], set[str]]:
    desired, problems = resolve_selection(library, entries)
    covered, global_problems = global_coverage(library)
    problems = global_problems + problems
    for name in list(desired):
        if name in covered:
            del desired[name]
    return desired, problems, covered


def cmd_status(library: Path, roots: list[Path]) -> int:
    print_library(library)
    code = report_global(library, write=False)
    for root in roots:
        print("")
        print(f"根目录: {root.resolve()}")
        config = root / ".agents" / CONFIG_NAME
        print(f"配置: {config}")
        try:
            entries = load_selection(root)
        except (OSError, json.JSONDecodeError, ValueError) as exc:
            print(f"配置无法读取: {exc}")
            code = 2
            continue
        if entries is None:
            print("未找到 .agents/skills.json")
            continue
        try:
            desired, problems, covered = workspace_plan(library, entries)
        except (OSError, json.JSONDecodeError, ValueError) as exc:
            print(f"global 配置无法读取: {exc}")
            code = 2
            continue
        code = max(code, apply_links(root, desired, problems, write=False, covered=covered))
    return code


def report_global(library: Path, write: bool) -> int:
    path = global_config_path()
    print("")
    print(f"global 配置: {path}")
    print(f"global 目标: {global_skills_dir()}")
    if not path.is_file():
        print("未找到 global skills.json")
        return 0
    try:
        entries = load_config_file(path)
    except (OSError, json.JSONDecodeError, ValueError) as exc:
        print(f"配置无法读取: {exc}")
        return 2
    desired, problems = resolve_selection(library, entries)
    return apply_links(
        agent_home(),
        desired,
        problems,
        write=write,
        folder=global_skills_dir(),
        manage_exclude=False,
    )


def cmd_link(library: Path, roots: list[Path]) -> int:
    code = 0
    for index, root in enumerate(roots):
        if index:
            print("")
        print(f"根目录: {root.resolve()}")
        if not root.is_dir():
            print(f"根目录不存在: {root}")
            code = 2
            continue
        config = root / ".agents" / CONFIG_NAME
        print(f"配置: {config}")
        try:
            entries = load_selection(root)
        except (OSError, json.JSONDecodeError, ValueError) as exc:
            print(f"配置无法读取: {exc}")
            code = 2
            continue
        if entries is None:
            print("未找到 .agents/skills.json。请写入字符串数组后再 link。")
            code = 2
            continue
        try:
            desired, problems, covered = workspace_plan(library, entries)
        except (OSError, json.JSONDecodeError, ValueError) as exc:
            print(f"global 配置无法读取: {exc}")
            code = 2
            continue
        code = max(code, apply_links(root, desired, problems, write=True, covered=covered))
    return code


def cmd_link_global(library: Path) -> int:
    path = global_config_path()
    print(f"global 配置: {path}")
    print(f"global 目标: {global_skills_dir()}")
    if not path.is_file():
        print("未找到 global skills.json。请先 add --global。")
        return 2
    try:
        entries = load_config_file(path)
    except (OSError, json.JSONDecodeError, ValueError) as exc:
        print(f"配置无法读取: {exc}")
        return 2
    desired, problems = resolve_selection(library, entries)
    return apply_links(
        agent_home(),
        desired,
        problems,
        write=True,
        folder=global_skills_dir(),
        manage_exclude=False,
    )


def cmd_add(library: Path, names: list[str], root: Path, use_global: bool) -> int:
    problems: list[str] = []
    for name in names:
        problems.extend(validate_entry(library, name))
    if problems:
        for line in problems:
            print(line if line.startswith("!") else f"! {line}")
        print("未修改配置")
        return 1
    if use_global:
        path = global_config_path()
        comment = "// global 技能。运行 /skill-manager link --global。"
        existing: list[str] = []
        if path.is_file():
            existing = load_config_file(path)
    else:
        if not root.is_dir():
            print(f"根目录不存在: {root}")
            return 2
        path = root / ".agents" / CONFIG_NAME
        comment = "// 启用的技能包。包名加载整包，包名:技能名 只加载一个。运行 /skill-manager link。"
        existing = load_selection(root) or []
    added: list[str] = []
    current = list(existing)
    seen = {item.casefold() for item in current}
    for name in names:
        if name.casefold() in seen:
            print(f"已存在 {name}")
            continue
        current.append(name)
        seen.add(name.casefold())
        added.append(name)
    print(f"配置: {path}")
    if not added:
        print("没有新条目")
        return 0
    write_selection(path, current, comment)
    for name in added:
        print(f"  + {name}")
    return 0


def cmd_lock(library: Path) -> int:
    if not library.is_dir():
        print(f"skill 库不存在: {library}")
        return 2
    write_lock(library)
    print(f"已按磁盘重写 {lock_path(library)}")
    print_library(library)
    return 0


def main(argv: list[str] | None = None) -> int:
    configure_stdio()
    parser = argparse.ArgumentParser(description="管理 skill 库与仓库链接")
    parser.add_argument("--library", type=Path, default=DEFAULT_LIBRARY)
    sub = parser.add_subparsers(dest="cmd")

    install = sub.add_parser("install", help="把本机目录装进 skill 库")
    install.add_argument("path", type=Path)

    remove = sub.add_parser("remove", help="从 skill 库删除一个包")
    remove.add_argument("pack")

    sub.add_parser("lock", help="按磁盘重写 .skill-lock.json")

    listing = sub.add_parser("list", help="列出包；指定包名时列出包内技能")
    listing.add_argument("pack", nargs="?")

    link = sub.add_parser("link", help="按仓库配置链接技能")
    link.add_argument("--root", action="append", type=Path)
    link.add_argument("--global", dest="use_global", action="store_true")

    add = sub.add_parser("add", help="把技能写入 skills.json，不创建链接")
    add.add_argument("names", nargs="+")
    add.add_argument("--root", type=Path)
    add.add_argument("--global", dest="use_global", action="store_true")

    status = sub.add_parser("status", help="只查看，不修改")
    status.add_argument("--root", action="append", type=Path)

    args = parser.parse_args(argv)
    library = args.library.expanduser()
    cmd = args.cmd or "status"
    if cmd == "install":
        return install_pack(library, args.path)
    if cmd == "remove":
        return remove_pack(library, args.pack)
    if cmd == "lock":
        return cmd_lock(library)
    if cmd == "list":
        return cmd_list(library, getattr(args, "pack", None))
    if cmd == "add":
        root = args.root or Path.cwd()
        return cmd_add(library, args.names, root, args.use_global)
    if cmd == "link" and getattr(args, "use_global", False):
        if args.root:
            print("link --global 不使用 --root")
        return cmd_link_global(library)
    roots = getattr(args, "root", None) or [Path.cwd()]
    if cmd == "link":
        return cmd_link(library, roots)
    return cmd_status(library, roots)


if __name__ == "__main__":
    raise SystemExit(main())
