package service

import (
	"context"
	"crypto/md5"
	cryptorand "crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"CameraIO/internal/model"

	"gorm.io/gorm"
)

// CameraService 摄像头业务逻辑，包含 ONVIF 自动调优。
type CameraService struct {
	db      *gorm.DB
	onvif   *ONVIFService
	gb28181 *GB28181Service
	cancel  context.CancelFunc
	ctx     context.Context
}

var digestAuthParamRe = regexp.MustCompile(`([A-Za-z]+)=(?:"([^"]*)"|([^,\s]+))`)

const (
	snapshotTimeout  = 5 * time.Second
	maxSnapshotBytes = 20 << 20
)

func NewCameraService(db *gorm.DB, onvif *ONVIFService) *CameraService {
	ctx, cancel := context.WithCancel(context.Background())
	return &CameraService{db: db, onvif: onvif, ctx: ctx, cancel: cancel}
}

// SetGB28181 injects the SIP service used for GB/T 28181-2022 snapshots.
func (s *CameraService) SetGB28181(g *GB28181Service) {
	s.gb28181 = g
}

// Shutdown 优雅关闭后台 goroutine。
func (s *CameraService) Shutdown() {
	s.cancel()
}

// ---------- Input DTOs ----------

type CreateCameraInput struct {
	Name            string `json:"name" binding:"required"`
	IP              string `json:"ip"`
	Port            int    `json:"port"`
	RTSPUrl         string `json:"rtsp_url"`
	Brand           string `json:"brand"`
	Username        string `json:"username"`
	Password        string `json:"password"`
	DeviceTimezone  string `json:"device_timezone"`
	AutoTuneEnabled *bool  `json:"auto_tune_enabled"`
	// 设备类型: "ipc" / "nvr" / "dvr" / "encoder"
	DeviceType string `json:"device_type"`
	// NVR/DVR 通道号（1-256），0 表示 IPC 直连
	NVRChannel int `json:"nvr_channel"`
	// 首选编码格式: "auto" / "h264" / "h265"
	PreferredCodec string `json:"preferred_codec"`
	// 码流类型: "main" (主码流) / "sub" (子码流)
	StreamType string `json:"stream_type"`
	// GB28181 字段
	AccessProtocol string `json:"access_protocol"` // "rtsp" / "gb28181" / "local"
	DeviceID       string `json:"device_id"`       // 20 位国标编码
	ChannelID      string `json:"channel_id"`      // 通道编码
	Transport      string `json:"transport"`       // "UDP" / "TCP"
	// 本地摄像头字段
	LocalIndex int    `json:"local_index"` // 设备索引
	LocalVID   string `json:"local_vid"`   // USB Vendor ID
	LocalPID   string `json:"local_pid"`   // USB Product ID
	LocalName  string `json:"local_name"`  // 设备名
}

type UpdateCameraInput struct {
	Name            *string `json:"name"`
	IP              *string `json:"ip"`
	Port            *int    `json:"port"`
	RTSPUrl         *string `json:"rtsp_url"`
	Brand           *string `json:"brand"`
	Username        *string `json:"username"`
	Password        *string `json:"password"`
	DeviceTimezone  *string `json:"device_timezone"`
	AutoTuneEnabled *bool   `json:"auto_tune_enabled"`
	Status          *string `json:"status"`
	// 设备类型: "ipc" / "nvr" / "dvr" / "encoder"
	DeviceType *string `json:"device_type"`
	// NVR/DVR 通道号（1-256），0 表示 IPC 直连
	NVRChannel *int `json:"nvr_channel"`
	// 首选编码格式: "auto" / "h264" / "h265"
	PreferredCodec *string `json:"preferred_codec"`
	// 码流类型: "main" / "sub"
	StreamType *string `json:"stream_type"`
	// GB28181 字段
	AccessProtocol *string `json:"access_protocol"`
	DeviceID       *string `json:"device_id"`
	ChannelID      *string `json:"channel_id"`
	Transport      *string `json:"transport"`
	// 本地摄像头字段
	LocalIndex *int    `json:"local_index"`
	LocalVID   *string `json:"local_vid"`
	LocalPID   *string `json:"local_pid"`
	LocalName  *string `json:"local_name"`
}

// ---------- CRUD ----------

func (s *CameraService) Create(in *CreateCameraInput) (*model.Camera, error) {
	if in.Port == 0 {
		in.Port = 554
	}
	if in.Brand == "" {
		in.Brand = model.BrandCustom
	}
	protocol := in.AccessProtocol
	if protocol == "" {
		protocol = model.ProtocolRTSP
	}
	// 只有 RTSP 需要 IP；GB28181 通过 SIP 注册，本地用系统设备，均不需要
	if protocol == model.ProtocolRTSP && in.IP == "" {
		return nil, errors.New("IP 地址必填")
	}
	if protocol == model.ProtocolGB28181 && in.DeviceID == "" {
		return nil, errors.New("设备编码 (device_id) 必填")
	}
	deviceType := in.DeviceType
	if deviceType == "" {
		deviceType = model.DeviceTypeIPC
	}
	preferredCodec := in.PreferredCodec
	if preferredCodec == "" {
		preferredCodec = "auto"
	}
	rtspURL := in.RTSPUrl
	if rtspURL == "" && protocol == model.ProtocolRTSP {
		// 优先通过 ONVIF GetStreamUri 获取真实 RTSP 地址（最可靠）
		if in.Username != "" && in.IP != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			// 根据通道号构造 profile token 猜测（宇视: token:17/0/{ch}/1/{ch}/s0）
			// 先尝试直接通过 discover 获取正确的 URL
			channels, err := s.onvif.DiscoverChannels(ctx, in.IP, in.Username, in.Password)
			cancel()
			if err == nil && len(channels) > 0 {
				ch := in.NVRChannel
				if ch <= 0 {
					ch = 1
				}
				for _, c := range channels {
					if c.Channel == ch && c.RTSPUrl != "" {
						rtspURL = c.RTSPUrl
						break
					}
				}
			}
		}
		// 回退：根据品牌规则构造 RTSP URL
		if rtspURL == "" {
			streamType := in.StreamType
			if streamType == "" {
				streamType = model.StreamTypeMain
			}
			rtspURL = buildRTSPURL(in.Brand, in.Username, in.Password, in.IP, in.Port, in.NVRChannel, streamType)
		}
	}
	if protocol == model.ProtocolRTSP {
		rtspURL = ensureRTSPCredentials(rtspURL, in.Username, in.Password)
	}
	streamType := in.StreamType
	if streamType == "" {
		streamType = model.StreamTypeMain
	}
	autoTune := true
	if in.AutoTuneEnabled != nil {
		autoTune = *in.AutoTuneEnabled
	}
	transport := in.Transport
	if transport == "" {
		transport = "UDP"
	}
	camera := model.Camera{
		Name:            in.Name,
		IP:              in.IP,
		Port:            in.Port,
		RTSPUrl:         rtspURL,
		Brand:           in.Brand,
		Username:        in.Username,
		Password:        in.Password,
		DeviceTimezone:  in.DeviceTimezone,
		AutoTuneEnabled: autoTune,
		Status:          model.CameraStatusOffline,
		DeviceType:      deviceType,
		NVRChannel:      in.NVRChannel,
		PreferredCodec:  preferredCodec,
		StreamType:      streamType,
		AccessProtocol:  protocol,
		DeviceID:        in.DeviceID,
		ChannelID:       in.ChannelID,
		Transport:       transport,
		LocalIndex:      in.LocalIndex,
		LocalVID:        in.LocalVID,
		LocalPID:        in.LocalPID,
		LocalName:       in.LocalName,
		CreatedAt:       time.Now(),
	}
	if err := s.db.Create(&camera).Error; err != nil {
		return nil, err
	}
	// 异步执行自动调优（仅 RTSP 摄像头），避免阻塞 API 响应。
	if autoTune && protocol == model.ProtocolRTSP {
		go s.autoTune(camera)
	}
	return &camera, nil
}

func (s *CameraService) Get(id uint) (*model.Camera, error) {
	var camera model.Camera
	if err := s.db.First(&camera, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("camera not found")
		}
		return nil, err
	}
	return &camera, nil
}

func (s *CameraService) List() ([]model.Camera, error) {
	var cameras []model.Camera
	if err := s.db.Find(&cameras).Error; err != nil {
		return nil, err
	}
	return cameras, nil
}

// CaptureSnapshot 获取设备原生 JPEG 快照。
// 该路径仅调用 ONVIF Media 和设备 HTTP 快照地址，不启动 RTSP、FFmpeg 或 MJPEG 预览。
func (s *CameraService) CaptureSnapshot(ctx context.Context, cameraID uint) ([]byte, error) {
	camera, err := s.Get(cameraID)
	if err != nil {
		return nil, err
	}
	if camera.AccessProtocol == model.ProtocolGB28181 {
		if s.gb28181 == nil {
			return nil, errors.New("GB28181 snapshot service is not available")
		}
		return s.gb28181.CaptureSnapshot(ctx, cameraID)
	}
	if camera.AccessProtocol != "" && camera.AccessProtocol != model.ProtocolRTSP {
		return nil, fmt.Errorf("native snapshot is only supported for RTSP cameras")
	}
	if camera.IP == "" {
		return nil, fmt.Errorf("camera IP is required for native snapshot")
	}

	ctx, cancel := context.WithTimeout(ctx, snapshotTimeout)
	defer cancel()
	profileToken, err := s.onvif.FindProfileToken(ctx, camera.IP, camera.Username, camera.Password, camera.NVRChannel)
	if err != nil {
		return nil, fmt.Errorf("resolve snapshot profile: %w", err)
	}
	snapshotURI, err := s.onvif.GetSnapshotURI(ctx, camera.IP, camera.Username, camera.Password, profileToken)
	if err != nil {
		return nil, fmt.Errorf("resolve snapshot URI: %w", err)
	}

	log.Printf("[snapshot] camera %d requesting %s", cameraID, snapshotURLForLog(snapshotURI))
	resp, err := fetchSnapshot(ctx, snapshotURI, camera.Username, camera.Password)
	if err != nil {
		log.Printf("[snapshot] camera %d request failed: %v", cameraID, err)
		return nil, fmt.Errorf("request camera snapshot failed")
	}
	defer resp.Body.Close()
	log.Printf("[snapshot] camera %d response status=%s content_type=%q", cameraID, resp.Status, resp.Header.Get("Content-Type"))
	if resp.StatusCode != http.StatusOK {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		if message := snapshotFailureDetail(detail); message != "" {
			log.Printf("[snapshot] camera %d response detail=%q", cameraID, message)
			return nil, fmt.Errorf("camera snapshot returned %s: %s", resp.Status, message)
		}
		return nil, fmt.Errorf("camera snapshot returned %s", resp.Status)
	}
	jpeg, err := io.ReadAll(io.LimitReader(resp.Body, maxSnapshotBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read camera snapshot: %w", err)
	}
	if len(jpeg) == 0 || len(jpeg) > maxSnapshotBytes {
		return nil, fmt.Errorf("camera snapshot has invalid size")
	}
	if !isJPEG(jpeg) {
		log.Printf("[snapshot] camera %d response is not JPEG: bytes=%d", cameraID, len(jpeg))
		return nil, fmt.Errorf("camera snapshot is not JPEG (content type %q)", strings.ToLower(resp.Header.Get("Content-Type")))
	}
	return jpeg, nil
}

// isJPEG 通过 JPEG 的 SOI/EOI 标记确认数据格式，兼容返回错误 MIME 类型的设备。
func isJPEG(data []byte) bool {
	return len(data) >= 4 && data[0] == 0xff && data[1] == 0xd8 && data[len(data)-2] == 0xff && data[len(data)-1] == 0xd9
}

func snapshotFailureDetail(body []byte) string {
	message := strings.Join(strings.Fields(string(body)), " ")
	if len(message) > 512 {
		return message[:512] + "..."
	}
	return message
}

func snapshotURLForLog(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "<invalid snapshot URI>"
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

func fetchSnapshot(ctx context.Context, snapshotURI, username, password string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, snapshotURI, nil)
	if err != nil {
		return nil, fmt.Errorf("invalid snapshot URI")
	}
	// Some ONVIF devices include Basic credentials in the returned URI. Do not
	// send them before the server has issued an authentication challenge.
	req.URL.User = nil
	req.Header.Set("Accept", "image/jpeg")
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusUnauthorized || username == "" {
		return resp, err
	}

	challenge := resp.Header.Get("WWW-Authenticate")
	resp.Body.Close()
	retry, err := http.NewRequestWithContext(ctx, req.Method, snapshotURI, nil)
	if err != nil {
		return nil, fmt.Errorf("invalid snapshot URI")
	}
	retry.URL.User = nil
	retry.Header.Set("Accept", "image/jpeg")
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(challenge)), "basic ") {
		retry.SetBasicAuth(username, password)
	} else if authorization, ok := buildDigestAuthorization(challenge, username, password, req.Method, req.URL.RequestURI()); ok {
		retry.Header.Set("Authorization", authorization)
	} else {
		return nil, fmt.Errorf("camera snapshot authentication failed")
	}
	return http.DefaultClient.Do(retry)
}

func buildDigestAuthorization(challenge, username, password, method, requestURI string) (string, bool) {
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(challenge)), "digest ") {
		return "", false
	}
	params := make(map[string]string)
	for _, match := range digestAuthParamRe.FindAllStringSubmatch(challenge, -1) {
		value := match[2]
		if value == "" {
			value = match[3]
		}
		params[strings.ToLower(match[1])] = value
	}
	realm, nonce := params["realm"], params["nonce"]
	if realm == "" || nonce == "" || (params["algorithm"] != "" && !strings.EqualFold(params["algorithm"], "MD5")) {
		return "", false
	}
	qop := ""
	for _, option := range strings.Split(params["qop"], ",") {
		if strings.TrimSpace(option) == "auth" {
			qop = "auth"
			break
		}
	}
	if params["qop"] != "" && qop == "" {
		return "", false
	}

	cnonceBytes := make([]byte, 16)
	if _, err := cryptorand.Read(cnonceBytes); err != nil {
		return "", false
	}
	cnonce := hex.EncodeToString(cnonceBytes)
	nc := "00000001"
	ha1 := digestMD5(username + ":" + realm + ":" + password)
	ha2 := digestMD5(method + ":" + requestURI)
	response := ""
	if qop == "auth" {
		response = digestMD5(ha1 + ":" + nonce + ":" + nc + ":" + cnonce + ":" + qop + ":" + ha2)
	} else {
		response = digestMD5(ha1 + ":" + nonce + ":" + ha2)
	}

	parts := []string{
		`username="` + escapeDigestValue(username) + `"`,
		`realm="` + escapeDigestValue(realm) + `"`,
		`nonce="` + escapeDigestValue(nonce) + `"`,
		`uri="` + escapeDigestValue(requestURI) + `"`,
		`response="` + response + `"`,
		"algorithm=MD5",
	}
	if opaque := params["opaque"]; opaque != "" {
		parts = append(parts, `opaque="`+escapeDigestValue(opaque)+`"`)
	}
	if qop != "" {
		parts = append(parts, "qop="+qop, "nc="+nc, `cnonce="`+cnonce+`"`)
	}
	return "Digest " + strings.Join(parts, ", "), true
}

func digestMD5(value string) string {
	sum := md5.Sum([]byte(value))
	return hex.EncodeToString(sum[:])
}

func escapeDigestValue(value string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value)
}

func (s *CameraService) Update(id uint, in *UpdateCameraInput) (*model.Camera, error) {
	camera, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	updates := map[string]any{}
	needRebuildRTSP := false

	if in.Name != nil {
		updates["name"] = *in.Name
	}
	if in.IP != nil {
		updates["ip"] = *in.IP
		needRebuildRTSP = true
	}
	if in.Port != nil {
		updates["port"] = *in.Port
		needRebuildRTSP = true
	}
	if in.RTSPUrl != nil {
		if *in.RTSPUrl != "" {
			updates["rtsp_url"] = *in.RTSPUrl
		} else {
			// 空字符串表示让后端自动重建
			needRebuildRTSP = true
		}
	}
	if in.Brand != nil {
		updates["brand"] = *in.Brand
		needRebuildRTSP = true
	}
	if in.Username != nil {
		updates["username"] = *in.Username
		needRebuildRTSP = true
	}
	if in.Password != nil {
		updates["password"] = *in.Password
		needRebuildRTSP = true
	}
	if in.DeviceTimezone != nil {
		updates["device_timezone"] = *in.DeviceTimezone
	}
	if in.AutoTuneEnabled != nil {
		updates["auto_tune_enabled"] = *in.AutoTuneEnabled
	}
	if in.Status != nil {
		updates["status"] = *in.Status
	}
	if in.DeviceType != nil {
		updates["device_type"] = *in.DeviceType
		needRebuildRTSP = true
	}
	if in.NVRChannel != nil {
		updates["nvr_channel"] = *in.NVRChannel
		needRebuildRTSP = true
	}
	if in.PreferredCodec != nil {
		updates["preferred_codec"] = *in.PreferredCodec
	}
	if in.StreamType != nil {
		updates["stream_type"] = *in.StreamType
		needRebuildRTSP = true
	}
	if in.AccessProtocol != nil {
		updates["access_protocol"] = *in.AccessProtocol
	}
	if in.DeviceID != nil {
		updates["device_id"] = *in.DeviceID
	}
	if in.ChannelID != nil {
		updates["channel_id"] = *in.ChannelID
	}
	if in.Transport != nil {
		updates["transport"] = *in.Transport
	}

	// 当 IP/端口/品牌/用户名/密码/设备类型/通道号/码流 变化时，自动重建 RTSP URL
	if needRebuildRTSP && in.RTSPUrl == nil && camera.AccessProtocol == model.ProtocolRTSP {
		ip := camera.IP
		if in.IP != nil {
			ip = *in.IP
		}
		port := camera.Port
		if in.Port != nil {
			port = *in.Port
		}
		brand := camera.Brand
		if in.Brand != nil {
			brand = *in.Brand
		}
		user := camera.Username
		if in.Username != nil {
			user = *in.Username
		}
		pass := camera.Password
		if in.Password != nil {
			pass = *in.Password
		}
		ch := camera.NVRChannel
		if in.NVRChannel != nil {
			ch = *in.NVRChannel
		}
		streamType := camera.StreamType
		if in.StreamType != nil {
			streamType = *in.StreamType
		}
		if streamType == "" {
			streamType = model.StreamTypeMain
		}
		updates["rtsp_url"] = buildRTSPURL(brand, user, pass, ip, port, ch, streamType)
	}
	if rtspURL, ok := updates["rtsp_url"].(string); ok {
		user := camera.Username
		if in.Username != nil {
			user = *in.Username
		}
		pass := camera.Password
		if in.Password != nil {
			pass = *in.Password
		}
		updates["rtsp_url"] = ensureRTSPCredentials(rtspURL, user, pass)
	}

	if len(updates) == 0 {
		return camera, nil
	}
	if err := s.db.Model(camera).Updates(updates).Error; err != nil {
		return nil, err
	}
	return s.Get(id)
}

func (s *CameraService) Delete(id uint) error {
	result := s.db.Delete(&model.Camera{}, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("camera not found")
	}
	return nil
}

// ---------- ONVIF 操作 ----------

// SyncTime 主动触发一次指定摄像头的时间同步。
func (s *CameraService) SyncTime(cameraID uint) error {
	camera, err := s.Get(cameraID)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer cancel()
	if err := s.onvif.SyncCameraTime(ctx, camera.IP, camera.Username, camera.Password, camera.NVRChannel, camera.DeviceTimezone); err != nil {
		return err
	}
	s.db.Model(&model.Camera{}).Where("id = ?", cameraID).
		Update("last_time_sync", time.Now())
	return nil
}

// TestConnection 测试指定摄像头的 ONVIF 连接（获取设备信息）。
func (s *CameraService) TestConnection(cameraID uint) (*Deviceinfo, error) {
	camera, err := s.Get(cameraID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer cancel()
	return s.onvif.TestConnection(ctx, camera.IP, camera.Username, camera.Password)
}

// TestConnectionByIP 通过 IP 和凭据测试 ONVIF 连接（添加摄像头前使用）。
func (s *CameraService) TestConnectionByIP(ip, user, pass string) (*Deviceinfo, error) {
	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer cancel()
	return s.onvif.TestConnection(ctx, ip, user, pass)
}

// DiscoverChannels 发现 NVR 上的所有通道。
func (s *CameraService) DiscoverChannels(ip, user, pass string) ([]ChannelInfo, error) {
	ctx, cancel := context.WithTimeout(s.ctx, 15*time.Second)
	defer cancel()
	return s.onvif.DiscoverChannels(ctx, ip, user, pass)
}

// SetVideoCodec 设置摄像头的视频编码格式（h264 或 h265）。
func (s *CameraService) SetVideoCodec(cameraID uint, codec string) error {
	camera, err := s.Get(cameraID)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(s.ctx, 15*time.Second)
	defer cancel()
	if err := s.onvif.SetVideoCodec(ctx, camera.Brand, camera.IP, camera.Username, camera.Password, codec, camera.NVRChannel); err != nil {
		return err
	}
	// 更新数据库中的 PreferredCodec
	s.db.Model(&model.Camera{}).Where("id = ?", cameraID).Update("preferred_codec", codec)
	return nil
}

// SetNetworkInterface 设置摄像头的网络配置（IP/DHCP 等）。
func (s *CameraService) SetNetworkInterface(ip, user, pass string, config NetworkConfig) error {
	ctx, cancel := context.WithTimeout(s.ctx, 15*time.Second)
	defer cancel()
	return s.onvif.SetNetworkInterface(ctx, ip, user, pass, config)
}

// autoTune 异步执行：时间同步 + 视频参数优化。
func (s *CameraService) autoTune(camera model.Camera) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1) 时间同步
	if err := s.onvif.SyncCameraTime(ctx, camera.IP, camera.Username, camera.Password, camera.NVRChannel, camera.DeviceTimezone); err != nil {
		log.Printf("[ONVIF] camera %d (%s) sync time failed: %v", camera.ID, camera.IP, err)
	} else {
		s.db.Model(&model.Camera{}).Where("id = ?", camera.ID).
			Update("last_time_sync", time.Now())
	}

	// 2) 视频参数优化
	if err := s.onvif.OptimizeVideoSettings(ctx, camera.Brand, camera.IP, camera.Username, camera.Password, camera.NVRChannel); err != nil {
		log.Printf("[ONVIF] camera %d (%s) optimize video failed: %v", camera.ID, camera.IP, err)
	}
}

// ---------- Helpers ----------

// buildRTSPURL 根据品牌、通道号等参数构造 RTSP URL。
//
// 各品牌 NVR 通道路径规则:
//   - 海康 (Hikvision): /Streaming/Channels/{channel}01 (主码流) 或 {channel}02 (子码流)
//     例: 通道1=101(主) / 102(子), 通道2=201(主) / 202(子)
//   - 宇视 (Uniview): /unicast/c{channel}/s0/live (主码流) 或 s1/live (子码流)
//     例: 通道1=c1/s0(主) / c1/s1(子)
//   - 通用/其他: /Streaming/Channels/{channel}01 (与海康兼容)
//
// channel=0 表示 IPC 直连（默认通道 1）。
// streamType: "main" / "sub"。
func buildRTSPURL(brand, username, password, ip string, port int, channel int, streamType string) string {
	if channel <= 0 {
		channel = 1
	}
	isSub := streamType == model.StreamTypeSub

	path := ""
	switch strings.ToLower(brand) {
	case model.BrandHikvision:
		streamID := channel * 100
		if isSub {
			streamID += 2 // 子码流
		} else {
			streamID += 1 // 主码流
		}
		path = fmt.Sprintf("/Streaming/Channels/%d", streamID)
	case model.BrandUniview:
		sub := "0"
		if isSub {
			sub = "1"
		}
		// 宇视 NVR: /unicast/c{channel}/s{sub}/live
		path = fmt.Sprintf("/unicast/c%d/s%s/live", channel, sub)
	default:
		streamID := channel * 100
		if isSub {
			streamID += 2
		} else {
			streamID += 1
		}
		path = fmt.Sprintf("/Streaming/Channels/%d", streamID)
	}

	u := &url.URL{
		Scheme: "rtsp",
		Host:   net.JoinHostPort(ip, strconv.Itoa(port)),
		Path:   path,
	}
	if username != "" {
		u.User = url.UserPassword(username, password)
	}
	return u.String()
}

// ensureRTSPCredentials 为 ONVIF 等返回的无凭据 URI 注入已保存的摄像头凭据。
func ensureRTSPCredentials(rawURL, username, password string) string {
	if rawURL == "" || username == "" {
		return rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "rtsp" || u.User != nil {
		return rawURL
	}
	u.User = url.UserPassword(username, password)
	return u.String()
}
