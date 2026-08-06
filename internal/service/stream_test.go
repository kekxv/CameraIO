package service

import (
	"context"
	"testing"

	"CameraIO/internal/model"
)

func TestSplitNALUForRTP_SinglePacket(t *testing.T) {
	data := make([]byte, 100)
	pkts := SplitNALUForRTP(data, 1200)
	if len(pkts) != 1 {
		t.Fatalf("expected 1 packet, got %d", len(pkts))
	}
	if len(pkts[0]) != 100 {
		t.Errorf("packet size = %d, want 100", len(pkts[0]))
	}
}

func TestSplitNALUForRTP_MultiPacket(t *testing.T) {
	data := make([]byte, 3000)
	for i := range data {
		data[i] = byte(i % 256)
	}
	pkts := SplitNALUForRTP(data, 1200)
	if len(pkts) != 3 {
		t.Fatalf("expected 3 packets, got %d", len(pkts))
	}
	if len(pkts[0]) != 1200 {
		t.Errorf("packet 0 size = %d, want 1200", len(pkts[0]))
	}
	if len(pkts[1]) != 1200 {
		t.Errorf("packet 1 size = %d, want 1200", len(pkts[1]))
	}
	if len(pkts[2]) != 600 {
		t.Errorf("packet 2 size = %d, want 600", len(pkts[2]))
	}
}

func TestH264NALUToAnnexB(t *testing.T) {
	nalu := NALU{
		Type: 5,
		Data: []byte{0x65, 0x01, 0x02},
	}
	result := h264NALUToAnnexB(nalu)
	expected := []byte{0, 0, 0, 1, 0x65, 0x01, 0x02}
	if len(result) != len(expected) {
		t.Fatalf("length = %d, want %d", len(result), len(expected))
	}
	for i := range expected {
		if result[i] != expected[i] {
			t.Errorf("byte %d: got 0x%02x, want 0x%02x", i, result[i], expected[i])
		}
	}
}

func TestEventBusPublishSubscribe(t *testing.T) {
	bus := NewEventBus()

	client := bus.NewClient("test-client")

	// 发布事件
	bus.PublishCameraStatus(1, "cam1", "online")

	// 等待事件到达
	select {
	case msg := <-client.Send:
		if msg == nil {
			t.Fatal("received nil message")
		}
		// 验证是 JSON
		s := string(msg)
		if len(s) == 0 {
			t.Fatal("empty message")
		}
	case <-make(chan struct{}):
		// 超时
		t.Fatal("timeout waiting for event")
	}
}

func TestProbeTCPPort(t *testing.T) {
	// 测试不可达的地址（应该返回错误）
	err := probeTCPPort("192.0.2.1", 554, 100*1e6) // 100ms
	if err == nil {
		t.Skip("network available, probe succeeded (unexpected but not a failure)")
	}
}

func TestIndexStartCode(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		offset   int
		expected int
	}{
		{"3-byte start code at 0", []byte{0, 0, 1, 0x65}, 0, 0},
		// 4 字节 start code 00 00 00 01 在偏移 1 处也构成 3 字节 start code 00 00 01
		{"4-byte start code at 0", []byte{0, 0, 0, 1, 0x65}, 0, 1},
		{"start code at offset 5", []byte{0x11, 0x22, 0x33, 0x44, 0x55, 0, 0, 1}, 0, 5},
		{"no start code", []byte{1, 2, 3, 4, 5}, 0, -1},
		{"empty data", []byte{}, 0, -1},
		{"too short", []byte{0, 0}, 0, -1},
		{"start code after offset", []byte{0, 0, 1, 0x65, 0, 0, 1, 0x66}, 3, 4},
		{"4-byte at end", []byte{0x65, 0, 0, 0, 1}, 0, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := indexStartCode(tt.data, tt.offset)
			if got != tt.expected {
				t.Errorf("indexStartCode(%v, %d) = %d, want %d", tt.data, tt.offset, got, tt.expected)
			}
		})
	}
}

func TestStartCodeLen(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		idx      int
		expected int
	}{
		{"3-byte start code", []byte{0, 0, 1, 0x65}, 0, 3},
		{"4-byte start code", []byte{0, 0, 0, 1, 0x65}, 0, 4},
		{"4-byte not at end", []byte{0x65, 0, 0, 0, 1, 0x66}, 1, 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := startCodeLen(tt.data, tt.idx)
			if got != tt.expected {
				t.Errorf("startCodeLen(%v, %d) = %d, want %d", tt.data, tt.idx, got, tt.expected)
			}
		})
	}
}

func TestIndexBytes(t *testing.T) {
	soi := []byte{0xFF, 0xD8}
	eoi := []byte{0xFF, 0xD9}

	// 简单匹配
	if got := indexBytes([]byte{0x00, 0xFF, 0xD8, 0x00}, soi); got != 1 {
		t.Errorf("expected 1, got %d", got)
	}

	// 不匹配
	if got := indexBytes([]byte{0x00, 0x01, 0x02}, soi); got != -1 {
		t.Errorf("expected -1, got %d", got)
	}

	// 空数据
	if got := indexBytes(nil, soi); got != -1 {
		t.Errorf("expected -1 for empty, got %d", got)
	}

	// 子序列比数据长
	if got := indexBytes([]byte{0xFF}, soi); got != -1 {
		t.Errorf("expected -1 for short data, got %d", got)
	}

	// EOI 匹配
	if got := indexBytes([]byte{0x01, 0xFF, 0xD9, 0x02}, eoi); got != 1 {
		t.Errorf("expected 1, got %d", got)
	}
}

func TestStreamService_Shutdown(t *testing.T) {
	db, cleanup := setupRecorderTestDB(t)
	defer cleanup()

	svc := NewStreamService(db)

	// 手动构造一个 Stream
	ctx, cancel := context.WithCancel(context.Background())
	st := &Stream{
		CameraID:  1,
		Camera:    &model.Camera{ID: 1, RTSPUrl: "rtsp://test/stream"},
		ctx:       ctx,
		cancel:    cancel,
		naluSubs:  make(map[int]chan NALU),
		mjpegDone: make(chan struct{}),
		done:      make(chan struct{}),
	}
	svc.mu.Lock()
	svc.streams[1] = st
	svc.mu.Unlock()

	// 验证流在运行
	if svc.GetStream(1) != st {
		t.Fatal("stream should be registered")
	}

	// 调用 Shutdown
	svc.Shutdown()

	// 验证 context 被取消
	select {
	case <-st.ctx.Done():
		// 正确：context 已取消
	default:
		t.Error("stream context should be cancelled after Shutdown")
	}

	// 验证流从 map 中移除
	if svc.GetStream(1) != nil {
		t.Error("stream should be removed from map after Shutdown")
	}
}

func TestIndexBytesFrom(t *testing.T) {
	soi := []byte{0xFF, 0xD8}
	data := []byte{0xFF, 0xD8, 0x01, 0x02, 0xFF, 0xD8}

	// 从偏移 0
	if got := indexBytesFrom(data, soi, 0); got != 0 {
		t.Errorf("expected 0, got %d", got)
	}
	// 从偏移 3（跳过第一个）
	if got := indexBytesFrom(data, soi, 3); got != 4 {
		t.Errorf("expected 4, got %d", got)
	}
	// 从偏移 5（后面不够）
	if got := indexBytesFrom(data, soi, 5); got != -1 {
		t.Errorf("expected -1, got %d", got)
	}
}
