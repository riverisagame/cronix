#!/bin/bash
# Cronix 一键安装脚本（适用于 Debian/Ubuntu）
# 用法: sudo bash install.sh

set -e
APP_DIR="/opt/cronix"
SERVICE_USER="cronix"
BIN_URL="https://github.com/riverisagame/cronix/releases/latest/download/cronix-linux-amd64"

echo "=== Cronix 生产环境安装 ==="

# 1. 创建专用用户（不分配登录 shell，安全隔离）
if ! id "$SERVICE_USER" &>/dev/null; then
    useradd -r -s /sbin/nologin -d "$APP_DIR" -M "$SERVICE_USER"
    echo "[OK] 创建用户 $SERVICE_USER"
fi

# 2. 创建目录结构
mkdir -p "$APP_DIR"/{data,deploy}
echo "[OK] 目录结构创建完成"

# 3. 下载/复制二进制文件
if [ -f "./cronix-linux" ]; then
    cp ./cronix-linux "$APP_DIR/"
else
    curl -L -o "$APP_DIR/cronix" "$BIN_URL"
fi
chmod +x "$APP_DIR/cronix"
echo "[OK] 二进制文件就绪"

# 4. 复制配置文件（如果不存在）
if [ ! -f "$APP_DIR/config.yaml" ]; then
    cp ./config.yaml "$APP_DIR/config.yaml"
    echo "[OK] 已创建默认配置文件"
else
    echo "[SKIP] 配置文件已存在，跳过"
fi

# 5. 设置文件权限
chown -R "$SERVICE_USER:$SERVICE_USER" "$APP_DIR"
chmod 600 "$APP_DIR/config.yaml"  # 配置文件含密码，仅 owner 可读写
echo "[OK] 文件权限已设置"

# 6. 安装 systemd 服务
cp ./deploy/cronix.service /etc/systemd/system/
systemctl daemon-reload
echo "[OK] systemd 服务已安装"

# 7. 提示下一步
echo ""
echo "============================================"
echo " 安装完成！接下来的步骤："
echo "============================================"
echo ""
echo "1. 设置管理员密码:"
echo "   sudo -u $SERVICE_USER $APP_DIR/cronix passwd -c $APP_DIR/config.yaml"
echo ""
echo "2. 启动服务:"
echo "   sudo systemctl start cronix"
echo "   sudo systemctl enable cronix  # 开机自启"
echo ""
echo "3. 查看状态:"
echo "   sudo systemctl status cronix"
echo "   sudo journalctl -u cronix -f  # 实时日志"
echo ""
echo "4. Web 界面:"
echo "   http://<服务器IP>:8080"
echo ""
echo "5. (推荐) 配置 Nginx 反向代理 + TLS:"
echo "   参考 deploy/nginx-cronix.conf"
echo "============================================"
