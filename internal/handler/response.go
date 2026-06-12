package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// respondOK 返回成功响应（code=0）
func respondOK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": data})
}

// respondOKMsg 返回成功响应（无 data，仅 message）
func respondOKMsg(c *gin.Context, msg string) {
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": msg})
}

// respondError 返回错误响应（非 0 code，HTTP 状态码 = code）
func respondError(c *gin.Context, code int, msg string) {
	c.JSON(code, gin.H{"code": code, "message": msg})
}
