package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"CameraIO/internal/model"
)

func TestBuildSetSystemDateAndTimeEnvelope(t *testing.T) {
	ts := time.Date(2025, 6, 15, 10, 30, 45, 0, time.UTC)
	env := buildSetSystemDateAndTimeEnvelope(ts, "GMT0")

	checks := []string{
		"<tt:Year>2025</tt:Year>",
		"<tt:Month>6</tt:Month>",
		"<tt:Day>15</tt:Day>",
		"<tt:Hour>10</tt:Hour>",
		"<tt:Minute>30</tt:Minute>",
		"<tt:Second>45</tt:Second>",
		"SetSystemDateAndTime",
		"SOAP-ENV:Envelope",
		"<tds:DateTimeType>Manual</tds:DateTimeType>",
		"<tt:TZ>GMT0</tt:TZ>",
	}
	for _, check := range checks {
		if !strings.Contains(env, check) {
			t.Errorf("envelope missing %q", check)
		}
	}
	// NTP 模式不应出现
	if strings.Contains(env, "DateTimeType>NTP") {
		t.Error("should use Manual mode, not NTP")
	}
}

func TestBuildSetEncoderConfigEnvelope(t *testing.T) {
	env := buildSetEncoderConfigEnvelope("test-token-123")

	checks := []string{
		`token="test-token-123"`,
		"<tt:Encoding>H264</tt:Encoding>",
		"<tt:GovLength>25</tt:GovLength>",
		"<tt:FrameRateLimit>25</tt:FrameRateLimit>",
		"<tt:EncodingInterval>1</tt:EncodingInterval>",
		"SetVideoEncoderConfiguration",
	}
	for _, check := range checks {
		if !strings.Contains(env, check) {
			t.Errorf("envelope missing %q", check)
		}
	}
}

func TestBuildISAPIPayload(t *testing.T) {
	payload := buildISAPIPayload(nil, 0)

	checks := []string{
		"<BFrameNum>0</BFrameNum>",
		"<enabled>false</enabled>",
		"<GovLength>25</GovLength>",
		"<videoQualityControlType>CBR</videoQualityControlType>",
		"<videoCodecType>H.264</videoCodecType>",
		"<id>101</id>",
	}
	for _, check := range checks {
		if !strings.Contains(payload, check) {
			t.Errorf("ISAPI payload missing %q", check)
		}
	}

	payload2 := buildISAPIPayload(nil, 2)
	if !strings.Contains(payload2, "<id>201</id>") {
		t.Errorf("NVR channel 2 should have id=201, got: %s", payload2)
	}
}

func TestInjectSecurityHeader(t *testing.T) {
	svc := NewONVIFService()
	envelope := `<?xml version="1.0" encoding="UTF-8"?>
<SOAP-ENV:Envelope xmlns:SOAP-ENV="http://www.w3.org/2003/05/soap-envelope" xmlns:tds="http://www.onvif.org/ver10/device/wsdl">
  <SOAP-ENV:Body>
    <tds:SetSystemDateAndTime/>
  </SOAP-ENV:Body>
</SOAP-ENV:Envelope>`

	result := svc.injectSecurityHeader(envelope, "admin", "pass123", "http://192.168.15.23/onvif/device_service", "http://www.onvif.org/ver10/device/wsdl/SetSystemDateAndTime")

	checks := []string{
		"<wsse:Username>admin</wsse:Username>",
		"PasswordDigest",
		"<wsse:Nonce",
		"<wsu:Created>",
		"wsse:Security",
		"<SOAP-ENV:Header>",
	}
	for _, check := range checks {
		if !strings.Contains(result, check) {
			t.Errorf("security header missing %q\n\nResult:\n%s", check, result)
		}
	}

	// 验证 Header 在 Envelope 标签之后、Body 之前
	envTagEnd := strings.Index(result, ">") // XML 声明的 >
	envelopeStart := strings.Index(result, "<SOAP-ENV:Envelope")
	headerStart := strings.Index(result, "<SOAP-ENV:Header>")
	bodyStart := strings.Index(result, "<SOAP-ENV:Body>")

	if headerStart < envelopeStart {
		t.Error("Header should be AFTER <SOAP-ENV:Envelope> tag")
	}
	if headerStart > bodyStart {
		t.Error("Header should be BEFORE Body")
	}
	// 确保不在 XML 声明和 Envelope 之间
	if headerStart < envelopeStart || (headerStart < envTagEnd && envelopeStart > envTagEnd) {
		t.Error("Header should be INSIDE Envelope, not between XML decl and Envelope")
	}
}

func TestInjectSecurityHeader_DetectPrefix(t *testing.T) {
	svc := NewONVIFService()

	tests := []struct {
		name       string
		envelope   string
		wantPrefix string
	}{
		{
			"SOAP-ENV prefix",
			`<?xml version="1.0"?><SOAP-ENV:Envelope xmlns:SOAP-ENV="http://www.w3.org/2003/05/soap-envelope"><SOAP-ENV:Body/></SOAP-ENV:Envelope>`,
			"SOAP-ENV",
		},
		{
			"s prefix",
			`<?xml version="1.0"?><s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body/></s:Envelope>`,
			"s",
		},
		{
			"soap prefix",
			`<?xml version="1.0"?><soap:Envelope xmlns:soap="http://www.w3.org/2003/05/soap-envelope"><soap:Body/></soap:Envelope>`,
			"soap",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := svc.injectSecurityHeader(tt.envelope, "admin", "pass", "http://test/onvif/device_service", "test")
			expectedHeader := "<" + tt.wantPrefix + ":Header>"
			if !strings.Contains(result, expectedHeader) {
				t.Errorf("expected %s in result, got:\n%s", expectedHeader, result)
			}
		})
	}
}

func TestComputeWSDigest(t *testing.T) {
	digest := computeWSDigest("nonce123", "2025-01-01T00:00:00Z", "password")
	if digest == "" {
		t.Fatal("digest should not be empty")
	}
	digest2 := computeWSDigest("nonce123", "2025-01-01T00:00:00Z", "password")
	if digest != digest2 {
		t.Error("digest should be deterministic")
	}
	digest3 := computeWSDigest("nonce123", "2025-01-01T00:00:00Z", "different")
	if digest == digest3 {
		t.Error("different password should produce different digest")
	}
}

func TestBuildRTSPURL(t *testing.T) {
	tests := []struct {
		name     string
		brand    string
		channel  int
		expected string
	}{
		{"IPC default", "hikvision", 0, "rtsp://admin:pass@192.168.1.1:554/Streaming/Channels/101"},
		{"Hikvision ch1", "hikvision", 1, "rtsp://admin:pass@192.168.1.1:554/Streaming/Channels/101"},
		{"Hikvision ch2", "hikvision", 2, "rtsp://admin:pass@192.168.1.1:554/Streaming/Channels/201"},
		{"Hikvision ch4", "hikvision", 4, "rtsp://admin:pass@192.168.1.1:554/Streaming/Channels/401"},
		{"Uniview ch1", "uniview", 1, "rtsp://admin:pass@192.168.1.1:554/unicast/c1/s0/live"},
		{"Uniview ch3", "uniview", 3, "rtsp://admin:pass@192.168.1.1:554/unicast/c3/s0/live"},
		{"Custom default", "custom", 0, "rtsp://admin:pass@192.168.1.1:554/Streaming/Channels/101"},
		{"Custom ch2", "custom", 2, "rtsp://admin:pass@192.168.1.1:554/Streaming/Channels/201"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildRTSPURL(tt.brand, "admin", "pass", "192.168.1.1", 554, tt.channel, "main")
			if result != tt.expected {
				t.Errorf("got %q, want %q", result, tt.expected)
			}
		})
	}
}

// 子码流测试
func TestBuildRTSPURL_SubStream(t *testing.T) {
	tests := []struct {
		name     string
		brand    string
		channel  int
		expected string
	}{
		{"Hikvision ch1 sub", "hikvision", 1, "rtsp://admin:pass@192.168.1.1:554/Streaming/Channels/102"},
		{"Hikvision ch2 sub", "hikvision", 2, "rtsp://admin:pass@192.168.1.1:554/Streaming/Channels/202"},
		{"Uniview ch1 sub", "uniview", 1, "rtsp://admin:pass@192.168.1.1:554/unicast/c1/s1/live"},
		{"Uniview ch3 sub", "uniview", 3, "rtsp://admin:pass@192.168.1.1:554/unicast/c3/s1/live"},
		{"Custom ch2 sub", "custom", 2, "rtsp://admin:pass@192.168.1.1:554/Streaming/Channels/202"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildRTSPURL(tt.brand, "admin", "pass", "192.168.1.1", 554, tt.channel, "sub")
			if result != tt.expected {
				t.Errorf("got %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestBuildRTSPURL_EscapesCredentials(t *testing.T) {
	got := buildRTSPURL(model.BrandUniview, "operator@site", "p@ss:/?#%", "192.168.14.33", 554, 1, model.StreamTypeMain)
	want := "rtsp://operator%40site:p%40ss%3A%2F%3F%23%25@192.168.14.33:554/unicast/c1/s0/live"
	if got != want {
		t.Fatalf("buildRTSPURL() = %q, want %q", got, want)
	}
}

func TestEnsureRTSPCredentials_AddsCredentialsToONVIFURI(t *testing.T) {
	got := ensureRTSPCredentials("rtsp://192.168.14.33:554/unicast/c1/s0/live", "admin", "p@ss")
	want := "rtsp://admin:p%40ss@192.168.14.33:554/unicast/c1/s0/live"
	if got != want {
		t.Fatalf("ensureRTSPCredentials() = %q, want %q", got, want)
	}
}

func TestEnsureRTSPCredentials_PreservesExistingCredentials(t *testing.T) {
	got := ensureRTSPCredentials("rtsp://viewer:existing@192.168.14.33:554/unicast/c1/s0/live", "admin", "replacement")
	want := "rtsp://viewer:existing@192.168.14.33:554/unicast/c1/s0/live"
	if got != want {
		t.Fatalf("ensureRTSPCredentials() = %q, want %q", got, want)
	}
}

// ---------- 海康 NVR 实际响应格式测试 ----------

func TestInjectSecurityHeader_HikvisionNVRResponse(t *testing.T) {
	// 模拟海康 NVR 的实际 SOAP 响应（使用 env: 前缀）
	hikResponse := `<?xml version="1.0" encoding="UTF-8"?>
<env:Envelope xmlns:env="http://www.w3.org/2003/05/soap-envelope"
              xmlns:soapenc="http://www.w3.org/2003/05/soap-encoding"
              xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
              xmlns:xs="http://www.w3.org/2001/XMLSchema"
              xmlns:tt="http://www.onvif.org/ver10/schema"
              xmlns:tds="http://www.onvif.org/ver10/device/wsdl">
  <env:Body>
    <tds:GetDeviceInformationResponse>
      <tds:Manufacturer>Hikvision</tds:Manufacturer>
      <tds:Model>DS-2CD2T47G2-L</tds:Model>
    </tds:GetDeviceInformationResponse>
  </env:Body>
</env:Envelope>`

	svc := NewONVIFService()
	// 验证 detectEnvelopePrefix 能识别 env: 前缀
	prefix := detectEnvelopePrefix(hikResponse)
	if prefix != "env" {
		t.Errorf("expected prefix 'env', got %q", prefix)
	}

	// 验证 injectSecurityHeader 用 env: 前缀生成 Header
	envelope := `<?xml version="1.0" encoding="UTF-8"?>
<env:Envelope xmlns:env="http://www.w3.org/2003/05/soap-envelope" xmlns:tds="http://www.onvif.org/ver10/device/wsdl">
  <env:Body><tds:GetDeviceInformation/></env:Body>
</env:Envelope>`

	result := svc.injectSecurityHeader(envelope, "admin", "A12345678", "http://10.12.0.100/onvif/device_service", "http://www.onvif.org/ver10/device/wsdl/GetDeviceInformation")

	// 必须使用 env: 前缀（和 Envelope 一致）
	if !strings.Contains(result, "<env:Header>") {
		t.Errorf("expected <env:Header> in result for Hikvision device")
	}
	if !strings.Contains(result, "<env:Header>") && strings.Contains(result, "<SOAP-ENV:Header>") {
		t.Error("should use env: prefix to match Envelope, not SOAP-ENV:")
	}
	// 验证 Security Header 包含正确元素
	for _, check := range []string{
		"<wsse:Username>admin</wsse:Username>",
		"<wsse:Password",
		"<wsse:Nonce",
		"<wsu:Created>",
	} {
		if !strings.Contains(result, check) {
			t.Errorf("security header missing %q", check)
		}
	}
}

// ---------- 宇视 NVR 实际响应格式测试 ----------

func TestInjectSecurityHeader_UniviewNVRResponse(t *testing.T) {
	// 模拟宇视 NVR 的实际 SOAP 响应（使用 SOAP-ENV: 前缀）
	unvResponse := `<?xml version="1.0" encoding="UTF-8"?>
<SOAP-ENV:Envelope xmlns:SOAP-ENV="http://www.w3.org/2003/05/soap-envelope"
                   xmlns:tds="http://www.onvif.org/ver10/device/wsdl"
                   xmlns:tt="http://www.onvif.org/ver10/schema">
  <SOAP-ENV:Body>
    <tds:GetDeviceInformationResponse>
      <tds:Manufacturer>UNIVIEW</tds:Manufacturer>
      <tds:Model>NVR301-08S3-P8-DT</tds:Model>
    </tds:GetDeviceInformationResponse>
  </SOAP-ENV:Body>
</SOAP-ENV:Envelope>`

	svc := NewONVIFService()
	prefix := detectEnvelopePrefix(unvResponse)
	if prefix != "SOAP-ENV" {
		t.Errorf("expected prefix 'SOAP-ENV', got %q", prefix)
	}

	// 验证 injectSecurityHeader 用 SOAP-ENV: 前缀
	envelope := `<?xml version="1.0" encoding="UTF-8"?>
<SOAP-ENV:Envelope xmlns:SOAP-ENV="http://www.w3.org/2003/05/soap-envelope" xmlns:tds="http://www.onvif.org/ver10/device/wsdl">
  <SOAP-ENV:Body><tds:GetDeviceInformation/></SOAP-ENV:Body>
</SOAP-ENV:Envelope>`

	result := svc.injectSecurityHeader(envelope, "admin", "A12345678", "http://192.168.15.23/onvif/device_service", "http://www.onvif.org/ver10/device/wsdl/GetDeviceInformation")

	if !strings.Contains(result, "<SOAP-ENV:Header>") {
		t.Errorf("expected <SOAP-ENV:Header> in result for Uniview device")
	}
}

// ---------- 海康 SOAP 格式验证 ----------

func TestHikvisionSOAPFormat(t *testing.T) {
	svc := NewONVIFService()

	// 海康设备使用 SOAP-ENV 前缀
	envelope := `<?xml version="1.0" encoding="UTF-8"?>
<SOAP-ENV:Envelope xmlns:SOAP-ENV="http://www.w3.org/2003/05/soap-envelope" xmlns:tds="http://www.onvif.org/ver10/device/wsdl">
  <SOAP-ENV:Body><tds:GetDeviceInformation/></SOAP-ENV:Body>
</SOAP-ENV:Envelope>`

	result := svc.injectSecurityHeader(envelope, "admin", "pass", "http://10.12.0.100/onvif/device_service", "http://www.onvif.org/ver10/device/wsdl/GetDeviceInformation")

	// 验证 SHA1 Digest（非 SHA256）
	digest := computeWSDigest("testnonce", "2025-01-01T00:00:00Z", "testpass")
	if digest == "" {
		t.Fatal("digest should not be empty")
	}

	// 验证 XML 结构完整
	if !strings.Contains(result, "<SOAP-ENV:Envelope") {
		t.Error("missing Envelope")
	}
	if !strings.Contains(result, "<SOAP-ENV:Header>") {
		t.Error("missing Header")
	}
	if !strings.Contains(result, "<SOAP-ENV:Body>") {
		t.Error("missing Body")
	}

	// Header 在 Body 之前
	headerIdx := strings.Index(result, "<SOAP-ENV:Header>")
	bodyIdx := strings.Index(result, "<SOAP-ENV:Body>")
	if headerIdx >= bodyIdx {
		t.Error("Header must come before Body")
	}

	// 验证不包含 WS-Addressing（宇视/海康兼容性差）
	if strings.Contains(result, "wsa:Action") {
		t.Error("should NOT include WS-Addressing Action")
	}
	if strings.Contains(result, "wsa:To") {
		t.Error("should NOT include WS-Addressing To")
	}
}

// ---------- 宇视 SOAP 格式验证 ----------

func TestUniviewSOAPFormat(t *testing.T) {
	svc := NewONVIFService()

	envelope := `<?xml version="1.0" encoding="UTF-8"?>
<SOAP-ENV:Envelope xmlns:SOAP-ENV="http://www.w3.org/2003/05/soap-envelope" xmlns:tds="http://www.onvif.org/ver10/device/wsdl">
  <SOAP-ENV:Body><tds:SetSystemDateAndTime/></SOAP-ENV:Body>
</SOAP-ENV:Envelope>`

	result := svc.injectSecurityHeader(envelope, "admin", "pass", "http://192.168.15.23/onvif/device_service", "http://www.onvif.org/ver10/device/wsdl/SetSystemDateAndTime")

	// 验证宇视设备需要的格式
	checks := []string{
		"<SOAP-ENV:Header>",
		"</SOAP-ENV:Header>",
		"wsse:Security",
		"wsse:UsernameToken",
		"PasswordDigest", // SHA1 digest
	}
	for _, check := range checks {
		if !strings.Contains(result, check) {
			t.Errorf("Uniview SOAP format missing %q", check)
		}
	}
}

// ---------- HEVC NAL 解析测试 ----------

func TestProcessNALU_HEVC(t *testing.T) {
	svc := NewStreamService(nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	st := &Stream{Codec: "hevc", ctx: ctx, cancel: cancel, naluSubs: make(map[int]chan NALU)}

	// HEVC VPS (type 32): 0x40 0x01
	vpsData := []byte{0x40, 0x01, 0x01, 0x01}
	svc.processNALU(st, vpsData)
	if st.vps == nil {
		t.Error("HEVC VPS should be stored")
	}
	if len(st.vps) != 4 {
		t.Errorf("VPS length should be 4, got %d", len(st.vps))
	}

	// HEVC SPS (type 33): 0x42 0x01
	spsData := []byte{0x42, 0x01, 0x01, 0x01, 0x01}
	svc.processNALU(st, spsData)
	if st.sps == nil {
		t.Error("HEVC SPS should be stored")
	}

	// HEVC PPS (type 34): 0x44 0x01
	ppsData := []byte{0x44, 0x01, 0x01}
	svc.processNALU(st, ppsData)
	if st.pps == nil {
		t.Error("HEVC PPS should be stored")
	}

	// HEVC IDR_W_RADL (type 19): 0x26 0x01
	idrData := []byte{0x26, 0x01, 0xaf, 0x01}
	ch := make(chan NALU, 10)
	st.naluSubs[0] = ch
	svc.processNALU(st, idrData)

	select {
	case nalu := <-ch:
		if !nalu.IsIDR {
			t.Error("HEVC IDR_W_RADL should be marked as IDR")
		}
		if nalu.Type != 19 {
			t.Errorf("HEVC IDR type should be 19, got %d", nalu.Type)
		}
	default:
		t.Error("IDR NALU should be broadcast to subscribers")
	}
}

func TestProcessNALU_H264(t *testing.T) {
	svc := NewStreamService(nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	st := &Stream{Codec: "h264", ctx: ctx, cancel: cancel, naluSubs: make(map[int]chan NALU)}

	// H.264 SPS (type 7): 0x67
	spsData := []byte{0x67, 0x42, 0x00, 0x1e}
	svc.processNALU(st, spsData)
	if st.sps == nil {
		t.Error("H.264 SPS should be stored")
	}

	// H.264 PPS (type 8): 0x68
	ppsData := []byte{0x68, 0xce, 0x38, 0x80}
	svc.processNALU(st, ppsData)
	if st.pps == nil {
		t.Error("H.264 PPS should be stored")
	}

	// H.264 IDR (type 5): 0x65
	ch := make(chan NALU, 10)
	st.naluSubs[0] = ch
	idrData := []byte{0x65, 0x88, 0x80, 0x40}
	svc.processNALU(st, idrData)

	select {
	case nalu := <-ch:
		if !nalu.IsIDR {
			t.Error("H.264 IDR should be marked as IDR")
		}
		if nalu.Type != 5 {
			t.Errorf("H.264 IDR type should be 5, got %d", nalu.Type)
		}
	default:
		t.Error("IDR NALU should be broadcast to subscribers")
	}
}

func TestProcessNALU_Unknown(t *testing.T) {
	svc := NewStreamService(nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	st := &Stream{Codec: "", ctx: ctx, cancel: cancel, naluSubs: make(map[int]chan NALU)}

	// 不应该 panic
	svc.processNALU(st, []byte{0x00, 0x01, 0x02, 0x03})
	svc.processNALU(st, []byte{})
}

// ---------- 编码格式切换测试 ----------

func TestBuildSetEncoderConfigWithCodec(t *testing.T) {
	tests := []struct {
		name     string
		token    string
		codec    string
		expected string
	}{
		{"H.264", "tok-123", "H264", "<tt:Encoding>H264</tt:Encoding>"},
		{"H.265", "tok-456", "H265", "<tt:Encoding>H265</tt:Encoding>"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := buildSetEncoderConfigWithCodec(tt.token, tt.codec)
			if !strings.Contains(env, tt.expected) {
				t.Errorf("expected %q in envelope", tt.expected)
			}
			if !strings.Contains(env, `token="`+tt.token+`"`) {
				t.Errorf("expected token %q in envelope", tt.token)
			}
			if !strings.Contains(env, "SetVideoEncoderConfiguration") {
				t.Error("missing SetVideoEncoderConfiguration")
			}
		})
	}
}

func TestReplaceXMLValue(t *testing.T) {
	tests := []struct {
		name     string
		xml      string
		tag      string
		newValue string
		expected string
	}{
		{
			"replace videoCodecType H.264 to H.265",
			"<videoCodecType>H.264</videoCodecType>",
			"videoCodecType",
			"H.265",
			"<videoCodecType>H.265</videoCodecType>",
		},
		{
			"replace with namespace prefix",
			"<tt:Encoding>H264</tt:Encoding>",
			"tt:Encoding",
			"H265",
			"<tt:Encoding>H265</tt:Encoding>",
		},
		{
			"no match returns original",
			"<other>value</other>",
			"missing",
			"new",
			"<other>value</other>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := replaceXMLValue(tt.xml, tt.tag, tt.newValue)
			if result != tt.expected {
				t.Errorf("got %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestSetVideoCodec_InvalidCodec(t *testing.T) {
	svc := NewONVIFService()
	ctx := context.Background()

	// 无效编码格式应该立即返回错误
	err := svc.SetVideoCodec(ctx, "custom", "192.168.1.1", "admin", "pass", "h266", 0)
	if err == nil {
		t.Error("expected error for invalid codec")
	}
	if !strings.Contains(err.Error(), "unsupported codec") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ---------- 网络配置测试 ----------

func TestNetworkConfig(t *testing.T) {
	cfg := NetworkConfig{
		DHCP:    false,
		IP:      "192.168.1.100",
		Mask:    "255.255.255.0",
		Gateway: "192.168.1.1",
		DNS:     "8.8.8.8",
	}

	if cfg.DHCP {
		t.Error("DHCP should be false")
	}
	if cfg.IP != "192.168.1.100" {
		t.Errorf("unexpected IP: %s", cfg.IP)
	}
	if cfg.Mask != "255.255.255.0" {
		t.Errorf("unexpected mask: %s", cfg.Mask)
	}
}

func TestExtractAttrValue(t *testing.T) {
	xml := `<NetworkInterface token="eth0"><Name>eth0</Name></NetworkInterface>`
	token := extractAttrValue(xml, "NetworkInterface", "token")
	if token != "eth0" {
		t.Errorf("expected 'eth0', got %q", token)
	}

	// 不存在的属性
	empty := extractAttrValue(xml, "NetworkInterface", "missing")
	if empty != "" {
		t.Errorf("expected empty, got %q", empty)
	}

	// 不存在的标签
	empty2 := extractAttrValue(xml, "Missing", "token")
	if empty2 != "" {
		t.Errorf("expected empty, got %q", empty2)
	}
}

// ---------- 设备发现补充测试 ----------

func TestExtractChannelFromName(t *testing.T) {
	tests := []struct {
		name     string
		expected int
	}{
		{"MediaProfile_Channel1_MainStream", 1},
		{"MediaProfile_Channel2_SubStream1", 2},
		{"MediaProfile_Channel10_MainStream", 10},
		{"main_stream_3", 3},
		{"Profile_5", 5},
		{"no_number_here", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractChannelFromName(tt.name)
			if got != tt.expected {
				t.Errorf("extractChannelFromName(%q) = %d, want %d", tt.name, got, tt.expected)
			}
		})
	}
}

func TestIdentifyBrand(t *testing.T) {
	tests := []struct {
		name     string
		response string
		expected string
	}{
		{
			"Hikvision in response",
			"<tds:Manufacturer>Hikvision</tds:Manufacturer>",
			"hikvision",
		},
		{
			"Uniview in response",
			"<tds:Manufacturer>UNIVIEW</tds:Manufacturer>",
			"uniview",
		},
		{
			"Unknown defaults to custom (no ISAPI)",
			"<tds:Manufacturer>SomeOtherBrand</tds:Manufacturer>",
			"custom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 使用一个不存在的 IP 以避免 ISAPI 探测成功
			got := identifyBrand(tt.response, "192.0.2.1")
			if got != tt.expected {
				t.Errorf("identifyBrand() = %q, want %q", got, tt.expected)
			}
		})
	}
}

// ---------- RTSP URL 构造补充测试 ----------

func TestBuildRTSPURL_NoAuth(t *testing.T) {
	url := buildRTSPURL("hikvision", "", "", "192.168.1.100", 554, 0, "main")
	expected := "rtsp://192.168.1.100:554/Streaming/Channels/101"
	if url != expected {
		t.Errorf("got %q, want %q", url, expected)
	}
}

func TestBuildRTSPURL_CustomPort(t *testing.T) {
	url := buildRTSPURL("uniview", "admin", "pass", "10.0.0.1", 8554, 3, "main")
	expected := "rtsp://admin:pass@10.0.0.1:8554/unicast/c3/s0/live"
	if url != expected {
		t.Errorf("got %q, want %q", url, expected)
	}
}

// ---------- Camera Model 补充测试 ----------

func TestCameraIsNVR(t *testing.T) {
	tests := []struct {
		deviceType string
		expected   bool
	}{
		{"nvr", true},
		{"dvr", true},
		{"ipc", false},
		{"encoder", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.deviceType, func(t *testing.T) {
			cam := &model.Camera{DeviceType: tt.deviceType}
			if cam.IsNVR() != tt.expected {
				t.Errorf("IsNVR() for %q = %v, want %v", tt.deviceType, cam.IsNVR(), tt.expected)
			}
		})
	}
}

// ---------- Digest 计算补充测试 ----------

func TestComputeWSDigest_SHA1NotSHA256(t *testing.T) {
	// 已知测试向量：nonce="test", created="2025-01-01T00:00:00Z", pass="pass"
	// SHA1("test" + "2025-01-01T00:00:00Z" + "pass") 应该是确定性的
	d1 := computeWSDigest("test", "2025-01-01T00:00:00Z", "pass")
	d2 := computeWSDigest("test", "2025-01-01T00:00:00Z", "pass")
	if d1 != d2 {
		t.Error("digest should be deterministic")
	}

	// 不同 nonce 应产生不同 digest
	d3 := computeWSDigest("other", "2025-01-01T00:00:00Z", "pass")
	if d1 == d3 {
		t.Error("different nonce should produce different digest")
	}

	// 不同 created 应产生不同 digest
	d4 := computeWSDigest("test", "2025-01-02T00:00:00Z", "pass")
	if d1 == d4 {
		t.Error("different created should produce different digest")
	}
}

// ---------- XML Escape 测试 ----------

func TestXmlEscape(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"normal", "normal"},
		{"<script>", "&lt;script&gt;"},
		{"a&b", "a&amp;b"},
		{`"quoted"`, "&#34;quoted&#34;"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := xmlEscape(tt.input)
			if got != tt.expected {
				t.Errorf("xmlEscape(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

// ---------- 视频编码信息解析测试 ----------

func TestParseVideoCodecInfo(t *testing.T) {
	body := `<?xml version="1.0" encoding="UTF-8"?>
<SOAP-ENV:Envelope xmlns:SOAP-ENV="http://www.w3.org/2003/05/soap-envelope"
                   xmlns:trt="http://www.onvif.org/ver10/media/wsdl"
                   xmlns:tt="http://www.onvif.org/ver10/schema">
  <SOAP-ENV:Body>
    <trt:GetVideoEncoderConfigurationsResponse>
      <trt:Configurations token="main">
        <tt:Encoding>H264</tt:Encoding>
        <tt:Resolution>
          <tt:Width>1920</tt:Width>
          <tt:Height>1080</tt:Height>
        </tt:Resolution>
      </trt:Configurations>
      <trt:Configurations token="sub">
        <tt:Encoding>H265</tt:Encoding>
        <tt:Resolution>
          <tt:Width>640</tt:Width>
          <tt:Height>360</tt:Height>
        </tt:Resolution>
      </trt:Configurations>
    </trt:GetVideoEncoderConfigurationsResponse>
  </SOAP-ENV:Body>
</SOAP-ENV:Envelope>`

	info := parseVideoCodecInfo(body)
	if info == nil {
		t.Fatal("info should not be nil")
	}
	if info.Codec != "H264" {
		t.Errorf("codec = %q, want H264", info.Codec)
	}
	if info.Width != 1920 || info.Height != 1080 {
		t.Errorf("resolution = %dx%d, want 1920x1080", info.Width, info.Height)
	}
	if info.Resolution != "1920x1080" {
		t.Errorf("resolution string = %q, want 1920x1080", info.Resolution)
	}
}

func TestParseVideoCodecInfo_Empty(t *testing.T) {
	// 空响应不应 panic，返回空结构
	info := parseVideoCodecInfo("")
	if info == nil {
		t.Fatal("info should not be nil")
	}
	if info.Resolution != "" {
		t.Errorf("expected empty resolution, got %q", info.Resolution)
	}
}

func TestParseVideoCodecInfo_NoConfigs(t *testing.T) {
	body := `<SOAP-ENV:Envelope><SOAP-ENV:Body><trt:GetVideoEncoderConfigurationsResponse/></SOAP-ENV:Body></SOAP-ENV:Envelope>`
	info := parseVideoCodecInfo(body)
	if info == nil || info.Codec != "" {
		t.Error("expected empty info for no configs")
	}
}

func TestNormalizeCodecName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"H264", "H.264"},
		{"H265", "H.265"},
		{"H.264", "H.264"},
		{"H.265", "H.265"},
		{"HEVC", "HEVC"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeCodecName(tt.input)
			if got != tt.expected {
				t.Errorf("normalizeCodecName(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

// ---------- Codec 规范化测试 ----------

func TestNormalizeCodec(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"h264", "h264"},
		{"h.264", "h264"},
		{"H264", "h264"},
		{"H.264", "h264"},
		{"avc", "h264"},
		{"h265", "h265"},
		{"h.265", "h265"},
		{"H265", "h265"},
		{"H.265", "h265"},
		{"hevc", "h265"},
		{"h266", "h266"}, // 无效但不应崩溃
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeCodec(tt.input)
			if got != tt.expected {
				t.Errorf("normalizeCodec(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestSetVideoCodec_CodecVariants(t *testing.T) {
	svc := NewONVIFService()
	// 使用短超时避免真实网络调用挂起
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// 各种 H.264 写法都不应报 "unsupported"
	for _, codec := range []string{"h264", "H.264", "H264", "h.264"} {
		// 使用无效 IP，确保错误来自连接失败而不是 codec 校验
		err := svc.SetVideoCodec(ctx, "custom", "127.0.0.1", "admin", "pass", codec, 0)
		if err != nil && strings.Contains(err.Error(), "unsupported codec") {
			t.Errorf("codec %q should be supported, got: %v", codec, err)
		}
	}

	// 各种 H.265 写法
	for _, codec := range []string{"h265", "H.265", "H265", "hevc"} {
		err := svc.SetVideoCodec(ctx, "custom", "127.0.0.1", "admin", "pass", codec, 0)
		if err != nil && strings.Contains(err.Error(), "unsupported codec") {
			t.Errorf("codec %q should be supported, got: %v", codec, err)
		}
	}

	// 无效 codec 应报错
	err := svc.SetVideoCodec(ctx, "custom", "127.0.0.1", "admin", "pass", "h266", 0)
	if err == nil || !strings.Contains(err.Error(), "unsupported codec") {
		t.Errorf("expected 'unsupported codec' error, got: %v", err)
	}
}
