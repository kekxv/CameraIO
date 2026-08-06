package service

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

// WebRTCService 管理 WebRTC 会话：从 Stream 订阅 NALU 并推送给 Pion PeerConnection。
type WebRTCService struct {
	api *webrtc.API
}

func NewWebRTCService() *WebRTCService {
	// 使用默认的 Pion API，可在此处注入 ICE/DTLS 配置。
	return &WebRTCService{
		api: webrtc.NewAPI(),
	}
}

// WebRTCOfferRequest 前端发送的 WebRTC 信令请求。
type WebRTCOfferRequest struct {
	SDP string `json:"sdp"`
}

// WebRTCOfferResponse 服务端返回的 Answer SDP。
type WebRTCOfferResponse struct {
	SDP string `json:"sdp"`
}

// HandleOffer 接收前端的 Offer SDP，创建 PeerConnection，绑定 Stream，返回 Answer SDP。
func (s *WebRTCService) HandleOffer(ctx context.Context, stream *Stream, offerSDP string) (string, error) {
	// 1. 创建 PeerConnection
	pc, err := s.api.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
		},
	})
	if err != nil {
		return "", fmt.Errorf("create peer connection: %w", err)
	}

	// 2. 创建 H.264 视频轨道
	track, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264, ClockRate: 90000},
		"video",
		"cameraio",
	)
	if err != nil {
		pc.Close()
		return "", fmt.Errorf("create track: %w", err)
	}

	sender, err := pc.AddTrack(track)
	if err != nil {
		pc.Close()
		return "", fmt.Errorf("add track: %w", err)
	}

	// 读取 RTCP（Pion 要求读取 RTCP 以获取 PLI/FIR 请求）
	go func() {
		rtcpBuf := make([]byte, 1500)
		for {
			_, _, err := sender.Read(rtcpBuf)
			if err != nil {
				return
			}
		}
	}()

	// 3. 订阅 Stream 的 NALU，打包为 RTP 发送给 PeerConnection
	naluCh, unsub := stream.Subscribe()
	done := make(chan struct{})

	// 当 PeerConnection 关闭时，取消订阅
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		if state == webrtc.PeerConnectionStateClosed ||
			state == webrtc.PeerConnectionStateFailed {
			unsub()
			pc.Close()
			close(done)
		}
	})

	// 启动 NALU → RTP 发送循环
	go func() {
		seqNum := uint16(0)
		ts := uint32(0)
		for {
			select {
			case <-done:
				return
			case nalu, ok := <-naluCh:
				if !ok {
					return
				}
				if err := s.sendNALUAsRTP(track, nalu, &seqNum, &ts); err != nil {
					log.Printf("[webrtc] send RTP error: %v", err)
				}
			}
		}
	}()

	// 4. 设置远端 SDP（Offer）
	offer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  offerSDP,
	}
	if err := pc.SetRemoteDescription(offer); err != nil {
		pc.Close()
		return "", fmt.Errorf("set remote description: %w", err)
	}

	// 5. 创建 Answer SDP
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		pc.Close()
		return "", fmt.Errorf("create answer: %w", err)
	}
	if err := pc.SetLocalDescription(answer); err != nil {
		pc.Close()
		return "", fmt.Errorf("set local description: %w", err)
	}

	// 等待 ICE 收集完成（最多 3 秒）
	gatherDone := make(chan struct{})
	pc.OnICEGatheringStateChange(func(state webrtc.ICEGatheringState) {
		if state == webrtc.ICEGatheringStateComplete {
			close(gatherDone)
		}
	})
	select {
	case <-gatherDone:
	case <-time.After(3 * time.Second):
	}

	return pc.LocalDescription().SDP, nil
}

// sendNALUAsRTP 将一个 H.264 NALU 打包为一个或多个 RTP 包并写入 Track。
func (s *WebRTCService) sendNALUAsRTP(track *webrtc.TrackLocalStaticRTP, nalu NALU, seq *uint16, ts *uint32) error {
	payload := nalu.Data
	if len(payload) == 0 {
		return nil
	}

	const maxPayload = 1200

	if len(payload) <= maxPayload {
		// 单包模式 (Single NAL Unit Packet)
		pkt := &rtp.Packet{
			Header: rtp.Header{
				Version:        2,
				PayloadType:    96,
				SequenceNumber: *seq,
				Timestamp:      *ts,
				Marker:         true, // 帧结束标记
				SSRC:           0,
			},
			Payload: payload,
		}
		*seq++
		*ts += 3000 // 90kHz / 30fps = 3000
		if err := track.WriteRTP(pkt); err != nil {
			return err
		}
		return nil
	}

	// FU-A 分片模式
	first := true
	for len(payload) > 0 {
		end := maxPayload
		if end > len(payload) {
			end = len(payload)
		}
		chunk := payload[:end]
		payload = payload[end:]

		isLast := len(payload) == 0

		// FU indicator: forbidden(0) + NRI(2 bits from original NAL) + Type(28=FU-A)
		nri := (nalu.Data[0] >> 5) & 0x03
		fuIndicator := (nri << 5) | 28

		// FU header: S(Start) + E(End) + R(0) + Type(original NAL type)
		fuHeader := byte(nalu.Type)
		if first {
			fuHeader |= 0x80 // S bit
			first = false
		}
		if isLast {
			fuHeader |= 0x40 // E bit
		}

		// RTP payload = FU indicator + FU header + chunk data
		// 跳过原始 NAL header byte (第一个 byte)
		dataStart := 0
		if first && len(chunk) > 0 {
			dataStart = 1
		}
		if !first && len(chunk) > 0 {
			dataStart = 1 // 跳过 NAL header
		}
		fuPayload := make([]byte, 2+len(chunk[dataStart:]))
		fuPayload[0] = fuIndicator
		fuPayload[1] = fuHeader
		copy(fuPayload[2:], chunk[dataStart:])

		pkt := &rtp.Packet{
			Header: rtp.Header{
				Version:        2,
				PayloadType:    96,
				SequenceNumber: *seq,
				Timestamp:      *ts,
				Marker:         isLast,
			},
			Payload: fuPayload,
		}
		*seq++
		if isLast {
			*ts += 3000
		}
		if err := track.WriteRTP(pkt); err != nil {
			return err
		}
	}
	return nil
}

// ---------- MJPEG 辅助 ----------

// StreamMJPEG 持续发送 JPEG 帧给 HTTP 客户端（multipart/x-mixed-replace）。
// 返回的 channel 用于接收 JPEG 数据。
func (st *Stream) MJPEGFrames() <-chan []byte {
	ch := make(chan []byte, 8)
	go func() {
		defer close(ch)
		ticker := time.NewTicker(50 * time.Millisecond) // 最多 20 FPS
		defer ticker.Stop()
		for {
			select {
			case <-st.ctx.Done():
				return
			case <-ticker.C:
				jpg := st.GetLatestJPEG()
				if jpg != nil {
					select {
					case ch <- jpg:
					default:
					}
				}
			}
		}
	}()
	return ch
}

// ---------- 辅助：将 NALU 转换为媒体 Sample（用于 Pion WriteSample） ----------

func naluToSample(nalu NALU, ts time.Duration) media.Sample {
	return media.Sample{
		Data:     h264NALUToAnnexB(nalu),
		Duration: 33 * time.Millisecond, // ~30fps
	}
}
