# skill-manager

管理本机技能库，并按工作区或全局工作区的技能声明链出技能。

## 先读

- **术语**：动手前读 [`CONTEXT.md`](CONTEXT.md)。对用户说明、提交说明和代码注释都用其中的词，不用 Avoid 栏里的说法。
- **临时文件**：往 `.scratch/` 写之前先读 [`.scratch/README.md`](.scratch/README.md)。只往当前会话自己的目录写，按里面的子目录约定归档。

## 项目地图

```
/
├── skills/skill-manager/    技能正文与启动脚本。发布时摊平到技能目录根
│   ├── SKILL.md
│   ├── scripts/
│   └── runtime/             本地编出来的运行时，不进版本库
├── CONTEXT.md               术语
├── README.md                给人看的用法
├── src/
│   ├── cmd/skill-manager/   入口，交给 internal/cli
│   ├── internal/cli/        命令分发与技能声明读写
│   ├── internal/library/    技能库布局、安装复制、.skill-lock.json
│   ├── internal/link/       按技能声明链出，并维护仓库 exclude
│   ├── internal/gitpack/    Git 包的 fetch / checkout
│   ├── internal/symlink/    符号链接；Windows 上失败时用目录联接
│   ├── internal/jsonc/      带注释和尾随逗号的 skills.json
│   └── tests/               集成测试。隔离 HOME，并用 --library
└── .github/workflows/       CI（go test）与按 tag 打开发布
```

需要 Go 1.22 或更新。在仓库根目录跑 `go test -C src ./...`。测试把 `HOME` 和 `USERPROFILE` 指到临时目录，不会读写真实的 `~/.agents`。

## 主要功能

启动脚本核对版本和校验和后执行运行时。命令都在 `src/internal/cli`。

| 命令 | 作用 |
| --- | --- |
| `init` | 初始化技能管理体系 |
| `list` | 查看当前技能库 |
| `install` | 安装技能到技能库 |
| `upgrade` | 更新技能库中的技能 |
| `lock` | 锁定技能库信息 |
| `status` | 查看全局工作区、工作区技能状态 |
| `add` | 添加技能声明 |
| `remove` | 移除技能声明 |
| `sync` | 按技能声明同步技能 |

安装或启用技能时按 `install` → `add` → `sync` 代跑。全局工作区用 `add --global` 和 `sync --global`。`add` 和 `remove` 只改技能声明，再 `sync` 链接才会变。没有从技能库删除整个包的命令。

同名技能报冲突并跳过，不覆盖、不加前缀。目标已是真实目录则跳过，`sync --global` 不删除、不覆盖。退出码不是 0 时停住，不要自行补链或复制目录。
