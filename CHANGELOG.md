# 变更记录

## [0.2.0] - 未发布

- `add` 支持 Git URL（及本机 git 目录、可选 `#ref`）：先 fetch 进 skill-library，再写 skills.json；失败不改配置
- 新增 `upgrade <包名>`：对 origin 默认分支 `pull --ff-only` 并更新 lock
- 新增 `init`：将 global `~/.agents/skills` 迁入库、写 `~/.agents/skills.json`、lock 与 `link --global`
- `install` 尽量保留 `.git`；lock 扩展 `ref`、`revision`，有 origin 时 `source` 用远程 URL
- 库中缺包时 `add`/`status` 输出固定安装提示

## [0.1.0] - 未发布

- 首次公开化整理：补 README、MIT 许可证、测试与持续集成
- global 配置从 `~/.agents/skills/skill-manager/skills.json` 迁到 `~/.agents/skills.json`
- `install` 额外忽略 `__pycache__`、`.venv`、`node_modules`、`.DS_Store` 与 `.skill-lock.json`
- 仓库文本与脚本写出的 lock、配置和 git exclude 统一为 LF
