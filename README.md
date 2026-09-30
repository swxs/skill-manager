# skill-manager

把本机技能目录装进 `~/.agents/skill-library`，再按配置链到当前仓库的 `.agents/skills`，或链到用户级 `~/.agents/skills`。

这个仓库是公开的项目。技能正文在 `skills/skill-manager/`。装到 `~/.agents/skills/skill-manager` 之后，技能正文里的脚本路径才成立。下载对应平台的完整压缩包并解压到这个目录，或把仓库克隆到普通项目目录后再安装。

## 做什么

- 把本机目录或 Git URL 安装进技能库，并默认重写 `.skill-lock.json`
- 按 `.agents/skills.json` 把选中的技能链接进当前仓库
- 按 `~/.agents/skills.json` 把选中的技能链接进 `~/.agents/skills`
- 查看库、列出包内技能、从 `skills.json` 删掉条目

## 不做什么

- 不提供从技能库删除整个包的命令
- 不修改 `SKILL.md` 正文
- 不覆盖已有链接，也不覆盖真实目录
- 不给冲突的技能名加前缀

## 依赖与平台

运行时是静态的 Go 单文件。承诺支持 Windows、macOS 与 Linux，各有 amd64 与 arm64。持续集成在 Ubuntu 与 Windows 上跑 `go test`。

二进制没有签名。macOS 上启动脚本会在核对通过后去掉隔离属性。Windows 首次运行可能被 SmartScreen 拦截，需要选择仍要运行。

## 安装

目标目录都是 `~/.agents/skills/skill-manager`。

下载 [GitHub Release](https://github.com/swxs/skill-manager/releases) 里当前平台的 zip，解压到这个目录。压缩包根上已经有 `SKILL.md`、两条启动脚本，以及 `runtime/` 下的运行时，解压后即可使用。

克隆仓库时放到普通项目目录，不要放到 `~/.agents/skills/skill-manager`：

```bash
git clone https://github.com/swxs/skill-manager.git
```

换版本时，zip 安装是覆盖整个技能目录，压缩包里的 `runtime/version` 与 `SKILL.md` 一致。同一版本的发布文件被替换时，已记下的运行时不会更新。

## 命令

Windows：

```powershell
powershell -NoProfile -File "$env:USERPROFILE\.agents\skills\skill-manager\scripts\skill-manager.ps1" status --root "<根目录>"
```

macOS / Linux：

```bash
~/.agents/skills/skill-manager/scripts/skill-manager status --root "<根目录>"
```

| 命令 | 作用 |
| --- | --- |
| `init` | 初始化技能管理体系 |
| `list` | 查看当前技能库 |
| `search` | 从收集站查找技能 |
| `install` | 安装技能到技能库 |
| `package-name` | 从 Git 地址推导包名 |
| `upgrade` | 更新技能库中的技能 |
| `lock` | 锁定技能库信息 |
| `status` | 查看全局工作区, 工作区技能状态 |
| `add` | 添加技能声明 |
| `remove` | 移除技能声明 |
| `sync` | 按技能声明同步技能 |

`skill-manager <命令> --help` 打印该命令的用法和旗标。总览不写旗标。

不写 `--root` 时，`add`、`remove`、`sync` 和 `status` 使用当前目录。链接和查看要对当前每个工作区根目录各传一次 `--root`。

`install <本机目录>` 用目录名当包名。库里已有该包且不带 `#ref` 时不覆盖文件，并写出「跳过复制」。带了 `#ref` 则检出；失败则不写锁定。本机目录不接受 `--package`，也不记安装地址。

`install <Git地址>` 把技能文件夹放进包，包里不留 `.git`。地址在 `/tree/<分支>/` 之后还有路径时只复制那一个文件夹。只到分支时只取仓库 `skills/` 的直接子目录。本机有 git 时浅克隆并只检出这些目录；没有 git 时从 GitHub 取这些目录。省略 `--package` 时从地址推导包名。同名技能的安装地址相同则覆盖，不同则整次不改磁盘。`--unlock` 只跳过写锁定。Git 地址里的 `#ref` 不参与取文件。

`package-name <Git地址>` 只打印推导出的包名，不安装。

`add` 和 `remove` 只接受库内 **包名** 或 **包名:技能名**，可以多个。缺包、混合包拆开或其他不合法条目会使整次不改配置，并提示先 `install`。`remove` 不连带删除 `包名:技能名`；有一条在配置里对不上就不改文件。已有条目不重复添加。

配置写入后需再执行 `sync` / `sync --global` 才会创建链接。Agent 安装或启用技能时，应尽量代跑 `install` → `add` → `sync`，不要手 copy 到 `.agents/skills`。

## 库布局

库根是 `~/.agents/skill-library/`。库根只放包，不直接放技能。

| 类型 | 磁盘结构 | 安装来源 |
| --- | --- | --- |
| 单个技能（single） | `skill-library/<名>/<名>/SKILL.md` | 目录根上有 `SKILL.md`，子目录没有 |
| 纯技能包（pack） | `skill-library/<包名>/<技能名>/SKILL.md` | 根上没有 `SKILL.md`，子目录有 |
| 混合包（mixed） | 包根和子目录都有 `SKILL.md`，保持原目录，不套同名层 | 两边都有 |

安装时跳过 `__pycache__`、`.venv`、`node_modules`、`.DS_Store` 和源里的 `.skill-lock.json`。以 `.` 开头的目录不计入技能，但其余文件仍会复制。Git 安装不把 `.git` 留在包里。

`.skill-lock.json` 是磁盘索引。每个包有 `kind`，以及 `skills`：技能文件夹名到安装地址。本地路径安装的技能没有地址，不写入。`install`（除非 `--unlock`）、`upgrade` 与 `lock` 会更新索引。`remove` 不更新索引。

## 配置格式

仓库配置是 `<仓库>/.agents/skills.json`。用户级配置是 `~/.agents/skills.json`。两者都是字符串数组，可以带 `//` 行注释、块注释和尾随逗号。

```json
["mattpocock", "baoyu:baoyu-translate"]
```

- `包名`：纯包展开，每个技能一条链接，目标是 `.agents/skills/<技能名>`。只含一个技能的包同样展开成那一个技能。
- `包名:技能名`：只链这一个技能。
- 混合包只能写包名，整包一条链接到 `.agents/skills/<包名>`。

## 链接行为

- 优先创建符号链接。没有权限时，在 Windows 上用目录联接（junction）兜底。
- 同名技能报冲突并跳过，不覆盖，不加前缀。
- 目标路径已经是真实目录时跳过。`sync --global` 不会删除或覆盖真实目录。
- global 配置里的技能名会盖住仓库同名链接：仓库 `sync` 不新建这些名字，并删掉仓库里已有的同名链接。`status` 用同一套规则判断是否对齐。
- 仓库 `sync` 会维护该仓库 `.git/info/exclude` 里由本工具管理的一段，把新链入的 `.agents/skills/<名字>` 排除出版本库。`sync --global` 不改 exclude。
- 配置里去掉的链接，下次 `sync` 时删除。
- 新链入的技能从下一次 Agent 对话开始可用。

## 平台支持

| 能力 | Windows | macOS / Linux |
| --- | --- | --- |
| 运行时 | `runtime/skill-manager-windows-<架构>.exe` | `runtime/skill-manager-<系统>-<架构>` |
| 符号链接 | 优先尝试 | 优先尝试 |
| 目录联接 | 符号链接失败时兜底 | 不使用 |
| 持续集成 | `windows-latest` 上的 `go test` | `ubuntu-latest` 上的 `go test` |
| 换行 | 仓库文本与脚本写出的文件均为 LF | 同左 |

## 卸载与回滚

撤回某一次选择：

```bash
~/.agents/skills/skill-manager/scripts/skill-manager remove --root "<根目录>" "<包名或包名:技能名>"
~/.agents/skills/skill-manager/scripts/skill-manager sync --root "<根目录>"
```

`remove` 只从 `skills.json` 删掉完全相同的条目。再跑 `sync` 才会删掉仓库里指向它的链接。技能库里的目录保持不动。用户级配置用 `remove --global`，再跑 `sync --global`。

卸掉这个工具本身：删掉 `~/.agents/skills/skill-manager` 这条链接。技能库里的包、其它链接和 `~/.agents/skills.json` 都还在。要连技能库里的这份一起去掉，再删 `~/.agents/skill-library/skill-manager`。

## 开发

源码在 `src/`。需要 Go 1.22 或更新。

```powershell
go test -C src ./...
```

测试把 `HOME` 和 `USERPROFILE` 指到临时目录，并用 `--library` 隔离技能库，不会读写真实的 `~/.agents`。

## 致谢

`search` 使用的收录来自下列收集站，感谢它们公开检索接口：

- [SkillsMP](https://skillsmp.com/)。
- [ModelScope 魔搭技能中心](https://www.modelscope.cn/skills)。

## 许可

[MIT](LICENSE)
