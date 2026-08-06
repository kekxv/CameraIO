package api

import (
	"net/http"
	"strconv"

	"CameraIO/internal/service"

	"github.com/gin-gonic/gin"
)

func (h *Handler) CreateCamera(c *gin.Context) {
	var req service.CreateCameraInput
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	camera, err := h.cameraSvc.Create(&req)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	created(c, camera)
}

func (h *Handler) GetCamera(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		return
	}
	camera, err := h.cameraSvc.Get(id)
	if err != nil {
		fail(c, http.StatusNotFound, err.Error())
		return
	}
	ok(c, camera)
}

func (h *Handler) ListCameras(c *gin.Context) {
	cameras, err := h.cameraSvc.List()
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, cameras)
}

func (h *Handler) UpdateCamera(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		return
	}
	var req service.UpdateCameraInput
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	camera, err := h.cameraSvc.Update(id, &req)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, camera)
}

func (h *Handler) DeleteCamera(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		return
	}
	if err := h.cameraSvc.Delete(id); err != nil {
		fail(c, http.StatusNotFound, err.Error())
		return
	}
	noContent(c)
}

func (h *Handler) SyncCameraTime(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		return
	}
	if err := h.cameraSvc.SyncTime(id); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"message": "time sync completed"})
}

// TestCameraConnection 测试摄像头 ONVIF 连接（获取设备版本信息）。
func (h *Handler) TestCameraConnection(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		return
	}
	info, err := h.cameraSvc.TestConnection(id)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, info)
}

// TestCameraConnectionByIP 通过 IP 和凭据测试连接（添加前使用）。
func (h *Handler) TestCameraConnectionByIP(c *gin.Context) {
	var req struct {
		IP       string `json:"ip" binding:"required"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	info, err := h.cameraSvc.TestConnectionByIP(req.IP, req.Username, req.Password)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, info)
}

// DiscoverNVRChannels 发现 NVR 上的所有可用通道。
func (h *Handler) DiscoverNVRChannels(c *gin.Context) {
	var req struct {
		IP       string `json:"ip" binding:"required"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	channels, err := h.cameraSvc.DiscoverChannels(req.IP, req.Username, req.Password)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, channels)
}

// SetCameraCodec 设置摄像头的视频编码格式。
func (h *Handler) SetCameraCodec(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		return
	}
	var req struct {
		Codec string `json:"codec" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "codec 字段必填 (h264 或 h265)")
		return
	}
	if err := h.cameraSvc.SetVideoCodec(id, req.Codec); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"message": "编码格式已设置为 " + req.Codec})
}

func parseUintParam(c *gin.Context, name string) (uint, error) {
	v, err := strconv.ParseUint(c.Param(name), 10, 64)
	if err != nil {
		fail(c, http.StatusBadRequest, "invalid "+name)
		return 0, err
	}
	return uint(v), nil
}
