# 纳米级计划：选项A（根目录清理与文档DDD化）

## 目标
1. 彻底清理根目录（`d:\claudeprj\codex`）下的零碎测试文件、日志、截图与多余构建产物。
2. 将根目录零散的测试/运维脚本统一归档至 `scripts/` 目录。
3. 对 `docs/` 目录进行符合 DDD（领域驱动设计）的物理层级划分。

## 原子化执行步骤 (Atomic Steps)

### Step 1: 物理删除临时垃圾文件
**动作**：执行 PowerShell/cmd 命令或 API，硬删除以下非版本控制内的无用产物：
- **构建二进制**：
  - `cronix.exe`
  - `cronix-test.exe`
  - `cronix_default.exe`
  - `cronix_linux`
  - `cronix-linux-amd64`
- **日志文件**：
  - `cronix.log`, `repro.log`, `server.log`, `stderr.log`, `stdout.log`, `test_out.log`, `test_wsl.log`
- **临时截图与杂项**：
  - `hash.txt`
  - `*.png` (dark-theme.png, dashboard-new.png, final-dark.png, initial-state.png, login-dark.png, login-fullscreen.png, login-page.png, tasks-page.png)

### Step 2: 归档零散脚本
**动作**：创建 `scripts/` 目录，并将以下文件移动至该目录下（如涉及git追踪，将使用 git mv）：
- `prod-test.sh`
- `repro.sh`
- `run_e2e.sh`
- `run_test.sh`
- `run_tests.sh`
- `stress-test.sh`
- `test_cronix.sh`
- `set-passwd.py`

### Step 3: 重建文档目录结构 (Docs DDD)
**动作**：在 `docs/` 目录下创建 DDD 骨架：
- 创建目录：`docs/domain/` (领域层)
- 创建目录：`docs/application/` (应用层)
- 创建目录：`docs/infrastructure/` (基础设施层)
- 创建目录：`docs/architecture/` (架构层)

### Step 4: 迁移现有文档
**动作**：将散落的现有文档迁移至对应领域：
- 移动 `docs/scheduling-rules.md` -> `docs/domain/scheduling-rules.md` （核心业务调度规则属于领域层）

## 测试与验收标准
- 根目录下执行 `dir` 或 `ls` 必须极简，没有 `.log`, `.exe`, `.png`, `.sh`, `.py` 文件（除可能的构建入口如 `main.go` 或 `Makefile` 外）。
- 目录 `docs/domain/`、`docs/application/`、`docs/infrastructure/`、`docs/architecture/` 创建成功。
- 代码必须正常通过 `go build`（证明无误删源码）。
