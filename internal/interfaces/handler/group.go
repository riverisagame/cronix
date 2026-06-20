package handler

import (
    "net/http"
    "strconv"

    "cronix/internal/domain/model"
    "cronix/internal/application/scheduler"
    "cronix/internal/application/service"

    "github.com/gin-gonic/gin"
)

type GroupHandler struct {
    GroupSvc *service.GroupService
    TaskSvc  *service.TaskService
    Executor *scheduler.Executor
}

func (h *GroupHandler) ListGroups(c *gin.Context) {
    groups, err := h.GroupSvc.ListGroups()
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
        return
    }
    if groups == nil {
        groups = []model.TaskGroup{}
    }
    respondOK(c, groups)
}

func (h *GroupHandler) CreateGroup(c *gin.Context) {
    var g model.TaskGroup
    if err := c.ShouldBindJSON(&g); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": err.Error()})
        return
    }
    if err := h.GroupSvc.CreateGroup(&g); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": err.Error()})
        return
    }
    respondOK(c, g)
}

func (h *GroupHandler) GetGroup(c *gin.Context) {
    id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
    g, err := h.GroupSvc.GetGroup(uint(id))
    if err != nil {
        c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "group not found"})
        return
    }
    members, _ := h.GroupSvc.GetGroupMembers(uint(id))
    if members == nil {
        members = []model.Task{}
    }
    c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": gin.H{"group": g, "members": members}})
}

func (h *GroupHandler) UpdateGroup(c *gin.Context) {
    id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
    var updates map[string]interface{}
    if err := c.ShouldBindJSON(&updates); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": err.Error()})
        return
    }
    if err := h.GroupSvc.UpdateGroup(uint(id), updates); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": err.Error()})
        return
    }
    respondOKMsg(c, "ok")
}

func (h *GroupHandler) DeleteGroup(c *gin.Context) {
    id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
    taskCount, logCount, err := h.GroupSvc.DeleteGroup(uint(id))
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
        return
    }
    c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": gin.H{
        "tasks_affected": taskCount,
        "logs_deleted":   logCount,
    }})
}

func (h *GroupHandler) SetMembers(c *gin.Context) {
    id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
    var req struct {
        TaskIDs []uint `json:"task_ids"`
    }
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": err.Error()})
        return
    }
    if err := h.GroupSvc.SetGroupMembers(uint(id), req.TaskIDs); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": err.Error()})
        return
    }
    respondOKMsg(c, "ok")
}

func (h *GroupHandler) RunGroup(c *gin.Context) {
    id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
    g, err := h.GroupSvc.GetGroup(uint(id))
    if err != nil {
        c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "group not found"})
        return
    }
    members, _ := h.GroupSvc.GetGroupMembers(uint(id))
    if len(members) == 0 {
        c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "group has no members"})
        return
    }
    go h.Executor.RunGroup(g, members, "manual")
    respondOK(c, gin.H{"mode": g.Mode, "member_count": len(members)})
}

func (h *GroupHandler) GetGroupLogs(c *gin.Context) {
    id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
    page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
    pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
    logs, total, err := h.GroupSvc.GetGroupLogs(uint(id), page, pageSize)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": err.Error()})
        return
    }
    c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": gin.H{"items": logs, "total": total}})
}
