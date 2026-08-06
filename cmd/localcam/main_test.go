package main

import (
	"testing"
	"time"
	"net"
	"bufio"
	"strings"
)

// TestCameraEnumeration 测试摄像头枚举功能
func TestCameraEnumeration(t *testing.T) {
	cams, err := enumerateLocalCameras()
	if err != nil {
		t.Fatalf("枚举摄像头失败: %v", err)
	}

	t.Logf("检测到 %d 个摄像头", len(cams))
	for _, cam := range cams {
		t.Logf("  [%d] %s (路径: %s)", cam.Index, cam.Name, cam.Path)
	}

	if len(cams) == 0 {
		t.Skip("未检测到摄像头，跳过测试")
	}
}

// TestRTSPServer 测试 RTSP 服务器基本功能
func TestRTSPServer(t *testing.T) {
	server := NewRTSPServer(8555, "/test")

	go func() {
		if err := server.Serve(); err != nil {
			t.Logf("RTSP 服务器错误: %v", err)
		}
	}()

	time.Sleep(1 * time.Second)

	// 测试连接
	conn, err := net.Dial("tcp", "localhost:8555")
	if err != nil {
		t.Fatalf("连接 RTSP 服务器失败: %v", err)
	}
	defer conn.Close()

	// 发送 OPTIONS 请求
	request := "OPTIONS rtsp://localhost:8555/test RTSP/1.0\r\n" +
		"CSeq: 1\r\n" +
		"\r\n"

	_, err = conn.Write([]byte(request))
	if err != nil {
		t.Fatalf("发送请求失败: %v", err)
	}

	// 读取响应
	reader := bufio.NewReader(conn)
	response, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("读取响应失败: %v", err)
	}

	if !strings.Contains(response, "200 OK") {
		t.Errorf("响应不正确: %s", response)
	}

	t.Log("RTSP 服务器测试通过")
}

// TestRTPPacket 测试 RTP 包封装
func TestRTPPacket(t *testing.T) {
	client := &rtspClient{
		seqNum: 0,
		ssrc:   12345,
	}

	// 创建 UDP 连接用于测试
	addr, _ := net.ResolveUDPAddr("udp", "localhost:5004")
	rtpConn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		t.Fatalf("创建 UDP 连接失败: %v", err)
	}
	defer rtpConn.Close()

	client.rtpConn = rtpConn

	// 测试小数据包
	smallPayload := []byte{0x67, 0x42, 0x00, 0x1e} // SPS NALU
	server := &RTSPServer{}
	server.sendRTPPacket(client, smallPayload, true)

	if client.seqNum != 1 {
		t.Errorf("序列号不正确: %d", client.seqNum)
	}

	// 测试大数据包（需要分片）
	largePayload := make([]byte, 2000)
	for i := range largePayload {
		largePayload[i] = byte(i % 256)
	}

	server.sendRTPPacket(client, largePayload, false)

	if client.seqNum < 2 {
		t.Errorf("分片后序列号不正确: %d", client.seqNum)
	}

	t.Log("RTP 包封装测试通过")
}

// TestNALUExtraction 测试 NALU 分割
func TestNALUExtraction(t *testing.T) {
	server := &RTSPServer{}

	// 测试数据包含 SPS 和 PPS
	testData := []byte{
		0x00, 0x00, 0x00, 0x01, 0x67, 0x42, 0x00, 0x1e, // SPS
		0x00, 0x00, 0x00, 0x01, 0x68, 0xce, 0x38, 0x80, // PPS
	}

	nalus := server.splitNALUs(testData)

	if len(nalus) != 2 {
		t.Errorf("NALU 数量不正确: %d", len(nalus))
	}

	// 检查第一个 NALU 是否是 SPS
	if len(nalus) > 0 && len(nalus[0]) > 0 {
		nalType := nalus[0][0] & 0x1F
		if nalType != 7 {
			t.Errorf("第一个 NALU 不是 SPS: %d", nalType)
		}
	}

	// 检查第二个 NALU 是否是 PPS
	if len(nalus) > 1 && len(nalus[1]) > 0 {
		nalType := nalus[1][0] & 0x1F
		if nalType != 8 {
			t.Errorf("第二个 NALU 不是 PPS: %d", nalType)
		}
	}

	t.Log("NALU 分割测试通过")
}
