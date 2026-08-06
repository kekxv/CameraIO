package api

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"CameraIO/internal/service"

	"github.com/gin-gonic/gin"
)

func (h *Handler) StartRecording(c *gin.Context) {
	var req service.StartRecordingInput
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	recording, err := h.recorderSvc.StartRecording(&req)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	created(c, recording)
}

func (h *Handler) StopRecording(c *gin.Context) {
	var req service.StopRecordingInput
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.recorderSvc.StopRecording(req.RecordingID); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"message": "recording stopped"})
}

func (h *Handler) ListRecordings(c *gin.Context) {
	query := service.RecordingQuery{}
	if v := c.Query("camera_id"); v != "" {
		id, err := strconv.ParseUint(v, 10, 64)
		if err == nil {
			uid := uint(id)
			query.CameraID = &uid
		}
	}
	if v := c.Query("status"); v != "" {
		query.Status = &v
	}
	if v := c.Query("page"); v != "" {
		query.Page, _ = strconv.Atoi(v)
	}
	if v := c.Query("page_size"); v != "" {
		query.PageSize, _ = strconv.Atoi(v)
	}

	recs, total, err := h.recorderSvc.List(query)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{
		"recordings": recs,
		"total":      total,
		"page":       query.Page,
		"page_size":  query.PageSize,
	})
}

func (h *Handler) GetRecording(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		return
	}
	rec, err := h.recorderSvc.GetRecording(id)
	if err != nil {
		fail(c, http.StatusNotFound, err.Error())
		return
	}
	ok(c, rec)
}

// DeleteRecording 删除录像记录和视频文件。
func (h *Handler) DeleteRecording(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		return
	}
	if err := h.recorderSvc.DeleteRecording(id); err != nil {
		fail(c, http.StatusNotFound, err.Error())
		return
	}
	noContent(c)
}

// DownloadRecording 下载录像文件，支持 HTTP Range 断点续传。
func (h *Handler) DownloadRecording(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		return
	}
	rec, err := h.recorderSvc.GetRecording(id)
	if err != nil {
		fail(c, http.StatusNotFound, err.Error())
		return
	}

	file, err := os.Open(rec.FilePath)
	if err != nil {
		fail(c, http.StatusNotFound, "file not found")
		return
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		fail(c, http.StatusInternalServerError, "stat file")
		return
	}
	fileSize := stat.Size()

	// 根据格式确定 Content-Type 和文件名
	ext := filepath.Ext(rec.FilePath)
	contentType := "video/mp4"
	fileName := fmt.Sprintf("recording_%d.mp4", id)
	switch ext {
	case ".webm":
		contentType = "video/webm"
		fileName = fmt.Sprintf("recording_%d.webm", id)
	case ".ts":
		contentType = "video/mp2t"
		fileName = fmt.Sprintf("recording_%d.ts", id)
	}

	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, fileName))
	c.Header("Accept-Ranges", "bytes")

	// 解析 Range 头
	rangeHeader := c.GetHeader("Range")
	if rangeHeader == "" {
		// 全量下载
		c.Header("Content-Length", strconv.FormatInt(fileSize, 10))
		c.Header("Content-Type", contentType)
		c.Status(http.StatusOK)
		io.Copy(c.Writer, file)
		return
	}

	// 解析 "bytes=START-END"
	if !strings.HasPrefix(rangeHeader, "bytes=") {
		fail(c, http.StatusRequestedRangeNotSatisfiable, "invalid range")
		return
	}
	rangeSpec := strings.TrimPrefix(rangeHeader, "bytes=")
	parts := strings.Split(rangeSpec, "-")
	if len(parts) != 2 {
		fail(c, http.StatusRequestedRangeNotSatisfiable, "invalid range")
		return
	}

	var start, end int64
	if parts[0] != "" {
		start, err = strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			fail(c, http.StatusRequestedRangeNotSatisfiable, "invalid range start")
			return
		}
	}
	if parts[1] != "" {
		end, err = strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			fail(c, http.StatusRequestedRangeNotSatisfiable, "invalid range end")
			return
		}
	} else {
		end = fileSize - 1
	}

	if start > end || start >= fileSize {
		fail(c, http.StatusRequestedRangeNotSatisfiable, "range not satisfiable")
		return
	}

	contentLength := end - start + 1
	if _, err := file.Seek(start, io.SeekStart); err != nil {
		fail(c, http.StatusInternalServerError, "seek failed")
		return
	}

	c.Header("Content-Length", strconv.FormatInt(contentLength, 10))
	c.Header("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, fileSize))
	c.Header("Content-Type", contentType)
	c.Status(http.StatusPartialContent)
	io.CopyN(c.Writer, file, contentLength)
}
