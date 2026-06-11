package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestTaskHandler_KillTask_Red(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	
	// 这里目前还没实现 KillTask，故意让它跑不通以满足 Red 阶段
	// 我们用一个空的 TaskHandler
	th := &TaskHandler{}
	
	router.POST("/api/tasks/:id/kill", th.KillTask)
	
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/tasks/9999/kill", nil)
	router.ServeHTTP(w, req)
	
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestTaskHandler_StreamTaskLog_Red(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	
	th := &TaskHandler{}
	router.GET("/api/tasks/:id/stream", th.StreamTaskLog)
	
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/tasks/9999/stream", nil)
	router.ServeHTTP(w, req)
	
	assert.Equal(t, http.StatusNotFound, w.Code)
}
