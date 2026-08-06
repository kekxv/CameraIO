package api

import (
	"context"
	"net/http"
	"time"

	"CameraIO/internal/service"

	"github.com/gin-gonic/gin"
)

// SetCameraNetwork 设置摄像头的网络配置。
func (h *Handler) SetCameraNetwork(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		return
	}
	var req service.NetworkConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	camera, err := h.cameraSvc.Get(id)
	if err != nil {
		fail(c, http.StatusNotFound, err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	_ = ctx // 预留给 WaitForDevice

	err = h.cameraSvc.SetNetworkInterface(camera.IP, camera.Username, camera.Password, req)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}

	ok(c, gin.H{
		"message": "网络配置已设置，设备正在重启。新 IP: " + req.IP,
		"new_ip":  req.IP,
	})
}
