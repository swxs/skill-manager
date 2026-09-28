# skill-manager

管理本机技能库，并按配置把技能链到仓库或用户级技能目录。

## Language

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
