package api

import (
	"errors"
	"net/http"
	"strings"

	"CameraIO/internal/pkg"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

type loginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

func (h *Handler) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "username and password required")
		return
	}
	result, err := h.userSvc.Login(req.Username, req.Password)
	if err != nil {
		fail(c, http.StatusUnauthorized, err.Error())
		return
	}
	ok(c, result)
}

type createUserRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
	Role     string `json:"role"`
}

func (h *Handler) CreateUser(c *gin.Context) {
	var req createUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "username and password required")
		return
	}
	user, err := h.userSvc.Create(req.Username, req.Password, req.Role)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			fail(c, http.StatusConflict, "username already exists")
			return
		}
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	created(c, user)
}

func (h *Handler) ListUsers(c *gin.Context) {
	users, err := h.userSvc.List()
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, users)
}

// JWTAuthMiddleware 解析并验证 JWT token。
// 支持两种方式: Authorization header 和 ?token= query 参数（用于 MJPEG 等流端点）。
func (h *Handler) JWTAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		var tokenStr string

		// 优先从 Authorization header 获取
		header := c.GetHeader("Authorization")
		if header != "" {
			parts := strings.SplitN(header, " ", 2)
			if len(parts) == 2 && strings.EqualFold(parts[0], "bearer") {
				tokenStr = parts[1]
			}
		}

		// 回退: 从 query 参数获取（MJPEG/流端点使用 img 标签，无法设置 header）
		if tokenStr == "" {
			tokenStr = c.Query("token")
		}

		if tokenStr == "" {
			fail(c, http.StatusUnauthorized, "missing authorization")
			c.Abort()
			return
		}

		claims, err := h.jwtCfg.Parse(tokenStr)
		if err != nil {
			if errors.Is(err, pkg.ErrTokenExpired) || errors.Is(err, jwt.ErrTokenExpired) {
				fail(c, http.StatusUnauthorized, "token expired")
			} else {
				fail(c, http.StatusUnauthorized, "invalid token")
			}
			c.Abort()
			return
		}
		c.Set("user_id", claims.UserID)
		c.Set("username", claims.Username)
		c.Set("role", claims.Role)
		c.Next()
	}
}
