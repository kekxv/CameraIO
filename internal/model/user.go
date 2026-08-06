package model

import (
	"time"
)

type User struct {
	ID           uint      `json:"id" gorm:"primaryKey"`
	Username     string    `json:"username" gorm:"type:varchar(32);uniqueIndex;not null"`
	PasswordHash string    `json:"-" gorm:"type:varchar(128);not null"`
	Role         string    `json:"role" gorm:"type:varchar(16);default:admin"`
	CreatedAt    time.Time `json:"created_at"`
}

const (
	RoleAdmin  = "admin"
	RoleViewer = "viewer"
)
