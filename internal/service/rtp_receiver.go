package service

import (
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"sync"
	"time"
)

// RTPReceiver 监听指定 UDP 端口，接收 GB28181 RTP 包并重组为 PS 流。
type RTPReceiver struct {
	Port     int
	conn     *net.UDPConn
	onPSData func([]byte) // PS 数据回调
	stopCh   chan struct{}
	wg       sync.WaitGroup

	// RTP 重组缓冲（处理乱序包）
	mu        sync.Mutex
	rtpBuf    map[uint16][]byte // seq → payload
	highestSeq uint16
}

func NewRTPReceiver(port int, onPSData func([]byte)) *RTPReceiver {
	return &RTPReceiver{
		Port:     port,
		onPSData: onPSData,
		stopCh:   make(chan struct{}),
		rtpBuf:   make(map[uint16][]byte),
	}
}

// Start 开始监听 RTP 端口。
func (r *RTPReceiver) Start() error {
	addr := &net.UDPAddr{Port: r.Port}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return fmt.Errorf("listen RTP port %d: %w", r.Port, err)
	}
	r.conn = conn

	r.wg.Add(1)
	go r.recvLoop()

	log.Printf("[RTP] receiver started on port %d", r.Port)
	return nil
}

// Stop 停止接收。
func (r *RTPReceiver) Stop() {
	close(r.stopCh)
	if r.conn != nil {
		r.conn.Close()
	}
	r.wg.Wait()
}

func (r *RTPReceiver) recvLoop() {
	defer r.wg.Done()
	buf := make([]byte, 64*1024)
	for {
		select {
		case <-r.stopCh:
			return
		default:
		}

		r.conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		n, _, err := r.conn.ReadFromUDP(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				// 30 秒无数据，视为流结束
				log.Printf("[RTP] port %d timeout, stopping", r.Port)
				return
			}
			select {
			case <-r.stopCh:
				return
			default:
				continue
			}
		}

		if n < 12 {
			continue // RTP 头最小 12 字节
		}

		// 解析 RTP 头
		payload := r.parseRTP(buf[:n])
		if payload != nil && r.onPSData != nil {
			r.onPSData(payload)
		}
	}
}

// parseRTP 解析 RTP 包，提取 payload。
// RTP 头结构：
//   0                   1                   2                   3
//   0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
//  +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//  |V=2|P|X|  CC   |M|     PT      |       sequence number         |
//  +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//  |                           timestamp                           |
//  +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//  |           synchronization source (SSRC) identifier            |
//  +=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+=+
func (r *RTPReceiver) parseRTP(data []byte) []byte {
	if len(data) < 12 {
		return nil
	}

	// byte 0: V(2) | P(1) | X(1) | CC(4)
	version := (data[0] >> 6) & 0x03
	if version != 2 {
		return nil
	}

	hasPadding := (data[0] >> 5) & 0x01
	hasExtension := (data[0] >> 4) & 0x01
	cc := int(data[0] & 0x0F)

	// PT (payload type): byte 1
	// Sequence number: bytes 2-3
	// Timestamp: bytes 4-7
	// SSRC: bytes 8-11

	offset := 12 + cc*4 // 固定头 + CSRC

	// 扩展头
	if hasExtension == 1 {
		if len(data) < offset+4 {
			return nil
		}
		extLen := int(binary.BigEndian.Uint16(data[offset+2:])) * 4
		offset += 4 + extLen
	}

	if offset >= len(data) {
		return nil
	}

	payload := data[offset:]

	// 去除 padding
	if hasPadding == 1 && len(payload) > 0 {
		padLen := int(payload[len(payload)-1])
		if padLen < len(payload) {
			payload = payload[:len(payload)-padLen]
		}
	}

	return payload
}
