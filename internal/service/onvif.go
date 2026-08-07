package service

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// ONVIFService 提供 ONVIF 设备时间同步与视频参数调优。
type ONVIFService struct{}

func NewONVIFService() *ONVIFService {
	return &ONVIFService{}
}

// ---------- 公共 API ----------

// SyncCameraTime 通过 ONVIF SetSystemDateAndTime 将摄像头时间同步为服务器当前时间。
// nvrChannel 为 NVR 通道号（0 表示 IPC 直连，1-256 表示 NVR 的第 N 路通道）。
func (s *ONVIFService) SyncCameraTime(ctx context.Context, ip, user, pass string, nvrChannel int) error {
	endpoint, err := s.probeDeviceEndpoint(ctx, ip, user, pass)
	if err != nil {
		return fmt.Errorf("probe device: %w", err)
	}
	// 先读取设备当前时区，用设备自己的时区来设置时间（宇视/海康兼容性最好）
	tz := s.getDeviceTimezone(ctx, endpoint, user, pass)
	now := time.Now()
	body := buildSetSystemDateAndTimeEnvelope(now, tz)
	_, err = s.callONVIF(ctx, endpoint, user, pass, body, "http://www.onvif.org/ver10/device/wsdl/SetSystemDateAndTime")
	return err
}

// TestConnection 测试 ONVIF 连接。
// 先用 GetSystemDateAndTime 验证凭据（低权限），再尝试 GetDeviceInformation。
// 部分海康设备 ONVIF 用户无 GetDeviceInformation 权限，此时回退到 ISAPI 获取信息。
func (s *ONVIFService) TestConnection(ctx context.Context, ip, user, pass string) (*Deviceinfo, error) {
	endpoint, err := s.probeDeviceEndpoint(ctx, ip, user, pass)
	if err != nil {
		return nil, fmt.Errorf("probe device: %w", err)
	}

	info := &Deviceinfo{}

	// 1) GetSystemDateAndTime — 验证连通性和凭据
	nowBody := `<?xml version="1.0" encoding="UTF-8"?>
<SOAP-ENV:Envelope xmlns:SOAP-ENV="http://www.w3.org/2003/05/soap-envelope"
                   xmlns:tds="http://www.onvif.org/ver10/device/wsdl">
  <SOAP-ENV:Body>
    <tds:GetSystemDateAndTime/>
  </SOAP-ENV:Body>
</SOAP-ENV:Envelope>`
	nowResp, err := s.callONVIF(ctx, endpoint, user, pass, nowBody, "http://www.onvif.org/ver10/device/wsdl/GetSystemDateAndTime")
	if err != nil {
		return nil, fmt.Errorf("连接失败: %w", err)
	}
	info.TimeZone = extractXMLTagValue(nowResp, "TZ")

	// 2) GetDeviceInformation — 可能需要更高权限
	infoBody := `<?xml version="1.0" encoding="UTF-8"?>
<SOAP-ENV:Envelope xmlns:SOAP-ENV="http://www.w3.org/2003/05/soap-envelope"
                   xmlns:tds="http://www.onvif.org/ver10/device/wsdl">
  <SOAP-ENV:Body>
    <tds:GetDeviceInformation/>
  </SOAP-ENV:Body>
</SOAP-ENV:Envelope>`
	infoResp, err := s.callONVIF(ctx, endpoint, user, pass, infoBody, "http://www.onvif.org/ver10/device/wsdl/GetDeviceInformation")
	if err == nil {
		if parsed, parseErr := parseDeviceinfo(infoResp); parseErr == nil && parsed != nil {
			info.Manufacturer = parsed.Manufacturer
			info.Model = parsed.Model
			info.FirmwareVer = parsed.FirmwareVer
			info.SerialNum = parsed.SerialNum
			info.HardwareID = parsed.HardwareID
		}
	} else {
		// GetDeviceInformation 权限不足时给提示
		info.PermissionNote = "ONVIF 凭据验证成功，但用户权限不足无法获取设备详情。请在设备 Web 管理界面中为 ONVIF 用户添加管理员权限。"
	}

	// 3) 如果 Manufacturer 仍为空，尝试 ISAPI 补充
	if info.Manufacturer == "" {
		s.enrichFromHTTP(ctx, ip, info)
	}

	return info, nil
}

// enrichFromHTTP 通过 HTTP/ISAPI 补充设备信息（ONVIF 权限不足时使用）。
func (s *ONVIFService) enrichFromHTTP(ctx context.Context, ip string, info *Deviceinfo) {
	for _, port := range []int{80, 8000} {
		isapiURL := fmt.Sprintf("http://%s:%d/ISAPI/System/deviceInfo", ip, port)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, isapiURL, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			continue
		}
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == 200 {
			body := string(data)
			info.Manufacturer = extractXMLTagValue(body, "manufacturer")
			info.Model = extractXMLTagValue(body, "model")
			info.FirmwareVer = extractXMLTagValue(body, "firmwareVersion")
			if info.Manufacturer == "" {
				info.Manufacturer = "Hikvision"
			}
			return
		}
	}
}

// DiscoverChannels 通过 ONVIF GetProfiles 发现 NVR 上的所有可用通道。
func (s *ONVIFService) DiscoverChannels(ctx context.Context, ip, user, pass string) ([]ChannelInfo, error) {
	endpoint, err := s.probeDeviceEndpoint(ctx, ip, user, pass)
	if err != nil {
		return nil, fmt.Errorf("probe device: %w", err)
	}
	devInfo, _ := s.TestConnection(ctx, ip, user, pass)

	mediaEndpoint := fmt.Sprintf("http://%s/onvif/media_service", ip)
	body := `<?xml version="1.0" encoding="UTF-8"?>
<SOAP-ENV:Envelope xmlns:SOAP-ENV="http://www.w3.org/2003/05/soap-envelope"
                   xmlns:trt="http://www.onvif.org/ver10/media/wsdl">
  <SOAP-ENV:Body>
    <trt:GetProfiles/>
  </SOAP-ENV:Body>
</SOAP-ENV:Envelope>`
	respBody, err := s.callONVIF(ctx, mediaEndpoint, user, pass, body, "http://www.onvif.org/ver10/media/wsdl/GetProfiles")
	if err != nil {
		respBody, err = s.callONVIF(ctx, endpoint, user, pass, body, "http://www.onvif.org/ver10/media/wsdl/GetProfiles")
		if err != nil {
			return nil, fmt.Errorf("get profiles: %w", err)
		}
	}
	channels := parseProfiles(respBody)
	for i := range channels {
		if devInfo != nil {
			channels[i].DeviceManufacturer = devInfo.Manufacturer
			channels[i].DeviceModel = devInfo.Model
		}
		// 获取每个通道的实际 RTSP 地址
		if channels[i].ProfileToken != "" {
			uri, err := s.GetStreamURI(ctx, ip, user, pass, channels[i].ProfileToken)
			if err == nil && uri != "" {
				channels[i].RTSPUrl = uri
			}
		}
	}
	return channels, nil
}

// GetStreamURI 通过 ONVIF GetStreamUri 获取指定 Profile 的实际 RTSP 流地址。
// 这比猜测 RTSP 路径格式更可靠，尤其是 NVR 设备。
func (s *ONVIFService) GetStreamURI(ctx context.Context, ip, user, pass, profileToken string) (string, error) {
	mediaEndpoint := fmt.Sprintf("http://%s/onvif/media_service", ip)
	body := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<SOAP-ENV:Envelope xmlns:SOAP-ENV="http://www.w3.org/2003/05/soap-envelope"
                   xmlns:trt="http://www.onvif.org/ver10/media/wsdl"
                   xmlns:tt="http://www.onvif.org/ver10/schema">
  <SOAP-ENV:Body>
    <trt:GetStreamUri>
      <trt:StreamSetup>
        <tt:Stream>RTP-Unicast</tt:Stream>
        <tt:Transport>
          <tt:Protocol>RTSP</tt:Protocol>
        </tt:Transport>
      </trt:StreamSetup>
      <trt:ProfileToken>%s</trt:ProfileToken>
    </trt:GetStreamUri>
  </SOAP-ENV:Body>
</SOAP-ENV:Envelope>`, xmlEscape(profileToken))

	respBody, err := s.callONVIF(ctx, mediaEndpoint, user, pass, body, "http://www.onvif.org/ver10/media/wsdl/GetStreamUri")
	if err != nil {
		return "", err
	}
	return extractXMLTagValue(respBody, "Uri"), nil
}

// GetSnapshotURI 通过 ONVIF GetSnapshotUri 获取指定 Profile 的原生 JPEG 地址。
// 该操作只查询设备 HTTP 快照能力，不会启动 RTSP 拉流或 FFmpeg。
func (s *ONVIFService) GetSnapshotURI(ctx context.Context, ip, user, pass, profileToken string) (string, error) {
	mediaEndpoint := fmt.Sprintf("http://%s/onvif/media_service", ip)
	body := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<SOAP-ENV:Envelope xmlns:SOAP-ENV="http://www.w3.org/2003/05/soap-envelope"
                   xmlns:trt="http://www.onvif.org/ver10/media/wsdl">
  <SOAP-ENV:Body>
    <trt:GetSnapshotUri>
      <trt:ProfileToken>%s</trt:ProfileToken>
    </trt:GetSnapshotUri>
  </SOAP-ENV:Body>
</SOAP-ENV:Envelope>`, xmlEscape(profileToken))

	respBody, err := s.callONVIF(ctx, mediaEndpoint, user, pass, body, "http://www.onvif.org/ver10/media/wsdl/GetSnapshotUri")
	if err != nil {
		return "", err
	}
	uri := extractXMLTagValue(respBody, "Uri")
	if uri == "" {
		return "", fmt.Errorf("ONVIF GetSnapshotUri response has no URI")
	}
	return uri, nil
}

// FindProfileToken 返回摄像头所对应 ONVIF Media Profile。
// IPC（nvrChannel 为 0）使用第一个可用 Profile；NVR/DVR 必须匹配指定通道。
func (s *ONVIFService) FindProfileToken(ctx context.Context, ip, user, pass string, nvrChannel int) (string, error) {
	mediaEndpoint := fmt.Sprintf("http://%s/onvif/media_service", ip)
	body := `<?xml version="1.0" encoding="UTF-8"?>
<SOAP-ENV:Envelope xmlns:SOAP-ENV="http://www.w3.org/2003/05/soap-envelope"
                   xmlns:trt="http://www.onvif.org/ver10/media/wsdl">
  <SOAP-ENV:Body>
    <trt:GetProfiles/>
  </SOAP-ENV:Body>
</SOAP-ENV:Envelope>`
	respBody, err := s.callONVIF(ctx, mediaEndpoint, user, pass, body, "http://www.onvif.org/ver10/media/wsdl/GetProfiles")
	if err != nil {
		return "", fmt.Errorf("get profiles: %w", err)
	}
	profiles := parseProfiles(respBody)
	if len(profiles) == 0 {
		return "", fmt.Errorf("no ONVIF media profile found")
	}
	if nvrChannel <= 0 {
		return profiles[0].ProfileToken, nil
	}
	for _, profile := range profiles {
		if profile.Channel == nvrChannel {
			return profile.ProfileToken, nil
		}
	}
	return "", fmt.Errorf("no ONVIF media profile found for channel %d", nvrChannel)
}

// Deviceinfo ONVIF 设备信息。
type Deviceinfo struct {
	Manufacturer   string `json:"manufacturer"`
	Model          string `json:"model"`
	FirmwareVer    string `json:"firmware_version"`
	SerialNum      string `json:"serial_number"`
	HardwareID     string `json:"hardware_id"`
	TimeZone       string `json:"timezone,omitempty"`
	PermissionNote string `json:"permission_note,omitempty"`
}

// ChannelInfo NVR 通道信息。
type ChannelInfo struct {
	Channel            int    `json:"channel"`
	ProfileToken       string `json:"profile_token"`
	Name               string `json:"name"`
	VideoSourceToken   string `json:"video_source_token"`
	RTSPUrl            string `json:"rtsp_url,omitempty"`
	DeviceManufacturer string `json:"device_manufacturer,omitempty"`
	DeviceModel        string `json:"device_model,omitempty"`
}

// OptimizeVideoSettings 尝试通过海康 ISAPI 下发低延迟编码参数；
// 若 ISAPI 不可用，回退到通用 ONVIF GetVideoEncoderConfiguration / SetVideoEncoderConfiguration。
func (s *ONVIFService) OptimizeVideoSettings(ctx context.Context, brand, ip, user, pass string, nvrChannel int) error {
	// 海康优先走 ISAPI，效率更高、参数更细。
	if strings.EqualFold(brand, "hikvision") {
		if err := s.optimizeViaISAPI(ctx, ip, user, pass, nvrChannel); err == nil {
			return nil
		}
		// fall through to ONVIF
	}
	return s.optimizeViaONVIF(ctx, ip, user, pass, nvrChannel)
}

// ---------- 编码格式切换 (H.264 / H.265) ----------

// SetVideoCodec 设置摄像头的视频编码格式。
// codec: "h264" 或 "h265"。
// 海康优先走 ISAPI，其他设备走 ONVIF。
func (s *ONVIFService) SetVideoCodec(ctx context.Context, brand, ip, user, pass, codec string, nvrChannel int) error {
	// 规范化 codec: 接受 "h264", "h.264", "H264", "H.264", "h265", "h.265" 等
	normalized := normalizeCodec(codec)
	if normalized != "h264" && normalized != "h265" {
		return fmt.Errorf("unsupported codec: %s (use 'h264' or 'h265')", codec)
	}

	// 海康优先走 ISAPI
	if strings.EqualFold(brand, "hikvision") {
		if err := s.setCodecViaISAPI(ctx, ip, user, pass, normalized, nvrChannel); err == nil {
			return nil
		}
		// fall through to ONVIF
	}
	return s.setCodecViaONVIF(ctx, ip, user, pass, normalized, nvrChannel)
}

// normalizeCodec 将各种 codec 输入格式规范化为 "h264" 或 "h265"。
func normalizeCodec(codec string) string {
	s := strings.ToLower(codec)
	s = strings.ReplaceAll(s, ".", "")
	s = strings.ReplaceAll(s, " ", "")
	switch s {
	case "h264", "264", "avc", "avc1":
		return "h264"
	case "h265", "265", "hevc", "hev1", "hvc1":
		return "h265"
	default:
		return s
	}
}

// setCodecViaISAPI 通过海康 ISAPI 设置编码格式。
func (s *ONVIFService) setCodecViaISAPI(ctx context.Context, ip, user, pass, codec string, nvrChannel int) error {
	url := fmt.Sprintf("http://%s/ISAPI/System/Video/encodingChannels", ip)
	if nvrChannel > 0 {
		url += fmt.Sprintf("?channel=%d", nvrChannel)
	}

	// 读取当前配置
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(user, pass)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("ISAPI get: %s", resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	// 修改 videoCodecType
	codecType := "H.264"
	if strings.EqualFold(codec, "h265") {
		codecType = "H.265"
	}

	// 替换原始 XML 中的 videoCodecType
	body := string(data)
	body = replaceXMLValue(body, "videoCodecType", codecType)

	// 写回
	putReq, err := http.NewRequestWithContext(ctx, http.MethodPut, url, strings.NewReader(body))
	if err != nil {
		return err
	}
	putReq.SetBasicAuth(user, pass)
	putReq.Header.Set("Content-Type", "application/xml")
	putResp, err := http.DefaultClient.Do(putReq)
	if err != nil {
		return err
	}
	defer putResp.Body.Close()
	if putResp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(putResp.Body)
		return fmt.Errorf("ISAPI put codec: %s: %s", putResp.Status, string(respBody))
	}
	return nil
}

// setCodecViaONVIF 通过 ONVIF SetVideoEncoderConfiguration 设置编码格式。
func (s *ONVIFService) setCodecViaONVIF(ctx context.Context, ip, user, pass, codec string, nvrChannel int) error {
	endpoint, err := s.probeDeviceEndpoint(ctx, ip, user, pass)
	if err != nil {
		return err
	}

	// 1) GetVideoEncoderConfigurations
	getBody := `<?xml version="1.0" encoding="UTF-8"?>
<SOAP-ENV:Envelope xmlns:SOAP-ENV="http://www.w3.org/2003/05/soap-envelope"
                   xmlns:trt="http://www.onvif.org/ver10/media/wsdl">
  <SOAP-ENV:Body>
    <trt:GetVideoEncoderConfigurations/>
  </SOAP-ENV:Body>
</SOAP-ENV:Envelope>`
	respBody, err := s.callONVIF(ctx, endpoint, user, pass, getBody, "http://www.onvif.org/ver10/media/wsdl/GetVideoEncoderConfigurations")
	if err != nil {
		return fmt.Errorf("get encoder config: %w", err)
	}
	token := extractEncoderToken(respBody)
	if token == "" {
		return fmt.Errorf("no encoder configuration token found")
	}

	// 2) SetVideoEncoderConfiguration with new codec
	onvifCodec := "H264"
	if strings.EqualFold(codec, "h265") {
		onvifCodec = "H265"
	}

	setBody := buildSetEncoderConfigWithCodec(token, onvifCodec)
	_, err = s.callONVIF(ctx, endpoint, user, pass, setBody, "http://www.onvif.org/ver10/media/wsdl/SetVideoEncoderConfiguration")
	return err
}

// buildSetEncoderConfigWithCodec 构造带指定编码格式的 SetVideoEncoderConfiguration SOAP 请求。
func buildSetEncoderConfigWithCodec(token, codec string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<SOAP-ENV:Envelope xmlns:SOAP-ENV="http://www.w3.org/2003/05/soap-envelope"
                   xmlns:trt="http://www.onvif.org/ver10/media/wsdl"
                   xmlns:tt="http://www.onvif.org/ver10/schema">
  <SOAP-ENV:Body>
    <trt:SetVideoEncoderConfiguration>
      <trt:Configuration token="%s">
        <tt:Name>mainStream</tt:Name>
        <tt:UseCount>1</tt:UseCount>
        <tt:Encoding>%s</tt:Encoding>
        <tt:Resolution>
          <tt:Width>1920</tt:Width>
          <tt:Height>1080</tt:Height>
        </tt:Resolution>
        <tt:Quality>4</tt:Quality>
        <tt:RateControl>
          <tt:FrameRateLimit>25</tt:FrameRateLimit>
          <tt:EncodingInterval>1</tt:EncodingInterval>
          <tt:BitrateLimit>4096</tt:BitrateLimit>
        </tt:RateControl>
        <tt:Multicast>
          <tt:Address>
            <tt:Type>IPv4</tt:Type>
            <tt:IPv4Address>0.0.0.0</tt:IPv4Address>
          </tt:Address>
          <tt:Port>0</tt:Port>
          <tt:TTL>1</tt:TTL>
          <tt:AutoStart>false</tt:AutoStart>
        </tt:Multicast>
        <tt:SessionTimeout>PT60S</tt:SessionTimeout>
      </trt:Configuration>
    </trt:SetVideoEncoderConfiguration>
  </SOAP-ENV:Body>
</SOAP-ENV:Envelope>`, token, codec)
}

// replaceXMLValue 替换 XML 中指定标签的值。
func replaceXMLValue(xml, tag, newValue string) string {
	re := regexp.MustCompile(`(<` + regexp.QuoteMeta(tag) + `>)([^<]*)(</` + regexp.QuoteMeta(tag) + `>)`)
	return re.ReplaceAllString(xml, "${1}"+newValue+"${3}")
}

// ---------- 视频编码信息获取 ----------

// VideoCodecInfo 视频编码信息。
type VideoCodecInfo struct {
	Codec      string `json:"codec"`      // "H.264" / "H.265" / ""
	Width      int    `json:"width"`      // 分辨率宽
	Height     int    `json:"height"`     // 分辨率高
	Resolution string `json:"resolution"` // "1920x1080"
}

// GetVideoCodecInfo 获取摄像头的视频编码格式和分辨率。
// 海康优先走 ISAPI，其他走 ONVIF。nvrChannel 为 NVR 通道号。
func (s *ONVIFService) GetVideoCodecInfo(ctx context.Context, brand, ip, user, pass string, nvrChannel int) (*VideoCodecInfo, error) {
	if strings.EqualFold(brand, "hikvision") {
		if info, err := s.getCodecInfoViaISAPI(ctx, ip, user, pass, nvrChannel); err == nil && info != nil {
			return info, nil
		}
	}
	return s.getCodecInfoViaONVIF(ctx, ip, user, pass, nvrChannel)
}

// getCodecInfoViaISAPI 通过海康 ISAPI 获取编码信息。
func (s *ONVIFService) getCodecInfoViaISAPI(ctx context.Context, ip, user, pass string, nvrChannel int) (*VideoCodecInfo, error) {
	url := fmt.Sprintf("http://%s/ISAPI/System/Video/encodingChannels", ip)
	if nvrChannel > 0 {
		url += fmt.Sprintf("?channel=%d", nvrChannel)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(user, pass)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("ISAPI get encodingChannels: %s", resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	body := string(data)
	info := &VideoCodecInfo{
		Codec: extractXMLTagValue(body, "videoCodecType"),
	}
	if w := extractXMLTagValue(body, "videoResolutionWidth"); w != "" {
		fmt.Sscanf(w, "%d", &info.Width)
	}
	if h := extractXMLTagValue(body, "videoResolutionHeight"); h != "" {
		fmt.Sscanf(h, "%d", &info.Height)
	}
	if info.Width > 0 && info.Height > 0 {
		info.Resolution = fmt.Sprintf("%dx%d", info.Width, info.Height)
	}
	return info, nil
}

// getCodecInfoViaONVIF 通过 ONVIF GetVideoEncoderConfigurations 获取编码信息。
func (s *ONVIFService) getCodecInfoViaONVIF(ctx context.Context, ip, user, pass string, nvrChannel int) (*VideoCodecInfo, error) {
	endpoint, err := s.probeDeviceEndpoint(ctx, ip, user, pass)
	if err != nil {
		return nil, err
	}
	getBody := `<?xml version="1.0" encoding="UTF-8"?>
<SOAP-ENV:Envelope xmlns:SOAP-ENV="http://www.w3.org/2003/05/soap-envelope"
                   xmlns:trt="http://www.onvif.org/ver10/media/wsdl">
  <SOAP-ENV:Body>
    <trt:GetVideoEncoderConfigurations/>
  </SOAP-ENV:Body>
</SOAP-ENV:Envelope>`
	respBody, err := s.callONVIF(ctx, endpoint, user, pass, getBody, "http://www.onvif.org/ver10/media/wsdl/GetVideoEncoderConfigurations")
	if err != nil {
		return nil, err
	}
	return parseVideoCodecInfo(respBody), nil
}

// parseVideoCodecInfo 从 GetVideoEncoderConfigurations 响应中解析编码信息。
func parseVideoCodecInfo(body string) *VideoCodecInfo {
	type resolution struct {
		Width  int `xml:"Width"`
		Height int `xml:"Height"`
	}
	type config struct {
		Encoding   string     `xml:"Encoding"`
		Resolution resolution `xml:"Resolution"`
	}
	type env struct {
		Body struct {
			Resp struct {
				Configs []config `xml:"Configurations"`
			} `xml:"GetVideoEncoderConfigurationsResponse"`
		} `xml:"Body"`
	}
	var e env
	if err := xml.Unmarshal([]byte(body), &e); err != nil {
		return &VideoCodecInfo{}
	}
	if len(e.Body.Resp.Configs) == 0 {
		return &VideoCodecInfo{}
	}
	c := e.Body.Resp.Configs[0]
	info := &VideoCodecInfo{
		Codec:  c.Encoding,
		Width:  c.Resolution.Width,
		Height: c.Resolution.Height,
	}
	if info.Width > 0 && info.Height > 0 {
		info.Resolution = fmt.Sprintf("%dx%d", info.Width, info.Height)
	}
	return info
}

// ---------- ONVIF: 设备发现 ----------

func (s *ONVIFService) probeDeviceEndpoint(ctx context.Context, ip, user, pass string) (string, error) {
	// 标准 ONVIF DeviceService 入口（宇视/海康通用）
	candidate := fmt.Sprintf("http://%s/onvif/device_service", ip)

	// 用 GetSystemDateAndTime（无需鉴权）验证可达性
	// 使用 SOAP-ENV 前缀，与宇视/海康设备保持一致
	body := `<?xml version="1.0" encoding="UTF-8"?>
<SOAP-ENV:Envelope xmlns:SOAP-ENV="http://www.w3.org/2003/05/soap-envelope"
                   xmlns:tds="http://www.onvif.org/ver10/device/wsdl">
  <SOAP-ENV:Body>
    <tds:GetSystemDateAndTime/>
  </SOAP-ENV:Body>
</SOAP-ENV:Envelope>`

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, candidate, strings.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/soap+xml; charset=utf-8")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		data, _ := io.ReadAll(resp.Body)
		body := string(data)
		if strings.Contains(body, "ONVIF integrate function is disabled") {
			log.Printf("[ONVIF] probe %s: ONVIF 功能未启用", candidate)
			return "", fmt.Errorf("设备 ONVIF 功能未启用，请在设备 Web 管理界面中开启（配置 → 网络 → 高级设置 → 集成协议 → 启用 ONVIF）")
		}
		log.Printf("[ONVIF] probe %s failed: %s\n%s", candidate, resp.Status, body)
		return "", fmt.Errorf("device probe failed: %s", resp.Status)
	}
	return candidate, nil
}

// ---------- ONVIF: SetSystemDateAndTime ----------

// buildSetSystemDateAndTimeEnvelope 构造 SetSystemDateAndTime SOAP 请求。
// 使用 Manual 模式（NTP 模式需要设备已配置 NTP 服务器，宇视/海康会拒绝）。
// timezone 为 POSIX 时区字符串（如 "GMT0", "CST-8" 等），默认 "GMT0"。
func buildSetSystemDateAndTimeEnvelope(t time.Time, timezone string) string {
	if timezone == "" {
		timezone = "GMT0"
	}
	utc := t.UTC()
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<SOAP-ENV:Envelope xmlns:SOAP-ENV="http://www.w3.org/2003/05/soap-envelope"
                   xmlns:tds="http://www.onvif.org/ver10/device/wsdl"
                   xmlns:tt="http://www.onvif.org/ver10/schema">
  <SOAP-ENV:Body>
    <tds:SetSystemDateAndTime>
      <tds:DateTimeType>Manual</tds:DateTimeType>
      <tds:DaylightSavings>false</tds:DaylightSavings>
      <tds:TimeZone>
        <tt:TZ>%s</tt:TZ>
      </tds:TimeZone>
      <tds:UTCDateTime>
        <tt:Date>
          <tt:Year>%d</tt:Year>
          <tt:Month>%d</tt:Month>
          <tt:Day>%d</tt:Day>
        </tt:Date>
        <tt:Time>
          <tt:Hour>%d</tt:Hour>
          <tt:Minute>%d</tt:Minute>
          <tt:Second>%d</tt:Second>
        </tt:Time>
      </tds:UTCDateTime>
    </tds:SetSystemDateAndTime>
  </SOAP-ENV:Body>
</SOAP-ENV:Envelope>`,
		timezone,
		utc.Year(), int(utc.Month()), utc.Day(),
		utc.Hour(), utc.Minute(), utc.Second(),
	)
}

// getDeviceTimezone 读取设备当前时区。如果读取失败，返回 "GMT0"。
func (s *ONVIFService) getDeviceTimezone(ctx context.Context, endpoint, user, pass string) string {
	body := `<?xml version="1.0" encoding="UTF-8"?>
<SOAP-ENV:Envelope xmlns:SOAP-ENV="http://www.w3.org/2003/05/soap-envelope"
                   xmlns:tds="http://www.onvif.org/ver10/device/wsdl">
  <SOAP-ENV:Body>
    <tds:GetSystemDateAndTime/>
  </SOAP-ENV:Body>
</SOAP-ENV:Envelope>`
	// GetSystemDateAndTime 无需鉴权
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(body))
	if err != nil {
		return "GMT0"
	}
	req.Header.Set("Content-Type", "application/soap+xml; charset=utf-8")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "GMT0"
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "GMT0"
	}
	// 从响应中提取 <tt:TZ>...</tt:TZ>
	tz := extractXMLTagValue(string(data), "TZ")
	if tz == "" {
		return "GMT0"
	}
	return tz
}

// parseDeviceInfo 从 GetDeviceInformation 响应中提取设备信息。
func parseDeviceinfo(body string) (*Deviceinfo, error) {
	type resp struct {
		Manufacturer string `xml:"Manufacturer"`
		Model        string `xml:"Model"`
		FirmwareVer  string `xml:"FirmwareVersion"`
		SerialNum    string `xml:"SerialNumber"`
		HardwareID   string `xml:"HardwareId"`
	}
	type env struct {
		Body struct {
			Resp resp `xml:"GetDeviceInformationResponse"`
		} `xml:"Body"`
	}
	var e env
	if err := xml.Unmarshal([]byte(body), &e); err != nil {
		return nil, fmt.Errorf("parse device info: %w", err)
	}
	return &Deviceinfo{
		Manufacturer: e.Body.Resp.Manufacturer,
		Model:        e.Body.Resp.Model,
		FirmwareVer:  e.Body.Resp.FirmwareVer,
		SerialNum:    e.Body.Resp.SerialNum,
		HardwareID:   e.Body.Resp.HardwareID,
	}, nil
}

// parseProfiles 从 GetProfiles 响应中提取通道列表。
// 宇视 NVR 格式: <trt:Profiles token="..." fixed="true"><tt:Name>MediaProfile_Channel1_MainStream</tt:Name>...
// 海康格式类似。
func parseProfiles(body string) []ChannelInfo {
	type profile struct {
		Token string `xml:"token,attr"`
		Fixed string `xml:"fixed,attr"`
		Name  string `xml:"Name"`
		VideoSource struct {
			Token string `xml:"token,attr"`
		} `xml:"VideoSourceConfiguration"`
	}
	type env struct {
		Body struct {
			Resp struct {
				Profiles []profile `xml:"Profiles"`
			} `xml:"GetProfilesResponse"`
		} `xml:"Body"`
	}
	var e env
	if err := xml.Unmarshal([]byte(body), &e); err != nil {
		log.Printf("[ONVIF] parseProfiles error: %v", err)
		return nil
	}

	// 按通道分组（主码流 = 一个通道）
	seen := make(map[int]bool)
	var channels []ChannelInfo
	for _, p := range e.Body.Resp.Profiles {
		ch := extractChannelFromName(p.Name)
		if ch <= 0 || seen[ch] {
			continue
		}
		seen[ch] = true
		channels = append(channels, ChannelInfo{
			Channel:          ch,
			ProfileToken:     p.Token,
			Name:             p.Name,
			VideoSourceToken: p.VideoSource.Token,
		})
	}
	return channels
}

// extractChannelFromName 从 Profile 名称中提取通道号。
// 宇视: "MediaProfile_Channel1_MainStream" → 1
// 海康: "Profile_1" 或 "main_stream_1" → 1
func extractChannelFromName(name string) int {
	re := regexp.MustCompile(`(?i)channel[_]?(\d+)`)
	matches := re.FindStringSubmatch(name)
	if len(matches) >= 2 {
		n := 0
		fmt.Sscanf(matches[1], "%d", &n)
		return n
	}
	// 回退：查找任何数字
	re2 := regexp.MustCompile(`(\d+)`)
	matches2 := re2.FindStringSubmatch(name)
	if len(matches2) >= 2 {
		n := 0
		fmt.Sscanf(matches2[1], "%d", &n)
		return n
	}
	return 0
}

// extractXMLTagValue 从 XML 中提取指定标签的文本值（简单实现，不依赖完整解析）。
func extractXMLTagValue(xmlStr, tag string) string {
	re := regexp.MustCompile(`<(?:\w+:)?` + regexp.QuoteMeta(tag) + `>([^<]+)</`)
	matches := re.FindStringSubmatch(xmlStr)
	if len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}
	return ""
}

// ---------- ONVIF: 通用编码参数调优 ----------

func (s *ONVIFService) optimizeViaONVIF(ctx context.Context, ip, user, pass string, nvrChannel int) error {
	endpoint, err := s.probeDeviceEndpoint(ctx, ip, user, pass)
	if err != nil {
		return err
	}
	getBody := `<?xml version="1.0" encoding="UTF-8"?>
<SOAP-ENV:Envelope xmlns:SOAP-ENV="http://www.w3.org/2003/05/soap-envelope"
                   xmlns:trt="http://www.onvif.org/ver10/media/wsdl">
  <SOAP-ENV:Body>
    <trt:GetVideoEncoderConfigurations/>
  </SOAP-ENV:Body>
</SOAP-ENV:Envelope>`
	respBody, err := s.callONVIF(ctx, endpoint, user, pass, getBody, "http://www.onvif.org/ver10/media/wsdl/GetVideoEncoderConfigurations")
	if err != nil {
		return fmt.Errorf("get encoder config: %w", err)
	}
	token := extractEncoderToken(respBody)
	if token == "" {
		return fmt.Errorf("no encoder configuration token found")
	}
	setBody := buildSetEncoderConfigEnvelope(token)
	_, err = s.callONVIF(ctx, endpoint, user, pass, setBody, "http://www.onvif.org/ver10/media/wsdl/SetVideoEncoderConfiguration")
	return err
}

func extractEncoderToken(body string) string {
	type cfg struct {
		Token string `xml:"token,attr"`
	}
	type env struct {
		XMLName xml.Name
		Body    struct {
			Resp struct {
				Configurations []cfg `xml:"Configurations"`
			} `xml:"GetVideoEncoderConfigurationsResponse"`
		} `xml:"Body"`
	}
	var e env
	if err := xml.Unmarshal([]byte(body), &e); err != nil {
		return ""
	}
	if len(e.Body.Resp.Configurations) == 0 {
		return ""
	}
	return e.Body.Resp.Configurations[0].Token
}

func buildSetEncoderConfigEnvelope(token string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<SOAP-ENV:Envelope xmlns:SOAP-ENV="http://www.w3.org/2003/05/soap-envelope"
                   xmlns:trt="http://www.onvif.org/ver10/media/wsdl"
                   xmlns:tt="http://www.onvif.org/ver10/schema">
  <SOAP-ENV:Body>
    <trt:SetVideoEncoderConfiguration>
      <trt:Configuration token="%s">
        <tt:Name>mainStream</tt:Name>
        <tt:UseCount>1</tt:UseCount>
        <tt:Encoding>H264</tt:Encoding>
        <tt:Resolution>
          <tt:Width>1920</tt:Width>
          <tt:Height>1080</tt:Height>
        </tt:Resolution>
        <tt:Quality>4</tt:Quality>
        <tt:RateControl>
          <tt:FrameRateLimit>25</tt:FrameRateLimit>
          <tt:EncodingInterval>1</tt:EncodingInterval>
          <tt:BitrateLimit>4096</tt:BitrateLimit>
        </tt:RateControl>
        <tt:H264>
          <tt:GovLength>25</tt:GovLength>
          <tt:H264Profile>Main</tt:H264Profile>
        </tt:H264>
        <tt:Multicast>
          <tt:Address>
            <tt:Type>IPv4</tt:Type>
            <tt:IPv4Address>0.0.0.0</tt:IPv4Address>
          </tt:Address>
          <tt:Port>0</tt:Port>
          <tt:TTL>1</tt:TTL>
          <tt:AutoStart>false</tt:AutoStart>
        </tt:Multicast>
        <tt:SessionTimeout>PT60S</tt:SessionTimeout>
      </trt:Configuration>
    </trt:SetVideoEncoderConfiguration>
  </SOAP-ENV:Body>
</SOAP-ENV:Envelope>`, token)
}

// ---------- 海康 ISAPI 低延迟调优 ----------

func (s *ONVIFService) optimizeViaISAPI(ctx context.Context, ip, user, pass string, nvrChannel int) error {
	url := fmt.Sprintf("http://%s/ISAPI/System/Video/encodingChannels", ip)
	if nvrChannel > 0 {
		url = fmt.Sprintf("http://%s/ISAPI/System/Video/encodingChannels?channel=%d", ip, nvrChannel)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(user, pass)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("ISAPI get encodingChannels: %s", resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	newPayload := buildISAPIPayload(data, nvrChannel)
	putReq, err := http.NewRequestWithContext(ctx, http.MethodPut, url, strings.NewReader(newPayload))
	if err != nil {
		return err
	}
	putReq.SetBasicAuth(user, pass)
	putReq.Header.Set("Content-Type", "application/xml")
	putResp, err := http.DefaultClient.Do(putReq)
	if err != nil {
		return err
	}
	defer putResp.Body.Close()
	if putResp.StatusCode >= 400 {
		return fmt.Errorf("ISAPI put encodingChannels: %s", putResp.Status)
	}
	return nil
}

func buildISAPIPayload(original []byte, nvrChannel int) string {
	channelID := 101
	if nvrChannel > 0 {
		channelID = nvrChannel*100 + 1
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<VideoEncodingChannelList xmlns="http://www.isapi.org/ver20/Streaming">
  <VideoEncodingChannel>
    <id>%d</id>
    <channelName>Camera</channelName>
    <Enabled>true</Enabled>
    <Video>
      <fixedQuality>40</fixedQuality>
      <vbrUpperCap>0</vbrUpperCap>
      <maxFrameRate>2500</maxFrameRate>
      <ConstantBitRate>4096</ConstantBitRate>
      <videoResolutionWidth>1920</videoResolutionWidth>
      <videoResolutionHeight>1080</videoResolutionHeight>
      <videoCodecType>H.264</videoCodecType>
      <videoScanType>progressive</videoScanType>
      <videoQualityControlType>CBR</videoQualityControlType>
      <ConstantBitRate>4096</ConstantBitRate>
      <fixedFrameRate>2500</fixedFrameRate>
      <GovLength>25</GovLength>
      <smoothing>0</smoothing>
      <H264Profile>Main</H264Profile>
      <SvcMvcCompatible>false</SvcMvcCompatible>
      <BFrameNum>0</BFrameNum>
      <refNum>0</refNum>
    </Video>
    <SmartCodec>
      <enabled>false</enabled>
    </SmartCodec>
  </VideoEncodingChannel>
</VideoEncodingChannelList>`, channelID)
}

// ---------- ONVIF 通用 HTTP 调用 (含 WS-UsernameToken 鉴权) ----------

func (s *ONVIFService) callONVIF(ctx context.Context, endpoint, user, pass, body, action string) (string, error) {
	var finalBody string
	if user != "" {
		finalBody = s.injectSecurityHeader(body, user, pass, endpoint, action)
	} else {
		finalBody = body
	}

	log.Printf("[ONVIF] >>> %s action=%s", endpoint, action)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(finalBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/soap+xml; charset=utf-8")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 400 {
		log.Printf("[ONVIF] <<< %s %s", resp.Status, string(data))
		return "", fmt.Errorf("ONVIF %s: %s\n%s", endpoint, resp.Status, string(data))
	}
	log.Printf("[ONVIF] <<< %s OK (%d bytes)", endpoint, len(data))
	return string(data), nil
}

// envelopeTagRe 匹配 Envelope 开始标签（支持任意前缀，含连字符如 SOAP-ENV）。
var envelopeTagRe = regexp.MustCompile(`<(?:[\w-]+:)?Envelope\b[^>]*>`)

// injectSecurityHeader 在 SOAP Envelope 的 Header 中插入 WS-UsernameToken。
//
// 关键修复:
// 1. 用正则定位 <SOAP-ENV:Envelope> 标签末尾，不再误匹配 XML 声明的 >
// 2. 使用显式 wsse:/wsu: 命名空间前缀
// 3. 使用 SHA1 Digest（ONVIF PasswordDigest 标准）
// 4. Header 前缀与 Envelope 保持一致（SOAP-ENV:）
func (s *ONVIFService) injectSecurityHeader(envelope, user, pass, endpoint, action string) string {
	nonce := generateNonce()
	created := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	// ONVIF 标准: Digest = Base64(SHA1(nonce + created + password))
	digest := computeWSDigest(nonce, created, pass)
	nonceB64 := base64.StdEncoding.EncodeToString([]byte(nonce))

	// 检测 Envelope 使用的前缀（SOAP-ENV 或 s 或 soap）
	envPrefix := detectEnvelopePrefix(envelope)

	// 查找 Envelope 标签的结束位置
	loc := envelopeTagRe.FindStringIndex(envelope)
	if loc == nil {
		return envelope
	}
	insertAt := loc[1]

	// 构造 WS-Security Header（不含 WS-Addressing，宇视设备对 wsa 兼容性差）
	securityHeader := fmt.Sprintf(`
  <%s:Header>
    <wsse:Security xmlns:wsse="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd"
                   xmlns:wsu="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-utility-1.0.xsd">
      <wsse:UsernameToken>
        <wsse:Username>%s</wsse:Username>
        <wsse:Password Type="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-username-token-profile-1.0#PasswordDigest">%s</wsse:Password>
        <wsse:Nonce EncodingType="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-soap-message-security-1.0#Base64Binary">%s</wsse:Nonce>
        <wsu:Created>%s</wsu:Created>
      </wsse:UsernameToken>
    </wsse:Security>
  </%s:Header>`, envPrefix, xmlEscape(user), digest, nonceB64, created, envPrefix)

	return envelope[:insertAt] + securityHeader + envelope[insertAt:]
}

// detectEnvelopePrefix 从 Envelope 标签中提取前缀。
// 例如 `<SOAP-ENV:Envelope ...>` 返回 "SOAP-ENV"，`<s:Envelope ...>` 返回 "s"。
func detectEnvelopePrefix(envelope string) string {
	loc := envelopeTagRe.FindStringIndex(envelope)
	if loc == nil {
		return "SOAP-ENV"
	}
	tag := envelope[loc[0]:loc[1]]
	// 去掉 '<' 和 ':'
	if idx := strings.Index(tag, ":"); idx > 0 {
		return tag[1:idx]
	}
	return "SOAP-ENV"
}

// computeWSDigest 计算 WS-UsernameToken PasswordDigest (SHA1)。
func computeWSDigest(nonce, created, password string) string {
	h := sha1.New()
	h.Write([]byte(nonce))
	h.Write([]byte(created))
	h.Write([]byte(password))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func generateNonce() string {
	b := make([]byte, 16)
	for i := range b {
		b[i] = byte(rand.Intn(256))
	}
	return string(b)
}

func xmlEscape(s string) string {
	var buf bytes.Buffer
	if err := xml.EscapeText(&buf, []byte(s)); err != nil {
		return s
	}
	return buf.String()
}
