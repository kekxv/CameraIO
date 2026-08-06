package service

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// NetworkConfig 网络配置。
type NetworkConfig struct {
	DHCP    bool   `json:"dhcp"`
	IP      string `json:"ip"`
	Mask    string `json:"mask"`
	Gateway string `json:"gateway"`
	DNS     string `json:"dns"`
}

// SetNetworkInterface 通过 ONVIF SetNetworkInterfaces 设置设备网络。
// 设置后设备通常会重启，需要等待一段时间后通过新 IP 连接。
func (s *ONVIFService) SetNetworkInterface(ctx context.Context, ip, user, pass string, config NetworkConfig) error {
	endpoint, err := s.probeDeviceEndpoint(ctx, ip, user, pass)
	if err != nil {
		return fmt.Errorf("probe device: %w", err)
	}

	// 1) GetNetworkInterfaces — 获取接口 token
	getBody := `<?xml version="1.0" encoding="UTF-8"?>
<SOAP-ENV:Envelope xmlns:SOAP-ENV="http://www.w3.org/2003/05/soap-envelope"
                   xmlns:tds="http://www.onvif.org/ver10/device/wsdl">
  <SOAP-ENV:Body>
    <tds:GetNetworkInterfaces/>
  </SOAP-ENV:Body>
</SOAP-ENV:Envelope>`
	respBody, err := s.callONVIF(ctx, endpoint, user, pass, getBody, "http://www.onvif.org/ver10/device/wsdl/GetNetworkInterfaces")
	if err != nil {
		return fmt.Errorf("get network interfaces: %w", err)
	}

	// 提取第一个接口 token
	ifToken := extractXMLTagValue(respBody, "token")
	if ifToken == "" {
		// 尝试从属性中提取
		ifToken = extractAttrValue(respBody, "NetworkInterface", "token")
	}
	if ifToken == "" {
		return fmt.Errorf("无法获取网络接口 token")
	}

	// 2) SetNetworkInterfaces — 设置新配置
	dhcpStr := "false"
	if config.DHCP {
		dhcpStr = "true"
	}

	ipAddr := config.IP
	if ipAddr == "" {
		ipAddr = ip // 保持原 IP
	}
	mask := config.Mask
	if mask == "" {
		mask = "255.255.255.0"
	}
	gateway := config.Gateway
	if gateway == "" {
		// 默认网关: 取 IP 的前三段 + .1
		parts := strings.Split(ipAddr, ".")
		if len(parts) == 4 {
			gateway = parts[0] + "." + parts[1] + "." + parts[2] + ".1"
		}
	}

	setBody := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<SOAP-ENV:Envelope xmlns:SOAP-ENV="http://www.w3.org/2003/05/soap-envelope"
                   xmlns:tds="http://www.onvif.org/ver10/device/wsdl"
                   xmlns:tt="http://www.onvif.org/ver10/schema">
  <SOAP-ENV:Body>
    <tds:SetNetworkInterfaces>
      <tds:InterfaceToken>%s</tds:InterfaceToken>
      <tds:NetworkInterface>
        <tt:Enabled>true</tt:Enabled>
        <tt:Link>
          <tt:AutoNegotiation>true</tt:AutoNegotiation>
          <tt:Speed>100</tt:Speed>
          <tt:Duplex>Full</tt:Duplex>
        </tt:Link>
        <tt:IPv4>
          <tt:Enabled>true</tt:Enabled>
          <tt:Manual>
            <tt:Address>%s</tt:Address>
            <tt:PrefixLength>24</tt:PrefixLength>
          </tt:Manual>
          <tt:DHCP>%s</tt:DHCP>
        </tt:IPv4>
      </tds:NetworkInterface>
    </tds:SetNetworkInterfaces>
  </SOAP-ENV:Body>
</SOAP-ENV:Envelope>`, ifToken, ipAddr, dhcpStr)

	_, err = s.callONVIF(ctx, endpoint, user, pass, setBody, "http://www.onvif.org/ver10/device/wsdl/SetNetworkInterfaces")
	if err != nil {
		return fmt.Errorf("set network interface: %w", err)
	}

	// 3) 触发设备重启（部分设备需要显式重启才生效）
	rebootBody := `<?xml version="1.0" encoding="UTF-8"?>
<SOAP-ENV:Envelope xmlns:SOAP-ENV="http://www.w3.org/2003/05/soap-envelope"
                   xmlns:tds="http://www.onvif.org/ver10/device/wsdl">
  <SOAP-ENV:Body>
    <tds:SystemReboot/>
  </SOAP-ENV:Body>
</SOAP-ENV:Envelope>`
	s.callONVIF(ctx, endpoint, user, pass, rebootBody, "http://www.onvif.org/ver10/device/wsdl/SystemReboot")

	return nil
}

// WaitForDevice 等待设备在新 IP 上重新上线。
func (s *ONVIFService) WaitForDevice(ctx context.Context, ip string, maxWait time.Duration) error {
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
		// 尝试探测 ONVIF 端点
		endpoint := fmt.Sprintf("http://%s/onvif/device_service", ip)
		body := `<?xml version="1.0" encoding="UTF-8"?>
<SOAP-ENV:Envelope xmlns:SOAP-ENV="http://www.w3.org/2003/05/soap-envelope"
                   xmlns:tds="http://www.onvif.org/ver10/device/wsdl">
  <SOAP-ENV:Body><tds:GetSystemDateAndTime/></SOAP-ENV:Body>
</SOAP-ENV:Envelope>`
		probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		req, _ := newRequest(probeCtx, endpoint, body)
		if req != nil {
			resp, err := httpClient().Do(req)
			cancel()
			if err == nil && resp.StatusCode < 400 {
				resp.Body.Close()
				return nil
			}
			if resp != nil {
				resp.Body.Close()
			}
		} else {
			cancel()
		}
	}
	return fmt.Errorf("设备在 %v 内未上线 (%s)", maxWait, ip)
}

// extractAttrValue 从 XML 中提取指定标签的 token 属性值。
func extractAttrValue(xmlStr, tag, attr string) string {
	// 简单匹配: <tag ... attr="value" ...>
	re := regexp.MustCompile(`<` + regexp.QuoteMeta(tag) + `[^>]*` + attr + `="([^"]*)"`)
	matches := re.FindStringSubmatch(xmlStr)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

// httpClient 返回共享的 HTTP 客户端。
func httpClient() *http.Client {
	return &http.Client{Timeout: 5 * time.Second}
}

// newRequest 创建 HTTP 请求。
func newRequest(ctx context.Context, url, body string) (*http.Request, error) {
	return http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(body))
}
