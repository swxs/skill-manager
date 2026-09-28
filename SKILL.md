---
name: skill-manager
version: v0.3.0
description: >-
  Installs local or remote (git URL) skill packs into ~/.agents/skill-library,
  rewrites .skill-lock.json, links from .agents/skills.json, and can init
  global skills. Use when the user runs /skill-manager, or asks to
  install, link, lock, list, add, remove, upgrade, init, or check skills.
disable-model-invocation: true
---

# skill-manager

管理本机技能库，并按仓库配置把技能链到当前仓库。不写子命令时只查看，不改文件。

## 命令

对用户这句话选择一个子命令。链接和查看要对当前窗口里的每个 workspace 根目录各传一次 `--root`。

Windows：

```powershell
powershell -NoProfile -File "$env:USERPROFILE\.agents\skills\skill-manager\scripts\skill-manager.ps1" status --root "<根目录>"
```

macOS / Linux：

```bash
~/.agents/skills/skill-manager/scripts/skill-manager status --root "<根目录>"
```

把 `status` 换成下面的子命令。脚本名后面的参数原样交给运行时。

```bash
install "<本机目录>"
link --root "<根目录>"
link --global
add --root "<根目录>" "<包名或包名:技能名>"
add --global "<包名、包名:技能名或 Git URL>"
upgrade "<包名>"
init
lock
list
list "<包名>"
remove "<包名>"
```

`list` 不带包名时只列出包。带包名时列出该包里的技能。

`add` 只改 json，不自动 link。参数可以是库内 **包名** / **包名:技能名**，或 **Git URL**（可选 `#ref`）：URL 会先 fetch 进 skill-library（保留 `.git`），成功后再把 **包名** 写入配置；fetch 失败则整个不改。不带 `--global` 时写 `--root` 对应仓库的 `.agents/skills.json`。`add --global` 写 `~/.agents/skills.json`。库中缺包时输出会提示向用户要 Git URL。混合包不能单拆。有一条不合法就整个不改。

用户要安装或启用技能时，尽量代跑 `add`（含 URL）→ `link` / `link --global`，不要手 copy 到 `.agents/skills`。

`link --global` 按 global 配置把技能链到 `~/.agents/skills`。配置文件不存在就报错，不建链接。真实目录 `skill-manager` 不删除、不覆盖。

仓库 `link` 会跳过 global 配置里的技能名：不新建，并删掉仓库里已有的同名链接。`status` 用同一套规则判断是否对齐。

把脚本输出告诉用户。退出码不是 0 时不要自行补链、覆盖真实目录或删除库里的包。

## 库

路径是 `~/.agents/skill-library/`。库根只放包，不直接放技能。

- 单个技能：`skill-library/<名>/<名>/SKILL.md`
- 纯技能包：`skill-library/<包名>/<技能名>/SKILL.md`
- 混合包：根上和子目录都有 `SKILL.md`，保持原目录，不套同名层

`install` 只接受本机目录，包名等于该目录名；尽量保留源目录的 `.git`，lock 的 `source`/`ref`/`revision` 从 git 解读。根上有 `SKILL.md` 且下面没有，按单个技能安装。根上没有、下面有，按纯包整棵复制。两边都有，按混合包原样复制。

`init` 只处理 global：把 `~/.agents/skills` 迁入 skill-library（`skill-manager` 真实目录跳过），写 `~/.agents/skills.json` 并 `link --global`。

`.skill-lock.json` 是磁盘的索引。`install`、`remove`、`upgrade` 和 `lock` 会更新 lock。

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
