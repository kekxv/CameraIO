package service

import (
	"errors"
	"time"

	"CameraIO/internal/model"
	"CameraIO/internal/pkg"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type UserService struct {
	db  *gorm.DB
	jwt *pkg.JWTConfig
}

func NewUserService(db *gorm.DB, jwtCfg *pkg.JWTConfig) *UserService {
	return &UserService{db: db, jwt: jwtCfg}
}

type LoginResult struct {
	Token string     `json:"token"`
	User  model.User `json:"user"`
}

func (s *UserService) Login(username, password string) (*LoginResult, error) {
	var user model.User
	if err := s.db.Where("username = ?", username).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("invalid username or password")
		}
		return nil, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, errors.New("invalid username or password")
	}
	token, err := s.jwt.Generate(user.ID, user.Username, user.Role)
	if err != nil {
		return nil, err
	}
	return &LoginResult{Token: token, User: user}, nil
}

func (s *UserService) Create(username, password, role string) (*model.User, error) {
	if username == "" || password == "" {
		return nil, errors.New("username and password are required")
	}
	if role == "" {
		role = model.RoleViewer
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	user := model.User{
		Username:     username,
		PasswordHash: string(hash),
		Role:         role,
		CreatedAt:    time.Now(),
	}
	if err := s.db.Create(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func (s *UserService) List() ([]model.User, error) {
	var users []model.User
	if err := s.db.Find(&users).Error; err != nil {
		return nil, err
	}
	return users, nil
}
