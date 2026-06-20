#!/bin/bash
# 性能与稳定优化集成验证自动化脚本
set -e

CONFIG_PATH="/home/dan/cronix-test/config.yaml"
BACKUP_PATH="/home/dan/cronix-test/config.yaml.bak"

echo "=== [1/5] 备份配置文件 ==="
cp "$CONFIG_PATH" "$BACKUP_PATH"

cleanup() {
    echo "=== 停止可能残存的服务进程 ==="
    PID=$(pgrep -f "cronix-linux serve" || true)
    if [ -n "$PID" ]; then
        echo "杀掉服务进程: $PID"
        kill -9 $PID || true
    fi
    echo "=== 恢复原始配置 ==="
    if [ -f "$BACKUP_PATH" ]; then
        mv "$BACKUP_PATH" "$CONFIG_PATH"
    fi
}
trap cleanup EXIT

# ----------------- 运行 test-suite.sh -----------------
echo "=== [2/5] 准备 test-suite.sh (密码设为 'x') ==="
python3 -c '
import yaml, bcrypt
cfg = yaml.safe_load(open("/home/dan/cronix-test/config.yaml"))
cfg["auth"]["password"] = bcrypt.hashpw(b"x", bcrypt.gensalt()).decode()
yaml.dump(cfg, open("/home/dan/cronix-test/config.yaml", "w"))
'

echo "启动 cronix-linux 服务..."
cd /home/dan/cronix-test
sqlite3 data/cronix.db "INSERT OR IGNORE INTO tasks (id, name, cron_expr, task_type, command, enabled, created_at, updated_at) VALUES (1, 'default-task', '0 0 1 * * *', 'shell', 'echo 1', 1, '2026-05-27 00:00:00', '2026-05-27 00:00:00');"
./cronix-linux serve -c config.yaml > server_test_suite.log 2>&1 &
sleep 2

echo "执行 test-suite.sh 并将输出保存到 test_suite.log..."
set +e
./test-suite.sh > test_suite.log 2>&1
TEST_EXIT_CODE=$?
set -e
echo "test-suite.sh 退出码: $TEST_EXIT_CODE"
if [ $TEST_EXIT_CODE -ne 0 ]; then
    echo "=== test-suite.log 内容 ==="
    cat test_suite.log
    exit $TEST_EXIT_CODE
fi

# ----------------- 运行 prod-test.sh -----------------
echo "=== [3/5] 准备 prod-test.sh (密码设为 'password123') ==="
PID=$(pgrep -f "cronix-linux serve" || true)
if [ -n "$PID" ]; then kill -9 $PID || true; fi

python3 -c '
import yaml, bcrypt
cfg = yaml.safe_load(open("/home/dan/cronix-test/config.yaml"))
cfg["auth"]["password"] = bcrypt.hashpw(b"password123", bcrypt.gensalt()).decode()
yaml.dump(cfg, open("/home/dan/cronix-test/config.yaml", "w"))
'

echo "重新启动服务..."
./cronix-linux serve -c config.yaml > server_prod_test.log 2>&1 &
sleep 2

echo "执行 prod-test.sh..."
./prod-test.sh || exit 1

# ----------------- 运行 stress-test.sh -----------------
echo "=== [4/5] 执行 stress-test.sh ==="
# 使用密码 x 进行 stress-test
PID=$(pgrep -f "cronix-linux serve" || true)
if [ -n "$PID" ]; then kill -9 $PID || true; fi

python3 -c '
import yaml, bcrypt
cfg = yaml.safe_load(open("/home/dan/cronix-test/config.yaml"))
cfg["auth"]["password"] = bcrypt.hashpw(b"x", bcrypt.gensalt()).decode()
yaml.dump(cfg, open("/home/dan/cronix-test/config.yaml", "w"))
'

echo "重新启动服务..."
./cronix-linux serve -c config.yaml > server_stress_test.log 2>&1 &
sleep 2

echo "执行 stress-test.sh..."
./stress-test.sh || exit 1

echo "=== [5/5] 所有集成/生产和压力测试全部通过！ ==="
