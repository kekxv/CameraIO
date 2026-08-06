package api

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// ScanNetwork 扫描局域网内的摄像头/录像机设备。
func (h *Handler) ScanNetwork(c *gin.Context) {
	var req struct {
		Subnet string `json:"subnet"` // "auto" 或 "192.168.1.0/24"
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		req.Subnet = "auto"
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	devices, err := h.discoverySvc.ScanLAN(ctx, req.Subnet)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, devices)
}
