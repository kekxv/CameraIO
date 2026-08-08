package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"CameraIO/internal/api"
	"CameraIO/internal/pkg"
	"CameraIO/internal/service"
)

type recordingStartup interface {
	ReconcileSegments() error
	ReconcileLegacyOrphaned()
	RunRetentionOnce() error
	StartRetention()
	StartSweep()
}

type scheduleStartup interface {
	Start()
}

func startRecordingSubsystems(recorder recordingStartup, scheduler scheduleStartup) error {
	if err := recorder.ReconcileSegments(); err != nil {
		return fmt.Errorf("reconcile recording segments: %w", err)
	}
	recorder.ReconcileLegacyOrphaned()
	if err := recorder.RunRetentionOnce(); err != nil {
		return fmt.Errorf("run initial recording retention: %w", err)
	}
	recorder.StartRetention()
	recorder.StartSweep()
	scheduler.Start()
	return nil
}

func main() {
	cfg := pkg.LoadConfig()

	// 确保记录目录存在
	if err := os.MkdirAll(cfg.RecordingsDir, 0o755); err != nil {
		log.Fatalf("create recordings dir: %v", err)
	}

	// 确保 FFmpeg 可用。若未安装则后台自动下载，不阻塞启动，
	// 可在 Web 界面查看下载进度（GET /api/v1/system/ffmpeg）。
	pkg.EnsureFFmpegAsync()
	switch st := pkg.GetFFmpegStatus(); st.State {
	case "installed":
		log.Printf("FFmpeg: %s", st.Path)
	case "downloading", "extracting":
		log.Printf("⚠️ FFmpeg 未安装，后台自动下载中，可在 Web 界面查看进度。下载完成后流媒体功能可用。")
	case "error":
		log.Printf("⚠️ FFmpeg 未找到: %s", st.Error)
		log.Printf("   流媒体功能将不可用。请安装 FFmpeg 或设置 CAMERAIO_FFMPEG_PATH 环境变量")
	default:
		log.Printf("⚠️ FFmpeg 状态未知: %s", st.State)
	}

	// 初始化数据库
	db, err := pkg.InitDB(cfg.DBPath)
	if err != nil {
		log.Fatalf("init database: %v", err)
	}

	// 初始化服务
	jwtCfg := pkg.NewJWTConfig(cfg.JWTSecret)
	userSvc := service.NewUserService(db, jwtCfg)
	onvifSvc := service.NewONVIFService()
	cameraSvc := service.NewCameraService(db, onvifSvc)
	streamSvc := service.NewStreamService(db)
	recorderSvc := service.NewRecorderService(db, cfg)
	recorderSvc.SetStreamService(streamSvc)
	eventBus := service.NewEventBus()
	recorderSvc.SetEventBus(eventBus)
	monitor := service.NewSystemMonitor(db, eventBus, onvifSvc)
	gb28181Svc := service.NewGB28181Service(cfg, db, eventBus, streamSvc)
	streamSvc.SetGB28181(gb28181Svc)
	cameraSvc.SetGB28181(gb28181Svc)
	localCamSvc := service.NewLocalCameraService()
	discoverySvc := service.NewDiscoveryService(onvifSvc)
	scheduleSvc := service.NewScheduleService(db, recorderSvc)

	// 启动后台服务
	if err := startRecordingSubsystems(recorderSvc, scheduleSvc); err != nil {
		log.Fatalf("start recording services: %v", err)
	}
	monitor.Start()
	if err := gb28181Svc.Start(); err != nil {
		log.Printf("[GB28181] failed to start SIP server: %v", err)
	}

	// 初始化 Handler
	handler := api.NewHandler(userSvc, cameraSvc, streamSvc, recorderSvc, eventBus, localCamSvc, discoverySvc, scheduleSvc, jwtCfg)
	router := handler.SetupRouter()

	// 注册前端静态文件服务
	registerFrontend(router)

	// 使用 http.Server 代替 router.Run()，这样可以通过 Shutdown() 优雅停止
	srv := &http.Server{
		Addr:    cfg.Addr,
		Handler: router,
	}

	// 启动 HTTP 服务（goroutine 中）
	go func() {
		log.Printf("CameraIO server starting on %s", cfg.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server run: %v", err)
		}
	}()

	// 打印 WebUI 访问地址（控制台模式下方便用户手动打开浏览器）
	if url := webUIURL(cfg.Addr); url != "" {
		log.Printf("WebUI: %s", url)
	}

	// 等待退出信号
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	log.Printf("收到信号 %v，开始优雅关闭...", sig)

	// 优雅关闭：15 秒超时
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// 1. 先停止后台服务（拉流/录像），让 MJPEG 等长连接请求尽快返回
	cameraSvc.Shutdown()
	streamSvc.Shutdown()
	scheduleSvc.Stop()
	recorderSvc.Shutdown()
	monitor.Stop()
	gb28181Svc.Stop()

	// 2. 停止接收新连接（等待现有请求完成）
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}

	log.Println("CameraIO 已关闭")
}

// webUIURL 根据监听地址构造 WebUI 访问 URL。无法确定时返回空字符串。
func webUIURL(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" || addr == ":0" {
		return ""
	}
	host := addr
	switch {
	case strings.HasPrefix(addr, ":"): // 如 ":8080"
		host = "localhost" + addr
	case strings.HasPrefix(addr, "0.0.0.0:"): // 监听所有网卡
		host = "localhost" + addr[len("0.0.0.0"):]
	case strings.HasPrefix(addr, "[::]:"): // IPv6 通配
		host = "localhost" + addr[len("[::]"):]
	}
	return "http://" + host
}
