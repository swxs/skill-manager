# 变更记录

## [0.5.4] - 未发布

- `remove` 在技能库里没有该包时仍从技能声明删掉对应条目
- `lock` 按包名、类型对齐，每个技能各占一行

## [0.5.3] - 未发布

- 本机有 git 时，GitHub 安装浅克隆并只检出需要的目录；没有 git 时从 GitHub 接口取这些目录

## [0.5.2] - 未发布

- Git 安装只把技能文件夹放进包，包里不留 `.git`。地址在 `/tree/<分支>/` 之后还有路径时只复制那一个；只到分支时只取 `skills/` 的直接子目录
- 新增 `package-name`。`install` 可写 `--package`；省略时按仓库名推导包名
- 锁定改为每个技能一条安装地址，不再写 `ref` 与 `revision`。本地路径安装不记地址
- `upgrade` 按安装地址重取并覆盖；没有地址则跳过
- 不再接受 `search --install`
- 总览 `--help` 的命令说明按列对齐
- 技能正文和启动脚本放在 `skills/skill-manager/`。发布 zip 的根目录仍是 `SKILL.md`、`scripts/` 和 `runtime/`
- `init` 不再按名字跳过 `skill-manager`。带 `SKILL.md` 的真实目录和其他技能一样收进技能库

## [0.5.1] - 未发布

- `search --install` 核对最近 5 次列出的收录，不再用稳定身份回收集站检索

## [0.5.0] - 未发布

- `search` 先问 SkillsMP，没有收录再问 ModelScope；`search --install` 把仓库根交给 `install`

## [0.4.2] - 未发布

- `status` 按全局技能声明、全局工作区、工作区技能声明、工作区打印；链接行写成 `包名:技能名`
- 只有全局和工作区都声明的技能才显示「global 已覆盖」；真实目录显示为「跳过 名字（真实目录）」
- `list` 按包名、类型、技能数对齐成列

## [0.4.1] - 未发布

- 启动脚本比较 `runtime/version` 与 `SKILL.md` 的 `version`，不一致时下载该版本，校验通过后才替换

## [0.4.0] - 未发布

- 总览帮助每条命令一句，子命令 `--help` 写用法；
- `init` 把全局工作区里后放入的真实目录换成指向技能库的链接
- `status` 先打印全局配置，再全局工作区，再工作区
- `install` 接受本机目录或 Git URL（可选 `#ref`），默认重写 lock，`--unlock` 跳过
- `add` 不再 fetch，只把包名或 `包名:技能名` 写入技能声明
- `remove` 改为从技能声明删除完全相同的条目，不再删除库内包
- `link` 改名为 `sync`，不再保留旧命令名

## [0.3.0] - 未发布

- 运行时改为 Go，安装不再需要 Python / uv

## [0.2.0] - 未发布

- 新增 `init`：将 global `~/.agents/skills` 迁入库、写 `~/.agents/skills.json`、lock 与 `link --global`
- 新增 `upgrade <包名>`：对 origin 默认分支 `pull --ff-only` 并更新 lock
- `install` 尽量保留 `.git`；lock 扩展 `ref`、`revision`，有 origin 时 `source` 用远程 URL
- `add` 支持 Git URL（及本机 git 目录、可选 `#ref`）：先 fetch 进 skill-library，再写 skills.json；失败不改配置
- 库中缺包时 `add`/`status` 输出固定安装提示

## [0.1.0] - 未发布

- 首次公开化整理：补 README、MIT 许可证、测试与持续集成
- global 配置从 `~/.agents/skills/skill-manager/skills.json` 迁到 `~/.agents/skills.json`
- `install` 额外忽略 `__pycache__`、`.venv`、`node_modules`、`.DS_Store` 与 `.skill-lock.json`
- 仓库文本与脚本写出的 lock、配置和 git exclude 统一为 LF
