package api

import (
	"net/http"

	"CameraIO/internal/model"

	"github.com/gin-gonic/gin"
)

// ListSchedules 列出所有定时录像计划。
func (h *Handler) ListSchedules(c *gin.Context) {
	schedules, err := h.scheduleSvc.List()
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, schedules)
}

// CreateSchedule 创建定时录像计划。
func (h *Handler) CreateSchedule(c *gin.Context) {
	var req model.RecordingSchedule
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	// 校验必填字段
	if req.Name == "" || req.CameraID == 0 {
		fail(c, http.StatusBadRequest, "名称和摄像头必填")
		return
	}
	if req.StartTime == "" || req.EndTime == "" {
		fail(c, http.StatusBadRequest, "开始时间和结束时间必填")
		return
	}
	if req.Days == 0 {
		req.Days = model.DayAllWeek
	}
	if req.Format == "" {
		req.Format = model.FormatMP4
	}
	if err := h.scheduleSvc.Create(&req); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	created(c, req)
}

// UpdateSchedule 更新定时录像计划。
func (h *Handler) UpdateSchedule(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		return
	}
	var req model.RecordingSchedule
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.scheduleSvc.Update(id, &req); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"message": "计划已更新"})
}

// DeleteSchedule 删除定时录像计划。
func (h *Handler) DeleteSchedule(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		return
	}
	if err := h.scheduleSvc.Delete(id); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	noContent(c)
}
