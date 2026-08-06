package service

import (
	"log"
	"sync"
)

// PSDemuxer 解封装 MPEG-2 PS (Program Stream)，提取 H.264/H.265 原始 ES 流。
// GB28181 视频流封装：RTP → PS → PES → H.264/H.265 ES
type PSDemuxer struct {
	// 接收到的 PS 数据缓冲
	mu     sync.Mutex
	buffer []byte

	// 输出的 ES 回调
	onNALU func([]byte)
}

func NewPSDemuxer(onNALU func([]byte)) *PSDemuxer {
	return &PSDemuxer{
		onNALU: onNALU,
	}
}

// Feed 喂入一段 PS 数据。
func (d *PSDemuxer) Feed(data []byte) {
	d.mu.Lock()
	d.buffer = append(d.buffer, data...)
	d.parseAvailable()
	d.mu.Unlock()
}

// parseAvailable 尝试解析所有可用的 PS 结构。
func (d *PSDemuxer) parseAvailable() {
	for {
		consumed := d.parseOne()
		if consumed <= 0 {
			return
		}
		d.buffer = d.buffer[consumed:]
	}
}

// parseOne 解析一个 PS 结构，返回消耗的字节数。
func (d *PSDemuxer) parseOne() int {
	buf := d.buffer
	if len(buf) < 4 {
		return 0
	}

	// 查找 start code: 0x000001xx
	for i := 0; i < len(buf)-3; i++ {
		if buf[i] == 0x00 && buf[i+1] == 0x00 && buf[i+2] == 0x01 {
			if i > 0 {
				// 丢弃前面的无效数据
				d.buffer = d.buffer[i:]
				buf = d.buffer
			}
			break
		}
	}

	if len(buf) < 4 {
		return 0
	}

	code := buf[3]

	switch {
	case code == 0xBA:
		// Pack header (0x000001BA)
		return d.parsePackHeader(buf)

	case code == 0xBB:
		// System header (0x000001BB)
		return d.parseSystemHeader(buf)

	case code == 0xBC:
		// Program stream map
		return d.parsePSM(buf)

	case code >= 0xBD && code <= 0xBF:
		// PES: 0xBD = private stream 1 (audio typically), 0xBE/0xBF padding/other
		return d.parsePES(buf, code)

	case code >= 0xE0 && code <= 0xEF:
		// Video PES stream
		return d.parsePES(buf, code)

	default:
		// 未知代码，跳过 start code
		return 4
	}
}

// parsePackHeader 解析 PS Pack 头。
// MPEG-2 格式：
//   0x000001BA + 6 bytes MPEG-2 system clock + 3 bytes mux rate + stuffing
func (d *PSDemuxer) parsePackHeader(buf []byte) int {
	if len(buf) < 14 {
		return 0 // 需要更多数据
	}
	// 简单的 MPEG-2 pack header 固定 14 字节
	// 实际实现应检查 marker bits 以区分 MPEG-1 和 MPEG-2
	mpeg2Flag := (buf[4] >> 6) & 0x01
	if mpeg2Flag == 1 {
		// MPEG-2: 14 bytes
		return 14
	}
	// MPEG-1: 12 bytes
	return 12
}

// parseSystemHeader 解析系统头。
func (d *PSDemuxer) parseSystemHeader(buf []byte) int {
	if len(buf) < 6 {
		return 0
	}
	// 结构: 0x000001BB + length(2 bytes) + payload
	length := int(buf[4])<<8 | int(buf[5])
	total := 6 + length
	if len(buf) < total {
		return 0
	}
	return total
}

// parsePSM 解析 Program Stream Map。
func (d *PSDemuxer) parsePSM(buf []byte) int {
	if len(buf) < 6 {
		return 0
	}
	length := int(buf[4])<<8 | int(buf[5])
	total := 6 + length
	if len(buf) < total {
		return 0
	}
	return total
}

// parsePES 解析 PES 包，提取 ES 数据。
func (d *PSDemuxer) parsePES(buf []byte, streamID byte) int {
	if len(buf) < 6 {
		return 0
	}

	// PES packet length (2 bytes, 可能为 0 表示未限制)
	pesLen := int(buf[4])<<8 | int(buf[5])

	if len(buf) < 6 {
		return 0
	}

	// PES optional header (至少 3 bytes):
	// byte 6: '10' (2 bits) + PES_scrambling (2) + PES_priority (1) + data_alignment (1) + copyright (1) + original (1)
	// byte 7: PTS_DTS_flags (2) + ESCR (1) + ES_rate (1) + DSM (1) + add_copy (1) + CRC (1) + ext (1)
	// byte 8: PES header length (N)
	if len(buf) < 9 {
		return 0
	}

	pesHeaderLen := int(buf[8])
	payloadStart := 9 + pesHeaderLen

	// 如果 pesLen > 0, total = 6 + pesLen
	// 否则需要依赖 stream 解析
	var total int
	if pesLen > 0 {
		total = 6 + pesLen
		if len(buf) < total {
			return 0
		}
	} else {
		// 对于未限定长度的 PES (常见于视频)，
		// 解析到下一个 start code 为止
		total = len(buf)
		for i := payloadStart + 1; i < len(buf)-2; i++ {
			if buf[i] == 0x00 && buf[i+1] == 0x00 && buf[i+2] == 0x01 {
				total = i
				break
			}
		}
	}

	if payloadStart >= total {
		return total
	}

	esData := buf[payloadStart:total]

	// 对于视频 PES (streamID 0xE0-0xEF), 提取 ES 数据
	if streamID >= 0xE0 && streamID <= 0xEF && d.onNALU != nil {
		// ES 数据是连续的 H.264/H.265 Annex B 流
		// 通过 start code 分割 NALU
		d.extractNALUs(esData)
	}

	return total
}

// extractNALUs 从 ES 数据中提取 NALU 并通过回调发送。
func (d *PSDemuxer) extractNALUs(data []byte) {
	// 查找所有 0x000001 或 0x00000001 start codes
	start := -1
	for i := 0; i < len(data)-2; i++ {
		if data[i] == 0x00 && data[i+1] == 0x00 {
			if data[i+2] == 0x01 {
				// 找到 start code
				if start >= 0 {
					// 发送前一个 NALU
					nalu := data[start:i]
					if len(nalu) > 0 {
						// 去掉可能的 trailing 0x00
						for len(nalu) > 0 && nalu[len(nalu)-1] == 0 {
							nalu = nalu[:len(nalu)-1]
						}
						if len(nalu) > 0 {
							d.onNALU(nalu)
						}
					}
				}
				start = i + 3
				i += 2
				continue
			}
			if i+3 < len(data) && data[i+2] == 0x00 && data[i+3] == 0x01 {
				// 4-byte start code
				if start >= 0 {
					nalu := data[start:i]
					for len(nalu) > 0 && nalu[len(nalu)-1] == 0 {
						nalu = nalu[:len(nalu)-1]
					}
					if len(nalu) > 0 {
						d.onNALU(nalu)
					}
				}
				start = i + 4
				i += 3
				continue
			}
		}
	}

	// 发送最后一个 NALU（到数据末尾）
	if start >= 0 && start < len(data) {
		nalu := data[start:]
		for len(nalu) > 0 && nalu[len(nalu)-1] == 0 {
			nalu = nalu[:len(nalu)-1]
		}
		if len(nalu) > 0 {
			d.onNALU(nalu)
		}
	}
}

// LogNALUType 辅助函数：打印 NALU 类型。
func LogNALUType(data []byte) {
	if len(data) == 0 {
		return
	}
	nalType := data[0] & 0x1F
	types := map[byte]string{
		1:  "Non-IDR slice",
		5:  "IDR slice",
		6:  "SEI",
		7:  "SPS",
		8:  "PPS",
		9:  "AUD",
		15: "Slice extension",
	}
	if name, ok := types[nalType]; ok {
		log.Printf("[PS] NALU type %d (%s), len=%d", nalType, name, len(data))
	} else {
		log.Printf("[PS] NALU type %d, len=%d", nalType, len(data))
	}
}
