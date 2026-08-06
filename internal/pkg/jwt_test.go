package pkg

import (
	"testing"
	"time"
)

func TestJWTGenerateAndParse(t *testing.T) {
	cfg := NewJWTConfig("test-secret-key-12345")

	// 生成 token
	token, err := cfg.Generate(42, "admin", "admin")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if token == "" {
		t.Fatal("Generate returned empty token")
	}

	// 解析 token
	claims, err := cfg.Parse(token)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if claims.UserID != 42 {
		t.Errorf("UserID = %d, want 42", claims.UserID)
	}
	if claims.Username != "admin" {
		t.Errorf("Username = %q, want %q", claims.Username, "admin")
	}
	if claims.Role != "admin" {
		t.Errorf("Role = %q, want %q", claims.Role, "admin")
	}
	if claims.Issuer != "CameraIO" {
		t.Errorf("Issuer = %q, want %q", claims.Issuer, "CameraIO")
	}
}

func TestJWTInvalidToken(t *testing.T) {
	cfg := NewJWTConfig("test-secret")

	_, err := cfg.Parse("invalid.token.here")
	if err == nil {
		t.Fatal("Parse should fail on invalid token")
	}
}

func TestJWTWrongSecret(t *testing.T) {
	cfg1 := NewJWTConfig("secret-1")
	cfg2 := NewJWTConfig("secret-2")

	token, err := cfg1.Generate(1, "user", "viewer")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	_, err = cfg2.Parse(token)
	if err == nil {
		t.Fatal("Parse should fail with wrong secret")
	}
}

func TestJWTExpiration(t *testing.T) {
	cfg := &JWTConfig{
		Secret:     []byte("test"),
		Expiration: 1 * time.Millisecond, // 极短过期时间
	}

	token, err := cfg.Generate(1, "user", "admin")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	// 等待过期
	time.Sleep(10 * time.Millisecond)

	_, err = cfg.Parse(token)
	if err == nil {
		t.Fatal("Parse should fail on expired token")
	}
	if err != ErrTokenExpired {
		t.Errorf("expected ErrTokenExpired, got %v", err)
	}
}
