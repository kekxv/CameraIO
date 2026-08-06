package api

import (
	"context"
	"net/http"
	"time"

	"CameraIO/internal/service"

	"github.com/gin-gonic/gin"
)

func (h *Handler) ListLocalCameras(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	cameras, err := h.localCamSvc.Enumerate(ctx)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	if cameras == nil {
		cameras = []service.LocalCamera{}
	}
	ok(c, cameras)
}
