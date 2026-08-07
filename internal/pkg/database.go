package pkg

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"CameraIO/internal/model"

	"github.com/glebarez/sqlite"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var (
	db   *gorm.DB
	once sync.Once
)

// InitDB 初始化数据库连接，自动建表并创建默认管理员账户。
func InitDB(dbPath string) (*gorm.DB, error) {
	var initErr error
	once.Do(func() {
		dir := filepath.Dir(dbPath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			initErr = fmt.Errorf("create db directory: %w", err)
			return
		}

		conn, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Warn),
		})
		if err != nil {
			initErr = fmt.Errorf("open sqlite: %w", err)
			return
		}

		sqliteDB, err := conn.DB()
		if err != nil {
			initErr = fmt.Errorf("get underlying db: %w", err)
			return
		}
		sqliteDB.SetMaxOpenConns(1)
		sqliteDB.SetMaxIdleConns(1)
		sqliteDB.SetConnMaxIdleTime(time.Hour)

		if err := conn.AutoMigrate(
			&model.User{},
			&model.Camera{},
			&model.Recording{},
			&model.RecordingSchedule{},
		); err != nil {
			initErr = fmt.Errorf("auto migrate: %w", err)
			return
		}

		// 种子数据：默认管理员 admin/admin
		var count int64
		conn.Model(&model.User{}).Count(&count)
		if count == 0 {
			hash, err := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.DefaultCost)
			if err != nil {
				initErr = fmt.Errorf("hash default password: %w", err)
				return
			}
			admin := model.User{
				Username:     "admin",
				PasswordHash: string(hash),
				Role:         model.RoleAdmin,
			}
			if err := conn.Create(&admin).Error; err != nil {
				initErr = fmt.Errorf("seed admin user: %w", err)
				return
			}
		}

		db = conn
	})
	return db, initErr
}

// GetDB 返回已初始化的全局 DB 实例。必须先调用 InitDB。
func GetDB() *gorm.DB {
	if db == nil {
		panic("database not initialized")
	}
	return db
}

// MigrateDB 执行数据库迁移（用于测试）。
func MigrateDB(conn *gorm.DB) error {
	return conn.AutoMigrate(
		&model.User{},
		&model.Camera{},
		&model.Recording{},
		&model.RecordingSchedule{},
	)
}
