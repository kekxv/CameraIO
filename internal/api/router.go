package api

import (
	"github.com/gin-gonic/gin"
)

// SetupRouter 注册所有路由。
func (h *Handler) SetupRouter() *gin.Engine {
	r := gin.Default()

	// CORS 中间件
	r.Use(corsMiddleware())

	// 公开路由
	r.POST("/api/v1/login", h.Login)

	// WebSocket（不经过 JWT 中间件，用 token 参数鉴权）
	r.GET("/ws/v1/system", h.WebSocketSystem)

	// 受保护路由
	protected := r.Group("/api/v1", h.JWTAuthMiddleware())
	{
		// Users
		protected.GET("/users", h.ListUsers)
		protected.POST("/users", h.CreateUser)

		// Cameras
		protected.GET("/cameras", h.ListCameras)
		protected.POST("/cameras", h.CreateCamera)
		protected.GET("/cameras/:id", h.GetCamera)
		protected.GET("/cameras/:id/snapshot", h.CaptureCameraSnapshot)
		protected.PUT("/cameras/:id", h.UpdateCamera)
		protected.DELETE("/cameras/:id", h.DeleteCamera)
		protected.POST("/cameras/:id/sync-time", h.SyncCameraTime)
		protected.POST("/cameras/:id/test", h.TestCameraConnection)
		protected.POST("/cameras/test-by-ip", h.TestCameraConnectionByIP)
		protected.POST("/cameras/discover-channels", h.DiscoverNVRChannels)
		protected.POST("/cameras/scan-network", h.ScanNetwork)
		protected.POST("/cameras/:id/set-codec", h.SetCameraCodec)
		protected.POST("/cameras/:id/set-network", h.SetCameraNetwork)

		// Local Cameras
		protected.GET("/local-cameras", h.ListLocalCameras)

		// System
		protected.GET("/system/ffmpeg", h.GetFFmpegStatus)

		// Streams
		protected.POST("/streams/:id/start", h.StartStream)
		protected.POST("/streams/:id/stop", h.StopStream)
		protected.GET("/streams/:id/mjpeg", h.StreamMJPEG)

		// Recordings
		protected.POST("/recordings/start", h.StartRecording)
		protected.POST("/recordings/stop", h.StopRecording)
		protected.GET("/recordings", h.ListRecordings)
		protected.GET("/recordings/timeline", h.RecordingTimeline)
		protected.GET("/recordings/play-at", h.RecordingPlayAt)
		protected.GET("/recordings/:id", h.GetRecording)
		protected.DELETE("/recordings/:id", h.DeleteRecording)
		protected.GET("/recordings/:id/download", h.DownloadRecording)
		protected.GET("/recording-segments/:id/media", h.RecordingSegmentMedia)

		// 定时录像计划
		protected.GET("/schedules", h.ListSchedules)
		protected.POST("/schedules", h.CreateSchedule)
		protected.PUT("/schedules/:id", h.UpdateSchedule)
		protected.DELETE("/schedules/:id", h.DeleteSchedule)
	}

	// 健康检查
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	return r
}

func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Origin, Content-Type, Authorization")
		c.Header("Access-Control-Max-Age", "86400")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	}
}
