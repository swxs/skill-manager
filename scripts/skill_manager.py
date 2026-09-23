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
    "__pycache__",
    ".venv",
    "node_modules",
    ".DS_Store",
    LOCK_NAME,
)
INIT_SKIP_REAL_DIRS = frozenset({"skill-manager"})
MISSING_PACK_HINT = (
    "? 请提供 Git 仓库 URL（可选 #ref）以安装，"
    "例如：add --global https://github.com/org/repo.git"
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


def git_command(cwd: Path, *args: str) -> str | None:
    result = subprocess.run(
        ["git", "-C", str(cwd), *args],
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
    )
    if result.returncode != 0:
        return None
    return (result.stdout or result.stderr or "").strip() or None


def pack_git_root(pack: Path) -> Path | None:
    if (pack / ".git").is_dir():
        return pack
    info = inspect_pack(pack)
    if info["kind"] == "single":
        nested = pack / pack.name
        if (nested / ".git").is_dir():
            return nested
    return None


def git_metadata(git_root: Path, fallback_source: str = "") -> dict[str, str]:
    meta: dict[str, str] = {}
    remote = git_command(git_root, "remote", "get-url", "origin")
    if remote:
        meta["source"] = remote
    elif fallback_source:
        meta["source"] = fallback_source
    revision = git_command(git_root, "rev-parse", "HEAD")
    if revision:
        meta["revision"] = revision
    ref = git_command(git_root, "symbolic-ref", "-q", "--short", "HEAD")
    if not ref:
        ref = git_command(git_root, "describe", "--tags", "--always")
    if ref:
        meta["ref"] = ref
    return meta


def is_git_url(text: str) -> bool:
    stripped = text.strip()
    if not stripped:
        return False
    if stripped.startswith("git@"):
        return True
    if "://" in stripped:
        return True
    if stripped.endswith(".git"):
        return True
    candidate = Path(stripped)
    try:
        if candidate.is_dir() and (candidate / ".git").exists():
            return True
    except OSError:
        pass
    return False


def parse_git_spec(text: str) -> tuple[str, str | None]:
    stripped = text.strip()
    ref: str | None = None
    if "#" in stripped:
        stripped, ref = stripped.rsplit("#", 1)
        ref = ref.strip() or None
    return stripped.strip(), ref


def pack_name_from_git_url(url: str) -> str:
    cleaned = url.rstrip("/")
    if cleaned.endswith(".git"):
        cleaned = cleaned[:-4]
    name = Path(cleaned.replace(":", "/")).name
    if not valid_name(name):
        raise ValueError(f"无法从 URL 推导包名: {url}")
    return name


def git_clone(url: str, dest: Path, ref: str | None = None) -> None:
    dest.parent.mkdir(parents=True, exist_ok=True)
    result = subprocess.run(
        ["git", "clone", url, str(dest)],
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
    )
    if result.returncode != 0:
        detail = (result.stdout or "") + (result.stderr or "")
        raise RuntimeError(detail.strip() or "git clone 失败")
    if ref:
        checkout = subprocess.run(
            ["git", "-C", str(dest), "checkout", ref],
            capture_output=True,
            text=True,
            encoding="utf-8",
            errors="replace",
        )
        if checkout.returncode != 0:
            shutil.rmtree(dest)
            detail = (checkout.stdout or "") + (checkout.stderr or "")
            raise RuntimeError(detail.strip() or f"无法检出 ref: {ref}")


def fetch_remote_pack(
    library: Path, url: str, ref: str | None = None, pack_name: str | None = None
) -> str:
    pack_name = pack_name or pack_name_from_git_url(url)
    if not valid_name(pack_name):
        raise ValueError(f"非法包名: {pack_name}")
    dest = library / pack_name
    if dest.exists() or link_target(dest) is not None:
        return pack_name
    library.mkdir(parents=True, exist_ok=True)
    try:
        git_clone(url, dest, ref)
        classify_source(dest)
    except Exception:
        if dest.exists() and link_target(dest) is None:
            shutil.rmtree(dest, ignore_errors=True)
        raise
    write_lock(library)
    return pack_name


def print_problems(problems: list[str]) -> None:
    for line in problems:
        print(line if line.startswith("!") else f"! {line}")
    if any("! 库中没有包" in item for item in problems):
        print(MISSING_PACK_HINT)


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
        fallback = overrides.get(pack.name, "")
        if not fallback and isinstance(old, dict) and isinstance(old.get("source"), str):
            fallback = old["source"]
        git_root = pack_git_root(pack)
        entry: dict[str, object] = {
            "kind": info["kind"],
            "source": fallback,
            "skills": info["skills"],
        }
        if git_root:
            entry.update(git_metadata(git_root, fallback_source=fallback))
        packs[pack.name] = entry
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
    git_root = pack_git_root(dest)
    fallback = str(src)
    if git_root:
        meta = git_metadata(git_root, fallback_source=fallback)
        fallback = meta.get("source", fallback)
    write_lock(library, {pack_name: fallback})
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
    if any("! 库中没有包" in item for item in problems):
        print(f"  {MISSING_PACK_HINT}")
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


def resolve_add_names(library: Path, names: list[str]) -> tuple[list[str], int]:
    resolved: list[str] = []
    for name in names:
        if is_git_url(name):
            url, ref = parse_git_spec(name)
            try:
                pack_name = fetch_remote_pack(library, url, ref)
            except (ValueError, RuntimeError, OSError) as exc:
                print(str(exc))
                return [], 1
            print(f"已获取远程包 {pack_name}")
            resolved.append(pack_name)
        else:
            resolved.append(name)
    return resolved, 0


def cmd_add(library: Path, names: list[str], root: Path, use_global: bool) -> int:
    resolved, code = resolve_add_names(library, names)
    if code != 0:
        print("未修改配置")
        return code
    problems: list[str] = []
    for name in resolved:
        problems.extend(validate_entry(library, name))
    if problems:
        print_problems(problems)
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
    for name in resolved:
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


def origin_default_branch(git_root: Path) -> str:
    ref = git_command(git_root, "symbolic-ref", "refs/remotes/origin/HEAD")
    if ref:
        return ref.rsplit("/", maxsplit=1)[-1]
    for branch in ("main", "master"):
        if git_command(git_root, "show-ref", "--verify", f"refs/remotes/origin/{branch}"):
            return branch
    remote = git_command(git_root, "branch", "-r")
    if remote:
        for line in remote.splitlines():
            line = line.strip()
            if line.startswith("origin/") and "HEAD" not in line:
                return line.split("/", maxsplit=1)[1]
    return "main"


def upgrade_pack(library: Path, pack_name: str) -> int:
    if not valid_name(pack_name):
        print(f"非法包名: {pack_name}")
        return 2
    dest = library / pack_name
    if not dest.is_dir() or link_target(dest) is not None:
        print(f"库里没有包: {pack_name}")
        return 1
    git_root = pack_git_root(dest)
    if git_root is None:
        print(f"包 {pack_name} 不是 git 工作区，无法 upgrade")
        return 2
    fetch = subprocess.run(
        ["git", "-C", str(git_root), "fetch", "origin"],
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
    )
    if fetch.returncode != 0:
        detail = (fetch.stdout or "") + (fetch.stderr or "")
        print(detail.strip() or "git fetch 失败")
        return 2
    branch = origin_default_branch(git_root)
    checkout = subprocess.run(
        ["git", "-C", str(git_root), "checkout", branch],
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
    )
    if checkout.returncode != 0:
        detail = (checkout.stdout or "") + (checkout.stderr or "")
        print(detail.strip() or f"无法切换到 {branch}")
        return 2
    pull = subprocess.run(
        ["git", "-C", str(git_root), "pull", "--ff-only", "origin", branch],
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
    )
    if pull.returncode != 0:
        detail = (pull.stdout or "") + (pull.stderr or "")
        print(detail.strip() or "git pull 失败")
        return 2
    write_lock(library)
    meta = git_metadata(git_root)
    print(f"已升级 {pack_name} ({branch} @ {meta.get('revision', '?')})")
    return 0


def init_selection_entries(library: Path, skills_folder: Path) -> list[str]:
    library_resolved = library.resolve()
    by_pack: dict[str, set[str]] = {}
    real_installs: list[Path] = []

    if skills_folder.is_dir():
        for entry in sorted(skills_folder.iterdir(), key=lambda item: item.name.casefold()):
            if entry.name.startswith("."):
                continue
            if entry.name in INIT_SKIP_REAL_DIRS and link_target(entry) is None:
                continue
            target = link_target(entry)
            if target is not None:
                try:
                    rel = target.resolve().relative_to(library_resolved)
                except ValueError:
                    resolved_target = target.resolve()
                    if resolved_target.is_dir():
                        real_installs.append(resolved_target)
                    continue
                pack_name = rel.parts[0]
                skill_key = rel.parts[1] if len(rel.parts) >= 2 else pack_name
                by_pack.setdefault(pack_name, set()).add(skill_key)
                continue
            real_installs.append(entry.resolve())

    for src in real_installs:
        if not src.is_dir():
            continue
        try:
            classify_source(src)
        except ValueError:
            continue
        pack_name = src.name
        if not valid_name(pack_name):
            continue
        dest = library / pack_name
        if not dest.exists() and link_target(dest) is None:
            install_pack(library, src)
        by_pack.setdefault(pack_name, set())

    entries: list[str] = []
    for pack_name in sorted(by_pack, key=str.casefold):
        pack = library / pack_name
        if not pack.is_dir():
            continue
        info = inspect_pack(pack)
        skills = info["skills"]
        assert isinstance(skills, list)
        linked = by_pack.get(pack_name, set())
        if info["kind"] == "mixed":
            entries.append(pack_name)
            continue
        skill_names = {str(item) for item in skills}
        if linked and linked >= skill_names:
            entries.append(pack_name)
        elif linked:
            for skill in sorted(linked, key=str.casefold):
                entries.append(f"{pack_name}:{skill}")
        else:
            entries.append(pack_name)
    return entries


def cmd_init(library: Path) -> int:
    home = agent_home()
    skills_folder = global_skills_dir()
    print(f"init global: {skills_folder}")
    print(f"库: {library}")
    entries = init_selection_entries(library, skills_folder)
    path = global_config_path()
    comment = "// global 技能。运行 /skill-manager link --global。"
    write_selection(path, entries, comment)
    print(f"配置: {path}")
    for entry in entries:
        print(f"  + {entry}")
    write_lock(library)
    print(f"lock: {lock_path(library)}")
    return cmd_link_global(library)


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

    upgrade = sub.add_parser("upgrade", help="从 origin 默认分支拉取 git 包")
    upgrade.add_argument("pack")

    sub.add_parser("init", help="将 global ~/.agents/skills 迁入 skill-library 并 link")

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
    if cmd == "upgrade":
        return upgrade_pack(library, args.pack)
    if cmd == "init":
        return cmd_init(library)
    return cmd_status(library, roots)


if __name__ == "__main__":
    raise SystemExit(main())
