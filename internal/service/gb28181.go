package service

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
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
	cfg    *pkg.Config
	db     *gorm.DB
	events *EventBus
	streams *StreamService // 用于将 GB28181 流接入现有分发体系

	mu         sync.RWMutex
	devices    map[string]*DeviceSession    // deviceID → session
	rtpPorts   atomic.Int32                 // 下一个可用 RTP 端口
	rtpRecvs   map[int]*RTPReceiver         // port → RTP 接收器
	tcpConns   map[string]net.Conn         // 远端地址 → TCP 连接（用于发送 SIP 响应）

	udpConn  *net.UDPConn
	tcpLn    net.Listener
	ctx      context.Context
	cancel   context.CancelFunc
}

// DeviceSession 代表一个已注册的 GB28181 设备。
type DeviceSession struct {
	DeviceID    string
	IP          string
	Port        int    // 设备 SIP 端口
	Transport   string // UDP / TCP
	RegisteredAt time.Time
	KeepaliveAt  time.Time
	Channels    []string // 通道 ID 列表
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
	log.Printf("[GB28181] Device registered: %s from %s", deviceID, remoteAddr)
}

func (s *GB28181Service) sendRegisterOK(req string, remoteAddr net.Addr, transport, callID, cseq string, expires int) {
	resp := buildSIPResponse(req, 200, "OK", map[string]string{
		"Call-ID": callID,
		"CSeq":    cseq,
		"Expires": fmt.Sprintf("%d", expires),
		"Date":    time.Now().UTC().Format("2006-01-02T15:04:05"),
		"Server":  "CameraIO/1.0",
	})
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

	// 更新心跳时间
	s.mu.Lock()
	if dev, ok := s.devices[deviceID]; ok {
		dev.KeepaliveAt = time.Now()
	}
	s.mu.Unlock()

	// 解析 XML body
	body := extractSIPBody(raw)
	if body == "" {
		s.sendSIPResponse(raw, remoteAddr, transport, 200, "OK")
		return
	}

	cmdType := extractXMLValue(body, "CmdType")
	switch cmdType {
	case "Keepalive":
		// 心跳回复 200 OK（Date 头即时间同步），并记录最后同步时间
		s.sendSIPResponse(raw, remoteAddr, transport, 200, "OK")
		s.db.Model(&model.Camera{}).
			Where("device_id = ? AND access_protocol = ?", deviceID, model.ProtocolGB28181).
			Update("last_time_sync", time.Now())

	case "Catalog":
		// 目录查询响应
		response := s.buildCatalogResponse(deviceID)
		s.sendSIPMessageResponse(raw, remoteAddr, transport, callID, cseq, deviceID, response)

	case "DeviceInfo":
		response := s.buildDeviceInfoResponse(deviceID)
		s.sendSIPMessageResponse(raw, remoteAddr, transport, callID, cseq, deviceID, response)

	default:
		s.sendSIPResponse(raw, remoteAddr, transport, 200, "OK")
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

	// 构建 INVITE SDP
	sdp := s.buildInviteSDP(rtpPort)
	subject := fmt.Sprintf("%s:0,%s:0", channelID, s.cfg.SIPServerID)

	// 发送 INVITE
	inviteReq := s.buildINVITE(channelID, dev, sdp, subject)
	addr := &net.UDPAddr{
		IP:   net.ParseIP(dev.IP),
		Port: dev.Port,
	}
	s.sendSIPRaw(inviteReq, addr, dev.Transport)

	return rtpPort, nil
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

	// 创建 PS 解封装器，将 NALU 注入 stream 的 NALU 广播
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
			case 5: // IDR
				// 提取 JPEG 用于 MJPEG 预览（节流，避免 FFmpeg 进程堆积）
				if s.streams != nil {
					stream.extractMu.Lock()
					shouldExtract := !stream.extracting && time.Since(stream.lastExtractAt) >= 800*time.Millisecond
					if shouldExtract {
						stream.extracting = true
						stream.lastExtractAt = time.Now()
					}
					stream.extractMu.Unlock()
					if shouldExtract {
						go func() {
							defer func() {
								stream.extractMu.Lock()
								stream.extracting = false
								stream.extractMu.Unlock()
							}()
							s.streams.extractJPEG(stream, nalu)
						}()
					}
				}
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

func (s *GB28181Service) buildInviteSDP(rtpPort int) string {
	return fmt.Sprintf("v=0\r\n"+
		"o=%s 0 0 IN IP4 %s\r\n"+
		"s=Play\r\n"+
		"c=IN IP4 %s\r\n"+
		"t=0 0\r\n"+
		"m=video %d RTP/AVP 96\r\n"+
		"a=recvonly\r\n"+
		"a=rtpmap:96 PS/90000\r\n",
		s.cfg.SIPServerID, s.getLocalIP(), s.getLocalIP(), rtpPort)
}

func (s *GB28181Service) buildINVITE(channelID string, dev *DeviceSession, sdp, subject string) string {
	callID := generateCallID()
	cseq := 1
	localIP := s.getLocalIP()

	return fmt.Sprintf("INVITE sip:%s@%s SIP/2.0\r\n"+
		"Via: SIP/2.0/UDP %s:5060;rport;branch=z9hG4bK%s\r\n"+
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
		channelID, dev.IP,
		localIP, generateBranch(),
		s.cfg.SIPServerID, s.cfg.SIPRealm,
		channelID, dev.IP,
		callID,
		cseq,
		subject,
		localIP, localIP,
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
	s.mu.Lock()
	defer s.mu.Unlock()

	for id, dev := range s.devices {
		if time.Since(dev.KeepaliveAt) > timeout {
			log.Printf("[GB28181] Device %s keepalive timeout, marking offline", id)
			delete(s.devices, id)

			s.db.Model(&model.Camera{}).
				Where("device_id = ? AND access_protocol = ?", id, model.ProtocolGB28181).
				Updates(map[string]any{
					"status":     model.CameraStatusOffline,
					"last_error": "心跳超时（设备离线）",
				})

			s.events.PublishCameraStatus(0, id, model.CameraStatusOffline)
		}
	}
}

// ---------- SIP 消息构造与发送 ----------

func (s *GB28181Service) sendSIPResponse(req string, remoteAddr net.Addr, transport string, status int, reason string) {
	resp := buildSIPResponse(req, status, reason, map[string]string{
		"Date":   time.Now().UTC().Format("2006-01-02T15:04:05"),
		"Server": "CameraIO/1.0",
	})
	s.sendSIPRaw(resp, remoteAddr, transport)
}

func (s *GB28181Service) sendSIPRaw(msg string, remoteAddr net.Addr, transport string) {
	switch addr := remoteAddr.(type) {
	case *net.UDPAddr:
		s.udpConn.WriteToUDP([]byte(msg), addr)
	case *net.TCPAddr:
		// 通过已维护的 TCP 连接发送
		s.mu.RLock()
		conn, ok := s.tcpConns[addr.String()]
		s.mu.RUnlock()
		if !ok {
			log.Printf("[GB28181] TCP connection to %s not found", addr.String())
			return
		}
		conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		_, _ = conn.Write([]byte(msg))
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
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "127.0.0.1"
	}
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
