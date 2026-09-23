---
name: skill-manager
description: >-
  Installs local skill directories into ~/.agents/skill-library, rewrites
  .skill-lock.json from disk, links selected packs into the current
  repo's .agents/skills from .agents/skills.json, and lists packs or the
  skills inside a pack. Use when the user runs /skill-manager, or asks to
  install, link, lock, list, add, remove, or check skills.
disable-model-invocation: true
---

# skill-manager

管理本机技能库，并按仓库配置把技能链到当前仓库。不写子命令时只查看，不改文件。

## 命令

对用户这句话选择一个子命令。链接和查看要对当前窗口里的每个 workspace 根目录各传一次 `--root`。

```bash
uv run python ~/.agents/skills/skill-manager/scripts/skill_manager.py status --root "<根目录>"
uv run python ~/.agents/skills/skill-manager/scripts/skill_manager.py install "<本机目录>"
uv run python ~/.agents/skills/skill-manager/scripts/skill_manager.py link --root "<根目录>"
uv run python ~/.agents/skills/skill-manager/scripts/skill_manager.py link --global
uv run python ~/.agents/skills/skill-manager/scripts/skill_manager.py add --root "<根目录>" "<包名或包名:技能名>"
uv run python ~/.agents/skills/skill-manager/scripts/skill_manager.py add --global "<包名或包名:技能名>"
uv run python ~/.agents/skills/skill-manager/scripts/skill_manager.py lock
uv run python ~/.agents/skills/skill-manager/scripts/skill_manager.py list
uv run python ~/.agents/skills/skill-manager/scripts/skill_manager.py list "<包名>"
uv run python ~/.agents/skills/skill-manager/scripts/skill_manager.py remove "<包名>"
```

`list` 不带包名时只列出包。带包名时列出该包里的技能。

`add` 只改 json，不创建链接。不带 `--global` 时写 `--root` 对应仓库的 `.agents/skills.json`，不写 `--root` 就用当前目录。`add --global` 写 `~/.agents/skills.json`，文件不存在就创建。库里必须有这项，混合包不能单拆，已有条目不重复添加。有一条不合法就整个不改。

`link --global` 按 global 配置把技能链到 `~/.agents/skills`。配置文件不存在就报错，不建链接。真实目录 `skill-manager` 不删除、不覆盖。

仓库 `link` 会跳过 global 配置里的技能名：不新建，并删掉仓库里已有的同名链接。`status` 用同一套规则判断是否对齐。

把脚本输出告诉用户。退出码不是 0 时不要自行补链、覆盖真实目录或删除库里的包。

## 库

路径是 `~/.agents/skill-library/`。库根只放包，不直接放技能。

- 单个技能：`skill-library/<名>/<名>/SKILL.md`
- 纯技能包：`skill-library/<包名>/<技能名>/SKILL.md`
- 混合包：根上和子目录都有 `SKILL.md`，保持原目录，不套同名层

`install` 只接受本机目录，包名等于该目录名。根上有 `SKILL.md` 且下面没有，按单个技能安装。根上没有、下面有，按纯包整棵复制。两边都有，按混合包原样复制。

`.skill-lock.json` 是磁盘的索引。`install` 和 `remove` 会同时改目录和 lock。`lock` 只按磁盘重写索引，不删目录。

## 仓库配置

`.agents/skills.json` 是字符串数组：

```json
["mattpocock", "baoyu:baoyu-translate"]
```

- `包名`：纯包展开，每个技能一条链接到 `.agents/skills/<技能名>`。单个技能的包同样展开成那一个技能。
- `包名:技能名`：只链这一个。
- 混合包只能写包名，整包一条链接到 `.agents/skills/<包名>`。

同名技能报冲突并跳过，不覆盖、不加前缀。配置里去掉的链接会删掉。目标已是真实目录则跳过。优先符号链接，没有权限时用目录联接。

新链入的技能从下一次 Agent 对话开始可用。
