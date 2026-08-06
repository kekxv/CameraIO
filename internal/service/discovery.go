package service

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// DiscoveryService 局域网设备扫描服务。
type DiscoveryService struct {
	onvif *ONVIFService
}

const scanDialTimeout = time.Second

func NewDiscoveryService(onvif *ONVIFService) *DiscoveryService {
	return &DiscoveryService{onvif: onvif}
}

// DiscoveredDevice 扫描发现的设备。
type DiscoveredDevice struct {
	IP              string `json:"ip"`
	Brand           string `json:"brand"`          // hikvision / uniview / custom
	DeviceType      string `json:"device_type"`    // ipc / nvr / unknown
	Manufacturer    string `json:"manufacturer"`
	Model           string `json:"model"`
	FirmwareVersion string `json:"firmware_version,omitempty"`
	RTSPUrl         string `json:"rtsp_url"`
	ONVIFEndpoint   string `json:"onvif_endpoint"`
	Channels        int    `json:"channels"`       // NVR 通道数（0 = IPC 或未知）
	HTTPPort        int    `json:"http_port"`
	RTSPEnabled     bool   `json:"rtsp_enabled"`
	MAC             string `json:"mac,omitempty"`
}

// ---------- 公共 API ----------

// ScanLAN 扫描局域网设备。subnet 为 "" 或 "auto" 时自动检测本机子网。
func (s *DiscoveryService) ScanLAN(ctx context.Context, subnet string) ([]DiscoveredDevice, error) {
	// 1. WS-Discovery 多播发现（快速路径）
	wsDevices := s.wsDiscovery(ctx)
	wsIPs := make(map[string]bool)
	for _, d := range wsDevices {
		wsIPs[d.IP] = true
	}
	log.Printf("[Discovery] WS-Discovery found %d devices", len(wsDevices))

	// 2. 子网 TCP 端口扫描
	ips, err := s.resolveSubnet(subnet)
	if err != nil {
		// 如果子网解析失败，只用 WS-Discovery 结果
		log.Printf("[Discovery] subnet resolve failed: %v, using WS-Discovery only", err)
		return wsDevices, nil
	}
	log.Printf("[Discovery] scanning %d IPs in subnet", len(ips))

	// 排除已发现的 IP
	var scanIPs []string
	for _, ip := range ips {
		if !wsIPs[ip] {
			scanIPs = append(scanIPs, ip)
		}
	}

	// 3. TCP 端口扫描
	tcpDevices := s.tcpScan(ctx, scanIPs)
	log.Printf("[Discovery] TCP scan found %d additional devices", len(tcpDevices))

	// 4. 合并结果（TCP 扫描的设备做 ONVIF 探测获取详细信息）
	for i := range tcpDevices {
		s.enrichDevice(ctx, &tcpDevices[i])
	}

	// 5. WS-Discovery 设备也补充信息
	for i := range wsDevices {
		if wsDevices[i].Manufacturer == "" {
			s.enrichDevice(ctx, &wsDevices[i])
		}
	}

	// 合并去重
	seen := make(map[string]bool)
	var result []DiscoveredDevice
	for _, d := range append(wsDevices, tcpDevices...) {
		if seen[d.IP] {
			continue
		}
		seen[d.IP] = true
		result = append(result, d)
	}
	return result, nil
}

// ---------- WS-Discovery 多播 ----------

func (s *DiscoveryService) wsDiscovery(ctx context.Context) []DiscoveredDevice {
	probeMsg := `<?xml version="1.0" encoding="UTF-8"?>
<e:Envelope xmlns:e="http://www.w3.org/2003/05/soap-envelope"
            xmlns:w="http://schemas.xmlsoap.org/ws/2004/08/addressing"
            xmlns:d="http://schemas.xmlsoap.org/ws/2005/04/discovery"
            xmlns:dn="http://www.onvif.org/ver10/network/wsdl">
  <e:Header>
    <w:MessageID>uuid:cameraio-scan</w:MessageID>
    <w:To e:mustUnderstand="true">urn:schemas-xmlsoap-org:ws:2005:04:discovery</w:To>
    <w:Action e:mustUnderstand="true">http://schemas.xmlsoap.org/ws/2005/04/discovery/Probe</w:Action>
  </e:Header>
  <e:Body>
    <d:Probe>
      <d:Types>dn:NetworkVideoTransmitter</d:Types>
    </d:Probe>
  </e:Body>
</e:Envelope>`

	addr, _ := net.ResolveUDPAddr("udp4", "239.255.255.250:3702")
	conn, err := net.ListenUDP("udp4", nil)
	if err != nil {
		log.Printf("[Discovery] WS-Discovery listen error: %v", err)
		return nil
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(2 * time.Second))

	// 发送 Probe
	if _, err := conn.WriteTo([]byte(probeMsg), addr); err != nil {
		log.Printf("[Discovery] WS-Discovery send error: %v", err)
		return nil
	}

	// 收集响应
	var devices []DiscoveredDevice
	seen := make(map[string]bool)
	buf := make([]byte, 8192)
	for {
		n, remoteAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			break // timeout or error
		}
		ip := remoteAddr.IP.String()
		if seen[ip] {
			continue
		}
		seen[ip] = true

		dev := DiscoveredDevice{
			IP:       ip,
			HTTPPort: 80,
		}
		// 从响应中提取 XAddrs（设备端点地址）
		resp := string(buf[:n])
		if xaddrs := extractXMLTagValue(resp, "XAddrs"); xaddrs != "" {
			// XAddrs 可能是 http://192.168.1.1:80/onvif/device_service
			dev.ONVIFEndpoint = xaddrs
			// 从 URL 中提取 IP（如果和 remoteAddr 不同）
			if host := extractHost(xaddrs); host != "" && host != ip {
				dev.IP = host
			}
		}
		devices = append(devices, dev)
	}
	return devices
}

// ---------- TCP 端口扫描 ----------

func (s *DiscoveryService) tcpScan(ctx context.Context, ips []string) []DiscoveredDevice {
	type result struct {
		ip   string
		http bool
		rtsp bool
		hik  bool
	}

	results := make(chan result, len(ips))
	sem := make(chan struct{}, 50) // 50 并发

	var wg sync.WaitGroup
	for _, ip := range ips {
		wg.Add(1)
		go func(ip string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			r := result{ip: ip}
			// 探测 HTTP 端口 (80)
			if probeTCPPort(ip, 80, scanDialTimeout) == nil {
				r.http = true
			}
			// 探测海康端口 (8000)
			if probeTCPPort(ip, 8000, scanDialTimeout) == nil {
				r.hik = true
			}
			// 探测 RTSP 端口 (554)
			if probeTCPPort(ip, 554, scanDialTimeout) == nil {
				r.rtsp = true
			}

			if r.http || r.hik || r.rtsp {
				results <- r
			}
		}(ip)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var devices []DiscoveredDevice
	for r := range results {
		dev := DiscoveredDevice{
			IP:          r.ip,
			RTSPEnabled: r.rtsp,
			RTSPUrl:     fmt.Sprintf("rtsp://%s:554/", r.ip),
		}
		if r.http {
			dev.HTTPPort = 80
		}
		if r.hik {
			dev.Brand = "hikvision"
			dev.HTTPPort = 8000
		}
		devices = append(devices, dev)
	}
	return devices
}

// ---------- 设备信息充实 ----------

// enrichDevice 通过 ONVIF 和 HTTP 探测获取设备的品牌、型号、通道数等详细信息。
func (s *DiscoveryService) enrichDevice(ctx context.Context, dev *DiscoveredDevice) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// 1. 尝试 ONVIF 探测（无需鉴权）
	if s.probeONVIF(ctx, dev) {
		return
	}
	// 2. 尝试 HTTP 特征识别（ISAPI 等）
	s.probeHTTP(ctx, dev)
	// 3. 如果 ONVIF 和 HTTP 都没识别出来，但有 RTSP，标记为通用设备
	if dev.Brand == "" && dev.RTSPEnabled {
		dev.Brand = "custom"
		dev.DeviceType = "unknown"
	}
}

// probeONVIF 尝试通过 ONVIF GetSystemDateAndTime + GetDeviceInformation 识别设备。
func (s *DiscoveryService) probeONVIF(ctx context.Context, dev *DiscoveredDevice) bool {
	// 尝试多个可能的 ONVIF 端点
	endpoints := []string{
		fmt.Sprintf("http://%s/onvif/device_service", dev.IP),
		fmt.Sprintf("http://%s:%d/onvif/device_service", dev.IP, dev.HTTPPort),
	}

	for _, ep := range endpoints {
		body := `<?xml version="1.0" encoding="UTF-8"?>
<SOAP-ENV:Envelope xmlns:SOAP-ENV="http://www.w3.org/2003/05/soap-envelope"
                   xmlns:tds="http://www.onvif.org/ver10/device/wsdl">
  <SOAP-ENV:Body>
    <tds:GetSystemDateAndTime/>
  </SOAP-ENV:Body>
</SOAP-ENV:Envelope>`

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, ep, strings.NewReader(body))
		if err != nil {
			continue
		}
		req.Header.Set("Content-Type", "application/soap+xml; charset=utf-8")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			continue
		}
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode < 400 && strings.Contains(string(data), "GetSystemDateAndTimeResponse") {
			dev.ONVIFEndpoint = ep
			// 提取设备信息（无需鉴权的部分）
			dev.Brand = identifyBrand(string(data), dev.IP)
			// 尝试获取通道数
			dev.Channels = s.countChannels(ctx, dev)
			if dev.Channels > 1 {
				dev.DeviceType = "nvr"
			} else if dev.Channels == 1 || dev.RTSPEnabled {
				dev.DeviceType = "ipc"
			}
			// 构造 RTSP URL
			dev.RTSPUrl = s.buildRTSPUrl(dev)
			return true
		}
	}
	return false
}

// probeHTTP 通过 HTTP 特征识别设备品牌（ONVIF 被禁用时的兜底方案）。
func (s *DiscoveryService) probeHTTP(ctx context.Context, dev *DiscoveredDevice) {
	ports := []int{80, 8000, 8080, 443}
	for _, port := range ports {
		// 海康 ISAPI 检测（即使 ONVIF 禁用，ISAPI 通常仍然可用）
		isapiURL := fmt.Sprintf("http://%s:%d/ISAPI/System/deviceInfo", dev.IP, port)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, isapiURL, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			continue
		}
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if isUniviewSignature(resp.Header, string(data)) {
			dev.Brand = "uniview"
			dev.HTTPPort = port
			dev.Manufacturer = "Uniview"
			dev.RTSPEnabled = true
			dev.RTSPUrl = fmt.Sprintf("rtsp://%s:554/unicast/c1/s0/live", dev.IP)
			return
		}

		if resp.StatusCode == 200 || resp.StatusCode == 401 {
			// ISAPI 存在 = 海康设备（401 表示需要认证但端点存在）
			dev.Brand = "hikvision"
			dev.HTTPPort = port
			dev.Manufacturer = "Hikvision"
			dev.RTSPEnabled = true
			if resp.StatusCode == 200 {
				dev.Model = extractXMLTagValue(string(data), "model")
				dev.FirmwareVersion = extractXMLTagValue(string(data), "firmwareVersion")
			}
			dev.DeviceType = "unknown"
			dev.RTSPUrl = fmt.Sprintf("rtsp://%s:554/Streaming/Channels/101", dev.IP)
			return
		}

		// 尝试登录页检测（海康/宇视 Web 登录页特征）
		if port != 80 && port != 443 {
			continue // 只在标准 HTTP 端口检测登录页
		}
		loginURL := fmt.Sprintf("http://%s:%d/", dev.IP, port)
		req2, _ := http.NewRequestWithContext(ctx, http.MethodGet, loginURL, nil)
		resp2, err := http.DefaultClient.Do(req2)
		if err != nil {
			continue
		}
		data2, _ := io.ReadAll(resp2.Body)
		resp2.Body.Close()
		if isUniviewSignature(resp2.Header, string(data2)) {
			dev.Brand = "uniview"
			dev.HTTPPort = port
			dev.Manufacturer = "Uniview"
			dev.RTSPEnabled = true
			dev.RTSPUrl = fmt.Sprintf("rtsp://%s:554/unicast/c1/s0/live", dev.IP)
			return
		}
		if resp2.StatusCode == 200 {
			body := strings.ToLower(string(data2))
			if strings.Contains(body, "hikvision") || strings.Contains(body, "hikvisi") || strings.Contains(body, "dna_nvr") {
				dev.Brand = "hikvision"
				dev.HTTPPort = port
				dev.Manufacturer = "Hikvision"
				dev.RTSPEnabled = true
				dev.RTSPUrl = fmt.Sprintf("rtsp://%s:554/Streaming/Channels/101", dev.IP)
				return
			}
			if strings.Contains(body, "uniview") || strings.Contains(body, "unv") || strings.Contains(body, "nvr301") {
				dev.Brand = "uniview"
				dev.HTTPPort = port
				dev.Manufacturer = "Uniview"
				dev.RTSPEnabled = true
				dev.RTSPUrl = fmt.Sprintf("rtsp://%s:554/unicast/c1/s0/live", dev.IP)
				return
			}
		}
	}
}

// countChannels 通过 ONVIF GetProfiles 计算 NVR 通道数。
func (s *DiscoveryService) countChannels(ctx context.Context, dev *DiscoveredDevice) int {
	if dev.ONVIFEndpoint == "" {
		return 0
	}
	// 从设备端点推导 media service 端点
	ip := dev.IP
	mediaEndpoints := []string{
		fmt.Sprintf("http://%s/onvif/media_service", ip),
		fmt.Sprintf("http://%s/onvif/device_service", ip),
	}

	body := `<?xml version="1.0" encoding="UTF-8"?>
<SOAP-ENV:Envelope xmlns:SOAP-ENV="http://www.w3.org/2003/05/soap-envelope"
                   xmlns:trt="http://www.onvif.org/ver10/media/wsdl">
  <SOAP-ENV:Body>
    <trt:GetProfiles/>
  </SOAP-ENV:Body>
</SOAP-ENV:Envelope>`

	for _, ep := range mediaEndpoints {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, ep, strings.NewReader(body))
		if err != nil {
			continue
		}
		req.Header.Set("Content-Type", "application/soap+xml; charset=utf-8")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			continue
		}
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode < 400 {
			channels := countProfileChannels(string(data))
			if channels > 0 {
				return channels
			}
		}
	}
	return 0
}

// buildRTSPUrl 根据品牌信息构造 RTSP URL。
func (s *DiscoveryService) buildRTSPUrl(dev *DiscoveredDevice) string {
	switch dev.Brand {
	case "hikvision":
		return fmt.Sprintf("rtsp://%s:554/Streaming/Channels/101", dev.IP)
	case "uniview":
		return fmt.Sprintf("rtsp://%s:554/unicast/c1/s0/live", dev.IP)
	default:
		return fmt.Sprintf("rtsp://%s:554/Streaming/Channels/101", dev.IP)
	}
}

// ---------- 子网计算 ----------

// resolveSubnet 获取本机子网的所有可用 IP 地址。
func (s *DiscoveryService) resolveSubnet(subnet string) ([]string, error) {
	if subnet != "" && subnet != "auto" {
		return enumerateCIDR(subnet)
	}
	// 自动检测本机子网
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var allIPs []string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok || ipNet.IP.To4() == nil {
				continue
			}
			cidr := ipNet.String()
			ips, _ := enumerateCIDR(cidr)
			allIPs = append(allIPs, ips...)
		}
	}
	return allIPs, nil
}

// enumerateCIDR 将 CIDR 展开为可用 IP 列表（排除网络地址和广播地址）。
func enumerateCIDR(cidr string) ([]string, error) {
	ip, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, err
	}
	var ips []string
	for ip := ip.Mask(ipNet.Mask); ipNet.Contains(ip); incIP(ip) {
		ipStr := ip.String()
		// 排除网络地址（.0）和广播地址（.255）
		parts := strings.Split(ipStr, ".")
		if len(parts) == 4 && (parts[3] == "0" || parts[3] == "255") {
			continue
		}
		ips = append(ips, ipStr)
	}
	return ips, nil
}

func incIP(ip net.IP) {
	for j := len(ip) - 1; j >= 0; j-- {
		ip[j]++
		if ip[j] > 0 {
			break
		}
	}
}

// ---------- 品牌识别 ----------

// identifyBrand 从 ONVIF 响应或 HTTP 特征识别设备品牌。
func identifyBrand(onvifResp string, ip string) string {
	respLower := strings.ToLower(onvifResp)
	if strings.Contains(respLower, "hikvision") || strings.Contains(respLower, "hikvisi") {
		return "hikvision"
	}
	if strings.Contains(respLower, "uniview") {
		return "uniview"
	}
	// 尝试 ISAPI 检测
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	isapiURL := fmt.Sprintf("http://%s/ISAPI/System/deviceInfo", ip)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, isapiURL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err == nil {
		resp.Body.Close()
		if resp.StatusCode == 200 || resp.StatusCode == 401 {
			return "hikvision"
		}
	}
	return "custom"
}

// isUniviewSignature 仅匹配宇视特有的响应特征，避免把普通的 401 误判为宇视设备。
func isUniviewSignature(headers http.Header, body string) bool {
	values := []string{body}
	for _, header := range []string{"Server", "WWW-Authenticate", "X-Device-Brand", "X-Device-Model"} {
		values = append(values, headers.Values(header)...)
	}
	for _, value := range values {
		lower := strings.ToLower(value)
		if strings.Contains(lower, "uniview") || strings.Contains(lower, "unv") || strings.Contains(lower, "nvr301") {
			return true
		}
	}
	return false
}

// countProfileChannels 从 GetProfiles 响应中统计不同通道数。
func countProfileChannels(body string) int {
	re := regexp.MustCompile(`(?i)channel[_]?(\d+)`)
	seen := make(map[int]bool)
	for _, match := range re.FindAllStringSubmatch(body, -1) {
		if len(match) >= 2 {
			n := 0
			fmt.Sscanf(match[1], "%d", &n)
			if n > 0 {
				seen[n] = true
			}
		}
	}
	return len(seen)
}

// extractHost 从 URL 中提取主机地址。
func extractHost(rawURL string) string {
	// 简单提取: 去掉协议前缀，取路径前的部分
	s := rawURL
	s = strings.TrimPrefix(s, "http://")
	s = strings.TrimPrefix(s, "https://")
	if idx := strings.Index(s, "/"); idx >= 0 {
		s = s[:idx]
	}
	// 去掉端口
	if host, _, err := net.SplitHostPort(s); err == nil {
		return host
	}
	return s
}

// ---------- 测试辅助 ----------

// DiscoveredDeviceXML 用于解析 GetDeviceInformation 响应（无需鉴权的版本）。
type discoveredDeviceXML struct {
	XMLName xml.Name `xml:"Envelope"`
	Body    struct {
		Resp struct {
			Manufacturer string `xml:"Manufacturer"`
			Model        string `xml:"Model"`
		} `xml:"GetDeviceInformationResponse"`
	} `xml:"Body"`
}
