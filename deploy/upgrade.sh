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

echo ""
echo "============================================"
echo " 升级完成！"
echo "============================================"
echo ""
echo "  回滚: sudo cp $APP_DIR/cronix.old $APP_DIR/cronix && sudo systemctl restart $SERVICE_NAME"
echo ""
