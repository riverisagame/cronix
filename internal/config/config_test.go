// ============================================================
// internal/config/config_test.go - 配置加载的测试文件
//
// Go 的测试文件必须以 _test.go 结尾
// Go 会自动发现并运行这些测试
// 文件名里的 _test 就是告诉 Go："这是测试代码"
// ============================================================
package config

import (
    // "os" 用于操作文件系统，这里用来写临时配置文件
    "os"
    // "path/filepath" 用于处理文件路径
    "path/filepath"
    // "testing" 是 Go 的测试框架
    // 提供 t.Errorf、t.Fatal 等测试断言方法
    "testing"
    // "time" 用于处理时间相关的类型
    "time"
)

// TestLoadConfig 测试：验证从 YAML 文件加载配置的功能
//
// 函数名规则：Test + 大写字母开头 = 公开测试（会被 Go 自动运行）
// 参数 t *testing.T：测试的"控制器"，用来报告测试结果
func TestLoadConfig(t *testing.T) {
    // ======== 第1步：准备测试数据 ========

    // 创建临时目录，用来存放测试用的配置文件
    // t.TempDir() 会自动创建一个临时文件夹，测试结束后自动删除
    tmpDir := t.TempDir()

    // filepath.Join 把多个路径片段拼成完整路径
    // 比如 Windows 下：C:\Temp\xxx + config.yaml = C:\Temp\xxx\config.yaml
    configPath := filepath.Join(tmpDir, "config.yaml")

    // 这是测试用的 YAML 内容
    // YAML 是一种配置文件格式，用缩进表示层级关系
    // 类似 JSON，但更易读（不需要花括号和引号）
    yamlContent := `
server:
  port: 9090
  graceful_timeout: 15s
  webui:
    enabled: false
  api:
    enabled: true

auth:
  username: testuser
  password: ""

database:
  path: ./testdata/cronix.db
  wal_mode: true
  busy_timeout: 3000

executor:
  pool_size: 16
  output_truncate_kb: 32

log:
  level: debug
  file: ./testdata/cronix.log
  max_size_mb: 50
  max_backups: 3
  max_age_days: 7
  retention_days: 7
  max_records: 5000

notify:
  retry: 2
  retry_interval: 3s

circuit_breaker:
  failure_threshold: 3
  cooldown_seconds: 30
`

    // os.WriteFile 把字符串写入文件
    // 参数1：文件路径
    // 参数2：字符串转成字节数组（计算机只能处理字节）
    // 参数3：0644 是文件权限（所有者可读写，其他人只读）
    if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
        // t.Fatalf 表示"致命错误，测试无法继续"
        // 就像程序 crash 了，后续代码不再执行
        t.Fatalf("写入测试配置文件失败: %v", err)
    }

    // ======== 第2步：调用被测试的函数 ========

    // Load 是我们即将实现的函数
    // 它读取 YAML 文件并返回一个配置对象
    cfg, err := Load(configPath)

    // ======== 第3步：验证结果（断言）========

    // 首先检查有没有返回错误
    if err != nil {
        t.Fatalf("加载配置失败: %v", err)
    }

    // 然后逐个字段检查值是否正确
    // 如果不对，用 t.Errorf 报告（不会停止测试，继续检查其他字段）

    if cfg.Server.Port != 9090 {
        t.Errorf("期望端口 9090, 实际 %d", cfg.Server.Port)
    }

    if cfg.Server.GracefulTimeout != 15*time.Second {
        t.Errorf("期望超时 15s, 实际 %v", cfg.Server.GracefulTimeout)
    }

    if cfg.Server.WebUI.Enabled != false {
        t.Errorf("期望 webui.enabled = false, 实际 true")
    }

    if cfg.Auth.Username != "testuser" {
        t.Errorf("期望用户名 testuser, 实际 %s", cfg.Auth.Username)
    }

    if cfg.Database.BusyTimeout != 3000 {
        t.Errorf("期望 busy_timeout 3000, 实际 %d", cfg.Database.BusyTimeout)
    }

    if cfg.Executor.PoolSize != 16 {
        t.Errorf("期望 pool_size 16, 实际 %d", cfg.Executor.PoolSize)
    }

    if cfg.CircuitBreaker.FailureThreshold != 3 {
        t.Errorf("期望 failure_threshold 3, 实际 %d", cfg.CircuitBreaker.FailureThreshold)
    }

    // t.Logf 记录一条信息（只在测试失败或 -v 模式下显示）
    t.Logf("配置加载成功: %+v", cfg)
}
