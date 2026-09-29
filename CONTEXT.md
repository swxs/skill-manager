# skill-manager

管理本机技能库，并按工作区或全局工作区的选择链出技能。

## Language

**技能库**:
本机集中存放技能包的目录，即 `~/.agents/skill-library`。
_Avoid_: 技能目录

**工作区**:
项目里的 `.agents/` 目录。
_Avoid_: 仓库, 项目, root

**全局工作区**:
用户级的 `.agents/` 目录，即 `~/.agents/`。
_Avoid_: global, 用户级目录, 家目录

**技能声明**:
工作区或全局工作区里的 `skills.json`。
_Avoid_: 配置, 选择

**运行时**:
用户机器上实际执行 skill-manager 命令的程序。一个版本在每个平台各有一份，是单文件可执行文件。
_Avoid_: 安装包, 脚本

**技能目录**:
放在 `~/.agents/skills/skill-manager` 的这份技能。来自某一版 Release，Agent 从这里读取技能正文。目录里不必有 git 仓库。
_Avoid_: 技能克隆, 安装

**版本**:
写在技能目录里 `SKILL.md` 的 frontmatter 中，字符串与 Release tag 相同，例如 `v0.2.0`。运行时就是这一版 Release 上的那一份。
_Avoid_: latest, 最新版, 当前 tag

**校验和**:
随版本一起发布的清单，用来核对运行时文件是不是这一版里的那一份。
_Avoid_: 签名

**启动脚本**:
技能目录里用来取回并执行运行时的薄脚本。Windows 上一份，macOS 与 Linux 上另一份。它不是运行时。
_Avoid_: 运行时, 安装包

**包**:
技能库里的一个目录，直接子目录是技能文件夹。
_Avoid_: 技能包, 技能包名

**技能文件夹**:
带有一份 `SKILL.md` 的目录。在包里它是直接子目录。
_Avoid_: skill 目录

**收集站**:
可替换的外部目录，用来把检索词变成可安装的 Git 地址。可以有多个。
_Avoid_: 技能收集网站, registry, marketplace, 来源

**收录项**:
Git 地址非空的标准收录。安装时按该地址取出技能文件夹放进包，不把整个仓库当作包。
_Avoid_: 命中, 搜索结果, 条目

**非 Git 条目**:
Git 地址为空的标准收录。`search` 不把它装进技能库。
_Avoid_: well-known, 非 git 技能

**标准收录**:
收集站结果映射之后的一条。安装前有稳定身份、所属收集站、显示名，以及 Git 地址。Git 地址可以为空，也可以带仓库内路径。
_Avoid_: DTO, 统一对象, schema
