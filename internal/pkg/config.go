package pkg

import (
	"os"
)

type Config struct {
	Addr          string
	DBPath        string
	JWTSecret     string
	RecordingsDir string

	// GB28181 SIP 服务配置
	SIPListenAddr string // SIP 信令监听地址 (如 ":5060")
	SIPServerID   string // SIP 服务器 ID（20 位国标编码）
	SIPRealm      string // SIP 域
	RTPPortMin    int    // RTP 端口范围下限
	RTPPortMax    int    // RTP 端口范围上限
}

func DefaultConfig() *Config {
	return &Config{
		Addr:          getEnv("CAMERAIO_ADDR", ":8080"),
		DBPath:        getEnv("CAMERAIO_DB_PATH", "data/cameradio.db"),
		JWTSecret:     getEnv("CAMERAIO_JWT_SECRET", "change-me-in-production"),
		RecordingsDir: getEnv("CAMERAIO_RECORDINGS_DIR", "data/recordings"),
		SIPListenAddr: getEnv("CAMERAIO_SIP_ADDR", ":5060"),
		SIPServerID:   getEnv("CAMERAIO_SIP_SERVER_ID", "34020000002000000001"),
		SIPRealm:      getEnv("CAMERAIO_SIP_REALM", "3402000000"),
		RTPPortMin:    getEnvInt("CAMERAIO_RTP_PORT_MIN", 10000),
		RTPPortMax:    getEnvInt("CAMERAIO_RTP_PORT_MAX", 11000),
	}
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		n := 0
		for _, c := range v {
			if c < '0' || c > '9' {
				return fallback
			}
			n = n*10 + int(c-'0')
		}
		return n
	}
	return fallback
}
