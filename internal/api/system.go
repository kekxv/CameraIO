package api

import (
	"CameraIO/internal/pkg"

	"github.com/gin-gonic/gin"
)

// GetFFmpegStatus 返回 FFmpeg 可用性/下载进度（供前端展示）。
func (h *Handler) GetFFmpegStatus(c *gin.Context) {
	ok(c, pkg.GetFFmpegStatus())
}
