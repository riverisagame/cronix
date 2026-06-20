# 文档结构DDD化与根目录清理方案 (SCAN阶段)

## 1. 现状分析

### 根目录临时文件泛滥
当前项目根目录（`d:\claudeprj\codex`）存在大量测试遗留文件和临时构建产物，严重影响代码阅读：
- **可执行文件/二进制**: `cronix.exe`, `cronix-test.exe`, `cronix_default.exe`, `cronix_linux`, `cronix-linux-amd64`
- **日志文件**: `cronix.log`, `repro.log`, `server.log`, `stderr.log`, `stdout.log`, `test_out.log`, `test_wsl.log`
- **临时产物**: `hash.txt`, 大量 `.png` 截图（`dark-theme.png`, `dashboard-new.png` 等）
- **测试脚本**: `prod-test.sh`, `repro.sh`, `run_e2e.sh`, `run_test.sh`, `run_tests.sh`, `stress-test.sh`, `test_cronix.sh`, `set-passwd.py`

### 文档结构缺乏领域划分
当前的 `docs/` 目录只有 `sps`（用于编译器流程记录）和 `superpowers` 等杂项，缺乏业务全景图。用户希望“彻底DDD且清晰”。

## 2. 方案选型 (Trade-off Analysis)

### A. 根目录清理策略
- **策略一：彻底删除**（推荐）。所有测试生成的 `.log`, `.exe`, `.png` 及无关产物全部删除，保证根目录极简。
- **策略二：归档到测试目录**。将所有 `*.sh` 脚本移动到 `scripts/` 或 `test/scripts/` 目录中统一管理。

### B. 文档DDD重构策略
采用严格的领域驱动设计（DDD）范式重组文档：
1. **`docs/domain/` (领域层)**：存放核心业务概念、实体、值对象、聚合根的定义。
2. **`docs/application/` (应用层)**：存放用例（Use Cases）、工作流描述。
3. **`docs/infrastructure/` (基础设施层)**：数据库设计、API规约、外部系统集成文档。
4. **`docs/architecture/` (架构层)**：系统整体架构图、ADR（架构决策记录，可复用现有的 `sps/decisions`）。
5. **保持 `docs/sps/`** 作为本系统的自动化工程规范目录。

## 3. 对现有功能的影响评估
- **无影响**：删除日志、二进制和图片文件不会影响代码库的核心运行逻辑。
- **需验证**：移动或删除 `*.sh` 测试脚本可能影响开发者的本地测试习惯，需要与用户确认是否需要保留。
