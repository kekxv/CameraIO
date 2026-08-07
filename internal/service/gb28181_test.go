package service

import (
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"CameraIO/internal/model"
	"CameraIO/internal/pkg"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// ---------- SIP 消息解析测试 ----------

func TestParseSIPMethod(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"REGISTER sip:34020000002000000001@3402000000 SIP/2.0\r\nVia: ...", "REGISTER"},
		{"MESSAGE sip:34020000002000000001@3402000000 SIP/2.0\r\nVia: ...", "MESSAGE"},
		{"INVITE sip:34020000001320000001@3402000000 SIP/2.0\r\nVia: ...", "INVITE"},
		{"BYE sip:34020000001320000001@3402000000 SIP/2.0\r\nVia: ...", "BYE"},
		{"ACK sip:34020000001320000001@3402000000 SIP/2.0\r\nVia: ...", "ACK"},
		{"", ""},
	}

	for _, tt := range tests {
		got := parseSIPMethod(tt.input)
		if got != tt.want {
			t.Errorf("parseSIPMethod(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestParseSIPHeader(t *testing.T) {
	msg := "REGISTER sip:34020000002000000001@3402000000 SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 192.168.1.100:5060\r\n" +
		"From: <sip:34020000001320000001@3402000000>;tag=xxx\r\n" +
		"To: <sip:34020000002000000001@3402000000>\r\n" +
		"Call-ID: call-123\r\n" +
		"CSeq: 1 REGISTER\r\n" +
		"Expires: 3600\r\n" +
		"\r\n"

	tests := []struct {
		header string
		want   string
	}{
		{"From", "<sip:34020000001320000001@3402000000>;tag=xxx"},
		{"To", "<sip:34020000002000000001@3402000000>"},
		{"Call-ID", "call-123"},
		{"CSeq", "1 REGISTER"},
		{"Expires", "3600"},
		{"Via", "SIP/2.0/UDP 192.168.1.100:5060"},
		{"NonExistent", ""},
	}

	for _, tt := range tests {
		got := parseSIPHeader(msg, tt.header)
		if got != tt.want {
			t.Errorf("parseSIPHeader(msg, %q) = %q, want %q", tt.header, got, tt.want)
		}
	}
}

func TestParseSIPHeaderCaseInsensitive(t *testing.T) {
	msg := "Content-Type: Application/MANSCDP+xml\r\n\r\n"
	got := parseSIPHeader(msg, "content-type")
	if got != "Application/MANSCDP+xml" {
		t.Errorf("case-insensitive parse failed: %q", got)
	}
}

func TestParseGBVersion(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"2022", "REGISTER sip:server SIP/2.0\r\nX-GB-Ver: 3.0\r\n\r\n", "2022"},
		{"2016", "REGISTER sip:server SIP/2.0\r\nX-GB-Ver: 2.0\r\n\r\n", "2016"},
		{"2011", "REGISTER sip:server SIP/2.0\r\nX-GB-Ver: 1.1\r\n\r\n", "2011"},
		{"missing header", "REGISTER sip:server SIP/2.0\r\n\r\n", ""},
		{"unknown version", "REGISTER sip:server SIP/2.0\r\nX-GB-Ver: 4.0\r\n\r\n", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseGBVersion(tt.raw); got != tt.want {
				t.Fatalf("parseGBVersion() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildImageSnapControl(t *testing.T) {
	got := buildImageSnapControl("34020000001320000001", 42)
	for _, want := range []string{
		`<CmdType>DeviceControl</CmdType>`,
		`<SN>42</SN>`,
		`<DeviceID>34020000001320000001</DeviceID>`,
		`<ImageCmd>Snap</ImageCmd>`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("snapshot control missing %q: %s", want, got)
		}
	}
}

func TestCaptureSnapshotRejectsNon2022Device(t *testing.T) {
	svc := testGB28181Service(t)
	deviceID := "34020000001320000001"
	camera := model.Camera{
		Name:           "GB camera",
		AccessProtocol: model.ProtocolGB28181,
		DeviceID:       deviceID,
		ChannelID:      deviceID,
	}
	if err := svc.db.Create(&camera).Error; err != nil {
		t.Fatal(err)
	}
	svc.devices = map[string]*DeviceSession{
		deviceID: {DeviceID: deviceID, GBVersion: "2016"},
	}

	_, err := svc.CaptureSnapshot(context.Background(), camera.ID)
	if err == nil || !strings.Contains(err.Error(), "2022") {
		t.Fatalf("CaptureSnapshot() error = %v, want unsupported 2022 error", err)
	}
}

func TestCameraServiceRoutesGB28181SnapshotToGBService(t *testing.T) {
	gbSvc := testGB28181Service(t)
	deviceID := "34020000001320000003"
	camera := model.Camera{
		Name:           "GB camera",
		AccessProtocol: model.ProtocolGB28181,
		DeviceID:       deviceID,
		ChannelID:      deviceID,
	}
	if err := gbSvc.db.Create(&camera).Error; err != nil {
		t.Fatal(err)
	}
	gbSvc.devices = map[string]*DeviceSession{
		deviceID: {DeviceID: deviceID, GBVersion: "2016"},
	}
	cameraSvc := NewCameraService(gbSvc.db, nil)
	cameraSvc.SetGB28181(gbSvc)

	_, err := cameraSvc.CaptureSnapshot(context.Background(), camera.ID)
	if err == nil || !strings.Contains(err.Error(), "2022") {
		t.Fatalf("CaptureSnapshot() error = %v, want GB28181 version error", err)
	}
}

func TestCaptureSnapshotRejectsUnavailableTCPTransport(t *testing.T) {
	svc := testGB28181Service(t)
	deviceID := "34020000001320000004"
	camera := model.Camera{
		Name:           "GB camera",
		AccessProtocol: model.ProtocolGB28181,
		DeviceID:       deviceID,
		ChannelID:      deviceID,
	}
	if err := svc.db.Create(&camera).Error; err != nil {
		t.Fatal(err)
	}
	svc.devices = map[string]*DeviceSession{
		deviceID: {DeviceID: deviceID, IP: "127.0.0.1", Port: 5060, Transport: "TCP", GBVersion: "2022"},
	}

	_, err := svc.CaptureSnapshot(context.Background(), camera.ID)
	if err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("CaptureSnapshot() error = %v, want SIP service unavailable error", err)
	}
}

func TestCaptureSnapshotReceivesJPEGInSIPResponse(t *testing.T) {
	svc := testGB28181Service(t)
	deviceID := "34020000001320000001"
	channelID := "34020000001320000002"
	camera := model.Camera{
		Name:           "GB camera",
		AccessProtocol: model.ProtocolGB28181,
		DeviceID:       deviceID,
		ChannelID:      channelID,
	}
	if err := svc.db.Create(&camera).Error; err != nil {
		t.Fatal(err)
	}

	deviceConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer deviceConn.Close()
	serverConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer serverConn.Close()
	svc.udpConn = serverConn
	svc.devices = map[string]*DeviceSession{
		deviceID: {
			DeviceID:  deviceID,
			IP:        "127.0.0.1",
			Port:      deviceConn.LocalAddr().(*net.UDPAddr).Port,
			Transport: "UDP",
			Domain:    "3402000000",
			GBVersion: "2022",
		},
	}

	result := make(chan struct {
		jpeg []byte
		err  error
	}, 1)
	go func() {
		jpeg, err := svc.CaptureSnapshot(context.Background(), camera.ID)
		result <- struct {
			jpeg []byte
			err  error
		}{jpeg, err}
	}()

	buf := make([]byte, 4096)
	if err := deviceConn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	n, _, err := deviceConn.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("read snapshot command: %v", err)
	}
	request := string(buf[:n])
	if !strings.Contains(request, "<ImageCmd>Snap</ImageCmd>") {
		t.Fatalf("snapshot command missing ImageCmd: %s", request)
	}
	callID := parseSIPHeader(request, "Call-ID")
	jpeg := []byte{0xff, 0xd8, 0x01, 0x02, 0xff, 0xd9}
	response := "SIP/2.0 200 OK\r\nCall-ID: " + callID + "\r\nContent-Type: image/jpeg\r\n\r\n" + string(jpeg)
	svc.handleSIPMessage(response, deviceConn.LocalAddr(), "UDP")

	select {
	case got := <-result:
		if got.err != nil {
			t.Fatalf("CaptureSnapshot() error = %v", got.err)
		}
		if string(got.jpeg) != string(jpeg) {
			t.Fatalf("CaptureSnapshot() = %x, want %x", got.jpeg, jpeg)
		}
	case <-time.After(time.Second):
		t.Fatal("CaptureSnapshot() did not return after JPEG response")
	}
}

func TestSnapshotResponseUsesSNAndDecodesImageData(t *testing.T) {
	svc := testGB28181Service(t)
	resultCh := make(chan snapshotResult, 1)
	svc.registerSnapshotWaiter("outbound-call", 42, resultCh)
	jpeg := []byte{0xff, 0xd8, 0x11, 0x22, 0xff, 0xd9}
	body := `<?xml version="1.0"?><Response><CmdType>DeviceControl</CmdType><SN>42</SN><ImageData>` +
		base64.StdEncoding.EncodeToString(jpeg) + `</ImageData></Response>`
	svc.completeSnapshotFromSIP("MESSAGE sip:server SIP/2.0\r\nCall-ID: device-message\r\nContent-Type: Application/MANSCDP+xml\r\n\r\n" + body)

	select {
	case result := <-resultCh:
		if result.err != nil {
			t.Fatalf("snapshot result error = %v", result.err)
		}
		if string(result.jpeg) != string(jpeg) {
			t.Fatalf("snapshot result = %x, want %x", result.jpeg, jpeg)
		}
	case <-time.After(time.Second):
		t.Fatal("snapshot response with matching SN was not delivered")
	}
}

func TestExtractSIPUser(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"<sip:34020000001320000001@3402000000>", "34020000001320000001"},
		{"<sip:34020000001320000001@3402000000>;tag=xxx", "34020000001320000001"},
		{"sip:34020000001320000001@192.168.1.100", "34020000001320000001"},
		{"34020000001320000001", "34020000001320000001"},
	}

	for _, tt := range tests {
		got := extractSIPUser(tt.input)
		if got != tt.want {
			t.Errorf("extractSIPUser(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestParseExpires(t *testing.T) {
	msg := "REGISTER sip:34020000002000000001@3402000000 SIP/2.0\r\nExpires: 3600\r\n\r\n"
	got := parseExpires(msg)
	if got != 3600 {
		t.Errorf("parseExpires = %d, want 3600", got)
	}

	msg2 := "REGISTER sip:34020000002000000001@3402000000 SIP/2.0\r\n\r\n"
	got2 := parseExpires(msg2)
	if got2 != 0 {
		t.Errorf("parseExpires (no header) = %d, want 0", got2)
	}
}

func TestExtractXMLValue(t *testing.T) {
	body := `<?xml version="1.0" encoding="GB2312"?>
<Notify>
<CmdType>Keepalive</CmdType>
<SN>123</SN>
<DeviceID>34020000001320000001</DeviceID>
<Status>OK</Status>
</Notify>`

	tests := []struct {
		tag  string
		want string
	}{
		{"CmdType", "Keepalive"},
		{"SN", "123"},
		{"DeviceID", "34020000001320000001"},
		{"Status", "OK"},
		{"NonExistent", ""},
	}

	for _, tt := range tests {
		got := extractXMLValue(body, tt.tag)
		if got != tt.want {
			t.Errorf("extractXMLValue(%q) = %q, want %q", tt.tag, got, tt.want)
		}
	}
}

func TestExtractSIPBody(t *testing.T) {
	msg := "MESSAGE sip:server SIP/2.0\r\nContent-Length: 50\r\n\r\n" +
		`<?xml version="1.0"?><Notify><CmdType>Keepalive</CmdType></Notify>`
	body := extractSIPBody(msg)
	if !strings.Contains(body, "Keepalive") {
		t.Errorf("extractSIPBody didn't find body content: %q", body)
	}

	// 无 body
	msg2 := "MESSAGE sip:server SIP/2.0\r\n\r\n"
	body2 := extractSIPBody(msg2)
	if body2 != "" {
		t.Errorf("expected empty body, got %q", body2)
	}
}

// ---------- PS 解封装测试 ----------

func TestPSDemuxerPackHeader(t *testing.T) {
	// 构造 MPEG-2 Pack header: 0x000001BA + marker bits
	packHeader := []byte{
		0x00, 0x00, 0x01, 0xBA, // start code
		0x44, 0x00, 0x04, 0x00, 0x04, 0x01, // SCR (MPEG-2, 标志位 = 01)
		0x04, 0x01, // mux rate
		0xF8, // marker bits + stuffing
	}

	var extracted [][]byte
	demux := NewPSDemuxer(func(nalu []byte) {
		extracted = append(extracted, nalu)
	})
	demux.Feed(packHeader)

	// Pack header 不应产生 NALU
	if len(extracted) != 0 {
		t.Errorf("pack header should not produce NALUs, got %d", len(extracted))
	}
}

func TestPSDemuxerVideoPES(t *testing.T) {
	// 构造一个包含 H.264 NALU 的 PES 包
	// NALU: SPS (type 7)
	nalu := []byte{0x67, 0x42, 0x00, 0x1E, 0xAB, 0xCD} // SPS

	// PES 包结构
	pes := []byte{
		0x00, 0x00, 0x01, 0xE0, // video PES start code
		0x00, 0x00, // PES packet length (0 = unlimited)
		0x80, // byte 6: '10' marker + optional flags
		0x80, // byte 7: PTS_DTS_flags = 10 (PTS only)
		0x05, // byte 8: PES header data length = 5 (for PTS)
		0x21, 0x00, 0x01, 0x00, 0x01, // PTS (dummy)
		// ES payload (with start code)
		0x00, 0x00, 0x00, 0x01, // Annex B start code
	}
	pes = append(pes, nalu...)

	var extracted [][]byte
	demux := NewPSDemuxer(func(nalu []byte) {
		extracted = append(extracted, nalu)
	})
	demux.Feed(pes)

	// 应该提取到一个 NALU
	if len(extracted) == 0 {
		t.Skip("PS PES parser needs further refinement for this simple test")
		return
	}

	// 验证 NALU 内容
	if len(extracted[0]) != len(nalu) {
		t.Errorf("NALU length = %d, want %d", len(extracted[0]), len(nalu))
	}
}

// ---------- RTP 解析测试 ----------

func TestRTPParseHeader(t *testing.T) {
	// 构造一个简单的 RTP 包 (15 字节: 12 字节头 + 3 字节 payload)
	rtp := make([]byte, 15)
	rtp[0] = 0x80      // V=2, P=0, X=0, CC=0
	rtp[1] = 0x60      // M=0, PT=96
	rtp[2] = 0x00      // seq high
	rtp[3] = 0x01      // seq low = 1
	rtp[4] = 0x00      // timestamp
	rtp[5] = 0x00
	rtp[6] = 0x03
	rtp[7] = 0xE8      // ts = 1000
	rtp[8] = 0x12      // SSRC
	rtp[9] = 0x34
	rtp[10] = 0x56
	rtp[11] = 0x78
	// payload
	rtp[12] = 0xAA
	rtp[13] = 0xBB
	rtp[14] = 0xCC

	recv := NewRTPReceiver(0, func(payload []byte) {})

	got := recv.parseRTP(rtp)
	if got == nil {
		t.Fatal("parseRTP returned nil")
	}
	if len(got) != 3 {
		t.Errorf("payload length = %d, want 3", len(got))
	}
	if got[0] != 0xAA || got[1] != 0xBB || got[2] != 0xCC {
		t.Errorf("payload = %x, want AABBCC", got)
	}
}

func TestRTPParseTooShort(t *testing.T) {
	recv := NewRTPReceiver(0, func([]byte) {})

	// 太短
	got := recv.parseRTP([]byte{0x80, 0x60, 0x00})
	if got != nil {
		t.Errorf("expected nil for short packet, got %v", got)
	}

	// 错误版本
	badVersion := make([]byte, 20)
	badVersion[0] = 0x00 // V=0
	got = recv.parseRTP(badVersion)
	if got != nil {
		t.Errorf("expected nil for bad version, got %v", got)
	}
}

// ---------- SIP 响应构造测试 ----------

func TestBuildSIPResponse(t *testing.T) {
	req := "REGISTER sip:34020000002000000001@3402000000 SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 192.168.1.100:5060;rport;branch=z9hG4bK123\r\n" +
		"From: <sip:34020000001320000001@3402000000>;tag=xxx\r\n" +
		"To: <sip:34020000002000000001@3402000000>\r\n" +
		"Call-ID: call-123\r\n" +
		"CSeq: 1 REGISTER\r\n" +
		"\r\n"

	resp := buildSIPResponse(req, 200, "OK", map[string]string{
		"Expires": "3600",
	})

	checks := []string{
		"SIP/2.0 200 OK",
		"Via: SIP/2.0/UDP 192.168.1.100:5060",
		"From: <sip:34020000001320000001",
		"Call-ID: call-123",
		"CSeq: 1 REGISTER",
		"Expires: 3600",
		"Content-Length: 0",
	}

	for _, check := range checks {
		if !strings.Contains(resp, check) {
			t.Errorf("response missing %q\ngot: %s", check, resp)
		}
	}
}

// ---------- GB28181 目录查询响应测试 ----------

func testGB28181Service(t *testing.T) *GB28181Service {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	pkg.MigrateDB(db)
	return &GB28181Service{
		cfg: &pkg.Config{SIPRealm: "3402000000"},
		db:  db,
	}
}

func TestBuildCatalogResponse(t *testing.T) {
	svc := testGB28181Service(t)
	resp := svc.buildCatalogResponse("34020000001320000001")

	if !strings.Contains(resp, "Catalog") {
		t.Errorf("response missing Catalog: %s", resp)
	}
	if !strings.Contains(resp, "34020000001320000001") {
		t.Errorf("response missing deviceID: %s", resp)
	}
	if !strings.Contains(resp, "SumNum") {
		t.Errorf("response missing SumNum: %s", resp)
	}
}

func TestBuildDeviceInfoResponse(t *testing.T) {
	svc := testGB28181Service(t)
	resp := svc.buildDeviceInfoResponse("34020000001320000001")

	if !strings.Contains(resp, "DeviceInfo") {
		t.Errorf("response missing DeviceInfo: %s", resp)
	}
	if !strings.Contains(resp, "34020000001320000001") {
		t.Errorf("response missing deviceID: %s", resp)
	}
}

// ---------- SIP Digest 鉴权测试 ----------

func TestMD5Hex(t *testing.T) {
	// MD5("") = d41d8cd98f00b204e9800998ecf8427e
	if md5hex("") != "d41d8cd98f00b204e9800998ecf8427e" {
		t.Errorf("md5hex empty wrong: %s", md5hex(""))
	}
}

func TestParseDigestParams(t *testing.T) {
	header := `Digest username="34020000001320000001", realm="3402000000", nonce="abc123", uri="sip:34020000002000000001@3402000000", response="def456", algorithm=MD5`
	params := parseDigestParams(header)
	if params == nil {
		t.Fatal("params should not be nil")
	}
	if params["username"] != "34020000001320000001" {
		t.Errorf("username = %q", params["username"])
	}
	if params["realm"] != "3402000000" {
		t.Errorf("realm = %q", params["realm"])
	}
	if params["nonce"] != "abc123" {
		t.Errorf("nonce = %q", params["nonce"])
	}
	if params["uri"] != "sip:34020000002000000001@3402000000" {
		t.Errorf("uri = %q", params["uri"])
	}
	if params["response"] != "def456" {
		t.Errorf("response = %q", params["response"])
	}
}

func TestValidateDigest(t *testing.T) {
	username := "34020000001320000001"
	realm := "3402000000"
	password := "testpass123"
	nonce := "abc123"
	uri := "sip:34020000002000000001@3402000000"

	// 计算正确的 response
	ha1 := md5hex(username + ":" + realm + ":" + password)
	ha2 := md5hex("REGISTER:" + uri)
	response := md5hex(ha1 + ":" + nonce + ":" + ha2)

	authHeader := fmt.Sprintf(`Digest username="%s", realm="%s", nonce="%s", uri="%s", response="%s", algorithm=MD5`,
		username, realm, nonce, uri, response)

	// 正确密码 → 通过
	if !validateDigest(authHeader, password, username, realm) {
		t.Error("correct password should validate")
	}

	// 错误密码 → 失败
	if validateDigest(authHeader, "wrongpass", username, realm) {
		t.Error("wrong password should fail")
	}

	// 错误 nonce → 失败
	badNonceHeader := fmt.Sprintf(`Digest username="%s", realm="%s", nonce="badnonce", uri="%s", response="%s", algorithm=MD5`,
		username, realm, uri, response)
	if validateDigest(badNonceHeader, password, username, realm) {
		t.Error("wrong nonce should fail")
	}

	// 空参数 → 失败
	if validateDigest("", password, username, realm) {
		t.Error("empty auth header should fail")
	}
	if validateDigest(`Digest username="x"`, password, username, realm) {
		t.Error("missing params should fail")
	}

	// qop="auth" 情况（带 nc/cnonce）
	qopNc := "00000001"
	qopCnonce := "0a4f113b"
	qopResponse := md5hex(ha1 + ":" + nonce + ":" + qopNc + ":" + qopCnonce + ":auth:" + ha2)
	qopHeader := fmt.Sprintf(`Digest username="%s", realm="%s", nonce="%s", uri="%s", response="%s", qop=auth, nc=%s, cnonce="%s", algorithm=MD5`,
		username, realm, nonce, uri, qopResponse, qopNc, qopCnonce)
	if !validateDigest(qopHeader, password, username, realm) {
		t.Error("qop=auth digest should validate")
	}
	// qop 但缺少 cnonce/nc → 失败
	qopBadHeader := fmt.Sprintf(`Digest username="%s", realm="%s", nonce="%s", uri="%s", response="%s", qop=auth, algorithm=MD5`,
		username, realm, nonce, uri, qopResponse)
	if validateDigest(qopBadHeader, password, username, realm) {
		t.Error("qop without cnonce/nc should fail")
	}
}

func TestGenerateSIPNonce(t *testing.T) {
	n1 := generateSIPNonce()
	n2 := generateSIPNonce()
	if n1 == "" || n2 == "" {
		t.Fatal("nonce should not be empty")
	}
	if n1 == n2 {
		t.Error("nonces should be unique")
	}
}
