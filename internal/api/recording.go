package api

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"CameraIO/internal/model"
	"CameraIO/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type startRecordingResponse struct {
	*model.Recording
	RecordingID uint `json:"recording_id"`
}

func (h *Handler) StartRecording(c *gin.Context) {
	var req service.StartRecordingInput
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	recording, err := h.recorderSvc.StartRecording(&req)
	if err != nil {
		var validationErr *service.RecordingValidationError
		if errors.As(err, &validationErr) {
			fail(c, http.StatusBadRequest, validationErr.Error())
			return
		}
		var unavailableErr *service.RecorderUnavailableError
		if errors.As(err, &unavailableErr) {
			fail(c, http.StatusServiceUnavailable, unavailableErr.Error())
			return
		}
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	created(c, startRecordingResponse{Recording: recording, RecordingID: recording.ID})
}

// RecordingHeartbeat renews the lease of an active manual recording session.
func (h *Handler) RecordingHeartbeat(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		return
	}
	recording, err := h.recorderSvc.HeartbeatRecording(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			fail(c, http.StatusNotFound, "recording not found")
			return
		}
		var conflictErr *service.RecordingSessionConflictError
		if errors.As(err, &conflictErr) {
			fail(c, http.StatusConflict, conflictErr.Error())
			return
		}
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	heartbeatAt := recording.HeartbeatAt.UTC()
	ok(c, gin.H{
		"recording_id":     recording.ID,
		"heartbeat_at":     heartbeatAt,
		"lease_expires_at": heartbeatAt.Add(60 * time.Second),
	})
}

// GetRecordingDownloadURL returns the authenticated download endpoint for a
// finalized single-file recording.
func (h *Handler) GetRecordingDownloadURL(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		return
	}
	recording, err := h.recorderSvc.GetRecording(id)
	if err != nil {
		fail(c, http.StatusNotFound, "recording not found")
		return
	}
	if recording.Status != "completed" || recording.StorageMode == "segmented" {
		fail(c, http.StatusConflict, "recording download is not ready")
		return
	}
	ok(c, gin.H{
		"recording_id": recording.ID,
		"download_url": fmt.Sprintf("/api/v1/recordings/%d/download", recording.ID),
	})
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
	data := gin.H{
		"message":      "recording stopped",
		"recording_id": req.RecordingID,
	}
	if recording, err := h.recorderSvc.GetRecording(req.RecordingID); err == nil {
		data["storage_mode"] = recording.StorageMode
		if recording.StorageMode != "segmented" {
			data["download_url"] = fmt.Sprintf("/api/v1/recordings/%d/download", req.RecordingID)
		}
	}
	ok(c, data)
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
	if v := c.Query("start_time"); v != "" {
		parsed, valid := parseUTCQuery(c, "start_time")
		if !valid {
			fail(c, http.StatusBadRequest, "start_time must be an RFC3339 UTC timestamp")
			return
		}
		query.StartTime = &parsed
	}
	if v := c.Query("end_time"); v != "" {
		parsed, valid := parseUTCQuery(c, "end_time")
		if !valid {
			fail(c, http.StatusBadRequest, "end_time must be an RFC3339 UTC timestamp")
			return
		}
		query.EndTime = &parsed
	}
	if query.StartTime != nil && query.EndTime != nil && !query.EndTime.After(*query.StartTime) {
		fail(c, http.StatusBadRequest, "start_time must be before end_time")
		return
	}
	if v := c.Query("page"); v != "" {
		query.Page, _ = strconv.Atoi(v)
	}
	if v := c.Query("page_size"); v != "" {
		query.PageSize, _ = strconv.Atoi(v)
	}

	recs, total, err := h.recorderSvc.ListHistory(query)
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

func (h *Handler) RecordingTimeline(c *gin.Context) {
	cameraID, validCameraID := parsePositiveUintQuery(c, "camera_id")
	if !validCameraID {
		return
	}
	from, fromOK := parseUTCQuery(c, "from")
	to, toOK := parseUTCQuery(c, "to")
	if !fromOK || !toOK {
		fail(c, http.StatusBadRequest, "from and to must be RFC3339 UTC timestamps")
		return
	}
	if !to.After(from) {
		fail(c, http.StatusBadRequest, "from must be before to")
		return
	}
	if to.Sub(from) > 24*time.Hour {
		fail(c, http.StatusBadRequest, "timeline range must not exceed 24 hours")
		return
	}

	segments, err := h.recorderSvc.ListTimeline(service.TimelineQuery{
		CameraID: cameraID,
		From:     from,
		To:       to,
	})
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"segments": segments})
}

func (h *Handler) RecordingPlayAt(c *gin.Context) {
	cameraID, validCameraID := parsePositiveUintQuery(c, "camera_id")
	if !validCameraID {
		return
	}
	at, valid := parseUTCQuery(c, "at")
	if !valid {
		fail(c, http.StatusBadRequest, "at must be an RFC3339 UTC timestamp")
		return
	}

	point, err := h.recorderSvc.ResolvePlaybackPoint(cameraID, at)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	if point == nil {
		fail(c, http.StatusNotFound, "recording segment not found")
		return
	}

	ok(c, gin.H{
		"segment":         point.Segment,
		"media_url":       fmt.Sprintf("/api/v1/recording-segments/%d/media", point.Segment.ID),
		"offset_ms":       point.OffsetMS,
		"next_segment_id": point.NextSegmentID,
	})
}

func parsePositiveUintQuery(c *gin.Context, name string) (uint, bool) {
	value, err := strconv.ParseUint(c.Query(name), 10, 64)
	if err != nil || value == 0 {
		fail(c, http.StatusBadRequest, name+" must be a positive integer")
		return 0, false
	}
	return uint(value), true
}

func parseUTCQuery(c *gin.Context, name string) (time.Time, bool) {
	value, err := time.Parse(time.RFC3339, c.Query(name))
	if err != nil {
		return time.Time{}, false
	}
	_, offset := value.Zone()
	if offset != 0 {
		return time.Time{}, false
	}
	return value.UTC(), true
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

// RecordingSegmentMedia serves inline segment media from the path stored in
// the segment database. http.ServeContent handles standard and suffix ranges.
func (h *Handler) RecordingSegmentMedia(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		return
	}
	segment, err := h.recorderSvc.GetRecordingSegment(id)
	if err != nil {
		fail(c, http.StatusNotFound, "recording segment not found")
		return
	}
	if segment.Status != "completed" || segment.DurationMS <= 0 || !segment.EndTime.After(segment.StartTime) || segment.FileSize <= 0 {
		fail(c, http.StatusNotFound, "recording segment not found")
		return
	}

	file, err := os.Open(segment.FilePath)
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

	c.Header("Content-Type", segmentContentType(segment.Format, segment.FilePath))
	c.Header("Content-Disposition", "inline")
	http.ServeContent(c.Writer, c.Request, filepath.Base(segment.FilePath), stat.ModTime(), file)
}

func segmentContentType(format, path string) string {
	switch strings.ToLower(format) {
	case "webm":
		return "video/webm"
	case "ts":
		return "video/mp2t"
	case "mp4":
		return "video/mp4"
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".webm":
		return "video/webm"
	case ".ts":
		return "video/mp2t"
	default:
		return "video/mp4"
	}
}
