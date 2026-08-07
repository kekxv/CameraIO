package pkg

import (
	"path/filepath"
	"testing"

	"CameraIO/internal/model"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestPureGoSQLiteDriverMigratesAndPersists(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cameraio.db")
	conn, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite database: %v", err)
	}
	if err := MigrateDB(conn); err != nil {
		t.Fatalf("migrate sqlite database: %v", err)
	}
	if err := conn.Create(&model.User{Username: "pure-go", PasswordHash: "hash", Role: model.RoleAdmin}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	sqlDB, err := conn.DB()
	if err != nil {
		t.Fatalf("get database handle: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}

	reopened, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("reopen sqlite database: %v", err)
	}
	var count int64
	if err := reopened.Model(&model.User{}).Where("username = ?", "pure-go").Count(&count).Error; err != nil {
		t.Fatalf("query persisted user: %v", err)
	}
	if count != 1 {
		t.Fatalf("persisted user count = %d, want 1", count)
	}
}
