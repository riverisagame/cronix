package service_test

import (
	"cronix/internal/domain/model"
	"cronix/internal/application/service"
	"testing"
)

// MockTaskReloader 模拟调度引擎的重载接口
type MockTaskReloader struct {
	CalledUpdate bool
	CalledRemove bool
}

func (m *MockTaskReloader) UpdateTaskSchedule(task model.Task) error {
	m.CalledUpdate = true
	return nil
}

func (m *MockTaskReloader) RemoveTaskSchedule(id uint) {
	m.CalledRemove = true
}

// MockDaemonReloader 模拟守护进程引擎的重载接口
type MockDaemonReloader struct {
	CalledReload bool
}

func (m *MockDaemonReloader) ReloadDaemon(task model.Task) {
	m.CalledReload = true
}

func (m *MockDaemonReloader) StopDaemon(taskID uint) {
	// mock empty
}

func TestTaskService_InterfaceMock(t *testing.T) {
	// [RED 阶段]
	// 期望：在没有进行接口隔离重构时，下面注入 mock 对象会导致编译错误，因为 TaskService.Engine 期待的是 *scheduler.Engine
	mockTaskReloader := &MockTaskReloader{}
	mockDaemonReloader := &MockDaemonReloader{}

	// 将会在这里产生编译报错，因为类型不匹配
	svc := &service.TaskService{
		Engine:    mockTaskReloader,
		DaemonMon: mockDaemonReloader,
	}

	// 模拟创建常驻任务（此时不走数据库，因为我们只测试接口回调逻辑是否连通）
	task := model.Task{TaskType: "daemon"}
	
	// 在没有修改实际调用逻辑前，这里只能先假设。
	// 这里通过显式调用接口来确认方法签名正确。
	svc.Engine.UpdateTaskSchedule(task)
	svc.DaemonMon.ReloadDaemon(task)

	if !mockTaskReloader.CalledUpdate {
		t.Error("Expected UpdateTaskSchedule to be called")
	}
	if !mockDaemonReloader.CalledReload {
		t.Error("Expected ReloadDaemon to be called")
	}
}
