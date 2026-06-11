package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"cronix/internal/model"
	"cronix/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// TestNotifyAPI 验证独立的 NotifyConfig GET/PUT API （RED阶段预期失败）
func TestNotifyAPI(t *testing.T) {
	// 1. 初始化内存数据库和依赖
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	assert.NoError(t, err)
	db.AutoMigrate(&model.Task{}, &model.NotifyConfig{})

	taskSvc := &service.TaskService{DB: db}
	handler := &TaskHandler{TaskSvc: taskSvc}

	// 创建路由
	gin.SetMode(gin.TestMode)
	router := gin.New()
	// 预期这两个新路由还不存在或抛错
	router.GET("/api/tasks/:id/notify", handler.GetTaskNotify)
	router.PUT("/api/tasks/:id/notify", handler.UpdateTaskNotify)

	// 先造一个任务
	task := model.Task{Name: "test-notify-task"}
	db.Create(&task)

	// 测试 GET：新任务应该返回 200，但 NotifyConfig 是空的（或者默认值）
	t.Run("GetNotifyConfig_NotExists", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/tasks/1/notify", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "Expected 200 even if no config exists (should return empty config)")
	})

	// 测试 PUT：更新/创建通知配置
	t.Run("UpdateNotifyConfig", func(t *testing.T) {
		cfg := model.NotifyConfig{
			WebhookURL: "https://example.com/webhook",
			OnFailure:  true,
			OnSuccess:  false,
		}
		body, _ := json.Marshal(cfg)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/tasks/1/notify", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "Expected 200 OK after updating notify config")

		// 验证数据库确实写入了
		var saved model.NotifyConfig
		err := db.Where("task_id = ?", 1).First(&saved).Error
		assert.NoError(t, err)
		assert.Equal(t, "https://example.com/webhook", saved.WebhookURL)
	})
}
