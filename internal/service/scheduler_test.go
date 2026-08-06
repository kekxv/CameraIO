package service

import (
	"testing"
	"time"

	"CameraIO/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestParseHHMM(t *testing.T) {
	tests := []struct {
		input    string
		expected int
		ok       bool
	}{
		{"09:00", 540, true},
		{"00:00", 0, true},
		{"23:59", 1439, true},
		{"12:30", 750, true},
		{"", 0, false},
		{"9:00", 540, true},   // 单数字小时也接受
		{"24:00", 0, false},   // 小时越界
		{"12:60", 0, false},   // 分钟越界
		{"invalid", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, ok := parseHHMM(tt.input)
			if ok != tt.ok {
				t.Errorf("parseHHMM(%q) ok = %v, want %v", tt.input, ok, tt.ok)
				return
			}
			if ok && got != tt.expected {
				t.Errorf("parseHHMM(%q) = %d, want %d", tt.input, got, tt.expected)
			}
		})
	}
}

func TestInScheduleWindow(t *testing.T) {
	// 同一天窗口 09:00-17:00，每天
	sameDay := &model.RecordingSchedule{
		StartTime: "09:00",
		EndTime:   "17:00",
		Days:      model.DayAllWeek,
	}

	// 周一 10:00 → 在窗口内
	monday10 := time.Date(2026, 8, 3, 10, 0, 0, 0, time.Local) // 2026-08-03 是周一
	if !inScheduleWindow(sameDay, monday10) {
		t.Error("10:00 Monday should be in window 09:00-17:00")
	}

	// 周一 08:00 → 不在窗口内
	monday8 := time.Date(2026, 8, 3, 8, 0, 0, 0, time.Local)
	if inScheduleWindow(sameDay, monday8) {
		t.Error("08:00 should be outside window 09:00-17:00")
	}

	// 周一 17:00 → 不在窗口内（半开区间 [start, end)）
	monday17 := time.Date(2026, 8, 3, 17, 0, 0, 0, time.Local)
	if inScheduleWindow(sameDay, monday17) {
		t.Error("17:00 should be outside window (end is exclusive)")
	}

	// 跨午夜窗口 22:00-06:00
	overnight := &model.RecordingSchedule{
		StartTime: "22:00",
		EndTime:   "06:00",
		Days:      model.DayAllWeek,
	}
	// 周一 23:00 → 在窗口内
	monday23 := time.Date(2026, 8, 3, 23, 0, 0, 0, time.Local)
	if !inScheduleWindow(overnight, monday23) {
		t.Error("23:00 should be in overnight window 22:00-06:00")
	}
	// 周二 05:00 → 在窗口内（跨午夜继续）
	tuesday5 := time.Date(2026, 8, 4, 5, 0, 0, 0, time.Local)
	if !inScheduleWindow(overnight, tuesday5) {
		t.Error("05:00 next day should be in overnight window")
	}
	// 周二 10:00 → 不在窗口内
	tuesday10 := time.Date(2026, 8, 4, 10, 0, 0, 0, time.Local)
	if inScheduleWindow(overnight, tuesday10) {
		t.Error("10:00 should be outside overnight window")
	}
}

func TestInScheduleWindow_DaysBitmask(t *testing.T) {
	// 只在周一(bit0=1)录像
	sch := &model.RecordingSchedule{
		StartTime: "00:00",
		EndTime:   "23:59",
		Days:      model.DayMonday,
	}

	// 周一在窗口内
	monday := time.Date(2026, 8, 3, 12, 0, 0, 0, time.Local) // 周一
	if !inScheduleWindow(sch, monday) {
		t.Error("Monday should be in window")
	}

	// 周二不在窗口内
	tuesday := time.Date(2026, 8, 4, 12, 0, 0, 0, time.Local) // 周二
	if inScheduleWindow(sch, tuesday) {
		t.Error("Tuesday should be outside window")
	}
}

func TestHasDay(t *testing.T) {
	sch := &model.RecordingSchedule{Days: model.DayMonday | model.DaySunday}

	// Monday (1) → bit0 → true
	if !sch.HasDay(1) {
		t.Error("Monday should match")
	}
	// Sunday (0) → bit6 → true
	if !sch.HasDay(0) {
		t.Error("Sunday should match")
	}
	// Wednesday (3) → false
	if sch.HasDay(3) {
		t.Error("Wednesday should not match")
	}
}

func TestScheduleService_CRUD(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	db.AutoMigrate(&model.RecordingSchedule{}, &model.Camera{})
	// 使用独立内存库
	sqlDB, _ := db.DB()
	sqlDB.Close()
	db, _ = gorm.Open(sqlite.Open("file:schedule_test_1?mode=memory&cache=shared"), &gorm.Config{})
	db.AutoMigrate(&model.RecordingSchedule{}, &model.Camera{})

	svc := NewScheduleService(db, nil)

	// 创建
	sch := &model.RecordingSchedule{
		Name:      "白天录像",
		CameraID:  1,
		StartTime: "09:00",
		EndTime:   "17:00",
		Days:      model.DayAllWeek,
		Format:    model.FormatMP4,
		Enabled:   true,
	}
	if err := svc.Create(sch); err != nil {
		t.Fatal(err)
	}
	if sch.ID == 0 {
		t.Error("created schedule should have ID")
	}

	// 列表
	schedules, err := svc.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(schedules) != 1 {
		t.Errorf("expected 1 schedule, got %d", len(schedules))
	}

	// 更新
	sch.Name = "晚上录像"
	sch.Enabled = false
	if err := svc.Update(sch.ID, sch); err != nil {
		t.Fatal(err)
	}
	var updated model.RecordingSchedule
	db.First(&updated, sch.ID)
	if updated.Name != "晚上录像" {
		t.Errorf("expected updated name, got %s", updated.Name)
	}
	if updated.Enabled {
		t.Error("enabled should be false after update")
	}

	// 删除
	if err := svc.Delete(sch.ID); err != nil {
		t.Fatal(err)
	}
	var count int64
	db.Model(&model.RecordingSchedule{}).Count(&count)
	if count != 0 {
		t.Errorf("expected 0 schedules after delete, got %d", count)
	}
}
