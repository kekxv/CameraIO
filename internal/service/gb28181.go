package service

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"CameraIO/internal/model"
	"CameraIO/internal/pkg"

	"gorm.io/gorm"
)

// GB28181Service 实现国标 GB/T 28181 SIP 信令服务。
// 作为 SIP UAS 接收摄像头注册、心跳、点播请求。
type GB28181Service struct {
	cfg     *pkg.Config
	db      *gorm.DB
	events  *EventBus
	streams *StreamService // 用于将 GB28181 流接入现有分发体系

	mu       sync.RWMutex
	devices  map[string]*DeviceSession // deviceID → session
	rtpPorts atomic.Int32              // 下一个可用 RTP 端口
	rtpRecvs map[int]*RTPReceiver      // port → RTP 接收器
	tcpConns map[string]net.Conn       // 远端地址 → TCP 连接（用于发送 SIP 响应）

	snapshotMu      sync.Mutex
	snapshotWaiters map[string]chan snapshotResult // SIP Call-ID → waiting capture
	snapshotBySN    map[string]string              // DeviceControl SN → SIP Call-ID
	snapshotSerial  atomic.Int64

	udpConn *net.UDPConn
	tcpLn   net.Listener
	ctx     context.Context
	cancel  context.CancelFunc
}

// DeviceSession 代表一个已注册的 GB28181 设备。
type DeviceSession struct {
	DeviceID     string
	IP           string
	Port         int    // 设备 SIP 端口
	Transport    string // UDP / TCP
	Domain       string // 设备注册时使用的 SIP 域
	GBVersion    string // "2022" / "2016" / "2011"，由 REGISTER 的 X-GB-Ver 确认
	RegisteredAt time.Time
	KeepaliveAt  time.Time
	Channels     []string // 通道 ID 列表
}

type snapshotResult struct {
	jpeg []byte
	err  error
}

func NewGB28181Service(cfg *pkg.Config, db *gorm.DB, events *EventBus, streams *StreamService) *GB28181Service {
	s := &GB28181Service{
		cfg:      cfg,
		db:       db,
		events:   events,
		streams:  streams,
		devices:  make(map[string]*DeviceSession),
		rtpRecvs: make(map[int]*RTPReceiver),
		tcpConns: make(map[string]net.Conn),
		snapshotWaiters: make(map[string]chan snapshotResult),
		snapshotBySN:    make(map[string]string),
	}
	s.rtpPorts.Store(int32(cfg.RTPPortMin))
	return s
}

// Start 启动 SIP 服务（UDP + TCP）。
func (s *GB28181Service) Start() error {
	s.ctx, s.cancel = context.WithCancel(context.Background())

	// 启动 UDP SIP 监听
	udpAddr, err := net.ResolveUDPAddr("udp", s.cfg.SIPListenAddr)
	if err != nil {
		return fmt.Errorf("resolve SIP UDP addr: %w", err)
	}
	s.udpConn, err = net.ListenUDP("udp", udpAddr)
	if err != nil {
		return fmt.Errorf("listen SIP UDP: %w", err)
	}
	go s.readUDPLoop()

	// 启动 TCP SIP 监听（备用）
	tcpAddr := s.cfg.SIPListenAddr
	s.tcpLn, err = net.Listen("tcp", tcpAddr)
	if err != nil {
		log.Printf("[GB28181] TCP listen failed (non-fatal): %v", err)
	} else {
		go s.acceptTCPLoop()
	}

	// 启动心跳超时检测
	go s.keepaliveCheckLoop()

	log.Printf("[GB28181] SIP server listening on %s (ID=%s, Realm=%s)",
		s.cfg.SIPListenAddr, s.cfg.SIPServerID, s.cfg.SIPRealm)
	return nil
}

// Stop 关闭 SIP 服务。
func (s *GB28181Service) Stop() {
	s.cancel()
	if s.udpConn != nil {
		s.udpConn.Close()
	}
	if s.tcpLn != nil {
		s.tcpLn.Close()
	}
	// 关闭所有 RTP 接收器
	s.mu.Lock()
	for _, recv := range s.rtpRecvs {
		recv.Stop()
	}
	s.rtpRecvs = make(map[int]*RTPReceiver)
	s.mu.Unlock()
}

// ---------- SIP UDP 读取循环 ----------

func (s *GB28181Service) readUDPLoop() {
	buf := make([]byte, 65536) // GB28181 的 XML 消息可能较大（如 Catalog），需足够大
	for {
		n, remoteAddr, err := s.udpConn.ReadFromUDP(buf)
		if err != nil {
			if s.ctx.Err() != nil {
				return
			}
			log.Printf("[GB28181] UDP read error: %v", err)
			continue
		}
		msg := string(buf[:n])
		go s.handleSIPMessage(msg, remoteAddr, "UDP")
	}
}

func (s *GB28181Service) acceptTCPLoop() {
	for {
		conn, err := s.tcpLn.Accept()
		if err != nil {
			if s.ctx.Err() != nil {
				return
			}
			continue
		}
		go s.handleTCPConn(conn)
	}
}

func (s *GB28181Service) handleTCPConn(conn net.Conn) {
	defer conn.Close()
	remoteAddr := conn.RemoteAddr().String()
	// 存储 TCP 连接，用于主动发送 SIP 响应
	s.mu.Lock()
	s.tcpConns[remoteAddr] = conn
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.tcpConns, remoteAddr)
		s.mu.Unlock()
	}()

	buf := make([]byte, 65536)
	for {
		conn.SetReadDeadline(time.Now().Add(120 * time.Second))
		n, err := conn.Read(buf)
		if err != nil {
			return
		}
		msg := string(buf[:n])
		s.handleSIPMessage(msg, conn.RemoteAddr(), "TCP")
	}
}

// ---------- SIP 消息处理 ----------

func (s *GB28181Service) handleSIPMessage(raw string, remoteAddr net.Addr, transport string) {
	method := parseSIPMethod(raw)
	switch method {
	case "REGISTER":
		s.handleRegister(raw, remoteAddr, transport)
	case "MESSAGE":
		s.handleMessage(raw, remoteAddr, transport)
	case "BYE":
		s.handleBye(raw, remoteAddr, transport)
	case "ACK":
		// ACK 无需处理
	default:
		// SIP 响应（如 INVITE 的 200 OK）
		if strings.HasPrefix(method, "SIP/2.0") {
			statusLine := strings.SplitN(raw, "\r\n", 2)[0]
			log.Printf("[GB28181] SIP response from %s: %s", remoteAddr, statusLine)
			s.completeSnapshotFromSIP(raw)
			// INVITE 的 200 OK 需要发送 ACK 完成会话，设备才会推流
			if strings.Contains(statusLine, "200") {
				cseqHdr := parseSIPHeader(raw, "CSeq")
				if strings.Contains(cseqHdr, "INVITE") {
					s.sendACKForInvite(raw, remoteAddr, transport)
				}
			}
			return
		}
		if method == "" {
			// 空方法：通常是截断的 UDP 包或非 SIP 探测，静默忽略
			log.Printf("[GB28181] ignoring non-SIP/truncated message from %s (%d bytes)", remoteAddr, len(raw))
		} else {
			log.Printf("[GB28181] unhandled SIP method: %s from %s", method, remoteAddr)
		}
	}
}

// ---------- REGISTER（设备注册） ----------

func (s *GB28181Service) handleRegister(raw string, remoteAddr net.Addr, transport string) {
	from := parseSIPHeader(raw, "From")
	deviceID := extractSIPUser(from)
	if deviceID == "" {
		s.sendSIPResponse(raw, remoteAddr, transport, 400, "Bad Request")
		return
	}
	// 设备注册时使用的 SIP 域（从 To 头提取），INVITE 时需匹配
	to := parseSIPHeader(raw, "To")
	domain := extractSIPDomain(to)
	if domain == "" {
		domain = extractSIPDomain(from)
	}

	callID := parseSIPHeader(raw, "Call-ID")
	cseq := parseSIPHeader(raw, "CSeq")

	// 查询设备密码（复用摄像头记录里的 password 字段）
	password := s.getDevicePassword(deviceID)

	// 需要鉴权：无 Authorization 头 → 返回 401 质询
	if password != "" {
		authHeader := parseSIPHeader(raw, "Authorization")
		if authHeader == "" {
			s.sendUnauthorized(raw, remoteAddr, transport)
			return
		}
		// 校验 Digest 摘要
		if !validateDigest(authHeader, password, deviceID, s.cfg.SIPRealm) {
			log.Printf("[GB28181] Device %s auth failed (wrong password)", deviceID)
			s.setCameraError(deviceID, "SIP 鉴权失败：密码不匹配")
			s.sendUnauthorized(raw, remoteAddr, transport)
			return
		}
	}

	expires := parseExpires(raw)
	if expires == 0 {
		expires = 3600
	}

	// 记录设备会话（支持 UDP 和 TCP 注册）
	var addr *net.UDPAddr
	switch a := remoteAddr.(type) {
	case *net.UDPAddr:
		addr = a
	case *net.TCPAddr:
		addr = &net.UDPAddr{IP: a.IP, Port: a.Port}
	}
	if addr == nil {
		s.sendSIPResponse(raw, remoteAddr, transport, 400, "Bad Request")
		return
	}

	deviceIP := addr.IP.String()
	s.mu.Lock()
	s.devices[deviceID] = &DeviceSession{
		DeviceID:     deviceID,
		IP:           deviceIP,
		Port:         addr.Port,
		Transport:    transport,
		Domain:       domain,
		GBVersion:    parseGBVersion(raw),
		RegisteredAt: time.Now(),
		KeepaliveAt:  time.Now(),
	}
	s.mu.Unlock()

	// 记录设备的注册 IP/端口/传输方式，供前端展示
	s.db.Model(&model.Camera{}).
		Where("device_id = ? AND access_protocol = ?", deviceID, model.ProtocolGB28181).
		Updates(map[string]any{
			"ip":        deviceIP,
			"port":      addr.Port,
			"transport": transport,
		})

	// 更新数据库中的摄像头状态（注册成功，清除错误）
	s.db.Model(&model.Camera{}).
		Where("device_id = ? AND access_protocol = ?", deviceID, model.ProtocolGB28181).
		Updates(map[string]any{
			"status":     model.CameraStatusOnline,
			"last_error": "",
		})

	// 广播事件
	s.events.PublishCameraStatus(0, deviceID, model.CameraStatusOnline)

	// 发送 200 OK
	s.sendRegisterOK(raw, remoteAddr, transport, callID, cseq, expires)
	log.Printf("[GB28181] Device registered: %s from %s (version=%s)", deviceID, remoteAddr, parseGBVersion(raw))

	// 注册成功后查询设备的 Catalog，获取真实通道列表
	go s.queryDeviceCatalog(deviceID)
}

func (s *GB28181Service) sendRegisterOK(req string, remoteAddr net.Addr, transport, callID, cseq string, expires int) {
	// 提取并回显 Contact（海康设备需要 Contact 确认注册成功）
	contact := parseSIPHeader(req, "Contact")
	to := parseSIPHeader(req, "To")

	// 构造完整的 200 OK（回显 Contact，To 加 tag）
	via := parseSIPHeader(req, "Via")
	from := parseSIPHeader(req, "From")
	callIDHdr := parseSIPHeader(req, "Call-ID")
	cseqHdr := parseSIPHeader(req, "CSeq")
	tag := generateBranch()

	resp := fmt.Sprintf("SIP/2.0 200 OK\r\n"+
		"Via: %s\r\n"+
		"From: %s\r\n"+
		"To: %s;tag=%s\r\n"+
		"Call-ID: %s\r\n"+
		"CSeq: %s\r\n"+
		"Contact: %s\r\n"+
		"Expires: %d\r\n"+
		"Date: %s\r\n"+
		"Server: CameraIO/1.0\r\n"+
		"Content-Length: 0\r\n"+
		"\r\n",
		via, from, to, tag, callIDHdr, cseqHdr, contact, expires,
		time.Now().Format("2006-01-02T15:04:05")) // 本地时间，设备直接用作本地时间
	s.sendSIPRaw(resp, remoteAddr, transport)
}

// ---------- SIP Digest 鉴权 ----------

// getDevicePassword 查询 GB28181 设备的鉴权密码（复用摄像头记录的 password 字段）。
// 未找到或未设置密码 → 返回空串（不鉴权）。
func (s *GB28181Service) getDevicePassword(deviceID string) string {
	var cam model.Camera
	if err := s.db.Where("device_id = ? AND access_protocol = ?", deviceID, model.ProtocolGB28181).First(&cam).Error; err != nil {
		return ""
	}
	return cam.Password
}

// setCameraError 记录 GB28181 设备的错误信息（供前端展示）。
func (s *GB28181Service) setCameraError(deviceID, msg string) {
	s.db.Model(&model.Camera{}).
		Where("device_id = ? AND access_protocol = ?", deviceID, model.ProtocolGB28181).
		Update("last_error", msg)
}

// sendUnauthorized 返回 401 + WWW-Authenticate Digest 质询。
func (s *GB28181Service) sendUnauthorized(req string, remoteAddr net.Addr, transport string) {
	nonce := generateSIPNonce()
	resp := buildSIPResponse(req, 401, "Unauthorized", map[string]string{
		"WWW-Authenticate": fmt.Sprintf(
			`Digest realm="%s", nonce="%s", algorithm=MD5, qop="auth"`,
			s.cfg.SIPRealm, nonce),
		"Server": "CameraIO/1.0",
	})
	s.sendSIPRaw(resp, remoteAddr, transport)
}

// validateDigest 校验 Authorization Digest 摘要。
// 支持 qop="auth"（含 nc/cnonce）和不带 qop 两种 RFC 2617 格式。
func validateDigest(authHeader, password, username, realm string) bool {
	params := parseDigestParams(authHeader)
	if params == nil {
		return false
	}

	respNonce := params["nonce"]
	respURI := params["uri"]
	response := params["response"]
	authUser := params["username"]

	if respNonce == "" || respURI == "" || response == "" {
		return false
	}
	if authUser != "" && authUser != username {
		return false
	}

	// HA1 = MD5(username:realm:password)，HA2 = MD5(method:uri)
	ha1 := md5hex(username + ":" + realm + ":" + password)
	ha2 := md5hex("REGISTER:" + respURI)

	var expected string
	if qop, hasQop := params["qop"]; hasQop && qop == "auth" {
		// 带 qop: response = MD5(HA1:nonce:nc:cnonce:qop:HA2)
		nc := params["nc"]
		cnonce := params["cnonce"]
		if nc == "" || cnonce == "" {
			return false
		}
		expected = md5hex(ha1 + ":" + respNonce + ":" + nc + ":" + cnonce + ":" + qop + ":" + ha2)
	} else {
		// 无 qop: response = MD5(HA1:nonce:HA2)
		expected = md5hex(ha1 + ":" + respNonce + ":" + ha2)
	}
	return strings.EqualFold(expected, response)
}

// parseDigestParams 解析 Digest Authorization 头的参数。
// 支持带引号（username/realm/nonce/uri/response/cnonce）和不带引号（qop/nc/algorithm）的值。
func parseDigestParams(header string) map[string]string {
	// 格式: Digest username="...", realm="...", nonce="...", qop=auth, nc=00000001, ...
	re := regexp.MustCompile(`(\w+)\s*=\s*(?:"([^"]*)"|([^,\s]+))`)
	matches := re.FindAllStringSubmatch(header, -1)
	if len(matches) == 0 {
		return nil
	}
	params := make(map[string]string)
	for _, m := range matches {
		value := m[2] // 带引号
		if value == "" {
			value = m[3] // 不带引号
		}
		params[strings.ToLower(m[1])] = value
	}
	return params
}

// generateSIPNonce 生成带时间戳的随机 nonce。
func generateSIPNonce() string {
	ts := fmt.Sprintf("%x", time.Now().Unix())
	randPart := fmt.Sprintf("%x", rand.Int63())
	return ts + randPart
}

// md5hex 计算 MD5 的十六进制字符串。
func md5hex(s string) string {
	h := md5.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}

// ---------- MESSAGE（心跳 / 目录 / PTZ） ----------

func (s *GB28181Service) handleMessage(raw string, remoteAddr net.Addr, transport string) {
	from := parseSIPHeader(raw, "From")
	deviceID := extractSIPUser(from)
	callID := parseSIPHeader(raw, "Call-ID")
	cseq := parseSIPHeader(raw, "CSeq")

	// 更新心跳时间；若设备不在会话中（如服务器重启后），自动创建会话
	s.mu.Lock()
	if dev, ok := s.devices[deviceID]; ok {
		dev.KeepaliveAt = time.Now()
	} else {
		// 从远端地址恢复会话（服务器重启后设备可能未重新 REGISTER 但仍在心跳）
		addrIP := ""
		addrPort := 0
		switch a := remoteAddr.(type) {
		case *net.UDPAddr:
			addrIP, addrPort = a.IP.String(), a.Port
		case *net.TCPAddr:
			addrIP, addrPort = a.IP.String(), a.Port
		}
		if addrIP != "" {
			s.devices[deviceID] = &DeviceSession{
				DeviceID:     deviceID,
				IP:           addrIP,
				Port:         addrPort,
				Transport:    transport,
				KeepaliveAt:  time.Now(),
				RegisteredAt: time.Now(),
			}
		}
	}
	s.mu.Unlock()

	// 解析 XML body
	body := extractSIPBody(raw)
	if body == "" {
		s.sendSIPResponse(raw, remoteAddr, transport, 200, "OK")
		return
	}
	// GB/T 28181-2022 抓拍回传可由设备主动发送 MESSAGE；先尝试把
	// JPEG 回传交给对应的抓拍请求，未匹配时再按普通国标消息处理。
	s.completeSnapshotFromSIP(raw)

	cmdType := extractXMLValue(body, "CmdType")
	switch cmdType {
	case "Keepalive":
		// 心跳回复 200 OK（Date 头即时间同步），标记在线 + 记录心跳时间
		s.sendSIPResponse(raw, remoteAddr, transport, 200, "OK")
		s.db.Model(&model.Camera{}).
			Where("device_id = ? AND access_protocol = ?", deviceID, model.ProtocolGB28181).
			Updates(map[string]any{
				"status":         model.CameraStatusOnline,
				"last_error":     "",
				"last_time_sync": time.Now(),
			})

	case "Catalog":
		// 设备返回的目录查询响应：提取通道列表并记录
		s.logDeviceChannels(body, deviceID)

	case "DeviceInfo":
		response := s.buildDeviceInfoResponse(deviceID)
		s.sendSIPMessageResponse(raw, remoteAddr, transport, callID, cseq, deviceID, response)

	default:
		s.sendSIPResponse(raw, remoteAddr, transport, 200, "OK")
	}
}

const gbSnapshotTimeout = 5 * time.Second

// CaptureSnapshot sends GB/T 28181-2022's ImageCmd=Snap DeviceControl and
// waits for a JPEG carried by the matching SIP response or MESSAGE. It does
// not start a preview stream or invoke FFmpeg.
func (s *GB28181Service) CaptureSnapshot(ctx context.Context, cameraID uint) ([]byte, error) {
	var camera model.Camera
	if err := s.db.First(&camera, cameraID).Error; err != nil {
		return nil, fmt.Errorf("camera %d not found: %w", cameraID, err)
	}
	if camera.AccessProtocol != model.ProtocolGB28181 {
		return nil, fmt.Errorf("camera %d is not a GB28181 camera", cameraID)
	}

	s.mu.RLock()
	dev, ok := s.devices[camera.DeviceID]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("GB28181 device %s is not registered", camera.DeviceID)
	}
	if dev.GBVersion != "2022" {
		return nil, fmt.Errorf("GB28181 device %s does not advertise GB/T 28181-2022 snapshot support", camera.DeviceID)
	}
	if strings.EqualFold(dev.Transport, "TCP") {
		key := net.JoinHostPort(dev.IP, fmt.Sprintf("%d", dev.Port))
		s.mu.RLock()
		_, connected := s.tcpConns[key]
		s.mu.RUnlock()
		if !connected {
			return nil, errors.New("GB28181 SIP service is not running for this TCP device")
		}
	} else if s.udpConn == nil {
		return nil, errors.New("GB28181 SIP service is not running")
	}

	channelID := camera.ChannelID
	if channelID == "" {
		channelID = camera.DeviceID
	}
	serial := s.snapshotSerial.Add(1)
	callID := generateCallID()
	resultCh := make(chan snapshotResult, 1)
	s.registerSnapshotWaiter(callID, serial, resultCh)
	defer s.removeSnapshotWaiter(callID, serial)

	message := s.buildImageSnapMessage(channelID, dev, callID, serial)
	addr := &net.UDPAddr{IP: net.ParseIP(dev.IP), Port: dev.Port}
	log.Printf("[GB28181] sending ImageCmd=Snap to device %s, channel %s", camera.DeviceID, channelID)
	s.sendSIPRaw(message, addr, dev.Transport)

	waitCtx, cancel := context.WithTimeout(ctx, gbSnapshotTimeout)
	defer cancel()
	select {
	case result := <-resultCh:
		return result.jpeg, result.err
	case <-waitCtx.Done():
		return nil, fmt.Errorf("GB28181 snapshot timed out waiting for device %s", camera.DeviceID)
	}
}

func (s *GB28181Service) registerSnapshotWaiter(callID string, serial int64, resultCh chan snapshotResult) {
	s.snapshotMu.Lock()
	defer s.snapshotMu.Unlock()
	if s.snapshotWaiters == nil {
		s.snapshotWaiters = make(map[string]chan snapshotResult)
		s.snapshotBySN = make(map[string]string)
	}
	s.snapshotWaiters[callID] = resultCh
	s.snapshotBySN[fmt.Sprintf("%d", serial)] = callID
}

func (s *GB28181Service) removeSnapshotWaiter(callID string, serial int64) {
	s.snapshotMu.Lock()
	defer s.snapshotMu.Unlock()
	delete(s.snapshotWaiters, callID)
	delete(s.snapshotBySN, fmt.Sprintf("%d", serial))
}

func (s *GB28181Service) completeSnapshotFromSIP(raw string) {
	body := extractSIPBody(raw)
	jpeg, ok := snapshotJPEGFromSIPBody(body)
	if !ok {
		return
	}

	callID := parseSIPHeader(raw, "Call-ID")
	s.snapshotMu.Lock()
	resultCh, found := s.snapshotWaiters[callID]
	if !found {
		callID = s.snapshotBySN[extractXMLValue(body, "SN")]
		resultCh, found = s.snapshotWaiters[callID]
	}
	if found {
		delete(s.snapshotWaiters, callID)
		for sn, pendingCallID := range s.snapshotBySN {
			if pendingCallID == callID {
				delete(s.snapshotBySN, sn)
			}
		}
	}
	s.snapshotMu.Unlock()
	if found {
		resultCh <- snapshotResult{jpeg: jpeg}
	}
}

func snapshotJPEGFromSIPBody(body string) ([]byte, bool) {
	data := []byte(body)
	if isJPEG(data) {
		return data, true
	}
	// GB/T 28181-2022 设备在实践中常把抓图放在 XML 内；兼容常见的
	// Base64 字段名称，同时仍要求 JPEG 标记，避免把错误文本当作图片。
	for _, tag := range []string{"ImageData", "PictureData", "ImageBase64"} {
		encoded := extractXMLValue(body, tag)
		if encoded == "" {
			continue
		}
		decoded, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(encoded), ""))
		if err == nil && isJPEG(decoded) {
			return decoded, true
		}
	}
	return nil, false
}

func (s *GB28181Service) buildImageSnapMessage(channelID string, dev *DeviceSession, callID string, serial int64) string {
	domain := dev.Domain
	if domain == "" {
		domain = s.cfg.SIPRealm
	}
	transport := dev.Transport
	if transport == "" {
		transport = "UDP"
	}
	localIP := s.getLocalIPFor(dev.IP)
	body := buildImageSnapControl(channelID, serial)
	return fmt.Sprintf("MESSAGE sip:%s@%s SIP/2.0\r\n"+
		"Via: SIP/2.0/%s %s:5060;rport;branch=z9hG4bK%s\r\n"+
		"From: <sip:%s@%s>;tag=CameraIO-snapshot\r\n"+
		"To: <sip:%s@%s>\r\n"+
		"Call-ID: %s\r\n"+
		"CSeq: 1 MESSAGE\r\n"+
		"Max-Forwards: 70\r\n"+
		"Content-Type: Application/MANSCDP+xml\r\n"+
		"Content-Length: %d\r\n"+
		"\r\n"+
		"%s",
		channelID, domain,
		transport, localIP, generateBranch(),
		s.cfg.SIPServerID, domain,
		channelID, domain,
		callID,
		len(body),
		body)
}

// logDeviceChannels 解析设备返回的 Catalog 响应，记录可用通道。
func (s *GB28181Service) logDeviceChannels(xmlBody, deviceID string) {
	// 提取所有 <DeviceID> 和 <Name>
	idRe := regexp.MustCompile(`<DeviceID>([^<]+)</DeviceID>`)
	nameRe := regexp.MustCompile(`<Name>([^<]+)</Name>`)
	ids := idRe.FindAllStringSubmatch(xmlBody, -1)
	names := nameRe.FindAllStringSubmatch(xmlBody, -1)

	if len(ids) == 0 {
		log.Printf("[GB28181] device %s catalog has no channels", deviceID)
		return
	}
	for i, m := range ids {
		name := ""
		if i < len(names) {
			name = names[i][1]
		}
		log.Printf("[GB28181] device %s channel: %s (%s)", deviceID, m[1], name)
	}
}

func (s *GB28181Service) buildCatalogResponse(deviceID string) string {
	// 查询数据库中属于该设备的所有通道
	var cameras []model.Camera
	s.db.Where("device_id = ? AND access_protocol = ?", deviceID, model.ProtocolGB28181).Find(&cameras)

	// 如果没有记录，默认返回设备本身作为一个通道
	if len(cameras) == 0 {
		cameras = []model.Camera{{DeviceID: deviceID, ChannelID: deviceID, Name: deviceID}}
	}

	deviceCount := len(cameras)
	sumNum := deviceCount

	var items strings.Builder
	for _, cam := range cameras {
		chID := cam.ChannelID
		if chID == "" {
			chID = cam.DeviceID
		}
		items.WriteString(fmt.Sprintf(`<Item>
<DeviceID>%s</DeviceID>
<Name>%s</Name>
<Manufacturer>CameraIO</Manufacturer>
<Model>Virtual</Model>
<Owner>0</Owner>
<CivilCode>%s</CivilCode>
<Address>0</Address>
<Parental>0</Parental>
<SafetyWay>0</SafetyWay>
<RegisterWay>1</RegisterWay>
<Secrecy>0</Secrecy>
<Status>ON</Status>
</Item>`, chID, xmlEscape(cam.Name), s.cfg.SIPRealm))
	}

	return fmt.Sprintf(`<?xml version="1.0" encoding="GB2312"?>
<Response>
<CmdType>Catalog</CmdType>
<SN>1</SN>
<DeviceID>%s</DeviceID>
<SumNum>%d</SumNum>
<DeviceList Num="%d">
%s
</DeviceList>
</Response>`, deviceID, sumNum, deviceCount, items.String())
}

func (s *GB28181Service) buildDeviceInfoResponse(deviceID string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="GB2312"?>
<Response>
<CmdType>DeviceInfo</CmdType>
<SN>1</SN>
<DeviceID>%s</DeviceID>
<Result>OK</Result>
<Manufacturer>CameraIO</Manufacturer>
<Model>Virtual</Model>
<Firmware>V1.0</Firmware>
<Channel>1</Channel>
</Response>`, deviceID)
}

func (s *GB28181Service) sendSIPMessageResponse(req string, remoteAddr net.Addr, transport, callID, cseq, fromDeviceID, body string) {
	// 从请求中获取 To 头（设备发送的 From 成为我们的 To）
	to := parseSIPHeader(req, "From")

	resp := buildSIPResponse(req, 200, "OK", map[string]string{
		"Call-ID":      callID,
		"CSeq":         cseq,
		"Content-Type": "Application/MANSCDP+xml",
		"To":           to,
	})
	// 添加 body
	resp += "\r\n" + body
	s.sendSIPRaw(resp, remoteAddr, transport)
}

// ---------- BYE ----------

func (s *GB28181Service) handleBye(raw string, remoteAddr net.Addr, transport string) {
	s.sendSIPResponse(raw, remoteAddr, transport, 200, "OK")
}

// ---------- INVITE（向设备点播） ----------

// InviteStream 向指定 GB28181 设备发送 INVITE 请求视频流。
func (s *GB28181Service) InviteStream(ctx context.Context, channelID string) (int, error) {
	// 查询通道对应的设备
	var cam model.Camera
	if err := s.db.Where("channel_id = ? OR device_id = ?", channelID, channelID).
		Where("access_protocol = ?", model.ProtocolGB28181).First(&cam).Error; err != nil {
		return 0, fmt.Errorf("channel %s not found: %w", channelID, err)
	}

	s.mu.RLock()
	dev, ok := s.devices[cam.DeviceID]
	s.mu.RUnlock()
	if !ok {
		return 0, fmt.Errorf("device %s not registered", cam.DeviceID)
	}

	// 分配 RTP 端口
	rtpPort := int(s.rtpPorts.Add(1))
	if rtpPort > s.cfg.RTPPortMax {
		s.rtpPorts.Store(int32(s.cfg.RTPPortMin))
		rtpPort = s.cfg.RTPPortMin
	}

	// 启动 RTP 接收器 + PS 解封装器
	if err := s.startRTPReceiver(rtpPort, cam.ID); err != nil {
		return 0, fmt.Errorf("start RTP receiver: %w", err)
	}

	// 确保 StreamService 中该摄像头的流已启动
	if s.streams != nil {
		_ = s.streams.StartStream(cam.ID)
	}

	// 构建 INVITE SDP（用与设备同子网的本地 IP 作为 RTP 回传地址）
	sdp := s.buildInviteSDP(rtpPort, dev.IP)
	subject := fmt.Sprintf("%s:0,%s:0", channelID, s.cfg.SIPServerID)

	// 发送 INVITE
	inviteReq := s.buildINVITE(channelID, dev, sdp, subject)
	addr := &net.UDPAddr{
		IP:   net.ParseIP(dev.IP),
		Port: dev.Port,
	}
	localIP := s.getLocalIPFor(dev.IP)
	log.Printf("[GB28181] sending INVITE to %s:%d via %s (channel %s, RTP port %d, SDP IP %s)",
		dev.IP, dev.Port, dev.Transport, channelID, rtpPort, localIP)
	log.Printf("[GB28181] INVITE:\n%s", inviteReq)
	s.sendSIPRaw(inviteReq, addr, dev.Transport)

	return rtpPort, nil
}

// sendACKForInvite 收到 INVITE 的 200 OK 后发送 ACK，完成 SIP 会话。
// 设备收到 ACK 后才会开始推送 RTP 媒体流。
func (s *GB28181Service) sendACKForInvite(req string, remoteAddr net.Addr, transport string) {
	callID := parseSIPHeader(req, "Call-ID")
	cseq := parseSIPHeader(req, "CSeq") // "1 INVITE"
	cseqNum := "1"
	if fields := strings.Fields(cseq); len(fields) > 0 {
		cseqNum = fields[0]
	}
	to := parseSIPHeader(req, "To") // <sip:channel@domain>;tag=xxx
	from := parseSIPHeader(req, "From")

	// 请求 URI = sip:user@domain
	user := extractSIPUser(to)
	domain := extractSIPDomain(to)
	reqURI := fmt.Sprintf("sip:%s@%s", user, domain)

	// 本地 IP 和 Via 传输协议
	var deviceIP string
	switch a := remoteAddr.(type) {
	case *net.UDPAddr:
		deviceIP = a.IP.String()
	case *net.TCPAddr:
		deviceIP = a.IP.String()
	}
	localIP := s.getLocalIPFor(deviceIP)
	viaTransport := "UDP"
	if transport == "TCP" {
		viaTransport = "TCP"
	}

	ack := fmt.Sprintf("ACK %s SIP/2.0\r\n"+
		"Via: SIP/2.0/%s %s:5060;rport;branch=z9hG4bK%s\r\n"+
		"From: %s\r\n"+
		"To: %s\r\n"+
		"Call-ID: %s\r\n"+
		"CSeq: %s ACK\r\n"+
		"Max-Forwards: 70\r\n"+
		"Content-Length: 0\r\n"+
		"\r\n",
		reqURI, viaTransport, localIP, generateBranch(),
		from, to, callID, cseqNum)

	s.sendSIPRaw(ack, remoteAddr, transport)
	log.Printf("[GB28181] sent ACK for INVITE (call %s)", callID)
}

// startRTPReceiver 启动 RTP 接收器，解封装 PS 后将 NALU 注入 StreamService。
func (s *GB28181Service) startRTPReceiver(port int, cameraID uint) error {
	// 查找或创建 Stream
	var stream *Stream
	if s.streams != nil {
		stream = s.streams.GetStream(cameraID)
		if stream == nil {
			// 如果 stream 还没启动，先手动创建一个
			if err := s.streams.StartStream(cameraID); err != nil {
				return err
			}
			stream = s.streams.GetStream(cameraID)
		}
	}

	// 启动持续 H.264→MJPEG 转码器（~12 FPS），供预览
	var mjpegWriter io.WriteCloser
	if stream != nil && s.streams != nil {
		w, err := s.streams.StartH264MJPEGTranscoder(stream)
		if err != nil {
			log.Printf("[GB28181] start MJPEG transcoder: %v", err)
		} else {
			mjpegWriter = w
		}
	}

	// 创建 PS 解封装器，将 NALU 注入 stream 的 NALU 广播
	startCode := []byte{0, 0, 0, 1}
	var demux *PSDemuxer
	if stream != nil {
		demux = NewPSDemuxer(func(nalu []byte) {
			if len(nalu) == 0 {
				return
			}
			nalType := nalu[0] & 0x1F
			n := NALU{
				Type:  nalType,
				Data:  make([]byte, len(nalu)),
				IsIDR: nalType == 5,
			}
			copy(n.Data, nalu)

			// 保存 SPS/PPS
			switch nalType {
			case 7: // SPS
				stream.sps = make([]byte, len(nalu))
				copy(stream.sps, nalu)
			case 8: // PPS
				stream.pps = make([]byte, len(nalu))
				copy(stream.pps, nalu)
			}

			// 喂给持续转码器（H.264 Annex B → MJPEG 12FPS）
			if mjpegWriter != nil {
				buf := make([]byte, 0, len(startCode)+len(nalu))
				buf = append(buf, startCode...)
				buf = append(buf, nalu...)
				_, _ = mjpegWriter.Write(buf)
			}

			// 广播给所有订阅者
			stream.mu.RLock()
			for _, ch := range stream.naluSubs {
				select {
				case ch <- n:
				default:
				}
			}
			stream.mu.RUnlock()
		})
	}

	// 创建 RTP 接收器
	recv := NewRTPReceiver(port, func(psPayload []byte) {
		if demux != nil {
			demux.Feed(psPayload)
		}
	})

	if err := recv.Start(); err != nil {
		return err
	}

	s.mu.Lock()
	s.rtpRecvs[port] = recv
	s.mu.Unlock()

	log.Printf("[GB28181] RTP receiver started on port %d for camera %d", port, cameraID)
	return nil
}

// queryDeviceCatalog 向设备发送 Catalog 查询，获取真实通道列表。
func (s *GB28181Service) queryDeviceCatalog(deviceID string) {
	s.mu.RLock()
	dev, ok := s.devices[deviceID]
	s.mu.RUnlock()
	if !ok {
		return
	}

	body := fmt.Sprintf(`<?xml version="1.0" encoding="GB2312"?>
<Query>
<CmdType>Catalog</CmdType>
<SN>1</SN>
<DeviceID>%s</DeviceID>
</Query>`, deviceID)

	callID := generateCallID()
	localIP := s.getLocalIP()
	domain := dev.Domain
	if domain == "" {
		domain = s.cfg.SIPRealm
	}
	msg := fmt.Sprintf("MESSAGE sip:%s@%s SIP/2.0\r\n"+
		"Via: SIP/2.0/%s %s:5060;rport;branch=z9hG4bK%s\r\n"+
		"From: <sip:%s@%s>;tag=CameraIO-catalog\r\n"+
		"To: <sip:%s@%s>\r\n"+
		"Call-ID: %s\r\n"+
		"CSeq: 1 MESSAGE\r\n"+
		"Max-Forwards: 70\r\n"+
		"Content-Type: Application/MANSCDP+xml\r\n"+
		"Content-Length: %d\r\n"+
		"\r\n"+
		"%s",
		deviceID, domain,
		dev.Transport, localIP, generateBranch(),
		s.cfg.SIPServerID, domain,
		deviceID, domain,
		callID,
		len(body),
		body)

	addr := &net.UDPAddr{IP: net.ParseIP(dev.IP), Port: dev.Port}
	s.sendSIPRaw(msg, addr, dev.Transport)
	log.Printf("[GB28181] sent Catalog query to device %s", deviceID)
}

// buildImageSnapControl creates the GB/T 28181-2022 DeviceControl payload for
// a single image capture. This command is sent only to a registered device
// that advertised X-GB-Ver: 3.0.
func buildImageSnapControl(channelID string, serial int64) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="GB2312"?>
<Control>
<CmdType>DeviceControl</CmdType>
<SN>%d</SN>
<DeviceID>%s</DeviceID>
<ImageCmd>Snap</ImageCmd>
</Control>`, serial, xmlEscape(channelID))
}

func (s *GB28181Service) buildInviteSDP(rtpPort int, deviceIP string) string {
	localIP := s.getLocalIPFor(deviceIP)
	// 生成 10 位 SSRC（海康要求 SDP 包含 y= 字段才推流）
	ssrc := fmt.Sprintf("%010d", time.Now().UnixNano()%10000000000)
	return fmt.Sprintf("v=0\r\n"+
		"o=%s 0 0 IN IP4 %s\r\n"+
		"s=Play\r\n"+
		"c=IN IP4 %s\r\n"+
		"t=0 0\r\n"+
		"m=video %d RTP/AVP 96\r\n"+
		"a=recvonly\r\n"+
		"a=rtpmap:96 PS/90000\r\n"+
		"a=encrypt:0\r\n"+
		"y=%s\r\n",
		s.cfg.SIPServerID, localIP, localIP, rtpPort, ssrc)
}

func (s *GB28181Service) buildINVITE(channelID string, dev *DeviceSession, sdp, subject string) string {
	callID := generateCallID()
	cseq := 1
	localIP := s.getLocalIP()
	// 使用设备注册时的域；若未记录则用 SIPRealm
	domain := dev.Domain
	if domain == "" {
		domain = s.cfg.SIPRealm
	}
	// Via 传输协议与发送方式一致
	viaTransport := "UDP"
	if dev.Transport == "TCP" {
		viaTransport = "TCP"
	}

	return fmt.Sprintf("INVITE sip:%s@%s SIP/2.0\r\n"+
		"Via: SIP/2.0/%s %s:5060;rport;branch=z9hG4bK%s\r\n"+
		"From: <sip:%s@%s>;tag=CameraIO\r\n"+
		"To: <sip:%s@%s>\r\n"+
		"Call-ID: %s\r\n"+
		"CSeq: %d INVITE\r\n"+
		"Max-Forwards: 70\r\n"+
		"Content-Type: APPLICATION/SDP\r\n"+
		"Subject: %s\r\n"+
		"User-Agent: CameraIO/1.0\r\n"+
		"Contact: <sip:%s@%s:5060>\r\n"+
		"Content-Length: %d\r\n"+
		"\r\n"+
		"%s",
		channelID, domain,
		viaTransport, localIP, generateBranch(),
		s.cfg.SIPServerID, domain,
		channelID, domain,
		callID,
		cseq,
		subject,
		s.cfg.SIPServerID, localIP,
		len(sdp),
		sdp)
}

// ---------- 心跳超时检测 ----------

func (s *GB28181Service) keepaliveCheckLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.checkKeepalive()
		}
	}
}

func (s *GB28181Service) checkKeepalive() {
	timeout := 3 * time.Minute // 3 个心跳周期未响应视为离线

	// 先检查内存会话
	s.mu.Lock()
	for id, dev := range s.devices {
		if time.Since(dev.KeepaliveAt) > timeout {
			log.Printf("[GB28181] Device %s keepalive timeout, marking offline", id)
			delete(s.devices, id)
			s.markOffline(id)
		}
	}
	s.mu.Unlock()

	// 兜底：扫描 DB 中状态为在线但心跳超时的 GB28181 设备
	// （覆盖服务器重启后内存会话为空、但设备未及时重新注册的情况）
	var cameras []model.Camera
	if err := s.db.Where("access_protocol = ? AND status = ?", model.ProtocolGB28181, model.CameraStatusOnline).Find(&cameras).Error; err != nil {
		return
	}
	for _, cam := range cameras {
		if cam.LastTimeSync != nil && time.Since(*cam.LastTimeSync) > timeout {
			s.markOffline(cam.DeviceID)
		}
	}
}

// markOffline 将 GB28181 设备标记为离线并广播事件。
func (s *GB28181Service) markOffline(deviceID string) {
	s.db.Model(&model.Camera{}).
		Where("device_id = ? AND access_protocol = ?", deviceID, model.ProtocolGB28181).
		Updates(map[string]any{
			"status":     model.CameraStatusOffline,
			"last_error": "心跳超时（设备离线）",
		})
	s.events.PublishCameraStatus(0, deviceID, model.CameraStatusOffline)
}

// ---------- SIP 消息构造与发送 ----------

func (s *GB28181Service) sendSIPResponse(req string, remoteAddr net.Addr, transport string, status int, reason string) {
	resp := buildSIPResponse(req, status, reason, map[string]string{
		"Date":   time.Now().Format("2006-01-02T15:04:05"), // 本地时间
		"Server": "CameraIO/1.0",
	})
	s.sendSIPRaw(resp, remoteAddr, transport)
}

func (s *GB28181Service) sendSIPRaw(msg string, remoteAddr net.Addr, transport string) {
	// 按 transport 优先（UDPAddr 可能是由 TCP 设备构造的，此时应走 TCP 连接）
	if transport == "TCP" {
		var key string
		switch a := remoteAddr.(type) {
		case *net.UDPAddr:
			key = net.JoinHostPort(a.IP.String(), fmt.Sprintf("%d", a.Port))
		case *net.TCPAddr:
			key = a.String()
		}
		s.mu.RLock()
		conn, ok := s.tcpConns[key]
		s.mu.RUnlock()
		if !ok {
			log.Printf("[GB28181] TCP connection to %s not found, falling back to UDP", key)
		} else {
			conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			_, _ = conn.Write([]byte(msg))
			return
		}
	}

	// UDP 发送
	if addr, ok := remoteAddr.(*net.UDPAddr); ok && s.udpConn != nil {
		s.udpConn.WriteToUDP([]byte(msg), addr)
	} else if addr != nil {
		log.Printf("[GB28181] UDP SIP service is not running; cannot send to %s", addr)
	}
}

// ---------- SIP 消息解析工具 ----------

func parseSIPMethod(raw string) string {
	lines := strings.SplitN(raw, "\r\n", 2)
	if len(lines) == 0 {
		return ""
	}
	parts := strings.SplitN(lines[0], " ", 3)
	if len(parts) < 1 {
		return ""
	}
	return parts[0]
}

func parseSIPHeader(raw, name string) string {
	lower := strings.ToLower(name)
	lines := strings.Split(raw, "\r\n")
	for _, line := range lines {
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		hName := strings.TrimSpace(line[:idx])
		if strings.ToLower(hName) == lower {
			return strings.TrimSpace(line[idx+1:])
		}
	}
	return ""
}

// parseGBVersion maps GB/T 28181's X-GB-Ver extension header to the
// corresponding standard edition. Unknown and absent values are deliberately
// treated as unsupported rather than assuming 2022-only features are present.
func parseGBVersion(raw string) string {
	switch strings.TrimSpace(parseSIPHeader(raw, "X-GB-Ver")) {
	case "3.0":
		return "2022"
	case "2.0":
		return "2016"
	case "1.0", "1.1":
		return "2011"
	default:
		return ""
	}
}

func extractSIPUser(sipURI string) string {
	// 从 "sip:34020000001320000001@192.168.1.100" 提取 34020000001320000001
	sipURI = strings.Trim(sipURI, "<> ")
	if idx := strings.Index(sipURI, "sip:"); idx >= 0 {
		sipURI = sipURI[idx+4:]
	}
	if idx := strings.Index(sipURI, "@"); idx >= 0 {
		return sipURI[:idx]
	}
	return sipURI
}

// extractSIPDomain 从 SIP URI 提取域部分。
func extractSIPDomain(sipURI string) string {
	sipURI = strings.Trim(sipURI, "<> ")
	if idx := strings.Index(sipURI, "sip:"); idx >= 0 {
		sipURI = sipURI[idx+4:]
	}
	if idx := strings.Index(sipURI, "@"); idx >= 0 {
		return sipURI[idx+1:]
	}
	return ""
}

func parseExpires(raw string) int {
	// 从 Expires header 或 Contact expires 参数解析
	expires := parseSIPHeader(raw, "Expires")
	if expires != "" {
		n := 0
		for _, c := range expires {
			if c >= '0' && c <= '9' {
				n = n*10 + int(c-'0')
			}
		}
		return n
	}
	return 0
}

func extractSIPBody(raw string) string {
	// 找到空行后的 body
	idx := strings.Index(raw, "\r\n\r\n")
	if idx < 0 {
		return ""
	}
	return raw[idx+4:]
}

func extractXMLValue(xmlStr, tag string) string {
	open := "<" + tag + ">"
	close := "</" + tag + ">"
	start := strings.Index(xmlStr, open)
	if start < 0 {
		return ""
	}
	start += len(open)
	end := strings.Index(xmlStr[start:], close)
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(xmlStr[start : start+end])
}

func buildSIPResponse(req string, status int, reason string, extraHeaders map[string]string) string {
	// 从请求中提取 Via、From、To、Call-ID、CSeq
	firstLine := strings.SplitN(req, "\r\n", 2)[0]
	_ = firstLine

	via := parseSIPHeader(req, "Via")
	from := parseSIPHeader(req, "From")
	to := parseSIPHeader(req, "To")
	callID := parseSIPHeader(req, "Call-ID")
	cseq := parseSIPHeader(req, "CSeq")

	resp := fmt.Sprintf("SIP/2.0 %d %s\r\n", status, reason)
	resp += "Via: " + via + "\r\n"
	resp += "From: " + from + "\r\n"
	resp += "To: " + to + "\r\n"
	resp += "Call-ID: " + callID + "\r\n"
	resp += "CSeq: " + cseq + "\r\n"

	for k, v := range extraHeaders {
		resp += k + ": " + v + "\r\n"
	}
	resp += "Content-Length: 0\r\n"
	resp += "\r\n"
	return resp
}

// ---------- 辅助 ----------

func (s *GB28181Service) getLocalIP() string {
	return s.getLocalIPFor("")
}

// getLocalIPFor 获取本机 IP。若指定目标 IP，优先返回与目标同子网的接口 IP，
// 否则返回第一个非回环 IPv4。
func (s *GB28181Service) getLocalIPFor(target string) string {
	var targetIP net.IP
	if target != "" {
		targetIP = net.ParseIP(target)
	}

	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "127.0.0.1"
	}

	// 第一次遍历：找与目标同子网的接口
	if targetIP != nil {
		for _, addr := range addrs {
			if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
				ip4 := ipNet.IP.To4()
				if ip4 == nil {
					continue
				}
				if ipNet.Contains(targetIP) {
					return ip4.String()
				}
			}
		}
	}

	// 回退：第一个非回环 IPv4
	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
			if ipNet.IP.To4() != nil {
				return ipNet.IP.String()
			}
		}
	}
	return "127.0.0.1"
}

func generateCallID() string {
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), time.Now().UnixMicro()%10000)
}

func generateBranch() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}
