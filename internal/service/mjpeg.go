package service

import (
	"time"
)

// MJPEGFrames 持续发送 JPEG 帧给 HTTP 客户端（multipart/x-mixed-replace）。
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
