package service

import (
	"context"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"CameraIO/internal/model"

	"gorm.io/gorm"
)

// ScheduleService 定时录像调度器。
// 每 30 秒检查一次启用的计划，进入时间窗口自动开始录像，离开自动停止。
// 多段录像：每天进入窗口自动开始新一段。
type ScheduleService struct {
	db       *gorm.DB
	recorder *RecorderService
	ctx      context.Context
	cancel   context.CancelFunc
	mu       sync.Mutex
	active   map[uint]*activeSchedule // scheduleID → 正在录制的任务
}

type activeSchedule struct {
	RecordingID uint
	CameraID    uint
	StartTime   time.Time
}

func NewScheduleService(db *gorm.DB, recorder *RecorderService) *ScheduleService {
	ctx, cancel := context.WithCancel(context.Background())
	return &ScheduleService{
		db:       db,
		recorder: recorder,
		ctx:      ctx,
		cancel:   cancel,
		active:   make(map[uint]*activeSchedule),
	}
}

// Start 启动定时检查循环。
func (s *ScheduleService) Start() {
	go s.loop()
	log.Println("[scheduler] scheduled recording started")
}

// ---------- 计划 CRUD ----------

// List 返回所有定时录像计划。
func (s *ScheduleService) List() ([]model.RecordingSchedule, error) {
	var schedules []model.RecordingSchedule
	if err := s.db.Order("created_at DESC").Find(&schedules).Error; err != nil {
		return nil, err
	}
	for i := range schedules {
		s.db.Preload("Camera").First(&schedules[i], schedules[i].ID)
	}
	return schedules, nil
}

// Create 创建定时录像计划。
func (s *ScheduleService) Create(sch *model.RecordingSchedule) error {
	return s.db.Create(sch).Error
}

// Update 更新定时录像计划。
func (s *ScheduleService) Update(id uint, sch *model.RecordingSchedule) error {
	return s.db.Model(&model.RecordingSchedule{}).Where("id = ?", id).Updates(map[string]any{
		"name":       sch.Name,
		"camera_id":  sch.CameraID,
		"start_time": sch.StartTime,
		"end_time":   sch.EndTime,
		"days":       sch.Days,
		"format":     sch.Format,
		"with_audio": sch.WithAudio,
		"enabled":    sch.Enabled,
	}).Error
}

// Delete 删除定时录像计划。
func (s *ScheduleService) Delete(id uint) error {
	return s.db.Delete(&model.RecordingSchedule{}, id).Error
}

// Stop 停止调度器并停止所有活跃的定时录像。
func (s *ScheduleService) Stop() {
	s.cancel()
	s.stopAllActive()
	log.Println("[scheduler] scheduled recording stopped")
}

func (s *ScheduleService) loop() {
	// 启动后立即检查一次，再每 15 秒检查
	s.checkSchedules()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.safeCheck()
		}
	}
}

// safeCheck 带 recover 的检查，防止异常导致调度循环终止。
func (s *ScheduleService) safeCheck() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[scheduler] check recovered from panic: %v", r)
		}
	}()
	s.checkSchedules()
}

// checkSchedules 检查所有启用的计划并执行开始/停止操作。
func (s *ScheduleService) checkSchedules() {
	var schedules []model.RecordingSchedule
	if err := s.db.Where("enabled = ?", true).Find(&schedules).Error; err != nil {
		log.Printf("[scheduler] list schedules: %v", err)
		return
	}

	now := time.Now()
	// 清理已完成的活跃记录（对应录像已停止的情况）
	s.cleanupStaleActive(now)

	// 1. 按时间窗口开始/停止
	for _, sch := range schedules {
		inWindow := inScheduleWindow(&sch, now)

		s.mu.Lock()
		_, isActive := s.active[sch.ID]
		s.mu.Unlock()

		if inWindow && !isActive {
			// 进入时间窗口 → 开始录像
			// MaxDuration 传入录像器，由录像器内部兜底到点强制停止
			rec, err := s.recorder.StartRecording(&StartRecordingInput{
				CameraID:    sch.CameraID,
				Format:      sch.Format,
				WithAudio:   sch.WithAudio,
				TriggerType: model.TriggerSchedule,
				MaxDuration: scheduleDurationMin(&sch) * 60,
			})
			if err != nil {
				log.Printf("[scheduler] schedule %d start recording failed: %v", sch.ID, err)
				continue
			}
			s.mu.Lock()
			s.active[sch.ID] = &activeSchedule{
				RecordingID: rec.ID,
				CameraID:    sch.CameraID,
				StartTime:   time.Now(),
			}
			s.mu.Unlock()
			log.Printf("[scheduler] schedule %d (%s) started recording #%d", sch.ID, sch.Name, rec.ID)
		} else if !inWindow && isActive {
			// 离开时间窗口 → 停止录像
			s.stopSchedule(sch.ID)
		}
	}

	// 2. Watchdog: 强制停止已超过时间窗口的活跃录像（防止因任何原因漏停）
	s.watchdogStop(now)
}

// watchdogStop 兜底：活跃录像运行时长超过其窗口时长（+10s 宽限）时强制停止。
// 不依赖 inWindow 判断，即使时间窗口逻辑有误也能兜住。
func (s *ScheduleService) watchdogStop(now time.Time) {
	s.mu.Lock()
	var toStop []uint
	for id, act := range s.active {
		var sch model.RecordingSchedule
		if err := s.db.First(&sch, id).Error; err != nil {
			continue
		}
		dur := scheduleDurationMin(&sch)
		if dur <= 0 {
			continue // 无法计算窗口时长（如跨午夜场景由 inWindow 处理）
		}
		elapsed := now.Sub(act.StartTime)
		if elapsed > time.Duration(dur)*time.Minute+10*time.Second {
			toStop = append(toStop, id)
		}
	}
	s.mu.Unlock()

	for _, id := range toStop {
		log.Printf("[scheduler] watchdog: schedule %d exceeded window, force stopping", id)
		s.stopSchedule(id)
	}
}

// scheduleDurationMin 计算计划窗口时长（分钟）。跨午夜（end<start）返回 0。
func scheduleDurationMin(sch *model.RecordingSchedule) int {
	start, ok1 := parseHHMM(sch.StartTime)
	end, ok2 := parseHHMM(sch.EndTime)
	if !ok1 || !ok2 {
		return 0
	}
	if end <= start {
		return 0 // 跨午夜，交给 inWindow 处理
	}
	return end - start
}

// stopSchedule 停止指定计划的录像。
func (s *ScheduleService) stopSchedule(scheduleID uint) {
	s.mu.Lock()
	act, ok := s.active[scheduleID]
	if ok {
		delete(s.active, scheduleID)
	}
	s.mu.Unlock()

	if !ok {
		return
	}
	if err := s.recorder.StopRecording(act.RecordingID); err != nil {
		log.Printf("[scheduler] schedule %d stop recording #%d: %v", scheduleID, act.RecordingID, err)
		return
	}
	log.Printf("[scheduler] schedule %d stopped recording #%d", scheduleID, act.RecordingID)
}

// stopAllActive 停止所有活跃的定时录像。
func (s *ScheduleService) stopAllActive() {
	s.mu.Lock()
	ids := make([]uint, 0, len(s.active))
	for id := range s.active {
		ids = append(ids, id)
	}
	s.mu.Unlock()

	for _, id := range ids {
		s.stopSchedule(id)
	}
}

// cleanupStaleActive 清理活跃列表中录像已不在运行的计划（录像失败/被手动停止）。
func (s *ScheduleService) cleanupStaleActive(now time.Time) {
	s.mu.Lock()
	stale := make([]uint, 0)
	for id, act := range s.active {
		// 检查录像记录是否还在录制
		var rec model.Recording
		if err := s.db.First(&rec, act.RecordingID).Error; err != nil {
			stale = append(stale, id)
			continue
		}
		if rec.Status != model.RecordingStatusRecording {
			stale = append(stale, id)
		}
	}
	for _, id := range stale {
		delete(s.active, id)
		log.Printf("[scheduler] schedule %d active recording no longer running, cleaned up", id)
	}
	s.mu.Unlock()
}

// ---------- 时间窗口判断 ----------

// inScheduleWindow 判断当前时间是否在计划的录像时间窗口内。
// 支持跨午夜（如 22:00-06:00）。
func inScheduleWindow(sch *model.RecordingSchedule, now time.Time) bool {
	// 检查星期
	weekday := int(now.Weekday()) // Sunday=0 ... Saturday=6
	if !sch.HasDay(weekday) {
		return false
	}

	startMin, ok1 := parseHHMM(sch.StartTime)
	endMin, ok2 := parseHHMM(sch.EndTime)
	if !ok1 || !ok2 {
		return false
	}

	nowMin := now.Hour()*60 + now.Minute()

	if startMin <= endMin {
		// 同一天内: start <= now < end
		return nowMin >= startMin && nowMin < endMin
	}
	// 跨午夜: start <= now 或 now < end
	return nowMin >= startMin || nowMin < endMin
}

// parseHHMM 解析 "HH:MM" 为分钟数。
func parseHHMM(s string) (int, bool) {
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return 0, false
	}
	h, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return 0, false
	}
	if h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}
