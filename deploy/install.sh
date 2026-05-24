#!/bin/bash
# Cronix 一键安装脚本（适用于 Debian/Ubuntu amd64）
# 用法: sudo bash install.sh
set -e

APP_DIR="/opt/cronix"
SERVICE_USER="cronix"
SERVICE_NAME="cronix"
BIN_URL="https://github.com/riverisagame/cronix/releases/latest/download/cronix-linux-amd64"
PORT=8080

red()   { echo -e "\033[31m$1\033[0m"; }
green() { echo -e "\033[32m$1\033[0m"; }
warn()  { echo -e "\033[33m$1\033[0m"; }

echo "============================================"
echo " Cronix 生产环境安装"
echo "============================================"

# ============================================================
# 0. 前置检查
# ============================================================

# 0.1 必须是 root 或 sudo 运行
if [ "$(id -u)" != "0" ]; then
    red "[FAIL] 请用 sudo 运行: sudo bash install.sh"
    exit 1
fi

# 0.2 检查架构（只支持 amd64）
ARCH=$(uname -m)
if [ "$ARCH" != "x86_64" ]; then
    warn "[WARN] 当前架构是 $ARCH，预编译二进制是 amd64 的，可能不兼容"
fi

# 0.3 检查磁盘空间（至少 100MB 可用）
AVAIL=$(df -m "$(dirname "$APP_DIR")" | tail -1 | awk '{print $4}')
if [ "$AVAIL" -lt 100 ]; then
    red "[FAIL] 磁盘空间不足（可用 ${AVAIL}MB，需要至少 100MB）"
    exit 1
fi
green "[OK] 磁盘空间充足（${AVAIL}MB）"

# 0.4 检查端口是否被占用
if ss -tlnp | grep -q ":$PORT "; then
    warn "[WARN] 端口 $PORT 已被占用："
    ss -tlnp | grep ":$PORT "
    echo "  如果这是旧的 Cronix 实例，安装完成后 systemctl restart cronix 即可"
fi

# ============================================================
# 1. 创建用户
# ============================================================
if id "$SERVICE_USER" &>/dev/null; then
    # 用户已存在：验证是否为系统用户
    USER_SHELL=$(getent passwd "$SERVICE_USER" | cut -d: -f7)
    if [ "$USER_SHELL" != "/sbin/nologin" ] && [ "$USER_SHELL" != "/usr/sbin/nologin" ]; then
        warn "[WARN] $SERVICE_USER 已存在但 shell=$USER_SHELL（建议设为 nologin）"
    fi
    green "[OK] 用户 $SERVICE_USER 已存在"
else
    useradd -r -s /sbin/nologin -d "$APP_DIR" -M "$SERVICE_USER"
    green "[OK] 已创建系统用户 $SERVICE_USER"
fi

# ============================================================
# 2. 停止旧服务（如果正在运行）
# ============================================================
if systemctl is-active --quiet "$SERVICE_NAME" 2>/dev/null; then
    echo "[INFO] 停止旧服务..."
    systemctl stop "$SERVICE_NAME"
    green "[OK] 旧服务已停止"
fi

# ============================================================
# 3. 创建目录
# ============================================================
mkdir -p "$APP_DIR"/{data,deploy}
green "[OK] 目录结构就绪: $APP_DIR/{data,deploy}"

# ============================================================
# 4. 部署二进制文件
# ============================================================
if [ -f "./cronix-linux" ]; then
    cp ./cronix-linux "$APP_DIR/cronix"
    green "[OK] 已复制本地二进制"
else
    echo "[INFO] 从 GitHub Release 下载..."
    curl -fsSL -o "$APP_DIR/cronix" "$BIN_URL" || {
        red "[FAIL] 下载失败，请检查网络或手动下载: $BIN_URL"
        exit 1
    }
    green "[OK] 下载完成"
fi

# ============================================================
# 5. 部署/保留配置文件
# ============================================================
if [ -f "$APP_DIR/config.yaml" ]; then
    # 备份旧配置
    cp "$APP_DIR/config.yaml" "$APP_DIR/config.yaml.bak.$(date +%Y%m%d%H%M%S)"
    green "[OK] 已备份旧配置到 .bak.$(date +%Y%m%d%H%M%S)"
else
    if [ -f "./config.yaml" ]; then
        cp ./config.yaml "$APP_DIR/config.yaml"
        green "[OK] 已创建配置文件"
    else
        warn "[WARN] 未找到 config.yaml，首次启动前请手动创建"
    fi
fi

# ============================================================
# 6. 设置权限和属主
# ============================================================
chown -R "$SERVICE_USER:$SERVICE_USER" "$APP_DIR"
chmod 750  "$APP_DIR/cronix"       # 属主 rwx，同组 rx，其他人无
chmod 600  "$APP_DIR/config.yaml"  # 含密码哈希，禁止他人读取
chmod 755  "$APP_DIR" "$APP_DIR/data"
green "[OK] 权限已设置"

# ============================================================
# 7. 以 cronix 用户身份验证
# ============================================================
if su -s /bin/sh -c "test -x $APP_DIR/cronix" "$SERVICE_USER"; then
    green "[OK] cronix 用户可执行二进制"
else
    red "[FAIL] cronix 用户无法执行 $APP_DIR/cronix"
    echo "  可能原因: 文件系统以 noexec 挂载"
    MOUNT_POINT=$(df "$APP_DIR" | tail -1 | awk '{print $6}')
    MOUNT_OPTS=$(mount | grep "on $MOUNT_POINT " | head -1)
    echo "  挂载信息: $MOUNT_OPTS"
    exit 1
fi

# ============================================================
# 8. 安装 systemd 服务（覆盖安装）
# ============================================================
if [ -f "./deploy/cronix.service" ]; then
    cp ./deploy/cronix.service /etc/systemd/system/cronix.service
elif [ -f "/opt/cronix/deploy/cronix.service" ]; then
    cp /opt/cronix/deploy/cronix.service /etc/systemd/system/cronix.service
else
    red "[FAIL] 找不到 cronix.service 文件"
    exit 1
fi
systemctl daemon-reload
green "[OK] systemd 服务已安装"

# ============================================================
# 9. 完成提示
# ============================================================
echo ""
echo "============================================"
echo " 安装完成！"
echo "============================================"
echo ""
echo "1. 设置管理员密码:"
echo "   sudo -u $SERVICE_USER $APP_DIR/cronix passwd -c $APP_DIR/config.yaml"
echo ""
echo "2. 启动服务:"
echo "   sudo systemctl start $SERVICE_NAME"
echo "   sudo systemctl enable $SERVICE_NAME   # 开机自启"
echo ""
echo "3. 日常管理:"
echo "   sudo systemctl status $SERVICE_NAME   # 查看状态"
echo "   sudo journalctl -u $SERVICE_NAME -f   # 实时日志"
echo ""
echo "4. 访问:  http://$(hostname -I 2>/dev/null | awk '{print $1}' || echo '服务器IP'):$PORT"
echo ""
echo "5. (推荐) 宝塔/Nginx 反向代理 + TLS 配置:"
echo "   参考 https://github.com/riverisagame/cronix/blob/master/deploy/nginx-cronix.conf"
echo "============================================"
