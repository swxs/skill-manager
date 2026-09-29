---
name: skill-manager
version: v0.4.1
description: >-
  Manages the skill library at ~/.agents/skill-library and links skills
  from a workspace or global-workspace skills.json. Use when the user runs
  /skill-manager, or asks to init, list, install, upgrade, lock, status,
  add, remove, or sync skills.
disable-model-invocation: true
---

# skill-manager

管理本机技能库，并按工作区或全局工作区的技能声明链出技能。

## 术语

- **技能库**：`~/.agents/skill-library`
- **工作区**：项目里的 `.agents/`
- **全局工作区**：`~/.agents/`
- **技能声明**：工作区或全局工作区里的 `skills.json`

## 调用

Windows：

```powershell
powershell -NoProfile -File "$env:USERPROFILE\.agents\skills\skill-manager\scripts\skill-manager.ps1" <命令>
```

macOS / Linux：

```bash
~/.agents/skills/skill-manager/scripts/skill-manager <命令>
```

脚本名后面的参数原样交给运行时。旗标以 `skill-manager <命令> --help` 为准。

`--root` 传包含该工作区的目录，也就是 `.agents/` 的上一级。当前窗口里每个这样的目录各传一次。不写则用当前目录。
`--global` 与 `--root` 同时出现时以全局工作区为准。

## 按意图选命令

| 意图 | 命令 |
| --- | --- |
| 初始化技能管理体系 | `init` |
| 查看当前技能库 | `list` |
| 安装技能到技能库 | `install` |
| 更新技能库中的技能 | `upgrade` |
| 锁定技能库信息 | `lock` |
| 查看全局配置、全局工作区、工作区技能状态 | `status` |
| 添加技能声明 | `add` |
| 移除技能声明 | `remove` |
| 按技能声明同步技能 | `sync` |

安装或启用技能时按这个顺序代跑：`install`，再 `add`，再 `sync`。全局工作区用 `add --global` 和 `sync --global`。不要把目录复制进 `.agents/skills`。

`add` 和 `remove` 只改技能声明。声明改完后再 `sync`，链接才会变。没有从技能库删除整个包的命令。

把脚本输出告诉用户。退出码不是 0 时停住，不要自行补链、覆盖真实目录或删除技能库里的包。

## 技能声明怎么写

技能声明是字符串数组：

```json
["mattpocock", "baoyu:baoyu-translate"]
```

- `包名`：纯包展开，每个技能一条链接。单个技能的包同样展开成那一个技能。
- `包名:技能名`：只链这一个。
- 混合包只写包名，整包一条链接到 `.agents/skills/<包名>`。

`remove` 只删完全相同的条目，不连带删掉 `包名:技能名`。

## 链出

同名技能报冲突并跳过，不覆盖、不加前缀。目标已是真实目录则跳过。`~/.agents/skills/skill-manager` 是真实目录，`sync --global` 不删除、不覆盖。

全局工作区技能声明里的技能名盖住工作区里的同名链接：工作区 `sync` 不新建这些名字，并删掉工作区里已有的同名链接。`status` 用同一套规则判断是否对齐。

新链入的技能从下一次 Agent 对话开始可用。
