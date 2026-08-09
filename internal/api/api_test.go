package api

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"CameraIO/internal/model"
	"CameraIO/internal/pkg"
	"CameraIO/internal/service"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func digestTestHash(value string) string {
	sum := md5.Sum([]byte(value))
	return hex.EncodeToString(sum[:])
}

func digestTestParameter(authorization, name string) string {
	for _, part := range strings.Split(strings.TrimPrefix(authorization, "Digest "), ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok && key == name {
			return strings.Trim(value, `"`)
		}
	}
	return ""
}

// setupTestHandler 创建测试用的 Handler（内存 SQLite）。
func setupTestHandler(t *testing.T) *Handler {
	h, _ := setupTestHandlerWithDB(t)
	return h
}

func setupTestHandlerWithDB(t *testing.T) (*Handler, *gorm.DB) {
	t.Helper()
	// 每个测试使用独立的内存数据库
	dbName := fmt.Sprintf("file:test_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dbName), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	pkg.MigrateDB(db)

	jwtCfg := pkg.NewJWTConfig("test-secret")
	userSvc := service.NewUserService(db, jwtCfg)
	onvifSvc := service.NewONVIFService()
	cameraSvc := service.NewCameraService(db, onvifSvc)
	streamSvc := service.NewStreamService(db)
	recorderSvc := service.NewRecorderService(db, pkg.DefaultConfig())
	eventBus := service.NewEventBus()
	localCamSvc := service.NewLocalCameraService()
	discoverySvc := service.NewDiscoveryService(onvifSvc)
	scheduleSvc := service.NewScheduleService(db, recorderSvc)

	handler := NewHandler(userSvc, cameraSvc, streamSvc, recorderSvc, eventBus, localCamSvc, discoverySvc, scheduleSvc, jwtCfg)
	return handler, db
}

// createTestUser 创建测试用户并返回 JWT token。
func createTestUser(t *testing.T, h *Handler) string {
	t.Helper()
	// 用内部 service 创建用户（绕过鉴权）
	_, err := h.userSvc.Create("admin", "admin123", "admin")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	// 登录获取 token
	loginBody := `{"username":"admin","password":"admin123"}`
	loginReq := httptest.NewRequest("POST", "/api/v1/login", bytes.NewBufferString(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginW := httptest.NewRecorder()
	router := h.SetupRouter()
	router.ServeHTTP(loginW, loginReq)

	var resp struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(loginW.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse login response: %v", err)
	}
	return resp.Data.Token
}

func TestHealthEndpoint(t *testing.T) {
	h := setupTestHandler(t)
	router := h.SetupRouter()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/health", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestLoginSuccess(t *testing.T) {
	h := setupTestHandler(t)
	router := h.SetupRouter()

	// 先创建用户
	h.userSvc.Create("testuser", "password123", "admin")

	// 登录
	body := `{"username":"testuser","password":"password123"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/login", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["code"].(float64) != 0 {
		t.Errorf("code = %v, want 0", resp["code"])
	}
}

func TestLoginWrongPassword(t *testing.T) {
	h := setupTestHandler(t)
	router := h.SetupRouter()

	h.userSvc.Create("testuser", "password123", "admin")

	body := `{"username":"testuser","password":"wrong"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/login", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestUnauthorizedAccess(t *testing.T) {
	h := setupTestHandler(t)
	router := h.SetupRouter()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/cameras", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestCameraSnapshotRequiresAuthentication(t *testing.T) {
	h := setupTestHandler(t)
	router := h.SetupRouter()

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cameras/1/snapshot", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body: %s", w.Code, w.Body.String())
	}
}

func TestCameraSnapshotReturnsNotFoundForMissingCamera(t *testing.T) {
	h := setupTestHandler(t)
	token := createTestUser(t, h)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cameras/999/snapshot", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	h.SetupRouter().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body: %s", w.Code, w.Body.String())
	}
}

func TestCameraSnapshotReportsDeviceFailureDetail(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/onvif/media_service":
			requestBody, _ := io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "application/soap+xml")
			if strings.Contains(string(requestBody), "GetProfiles") {
				_, _ = w.Write([]byte(`<?xml version="1.0"?><SOAP-ENV:Envelope xmlns:SOAP-ENV="http://www.w3.org/2003/05/soap-envelope" xmlns:trt="http://www.onvif.org/ver10/media/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema"><SOAP-ENV:Body><trt:GetProfilesResponse><trt:Profiles token="profile-1"><tt:Name>MediaProfile_Channel1_MainStream</tt:Name></trt:Profiles></trt:GetProfilesResponse></SOAP-ENV:Body></SOAP-ENV:Envelope>`))
				return
			}
			if strings.Contains(string(requestBody), "GetSnapshotUri") {
				_, _ = w.Write([]byte(`<?xml version="1.0"?><SOAP-ENV:Envelope xmlns:SOAP-ENV="http://www.w3.org/2003/05/soap-envelope" xmlns:trt="http://www.onvif.org/ver10/media/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema"><SOAP-ENV:Body><trt:GetSnapshotUriResponse><trt:MediaUri><tt:Uri>` + server.URL + `/snapshot</tt:Uri></trt:MediaUri></trt:GetSnapshotUriResponse></SOAP-ENV:Body></SOAP-ENV:Envelope>`))
				return
			}
			t.Fatalf("unexpected ONVIF request: %s", requestBody)
		case "/snapshot":
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(599)
			_, _ = w.Write([]byte("LAPI error: media source unavailable"))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	h, db := setupTestHandlerWithDB(t)
	camera := model.Camera{Name: "failing snapshot camera", IP: strings.TrimPrefix(server.URL, "http://"), Port: 554, RTSPUrl: "rtsp://example.invalid/live", AccessProtocol: model.ProtocolRTSP}
	if err := db.Create(&camera).Error; err != nil {
		t.Fatalf("create camera: %v", err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cameras/"+uintToStr(camera.ID)+"/snapshot", nil)
	req.Header.Set("Authorization", "Bearer "+createTestUser(t, h))
	h.SetupRouter().ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "LAPI error: media source unavailable") {
		t.Fatalf("response does not include device failure detail: %s", w.Body.String())
	}
}

func TestCameraSnapshotAcceptsJPEGWithGenericContentTypeWithoutStartingStream(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/onvif/media_service":
			requestBody, _ := io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "application/soap+xml")
			if strings.Contains(string(requestBody), "GetProfiles") {
				_, _ = w.Write([]byte(`<?xml version="1.0"?>
<SOAP-ENV:Envelope xmlns:SOAP-ENV="http://www.w3.org/2003/05/soap-envelope" xmlns:trt="http://www.onvif.org/ver10/media/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema">
  <SOAP-ENV:Body><trt:GetProfilesResponse><trt:Profiles token="profile-1"><tt:Name>MediaProfile_Channel1_MainStream</tt:Name></trt:Profiles></trt:GetProfilesResponse></SOAP-ENV:Body>
</SOAP-ENV:Envelope>`))
				return
			}
			if strings.Contains(string(requestBody), "GetSnapshotUri") {
				_, _ = w.Write([]byte(`<?xml version="1.0"?>
<SOAP-ENV:Envelope xmlns:SOAP-ENV="http://www.w3.org/2003/05/soap-envelope" xmlns:trt="http://www.onvif.org/ver10/media/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema">
  <SOAP-ENV:Body><trt:GetSnapshotUriResponse><trt:MediaUri><tt:Uri>` + server.URL + `/snapshot</tt:Uri></trt:MediaUri></trt:GetSnapshotUriResponse></SOAP-ENV:Body>
</SOAP-ENV:Envelope>`))
				return
			}
			t.Fatalf("unexpected ONVIF request: %s", requestBody)
		case "/snapshot":
			user, pass, ok := r.BasicAuth()
			if !ok || user != "admin" || pass != "pass" {
				w.Header().Set("WWW-Authenticate", `Basic realm="camera"`)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write([]byte{0xff, 0xd8, 0xff, 0xd9})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	h, db := setupTestHandlerWithDB(t)
	camera := model.Camera{
		Name:           "snapshot camera",
		IP:             strings.TrimPrefix(server.URL, "http://"),
		Port:           554,
		RTSPUrl:        "rtsp://example.invalid/live",
		Username:       "admin",
		Password:       "pass",
		AccessProtocol: model.ProtocolRTSP,
		DeviceType:     model.DeviceTypeIPC,
	}
	if err := db.Create(&camera).Error; err != nil {
		t.Fatalf("create camera: %v", err)
	}

	token := createTestUser(t, h)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cameras/"+uintToStr(camera.ID)+"/snapshot", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	h.SetupRouter().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	if contentType := w.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "image/jpeg") {
		t.Fatalf("Content-Type = %q, want image/jpeg", contentType)
	}
	if got, want := w.Body.Bytes(), []byte{0xff, 0xd8, 0xff, 0xd9}; !bytes.Equal(got, want) {
		t.Fatalf("snapshot = %x, want %x", got, want)
	}
	if stream := h.streamSvc.GetStream(camera.ID); stream != nil {
		t.Fatal("snapshot must not start an RTSP stream")
	}
}

func TestCameraSnapshotRetriesWithDigestAuthentication(t *testing.T) {
	var server *httptest.Server
	snapshotRequests := 0
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/onvif/media_service":
			requestBody, _ := io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "application/soap+xml")
			if strings.Contains(string(requestBody), "GetProfiles") {
				_, _ = w.Write([]byte(`<?xml version="1.0"?>
<SOAP-ENV:Envelope xmlns:SOAP-ENV="http://www.w3.org/2003/05/soap-envelope" xmlns:trt="http://www.onvif.org/ver10/media/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema">
  <SOAP-ENV:Body><trt:GetProfilesResponse><trt:Profiles token="profile-1"><tt:Name>MediaProfile_Channel1_MainStream</tt:Name></trt:Profiles></trt:GetProfilesResponse></SOAP-ENV:Body>
</SOAP-ENV:Envelope>`))
				return
			}
			_, _ = w.Write([]byte(`<?xml version="1.0"?>
<SOAP-ENV:Envelope xmlns:SOAP-ENV="http://www.w3.org/2003/05/soap-envelope" xmlns:trt="http://www.onvif.org/ver10/media/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema">
  <SOAP-ENV:Body><trt:GetSnapshotUriResponse><trt:MediaUri><tt:Uri>` + server.URL + `/snapshot</tt:Uri></trt:MediaUri></trt:GetSnapshotUriResponse></SOAP-ENV:Body>
</SOAP-ENV:Envelope>`))
		case "/snapshot":
			snapshotRequests++
			if snapshotRequests == 1 {
				w.Header().Set("WWW-Authenticate", `Digest realm="camera", nonce="nonce-1", qop="auth", algorithm=MD5`)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			authorization := r.Header.Get("Authorization")
			if !strings.HasPrefix(authorization, `Digest username="admin"`) {
				t.Fatalf("authorization = %q, want Digest authentication", authorization)
			}
			cnonce := digestTestParameter(authorization, "cnonce")
			response := digestTestParameter(authorization, "response")
			expected := digestTestHash(digestTestHash("admin:camera:pass") + ":nonce-1:00000001:" + cnonce + ":auth:" + digestTestHash("GET:/snapshot"))
			if response != expected {
				t.Fatalf("Digest response = %q, want %q", response, expected)
			}
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte{0xff, 0xd8, 0xff, 0xd9})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	h, db := setupTestHandlerWithDB(t)
	camera := model.Camera{
		Name:           "digest snapshot camera",
		IP:             strings.TrimPrefix(server.URL, "http://"),
		RTSPUrl:        "rtsp://example.invalid/live",
		Username:       "admin",
		Password:       "pass",
		AccessProtocol: model.ProtocolRTSP,
	}
	if err := db.Create(&camera).Error; err != nil {
		t.Fatalf("create camera: %v", err)
	}

	token := createTestUser(t, h)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cameras/"+uintToStr(camera.ID)+"/snapshot", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	h.SetupRouter().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	if snapshotRequests != 2 {
		t.Fatalf("snapshot requests = %d, want 2", snapshotRequests)
	}
}

func TestCameraCRUD(t *testing.T) {
	h := setupTestHandler(t)
	router := h.SetupRouter()
	token := createTestUser(t, h)

	// 1. Create camera
	createBody := `{"name":"cam1","ip":"192.168.1.100","username":"admin","password":"pass"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/cameras", bytes.NewBufferString(createBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201; body: %s", w.Code, w.Body.String())
	}

	var createResp struct {
		Data struct {
			ID   uint   `json:"id"`
			Name string `json:"name"`
			IP   string `json:"ip"`
		} `json:"data"`
	}
	json.Unmarshal(w.Body.Bytes(), &createResp)
	if createResp.Data.Name != "cam1" {
		t.Errorf("name = %q, want cam1", createResp.Data.Name)
	}
	cameraID := createResp.Data.ID

	// 2. Get camera
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/api/v1/cameras/"+uintToStr(cameraID), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("get status = %d, want 200", w.Code)
	}

	// 3. List cameras
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/api/v1/cameras", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("list status = %d, want 200", w.Code)
	}

	// 4. Update camera
	updateBody := `{"name":"cam1-updated"}`
	w = httptest.NewRecorder()
	req = httptest.NewRequest("PUT", "/api/v1/cameras/"+uintToStr(cameraID), bytes.NewBufferString(updateBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("update status = %d, want 200", w.Code)
	}

	// 5. Delete camera
	w = httptest.NewRecorder()
	req = httptest.NewRequest("DELETE", "/api/v1/cameras/"+uintToStr(cameraID), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Errorf("delete status = %d, want 204", w.Code)
	}
}

func TestRecordingAPI(t *testing.T) {
	h := setupTestHandler(t)
	router := h.SetupRouter()
	token := createTestUser(t, h)

	// 先创建摄像头
	createBody := `{"name":"cam1","ip":"192.168.1.100"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/cameras", bytes.NewBufferString(createBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)

	// 查询录像列表（应该为空）
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/api/v1/recordings", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("list recordings status = %d, want 200", w.Code)
	}
}

func TestRecordingListFilters(t *testing.T) {
	h := setupTestHandler(t)
	router := h.SetupRouter()
	token := createTestUser(t, h)

	// 测试分页参数
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/recordings?page=1&page_size=5", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("list with pagination status = %d, want 200", w.Code)
	}

	// 测试按摄像头过滤
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/api/v1/recordings?camera_id=1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("list with camera filter status = %d, want 200", w.Code)
	}

	// 测试按状态过滤
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/api/v1/recordings?status=completed", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("list with status filter status = %d, want 200", w.Code)
	}
}

func TestRecordingTimelineRejectsMalformedUTCTimestamps(t *testing.T) {
	h := setupTestHandler(t)
	router := h.SetupRouter()
	token := createTestUser(t, h)

	for _, query := range []url.Values{
		{"camera_id": {"1"}, "from": {"not-a-time"}, "to": {"2026-08-08T11:00:00Z"}},
		{"camera_id": {"1"}, "from": {"2026-08-08T10:00:00Z"}, "to": {"2026-08-08T11:00:00"}},
		{"camera_id": {"1"}, "from": {"2026-08-08T10:00:00+01:00"}, "to": {"2026-08-08T11:00:00Z"}},
	} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/recordings/timeline?"+query.Encode(), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		router.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("timeline malformed UTC status = %d, want 400: %s", w.Code, w.Body.String())
		}
		var resp response
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode timeline validation response: %v", err)
		}
		if resp.Message != "from and to must be RFC3339 UTC timestamps" {
			t.Fatalf("timeline malformed UTC message = %q, want RFC3339 UTC validation", resp.Message)
		}
	}
}

func TestRecordingTimelineRejectsRangesLongerThan24Hours(t *testing.T) {
	h := setupTestHandler(t)
	router := h.SetupRouter()
	token := createTestUser(t, h)
	query := url.Values{
		"camera_id": {"1"},
		"from":      {"2026-08-08T10:00:00Z"},
		"to":        {"2026-08-09T10:00:01Z"},
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/recordings/timeline?"+query.Encode(), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("timeline over 24 hours status = %d, want 400: %s", w.Code, w.Body.String())
	}
	var resp response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode timeline range response: %v", err)
	}
	if resp.Message != "timeline range must not exceed 24 hours" {
		t.Fatalf("timeline over 24 hours message = %q, want range validation", resp.Message)
	}
}

func TestRecordingTimelineReturnsIntervalSpanningSegmentForRequestedCamera(t *testing.T) {
	h, db := setupTestHandlerWithDB(t)
	router := h.SetupRouter()
	token := createTestUser(t, h)
	start := time.Date(2026, 8, 8, 9, 55, 0, 0, time.UTC)
	wanted := model.RecordingSegment{
		RecordingID: 41,
		CameraID:    7,
		Sequence:    1,
		FilePath:    filepath.Join(t.TempDir(), "wanted.mp4"),
		FileSize:    4096,
		StartTime:   start,
		EndTime:     start.Add(10 * time.Minute),
		DurationMS:  600000,
		Status:      model.RecordingStatusCompleted,
		Format:      model.FormatMP4,
	}
	if err := db.Create(&wanted).Error; err != nil {
		t.Fatalf("create wanted segment: %v", err)
	}
	otherCamera := wanted
	otherCamera.ID = 0
	otherCamera.RecordingID = 42
	otherCamera.CameraID = 8
	otherCamera.FilePath = filepath.Join(t.TempDir(), "other.mp4")
	if err := db.Create(&otherCamera).Error; err != nil {
		t.Fatalf("create other-camera segment: %v", err)
	}
	query := url.Values{
		"camera_id": {"7"},
		"from":      {"2026-08-08T10:00:00Z"},
		"to":        {"2026-08-08T10:01:00Z"},
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/recordings/timeline?"+query.Encode(), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("timeline status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Data struct {
			Segments []service.TimelineSegment `json:"segments"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode timeline response: %v", err)
	}
	if len(resp.Data.Segments) != 1 || resp.Data.Segments[0].ID != wanted.ID {
		t.Fatalf("timeline segments = %+v, want only segment %d", resp.Data.Segments, wanted.ID)
	}
}

func TestRecordingPlayAtReturnsStructuredNotFoundForUnknownPoint(t *testing.T) {
	h := setupTestHandler(t)
	router := h.SetupRouter()
	token := createTestUser(t, h)
	query := url.Values{"camera_id": {"1"}, "at": {"2026-08-08T10:00:00Z"}}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/recordings/play-at?"+query.Encode(), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("play-at missing point status = %d, want 404: %s", w.Code, w.Body.String())
	}
	var resp response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode play-at error response: %v", err)
	}
	if resp.Code != http.StatusNotFound {
		t.Fatalf("play-at error code = %d, want 404", resp.Code)
	}
}

func TestRecordingPlayAtReturnsMediaURLAndOffset(t *testing.T) {
	h, db := setupTestHandlerWithDB(t)
	router := h.SetupRouter()
	token := createTestUser(t, h)
	start := time.Date(2026, 8, 8, 10, 0, 0, 0, time.UTC)
	current := model.RecordingSegment{
		RecordingID: 51,
		CameraID:    9,
		Sequence:    1,
		FilePath:    filepath.Join(t.TempDir(), "current.mp4"),
		FileSize:    4096,
		StartTime:   start,
		EndTime:     start.Add(time.Minute),
		DurationMS:  60000,
		Status:      model.RecordingStatusCompleted,
		Format:      model.FormatMP4,
	}
	if err := db.Create(&current).Error; err != nil {
		t.Fatalf("create current segment: %v", err)
	}
	next := current
	next.ID = 0
	next.Sequence = 2
	next.FilePath = filepath.Join(t.TempDir(), "next.mp4")
	next.StartTime = current.EndTime.Add(time.Second)
	next.EndTime = next.StartTime.Add(time.Minute)
	if err := db.Create(&next).Error; err != nil {
		t.Fatalf("create next segment: %v", err)
	}
	window := []model.RecordingSegment{current, next}
	for i := 3; i <= 6; i++ {
		successor := window[len(window)-1]
		successor.ID = 0
		successor.Sequence = i
		successor.FilePath = filepath.Join(t.TempDir(), fmt.Sprintf("segment-%d.mp4", i))
		successor.StartTime = window[len(window)-1].EndTime
		successor.EndTime = successor.StartTime.Add(time.Minute)
		if err := db.Create(&successor).Error; err != nil {
			t.Fatalf("create successor segment %d: %v", i, err)
		}
		window = append(window, successor)
	}
	query := url.Values{"camera_id": {"9"}, "at": {"2026-08-08T10:00:02.500Z"}}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/recordings/play-at?"+query.Encode(), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("play-at status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Data struct {
			Segment       service.TimelineSegment   `json:"segment"`
			Segments      []service.TimelineSegment `json:"segments"`
			MediaURL      string                    `json:"media_url"`
			OffsetMS      int64                     `json:"offset_ms"`
			NextSegmentID *uint                     `json:"next_segment_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode play-at response: %v", err)
	}
	wantMediaURL := fmt.Sprintf("/api/v1/recording-segments/%d/media", current.ID)
	if resp.Data.Segment.ID != current.ID || resp.Data.MediaURL != wantMediaURL || resp.Data.OffsetMS != 2500 {
		t.Fatalf("play-at data = %+v, want segment=%d media_url=%q offset_ms=2500", resp.Data, current.ID, wantMediaURL)
	}
	if resp.Data.NextSegmentID == nil || *resp.Data.NextSegmentID != next.ID {
		t.Fatalf("next_segment_id = %v, want %d", resp.Data.NextSegmentID, next.ID)
	}
	if len(resp.Data.Segments) != 5 {
		t.Fatalf("playback segments = %+v, want five entries", resp.Data.Segments)
	}
	for i, segment := range resp.Data.Segments {
		if segment.ID != window[i].ID {
			t.Fatalf("playback segment %d = %d, want %d", i, segment.ID, window[i].ID)
		}
		if i > 0 && segment.StartTime.Before(resp.Data.Segments[i-1].StartTime) {
			t.Fatalf("playback segments are not chronological: %+v", resp.Data.Segments)
		}
	}
}

func TestRecordingSegmentMediaServesSeekableInlineMP4FromDatabasePath(t *testing.T) {
	h, db := setupTestHandlerWithDB(t)
	router := h.SetupRouter()
	token := createTestUser(t, h)
	mediaBytes := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}
	mediaPath := filepath.Join(t.TempDir(), "segment.mp4")
	if err := os.WriteFile(mediaPath, mediaBytes, 0o600); err != nil {
		t.Fatalf("write media fixture: %v", err)
	}
	secretPath := filepath.Join(t.TempDir(), "secret.mp4")
	if err := os.WriteFile(secretPath, []byte("must-not-be-served"), 0o600); err != nil {
		t.Fatalf("write secret fixture: %v", err)
	}
	segment := model.RecordingSegment{
		RecordingID: 61,
		CameraID:    10,
		Sequence:    1,
		FilePath:    mediaPath,
		FileSize:    int64(len(mediaBytes)),
		StartTime:   time.Date(2026, 8, 8, 10, 0, 0, 0, time.UTC),
		EndTime:     time.Date(2026, 8, 8, 10, 1, 0, 0, time.UTC),
		DurationMS:  60000,
		Status:      model.RecordingStatusCompleted,
		Format:      model.FormatMP4,
	}
	if err := db.Create(&segment).Error; err != nil {
		t.Fatalf("create media segment: %v", err)
	}
	mediaURL := fmt.Sprintf("/api/v1/recording-segments/%d/media?%s", segment.ID, url.Values{
		"token": {token},
		"path":  {secretPath},
	}.Encode())

	for _, test := range []struct {
		name        string
		rangeHeader string
		wantBody    []byte
		wantRange   string
	}{
		{name: "explicit range", rangeHeader: "bytes=2-5", wantBody: []byte{2, 3, 4, 5}, wantRange: "bytes 2-5/10"},
		{name: "suffix range", rangeHeader: "bytes=-4", wantBody: []byte{6, 7, 8, 9}, wantRange: "bytes 6-9/10"},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, mediaURL, nil)
			req.Header.Set("Range", test.rangeHeader)
			router.ServeHTTP(w, req)
			if w.Code != http.StatusPartialContent {
				t.Fatalf("media status = %d, want 206: %s", w.Code, w.Body.String())
			}
			if got := w.Body.Bytes(); !bytes.Equal(got, test.wantBody) {
				t.Fatalf("media body = %v, want %v", got, test.wantBody)
			}
			if got := w.Header().Get("Content-Range"); got != test.wantRange {
				t.Fatalf("Content-Range = %q, want %q", got, test.wantRange)
			}
			if got := w.Header().Get("Content-Type"); got != "video/mp4" {
				t.Fatalf("Content-Type = %q, want video/mp4", got)
			}
			if got := w.Header().Get("Content-Disposition"); got != "inline" {
				t.Fatalf("Content-Disposition = %q, want inline", got)
			}
			if got := w.Header().Get("Accept-Ranges"); got != "bytes" {
				t.Fatalf("Accept-Ranges = %q, want bytes", got)
			}
		})
	}
}

func TestRecordingSegmentMediaRejectsNonservableFragments(t *testing.T) {
	h, db := setupTestHandlerWithDB(t)
	router := h.SetupRouter()
	token := createTestUser(t, h)
	mediaPath := filepath.Join(t.TempDir(), "segment.mp4")
	if err := os.WriteFile(mediaPath, []byte("fragment"), 0o600); err != nil {
		t.Fatalf("write media fixture: %v", err)
	}
	start := time.Date(2026, 8, 8, 10, 0, 0, 0, time.UTC)
	for index, test := range []struct {
		name       string
		status     string
		durationMS int64
	}{
		{name: "failed", status: model.RecordingStatusFailed, durationMS: 1000},
		{name: "incomplete", status: model.RecordingStatusRecording, durationMS: 1000},
		{name: "zero duration", status: model.RecordingStatusCompleted, durationMS: 0},
		{name: "negative duration", status: model.RecordingStatusCompleted, durationMS: -1},
	} {
		t.Run(test.name, func(t *testing.T) {
			segment := model.RecordingSegment{
				RecordingID: 80 + uint(index), CameraID: 7, Sequence: 1,
				FilePath: fmt.Sprintf("%s-%d", mediaPath, index), FileSize: 8,
				StartTime: start, EndTime: start.Add(time.Second), DurationMS: test.durationMS,
				Status: test.status, Format: model.FormatMP4,
			}
			if err := os.WriteFile(segment.FilePath, []byte("fragment"), 0o600); err != nil {
				t.Fatalf("write segment: %v", err)
			}
			if err := db.Create(&segment).Error; err != nil {
				t.Fatalf("create segment: %v", err)
			}
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/recording-segments/%d/media?token=%s", segment.ID, token), nil)
			router.ServeHTTP(w, req)
			if w.Code != http.StatusNotFound {
				t.Fatalf("media status = %d, want 404: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestListRecordingsParsesUTCOverlapFilters(t *testing.T) {
	h, db := setupTestHandlerWithDB(t)
	router := h.SetupRouter()
	token := createTestUser(t, h)
	from := time.Date(2026, 8, 8, 10, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	for _, rec := range []model.Recording{
		{CameraID: 1, FilePath: "overlap.mp4", StartTime: from.Add(-time.Minute), EndTime: timePointer(to.Add(time.Minute)), Status: model.RecordingStatusCompleted},
		{CameraID: 1, FilePath: "ends-at-from.mp4", StartTime: from.Add(-time.Hour), EndTime: timePointer(from), Status: model.RecordingStatusCompleted},
		{CameraID: 1, FilePath: "starts-at-to.mp4", StartTime: to, EndTime: timePointer(to.Add(time.Hour)), Status: model.RecordingStatusCompleted},
	} {
		if err := db.Create(&rec).Error; err != nil {
			t.Fatalf("create recording: %v", err)
		}
	}
	query := url.Values{"start_time": {from.Format(time.RFC3339)}, "end_time": {to.Format(time.RFC3339)}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/recordings?"+query.Encode(), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var response struct {
		Data struct {
			Recordings []model.Recording `json:"recordings"`
			Total      int64             `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if response.Data.Total != 1 || len(response.Data.Recordings) != 1 || response.Data.Recordings[0].FilePath != "overlap.mp4" {
		t.Fatalf("filtered recordings = %+v total=%d", response.Data.Recordings, response.Data.Total)
	}
}

func TestListRecordingsRejectsInvalidTimeFilters(t *testing.T) {
	h := setupTestHandler(t)
	router := h.SetupRouter()
	token := createTestUser(t, h)
	for _, rawQuery := range []string{
		"start_time=not-a-time",
		"start_time=2026-08-08T10%3A00%3A00%2B08%3A00",
		"start_time=2026-08-08T11%3A00%3A00Z&end_time=2026-08-08T10%3A00%3A00Z",
	} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/recordings?"+rawQuery, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		router.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("query %q status = %d, want 400: %s", rawQuery, w.Code, w.Body.String())
		}
	}
}

func TestStartRecordingReturnsServiceUnavailableAfterRecorderShutdown(t *testing.T) {
	h, db := setupTestHandlerWithDB(t)
	camera := model.Camera{Name: "shutdown", RTSPUrl: "rtsp://camera/live", AccessProtocol: model.ProtocolRTSP}
	if err := db.Create(&camera).Error; err != nil {
		t.Fatalf("create camera: %v", err)
	}
	h.recorderSvc.Shutdown()
	router := h.SetupRouter()
	token := createTestUser(t, h)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/recordings/start", bytes.NewBufferString(fmt.Sprintf(`{"camera_id":%d,"format":"mp4"}`, camera.ID)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("start after shutdown status = %d, want 503: %s", w.Code, w.Body.String())
	}
}

func timePointer(value time.Time) *time.Time { return &value }

func TestDownloadRecording_NotFound(t *testing.T) {
	h := setupTestHandler(t)
	router := h.SetupRouter()
	token := createTestUser(t, h)

	// 下载不存在的录像
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/recordings/999/download", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("download non-existent status = %d, want 404", w.Code)
	}
}

func TestDownloadRecording_Unauthorized(t *testing.T) {
	h := setupTestHandler(t)
	router := h.SetupRouter()

	// 无 token 下载
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/recordings/1/download", nil)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("download without auth status = %d, want 401", w.Code)
	}
}

func TestStartRecording_InvalidInput(t *testing.T) {
	h := setupTestHandler(t)
	router := h.SetupRouter()
	token := createTestUser(t, h)

	// 缺少 camera_id
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/recordings/start", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("start without camera_id status = %d, want 400", w.Code)
	}
}

func TestStartRecording_UnsafeOptionsReturnBadRequest(t *testing.T) {
	h, db := setupTestHandlerWithDB(t)
	router := h.SetupRouter()
	token := createTestUser(t, h)
	camera := &model.Camera{Name: "front", RTSPUrl: "rtsp://camera/live", AccessProtocol: model.ProtocolRTSP}
	if err := db.Create(camera).Error; err != nil {
		t.Fatalf("create camera: %v", err)
	}

	for _, body := range []string{
		fmt.Sprintf(`{"camera_id":%d,"format":"webm"}`, camera.ID),
		fmt.Sprintf(`{"camera_id":%d,"format":"mp4","bitrate":600}`, camera.ID),
	} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/v1/recordings/start", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		router.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("unsafe start status = %d, want 400: %s", w.Code, w.Body.String())
		}
	}
}

func TestStopRecording_NotActive(t *testing.T) {
	h := setupTestHandler(t)
	router := h.SetupRouter()
	token := createTestUser(t, h)

	// 停止不存在的录像
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/recordings/stop", bytes.NewBufferString(`{"recording_id": 999}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("stop non-active status = %d, want 500", w.Code)
	}
}

func TestStopRecording_ReturnsDownloadURL(t *testing.T) {
	h, db := setupTestHandlerWithDB(t)
	router := h.SetupRouter()
	token := createTestUser(t, h)

	recording := &model.Recording{
		CameraID:  7,
		FilePath:  "/tmp/kiosk-recording.mp4",
		StartTime: time.Now().Add(-time.Minute),
		Status:    model.RecordingStatusRecording,
		Format:    model.FormatMP4,
	}
	if err := db.Create(recording).Error; err != nil {
		t.Fatalf("create recording: %v", err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/recordings/stop", bytes.NewBufferString(fmt.Sprintf(`{"recording_id":%d}`, recording.ID)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("stop status = %d, want 200: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Data struct {
			RecordingID uint   `json:"recording_id"`
			DownloadURL string `json:"download_url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Data.RecordingID != recording.ID {
		t.Fatalf("recording_id = %d, want %d", resp.Data.RecordingID, recording.ID)
	}
	wantURL := fmt.Sprintf("/api/v1/recordings/%d/download", recording.ID)
	if resp.Data.DownloadURL != wantURL {
		t.Fatalf("download_url = %q, want %q", resp.Data.DownloadURL, wantURL)
	}
}

func TestStopSegmentedRecordingDoesNotAdvertiseLegacyDownload(t *testing.T) {
	h, db := setupTestHandlerWithDB(t)
	router := h.SetupRouter()
	token := createTestUser(t, h)
	end := time.Now().UTC()
	recording := &model.Recording{
		CameraID: 7, FilePath: filepath.Join(t.TempDir(), "7", "105"),
		StartTime: end.Add(-time.Minute), EndTime: &end, Status: model.RecordingStatusCompleted,
		Format: model.FormatMP4, StorageMode: model.StorageModeSegmented,
	}
	if err := db.Create(recording).Error; err != nil {
		t.Fatalf("create recording: %v", err)
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/recordings/stop", bytes.NewBufferString(fmt.Sprintf(`{"recording_id":%d}`, recording.ID)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("stop status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if _, advertised := resp.Data["download_url"]; advertised {
		t.Fatalf("segmented stop advertised a legacy full-session download: %+v", resp.Data)
	}
	if resp.Data["storage_mode"] != model.StorageModeSegmented {
		t.Fatalf("storage_mode = %v, want segmented", resp.Data["storage_mode"])
	}
}

func TestSyncTimeEndpoint(t *testing.T) {
	h := setupTestHandler(t)
	router := h.SetupRouter()
	token := createTestUser(t, h)

	// 创建摄像头
	createBody := `{"name":"cam1","ip":"192.168.1.100"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/cameras", bytes.NewBufferString(createBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)

	var createResp struct {
		Data struct {
			ID uint `json:"id"`
		} `json:"data"`
	}
	json.Unmarshal(w.Body.Bytes(), &createResp)

	// 调用 sync-time（会失败因为没有真实设备，但 API 端点应该存在）
	w = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/api/v1/cameras/"+uintToStr(createResp.Data.ID)+"/sync-time", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)

	// 预期返回 500（因为设备不可达），但 API 端点应该正常路由
	if w.Code == http.StatusNotFound {
		t.Error("sync-time endpoint not found")
	}
}

func TestDeleteRecording_Endpoint(t *testing.T) {
	h := setupTestHandler(t)
	router := h.SetupRouter()
	token := createTestUser(t, h)

	// 删除不存在的录像 → 404
	w := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/v1/recordings/999", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("delete non-existent status = %d, want 404", w.Code)
	}

	// 无 token → 401
	w = httptest.NewRecorder()
	req = httptest.NewRequest("DELETE", "/api/v1/recordings/1", nil)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("delete without auth status = %d, want 401", w.Code)
	}
}

func TestScheduleAPI(t *testing.T) {
	h := setupTestHandler(t)
	router := h.SetupRouter()
	token := createTestUser(t, h)

	// 1. 空列表
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/schedules", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("list schedules status = %d, want 200", w.Code)
	}

	// 2. 创建计划（缺必填字段 → 400）
	w = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/api/v1/schedules", bytes.NewBufferString(`{"name":"白天录像"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("create schedule missing camera_id status = %d, want 400", w.Code)
	}

	// 3. 创建完整计划
	createBody := `{"name":"白天录像","camera_id":1,"start_time":"09:00","end_time":"17:00","days":127,"format":"mp4"}`
	w = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/api/v1/schedules", bytes.NewBufferString(createBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Errorf("create schedule status = %d, want 201. Body: %s", w.Code, w.Body.String())
	}

	// 4. 列表应该有 1 条
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/api/v1/schedules", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("list schedules after create status = %d, want 200", w.Code)
	}

	// 5. 更新
	updateBody := `{"name":"晚上录像","camera_id":1,"start_time":"18:00","end_time":"22:00","days":62,"format":"webm","with_audio":true,"enabled":false}`
	w = httptest.NewRecorder()
	req = httptest.NewRequest("PUT", "/api/v1/schedules/1", bytes.NewBufferString(updateBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("update schedule status = %d, want 200", w.Code)
	}

	// 6. 删除
	w = httptest.NewRecorder()
	req = httptest.NewRequest("DELETE", "/api/v1/schedules/1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Errorf("delete schedule status = %d, want 204", w.Code)
	}
}

// uintToStr 辅助函数
func uintToStr(v uint) string {
	return fmt.Sprintf("%d", v)
}
