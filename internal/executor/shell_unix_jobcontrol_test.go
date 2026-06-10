//go:build !windows
// +build !windows

package executor

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"cronix/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecutor_JobControl_Escape_Red(t *testing.T) {
	// 确保 CGroups 关闭，使用基础 Shell 回退
	config.AppConfig = &config.Config{}
	config.AppConfig.Executor.EnableCGroups = false

	// 该脚本启动一个常驻后台的孙子进程
	// 并输出其 PID 到标准输出以便我们在测试中读取
	command := `
bash -c 'echo "GRANDCHILD_PID=$$"; while true; do sleep 1; done' &
sleep 5
`

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	res := ExecuteShell(ctx, command, "", 5, "", 0)

	// 解析输出找到孙子进程的 PID
	output := res.Output
	var grandchildPID string
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "GRANDCHILD_PID=") {
			grandchildPID = strings.TrimPrefix(line, "GRANDCHILD_PID=")
			break
		}
	}

	require.NotEmpty(t, grandchildPID, "Could not find grandchild PID in output: %s", output)

	// 测试孙子进程是否存活
	// kill -0 <pid> 可以检查进程是否存在
	err := exec.Command("kill", "-0", grandchildPID).Run()
	
	// 如果 err == nil，说明进程存活（逃逸成功！）
	// 在 RED 阶段，这会触发 assert 失败（因为我们期望进程必须被杀掉）
	assert.Error(t, err, "Grandchild process %s escaped and is still running! This is a severe process leak.", grandchildPID)

	// 为了不污染测试机，如果它还活着，我们需要强杀它
	if err == nil {
		_ = exec.Command("kill", "-9", grandchildPID).Run()
	}
}
