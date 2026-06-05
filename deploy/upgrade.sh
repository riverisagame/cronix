#!/bin/bash
# Cronix 升级脚本（仅下载新二进制 + 重启服务，不动配置）
# 用法: sudo bash upgrade.sh
# 或者: curl -fsSL https://raw.githubusercontent.com/riverisagame/cronix/master/deploy/upgrade.sh | sudo bash
set -e

APP_DIR="/opt/cronix"
SERVICE_NAME="cronix"
BIN_URL="https://github.com/riverisagame/cronix/releases/latest/download/cronix-linux-amd64"

red()   { echo -e "\033[31m$1\033[0m"; }
green() { echo -e "\033[32m$1\033[0m"; }

echo "============================================"
echo " Cronix 升级"
echo "============================================"

if [ "$(id -u)" != "0" ]; then
    red "[FAIL] 请用 sudo 运行"
    exit 1
fi

if [ ! -d "$APP_DIR" ]; then
    red "[FAIL] $APP_DIR 不存在，请先运行 install.sh 安装"
    exit 1
fi

# 1. 下载新二进制（保留旧文件作为备份）
echo "[INFO] 下载新版本..."
curl -fSL --progress-bar -o "$APP_DIR/cronix.new" "$BIN_URL" || {
    red "[FAIL] 下载失败: $BIN_URL"
    exit 1
}

# 2. 替换
if [ -f "$APP_DIR/cronix" ]; then
    cp "$APP_DIR/cronix" "$APP_DIR/cronix.old"
    green "[OK] 已备份旧二进制 -> cronix.old"
fi
mv "$APP_DIR/cronix.new" "$APP_DIR/cronix"
chmod 755 "$APP_DIR/cronix"
green "[OK] 二进制已更新"

# 3. 重启服务
if systemctl is-active --quiet "$SERVICE_NAME" 2>/dev/null; then
    systemctl restart "$SERVICE_NAME"
    green "[OK] 服务已重启"
else
    echo "[INFO] 服务未运行，正在启动..."
    systemctl start "$SERVICE_NAME"
    green "[OK] 服务已启动"
fi

# 4. 检测并安全合并新增的配置项
CONFIG_FILE="$APP_DIR/config.yaml"
if [ -f "$CONFIG_FILE" ]; then
    # 检查是否已有 Nice 优先级及日志限额等新字段，没有则追加
    if ! grep -q "nice_value" "$CONFIG_FILE"; then
        echo "[INFO] 检测到旧版本配置文件缺失安全限额参数，正在自动注入 Linux 资源限制配置..."
        cp "$CONFIG_FILE" "${CONFIG_FILE}.bak"
        sed -i '/pool_size:/a\    cpu_quota: 50\n    enable_cgroups: false\n    nice_value: 19\n    ionice_class: 3' "$CONFIG_FILE"
        green "[OK] 已自动将 nice_value/cpu_quota 资源隔离配置合并至 config.yaml"
    fi
    if ! grep -q "max_logs_per_task" "$CONFIG_FILE"; then
        echo "[INFO] 检测到旧版本配置文件缺失日志配额参数，正在自动注入日志熔断与配额配置..."
        sed -i '/max_records:/a\    max_logs_per_task: 1000\n    task_log_dir: ./data/logs\n    file_max_size_mb: 50\n    file_max_backups: 5\n    file_max_age_days: 30\n    min_free_disk_space_percent: 10\n    min_free_disk_space_gb: 10' "$CONFIG_FILE"
        green "[OK] 已自动将 max_logs_per_task 磁盘/数据库限额配置合并至 config.yaml"
    fi
    # 触发服务重启以加载合并后的配置
    systemctl restart "$SERVICE_NAME"
fi

echo ""
echo "============================================"
echo " 升级完成！"
echo "============================================"
echo ""
echo "  回滚: sudo cp $APP_DIR/cronix.old $APP_DIR/cronix && sudo systemctl restart $SERVICE_NAME"
echo ""
