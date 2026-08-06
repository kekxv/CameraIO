package api

import (
	"net/http"

	"CameraIO/internal/pkg"
	"CameraIO/internal/service"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	userSvc      *service.UserService
	cameraSvc    *service.CameraService
	streamSvc    *service.StreamService
	webrtcSvc    *service.WebRTCService
	recorderSvc  *service.RecorderService
	eventBus     *service.EventBus
	localCamSvc  *service.LocalCameraService
	discoverySvc *service.DiscoveryService
	scheduleSvc  *service.ScheduleService
	jwtCfg       *pkg.JWTConfig
}

func NewHandler(
	userSvc *service.UserService,
	cameraSvc *service.CameraService,
	streamSvc *service.StreamService,
	webrtcSvc *service.WebRTCService,
	recorderSvc *service.RecorderService,
	eventBus *service.EventBus,
	localCamSvc *service.LocalCameraService,
	discoverySvc *service.DiscoveryService,
	scheduleSvc *service.ScheduleService,
	jwtCfg *pkg.JWTConfig,
) *Handler {
	return &Handler{
		userSvc:      userSvc,
		cameraSvc:    cameraSvc,
		streamSvc:    streamSvc,
		webrtcSvc:    webrtcSvc,
		recorderSvc:  recorderSvc,
		eventBus:     eventBus,
		localCamSvc:  localCamSvc,
		discoverySvc: discoverySvc,
		scheduleSvc:  scheduleSvc,
		jwtCfg:       jwtCfg,
	}
}

type response struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

func ok(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, response{Code: 0, Message: "ok", Data: data})
}

func created(c *gin.Context, data interface{}) {
	c.JSON(http.StatusCreated, response{Code: 0, Message: "created", Data: data})
}

func fail(c *gin.Context, httpCode int, msg string) {
	c.JSON(httpCode, response{Code: httpCode, Message: msg})
}

func noContent(c *gin.Context) {
	c.Status(http.StatusNoContent)
}
