package api

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

func (h *Handler) StartStream(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		return
	}
	if err := h.streamSvc.StartStream(id); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"message": "stream started"})
}

func (h *Handler) StopStream(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		return
	}
	h.streamSvc.StopStream(id)
	ok(c, gin.H{"message": "stream stopped"})
}

func (h *Handler) StreamMJPEG(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		return
	}

	stream := h.streamSvc.GetStream(id)
	if stream == nil {
		// 自动启动流
		if err := h.streamSvc.StartStream(id); err != nil {
			fail(c, http.StatusInternalServerError, err.Error())
			return
		}
		stream = h.streamSvc.GetStream(id)
	}

	// 等待第一帧 JPEG 出现（最多 10 秒），同时响应客户端断开和流停止
	deadline := time.Now().Add(10 * time.Second)
	for stream.GetLatestJPEG() == nil {
		select {
		case <-c.Request.Context().Done():
			return // 客户端断开
		case <-stream.Done():
			return // 流已停止
		default:
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	c.Writer.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=--frame")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "close")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		fail(c, http.StatusInternalServerError, "streaming not supported")
		return
	}

	frames := stream.MJPEGFrames()
	for {
		select {
		case <-c.Request.Context().Done():
			return // 客户端断开
		case <-stream.Done():
			return // 流已停止
		case jpg, ok := <-frames:
			if !ok {
				return
			}
			io.WriteString(c.Writer, "--frame\r\n")
			io.WriteString(c.Writer, "Content-Type: image/jpeg\r\n")
			io.WriteString(c.Writer, fmt.Sprintf("Content-Length: %d\r\n\r\n", len(jpg)))
			c.Writer.Write(jpg)
			io.WriteString(c.Writer, "\r\n")
			flusher.Flush()
		}
	}
}
