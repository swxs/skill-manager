---
name: submit-branch
description: >-
  Develops skill-manager features on the develop branch under a version
  the user names, or the current changelog version when none is given,
  then builds that version into the skill library. Use when the user
  asks to 提交功能, 指定版本, 在 develop 上开发, or 提交新分支 in this repo.
---

# 在 develop 上提交

功能都在 `develop` 上开发并提交，并把这一版构建进技能库，覆盖原来的包。不新建分支。

## 步骤

### 1. 看清现状

在仓库根目录看 `git status`、`git diff`、`git log -8 --oneline`，以及 `CHANGELOG.md` 文首版本和 `skills/skill-manager/SKILL.md` 的 `version`。

完成：能指出当前分支、待提交的文件，以及变更记录里已有的最高版本。

### 2. 留在 develop

切到 `develop`，在这条分支上继续。不新建分支。

完成：`HEAD` 是 `develop`。

### 3. 确定版本

先用用户给出的版本号。用户没有给出时，用变更记录里已有的最高版本，不增加补丁号。

该版本在变更记录里还没有一节时，文首新开 `## [X.Y.Z] - 未发布`，并把技能正文 frontmatter 的 `version` 写成 `vX.Y.Z`。已经有这一节时，只追加条目，不改技能正文的 `version`。

完成：变更记录使用的版本与技能正文的 `version` 一致。

### 4. 测试

在仓库根目录跑 `go test -C src ./tests`。

完成：退出码是 0。

### 5. 构建并覆盖技能库里的这一份

为当前平台构建运行时，覆盖包 `~/.agents/skill-library/skill-manager` 里的技能文件夹 `skill-manager`。清空该文件夹里的文件，留下这个目录，再写入：

- 技能正文 `SKILL.md`
- 两条启动脚本 `scripts/skill-manager` 与 `scripts/skill-manager.ps1`
- `bin/` 里的运行时。Windows 上是 `skill-manager.exe`，macOS 与 Linux 上是 `skill-manager`
- `bin/version`，内容是技能正文 frontmatter 的 `version`，末尾一个换行

在 `src` 下构建，`CGO_ENABLED=0`。Windows 把产物写到该技能文件夹的 `bin/skill-manager.exe`，其它平台写到 `bin/skill-manager`。

完成：技能文件夹里 `SKILL.md` 的 version 与 `bin/version` 相同，`bin` 里是这次构建的运行时。

### 6. 提交

提交说明一行，对齐 `git log`：`vX.Y.Z：为什么。`

只暂存这次改动的文件。`.agents/` 是本机技能，不纳入提交。

完成：新提交在 `develop` 上，说明以该版本开头。

### 7. 推送

用户要求推送时再 `git push`。不新建分支。用户另外要求 Pull Request 时再打开。

完成：没要求推送时，提交留在本地 `develop`。要求推送时，`origin/develop` 含这次提交。
