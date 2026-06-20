// ============================================================
// internal/service/task_service_test.go - 任务服务单元测试
//
// 【纳米级源码说明书 - 测试篇】
// 这里的角色是“电路板测试员”。
// 专门用来测试“任务服务（TaskService）”在断开外部真实引擎连接时，
// 能不能靠“假冒的模拟引擎（Mock）”跑通。
//
// 💡 【大厂面试·底层原理扩展（初二小白版）】
// 面试官问：什么是依赖倒置（DI）？为什么要用 Mock（模拟）测试？
// 答（小白比喻）：
// 假设你要测试一个“遥控器（TaskService）”好不好使。
// 错误做法：必须买一台真实的“电视机（真实调度引擎）”回来，按遥控器看电视机亮不亮。
// 这样太重了，万一电视机坏了，你还会以为是遥控器坏了。
// 正确做法（依赖倒置 + Mock）：
// 在遥控器上弄个“通用插口（Interface）”，只要符合这个插口的设备都能接。
// 测试时，我们接一个小灯泡（MockTaskReloader）。按一下遥控器，小灯泡亮了，就证明遥控器没问题！
// ============================================================
package service_test

import (
	"cronix/internal/domain/model"
	"cronix/internal/application/service"
	"testing"
)

// MockTaskReloader 这就是上面说的“测试用小灯泡”（假冒的调度引擎）
// 里面不装复杂的定时器，只有两个开关（布尔值），记录有没有被按过。
type MockTaskReloader struct {
	CalledUpdate bool
	CalledRemove bool
}

// UpdateTaskSchedule 遥控器按下“更新”按钮时，这个方法被触发，灯泡亮起（CalledUpdate = true）
func (m *MockTaskReloader) UpdateTaskSchedule(task model.Task) error {
	m.CalledUpdate = true
	return nil
}

func (m *MockTaskReloader) RemoveTaskSchedule(id uint) {
	m.CalledRemove = true
}

// MockDaemonReloader 这是另一个“测试用小喇叭”（假冒的守护进程监视器）
type MockDaemonReloader struct {
	CalledReload bool
}

func (m *MockDaemonReloader) ReloadDaemon(task model.Task) {
	m.CalledReload = true
}

func (m *MockDaemonReloader) StopDaemon(taskID uint) {
	// mock empty
}

// TestTaskService_InterfaceMock 测试依赖倒置的插口是否连通
func TestTaskService_InterfaceMock(t *testing.T) {
	// [RED 阶段]
	// 这个注释来自于 TDD（测试驱动开发）。意思是我们要先写出测试代码，哪怕它现在报错。
	// 这里准备两个假冒的设备（Mock 对象）
	mockTaskReloader := &MockTaskReloader{}
	mockDaemonReloader := &MockDaemonReloader{}

	// 把假冒设备插到遥控器（TaskService）的插口上。
	// 因为我们前面重构了代码（把具体的 Engine 换成了抽象的 Interface），
	// 所以这里可以无缝插进去！
	svc := &service.TaskService{
		Engine:    mockTaskReloader,
		DaemonMon: mockDaemonReloader,
	}

	// 模拟按遥控器：创建一个类型为 daemon 的任务
	task := model.Task{TaskType: "daemon"}
	
	// 按下更新按钮和重载按钮
	svc.Engine.UpdateTaskSchedule(task)
	svc.DaemonMon.ReloadDaemon(task)

	// 检查小灯泡和小喇叭有没有反应
	if !mockTaskReloader.CalledUpdate {
		t.Error("Expected UpdateTaskSchedule to be called") // 遥控器按了，灯没亮，遥控器坏了！
	}
	if !mockDaemonReloader.CalledReload {
		t.Error("Expected ReloadDaemon to be called")
	}
}
