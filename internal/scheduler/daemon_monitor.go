// ============================================================
// internal/scheduler/daemon_monitor.go - 常驻进程守护控制器
//
// 类似于 Supervisor 的功能，负责管理 RunMode == "daemon" 的常驻任务：
//   1. 系统启动时自动扫描并拉起所有已启用的常驻守护任务
//   2. 任务异常退出后自动重启（Keep-Alive），支持指数退避延迟
//   3. 连续失败超过阈值后熔断（FATAL），不再自动拉起
//   4. 支持手动 Start / Stop 控制
//   5. 提供实时状态查询接口供前端/API使用
//
// 状态流转：STOPPED -> STARTING -> RUNNING -> (异常退出) -> BACKOFF -> RUNNING
//                                                       -> (超限) -> FATAL
//
// @Ref: docs/sps/plans/20260605_daemon_supervisor_feature.md | @Date: 2026-06-05
// ============================================================
package scheduler

import (
	"context"
	"sync"
	"time"

	"cronix/internal/model"

	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

// DaemonState 状态常量
const (
    DaemonStopped  = "STOPPED"
    DaemonStarting = "STARTING"
    DaemonRunning  = "RUNNING"
    DaemonBackoff  = "BACKOFF"
    DaemonFatal    = "FATAL"
)

// DaemonState 描述常驻守护任务的当前运行状态（对外暴露，供 API 查询）
type DaemonState struct {
	// Status 当前状态：STOPPED / STARTING / RUNNING / BACKOFF / FATAL
	Status string `json:"status"`
	// RestartCount 连续重启失败次数（成功执行后归零）
	RestartCount int `json:"restart_count"`
	// MaxRestartAttempts daemon 任务配置的最大重试次数（来自 Task 模型）
	MaxRestartAttempts int `json:"max_restart_attempts"`
	// LastError 最后一次执行的错误信息
	LastError string `json:"last_error,omitempty"`
	// LastStartTime 最后一次启动时间
	LastStartTime *time.Time `json:"last_start_time,omitempty"`
	// Uptime 当前运行时长（仅 RUNNING 状态有效）
	Uptime string `json:"uptime,omitempty"`
}

// daemonTaskState 内部状态管理结构（包含 context 取消句柄等内部字段）
type daemonTaskState struct {
	// 对外可见的状态信息
	DaemonState
	// cancel 用于停止该守护协程（撤销 context 取消信号）
	cancel context.CancelFunc
	// parentCtx 父级上下文（全局 Start 传入的 ctx）
	parentCtx context.Context
}

// DaemonMonitor 常驻进程守护控制器
// 内部使用 sync.RWMutex 保护并发状态访问
type DaemonMonitor struct {
	db       *gorm.DB
	executor *Executor
	mu       sync.RWMutex
	states   map[uint]*daemonTaskState
}

// NewDaemonMonitor 创建常驻进程守护控制器
// 参数 db: 数据库连接（用于扫描常驻任务列表）
// 参数 executor: 任务执行器（用于实际运行任务）
func NewDaemonMonitor(db *gorm.DB, executor *Executor) *DaemonMonitor {
	return &DaemonMonitor{
		db:       db,
		executor: executor,
		states:   make(map[uint]*daemonTaskState),
	}
}

// Start 启动守护控制器，扫描数据库中所有已启用的常驻任务并自动拉起
// 参数 ctx: 全局上下文，取消时所有守护任务将被停止
func (m *DaemonMonitor) Start(ctx context.Context) {
	var tasks []model.Task
	if err := m.db.Where("enabled = ? AND run_mode = ?", true, "daemon").Find(&tasks).Error; err != nil {
		log.Error().Err(err).Msg("daemon monitor: 扫描常驻任务失败")
		return
	}

	log.Info().Int("count", len(tasks)).Msg("daemon monitor: 已扫描到常驻守护任务")

	for _, task := range tasks {
		m.startDaemonInternal(ctx, task.ID)
	}
}

// StartDaemon 手动启动一个常驻守护任务（供 API 调用）
// 如果任务已经在运行中，不会重复启动
func (m *DaemonMonitor) StartDaemon(taskID uint) {
	m.mu.RLock()
	if st, exists := m.states[taskID]; exists && (st.Status == DaemonRunning || st.Status == DaemonStarting || st.Status == DaemonBackoff) {
		m.mu.RUnlock()
		log.Warn().Uint("task_id", taskID).Str("status", st.Status).Msg("daemon monitor: 任务已在运行中，跳过重复启动")
		return
	}
	m.mu.RUnlock()

	// 使用已有的 parentCtx 或 background context
	m.mu.RLock()
	var parentCtx context.Context
	if st, exists := m.states[taskID]; exists && st.parentCtx != nil {
		parentCtx = st.parentCtx
	} else {
		parentCtx = context.Background()
	}
	m.mu.RUnlock()

	m.startDaemonInternal(parentCtx, taskID)
}

// startDaemonInternal 内部启动守护协程的核心逻辑
func (m *DaemonMonitor) startDaemonInternal(parentCtx context.Context, taskID uint) {
	// 从数据库加载任务配置
	var task model.Task
	if err := m.db.First(&task, taskID).Error; err != nil {
		log.Error().Err(err).Uint("task_id", taskID).Msg("daemon monitor: 加载任务失败")
		return
	}

	// 创建该守护任务专属的可取消上下文
	ctx, cancel := context.WithCancel(parentCtx)

	now := time.Now()
	state := &daemonTaskState{
		DaemonState: DaemonState{
			Status:             DaemonStarting,
			RestartCount:       0,
			MaxRestartAttempts: task.MaxRestartAttempts,
			LastStartTime:      &now,
		},
		cancel:    cancel,
		parentCtx: parentCtx,
	}

	m.mu.Lock()
	m.states[taskID] = state
	m.mu.Unlock()

	// 在独立协程中运行守护循环
	go m.runDaemonLoop(ctx, taskID, &task)
}

// runDaemonLoop 守护循环核心：执行 -> 检查退出 -> 退避 -> 重新拉起
func (m *DaemonMonitor) runDaemonLoop(ctx context.Context, taskID uint, task *model.Task) {
	defer func() {
		if r := recover(); r != nil {
			log.Error().Interface("panic", r).Uint("task_id", taskID).Msg("daemon monitor: 守护协程 panic 恢复")
		}
	}()

	maxAttempts := task.MaxRestartAttempts
	if maxAttempts <= 0 {
		maxAttempts = 10
	}
	restartPolicy := task.RestartPolicy
	if restartPolicy == "" {
		restartPolicy = "always"
	}

	// 计算重启延迟：RestartDelaySec > 0 时使用固定间隔
	// 否则成功 1s，失败指数退避
	restartDelaySec := task.RestartDelaySec
	useFixedDelay := restartDelaySec > 0
	scheduledRestartSec := task.ScheduledRestartSec
	restartCount := 0

	for {
		// 检查上下文是否已被取消（手动 Stop 或全局关闭）
		select {
		case <-ctx.Done():
			m.setStatus(taskID, DaemonStopped, "")
			log.Info().Uint("task_id", taskID).Msg("daemon monitor: 守护协程收到停止信号，退出")
			return
		default:
		}

		// 更新状态为 RUNNING
		now := time.Now()
		m.mu.Lock()
		if st, ok := m.states[taskID]; ok {
			st.Status = DaemonRunning
			st.LastStartTime = &now
		}
		m.mu.Unlock()

		log.Info().Uint("task_id", taskID).Str("task", task.Name).Int("restart_count", restartCount).Msg("daemon monitor: 拉起常驻任务")

		// 主动定时重启：创建子 context + 定时器
		wasScheduled := false
		if scheduledRestartSec > 0 {
			execCtx, execCancel := context.WithCancel(ctx)
			timer := time.AfterFunc(time.Duration(scheduledRestartSec)*time.Second, func() {
				execCancel()
				log.Info().Uint("task_id", taskID).Int("interval_sec", scheduledRestartSec).
					Msg("daemon monitor: 定时重启触发")
			})
			m.executor.ExecuteTaskWithContext(execCtx, taskID)
			timer.Stop()
			wasScheduled = execCtx.Err() != nil && ctx.Err() == nil
			execCancel() // 始终释放子 context，避免 goroutine 泄漏（idempotent）
		} else {
			m.executor.ExecuteTaskWithContext(ctx, taskID)
		}

		// 再次检查 ctx 是否已被取消（手动 Stop）
		select {
		case <-ctx.Done():
			m.setStatus(taskID, DaemonStopped, "")
			log.Info().Uint("task_id", taskID).Msg("daemon monitor: 任务执行期间收到停止信号，退出守护")
			return
		default:
		}

		// 查询最新的执行日志，判断退出状态
		var latestLog model.ExecutionLog
		err := m.db.Where("task_id = ?", taskID).Order("id DESC").First(&latestLog).Error
		exitSuccess := (err == nil && latestLog.Status == "success") || wasScheduled

		// 根据重启策略判定是否需要重启
		// 根据重启策略判定是否需要重启（定时重启强制重启）
		shouldRestart := wasScheduled
		if !shouldRestart {
			switch restartPolicy {
			case "always":
				shouldRestart = true
			case "on-failure":
				shouldRestart = !exitSuccess
			}
		}

		if !shouldRestart {
			m.setStatus(taskID, DaemonStopped, "")
			log.Info().Uint("task_id", taskID).Str("policy", restartPolicy).Msg("daemon monitor: 重启策略判定不需要重启，守护退出")
			return
		}

		// 累加连续失败计数（成功执行后归零）
		if exitSuccess {
			restartCount = 0
			delay := 1 * time.Second
			if useFixedDelay {
				delay = time.Duration(restartDelaySec) * time.Second
			}
			select {
			case <-ctx.Done():
				m.setStatus(taskID, DaemonStopped, "")
				log.Info().Uint("task_id", taskID).Msg("daemon monitor: 成功退出后退避期间收到停止信号，退出守护")
				return
			case <-time.After(delay):
			}
		} else {
			restartCount++
			lastErr := ""
			if latestLog.ErrorMsg != "" {
				lastErr = latestLog.ErrorMsg
			}
			m.setStatus(taskID, DaemonBackoff, lastErr)

			// 检查是否超过最大重启次数 -> FATAL 熔断
			if restartPolicy != "always" && restartCount >= maxAttempts {
				m.mu.Lock()
				if st, ok := m.states[taskID]; ok {
					st.Status = DaemonFatal
					st.RestartCount = restartCount
					st.LastError = lastErr
				}
				m.mu.Unlock()
				log.Error().Uint("task_id", taskID).Int("attempts", restartCount).Int("max", maxAttempts).
					Msg("daemon monitor: 连续重启失败超过阈值，进入 FATAL 熔断")
				return
			}

			// 延迟计算：固定间隔 > 指数退避
			var backoff time.Duration
			if useFixedDelay {
				backoff = time.Duration(restartDelaySec) * time.Second
			} else {
				// 指数退避：1s -> 2s -> 4s -> 8s -> ... 最大 60s
				if restartCount >= 7 {
					backoff = 60 * time.Second
				} else {
					backoff = time.Duration(1<<uint(restartCount-1)) * time.Second
				}
			}

			m.mu.Lock()
			if st, ok := m.states[taskID]; ok {
				st.RestartCount = restartCount
			}
			m.mu.Unlock()

			log.Warn().Uint("task_id", taskID).Str("error", lastErr).Dur("backoff", backoff).Int("attempt", restartCount).
				Msg("daemon monitor: 执行失败，进入退避等待")

			select {
			case <-ctx.Done():
				m.setStatus(taskID, DaemonStopped, "")
				log.Info().Uint("task_id", taskID).Msg("daemon monitor: 退避期间收到停止信号，退出守护")
				return
			case <-time.After(backoff):
			}
		}
	}
}

// StopDaemon 手动停止一个常驻守护任务
// 撤销该任务的 context，使守护协程退出并强杀正在运行的子进程
func (m *DaemonMonitor) StopDaemon(taskID uint) {
	m.mu.Lock()
	st, exists := m.states[taskID]
	if !exists {
		m.mu.Unlock()
		log.Warn().Uint("task_id", taskID).Msg("daemon monitor: 停止失败，任务不存在")
		return
	}

	if st.cancel != nil {
		st.cancel()
	}
	st.Status = DaemonStopped
	m.mu.Unlock()

	log.Info().Uint("task_id", taskID).Msg("daemon monitor: 已发送停止信号")
}

// GetDaemonState 查询某个常驻守护任务的当前状态
// 返回值：DaemonState 状态快照，bool 是否存在
func (m *DaemonMonitor) GetDaemonState(taskID uint) (DaemonState, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	st, exists := m.states[taskID]
	if !exists {
		return DaemonState{}, false
	}

	result := st.DaemonState
	// 计算实时运行时长
	if st.Status == DaemonRunning && st.LastStartTime != nil {
		result.Uptime = time.Since(*st.LastStartTime).Truncate(time.Second).String()
	}
	return result, true
}

// GetAllDaemonStates 获取所有常驻守护任务的状态快照（供前端仪表盘使用）
func (m *DaemonMonitor) GetAllDaemonStates() map[uint]DaemonState {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[uint]DaemonState, len(m.states))
	for id, st := range m.states {
		ds := st.DaemonState
		if st.Status == DaemonRunning && st.LastStartTime != nil {
			ds.Uptime = time.Since(*st.LastStartTime).Truncate(time.Second).String()
		}
		result[id] = ds
	}
	return result
}

// setStatus 内部辅助方法：安全地更新指定任务的状态和错误信息
func (m *DaemonMonitor) setStatus(taskID uint, status, lastError string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if st, ok := m.states[taskID]; ok {
		st.Status = status
		if lastError != "" {
			st.LastError = lastError
		}
	}
}

// ReloadDaemon 响应任务配置热更新
// @Ref: docs/sps/decisions/20260605_architect_review_daemon_supervisor.md | @Date: 2026-06-05
func (m *DaemonMonitor) ReloadDaemon(task model.Task) {
	m.mu.RLock()
	st, exists := m.states[task.ID]
	m.mu.RUnlock()

	// 1. 如果新状态为 disabled 或切换为 cron，则彻底停止正在运行的守护进程
	if !task.Enabled || task.RunMode != "daemon" {
		if exists {
			log.Info().Uint("task_id", task.ID).Msg("daemon monitor: 检测到任务禁用或模式切换，正在停止常驻任务")
			m.StopDaemon(task.ID)
		}
		return
	}

	// 2. 如果任务是 daemon，且原来是启用的（不管什么状态，只要不是 STOPPED 就尝试重启或拉起）
	// 如果是手动 STOPPED 状态，更新配置不应该自动拉起，除非是从 cron 转成 daemon。
	// 为了简化，只要处于任何正在重试或运行或熔断状态，更新后都重启。
	if exists && (st.Status == DaemonRunning || st.Status == DaemonStarting || st.Status == DaemonBackoff || st.Status == DaemonFatal) {
		log.Info().Uint("task_id", task.ID).Msg("daemon monitor: 检测到配置更新，正在热重载守护任务")
		m.StopDaemon(task.ID)
		
		// 稍微延迟一小段以确保旧进程已收到 SIGKILL 后再拉起新进程
		go func(id uint) {
			time.Sleep(200 * time.Millisecond)
			m.StartDaemon(id)
		}(task.ID)
	}
}
