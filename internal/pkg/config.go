package pkg

import (
	"encoding/json"
	"log"
	"os"
)

// Config 服务配置。
type Config struct {
	Addr          string `json:"addr"`
	DBPath        string `json:"db_path"`
	JWTSecret     string `json:"jwt_secret"`
	RecordingsDir string `json:"recordings_dir"`

	// GB28181 SIP 服务配置
	SIPListenAddr string `json:"sip_listen_addr"` // SIP 信令监听地址 (如 ":5060")
	SIPServerID   string `json:"sip_server_id"`   // SIP 服务器 ID（20 位国标编码）
	SIPRealm      string `json:"sip_realm"`       // SIP 域
	RTPPortMin    int    `json:"rtp_port_min"`    // RTP 端口范围下限
	RTPPortMax    int    `json:"rtp_port_max"`    // RTP 端口范围上限

	ConfigPath string `json:"-"` // 配置文件路径（不写入文件本身）
}

// DefaultConfig 返回内置默认配置（不读取环境变量、不读取配置文件）。
func DefaultConfig() *Config {
	return &Config{
		Addr:          ":8080",
		DBPath:        "data/cameradio.db",
		JWTSecret:     "change-me-in-production",
		RecordingsDir: "data/recordings",
		SIPListenAddr: ":5060",
		SIPServerID:   "34020000002000000001",
		SIPRealm:      "3402000000",
		RTPPortMin:    10000,
		RTPPortMax:    11000,
	}
}

// LoadConfig 加载配置，优先级：环境变量 > 配置文件 > 内置默认值。
// 配置文件不存在时自动创建（内容为默认值）。
func LoadConfig() *Config {
	cfg := DefaultConfig()

	path := getEnv("CAMERAIO_CONFIG", "config.json")
	cfg.ConfigPath = path

	if data, err := os.ReadFile(path); err == nil {
		// 配置文件存在：JSON 覆盖默认值
		if err := json.Unmarshal(data, cfg); err != nil {
			log.Printf("[config] 解析 %s 失败，使用默认值: %v", path, err)
		} else {
			log.Printf("[config] 已加载配置文件: %s", path)
		}
	} else if os.IsNotExist(err) {
		// 配置文件不存在：用默认值创建
		if err := writeDefaultConfigFile(path, cfg); err != nil {
			log.Printf("[config] 创建默认配置文件失败: %v", err)
		}
	} else {
		log.Printf("[config] 读取 %s 失败: %v", path, err)
	}

	// 环境变量覆盖（最高优先级）
	applyEnvOverrides(cfg)

	return cfg
}

// writeDefaultConfigFile 将默认配置写入文件。
func writeDefaultConfigFile(path string, cfg *Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	// 默认配置文件的 jwt_secret 仍为占位符，保持默认值不变
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return err
	}
	log.Printf("[config] 配置文件不存在，已创建默认配置: %s", path)
	return nil
}

// applyEnvOverrides 用环境变量覆盖配置文件/默认值。
func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("CAMERAIO_ADDR"); v != "" {
		cfg.Addr = v
	}
	if v := os.Getenv("CAMERAIO_DB_PATH"); v != "" {
		cfg.DBPath = v
	}
	if v := os.Getenv("CAMERAIO_JWT_SECRET"); v != "" {
		cfg.JWTSecret = v
	}
	if v := os.Getenv("CAMERAIO_RECORDINGS_DIR"); v != "" {
		cfg.RecordingsDir = v
	}
	if v := os.Getenv("CAMERAIO_SIP_ADDR"); v != "" {
		cfg.SIPListenAddr = v
	}
	if v := os.Getenv("CAMERAIO_SIP_SERVER_ID"); v != "" {
		cfg.SIPServerID = v
	}
	if v := os.Getenv("CAMERAIO_SIP_REALM"); v != "" {
		cfg.SIPRealm = v
	}
	if v := os.Getenv("CAMERAIO_RTP_PORT_MIN"); v != "" {
		cfg.RTPPortMin = getEnvInt("CAMERAIO_RTP_PORT_MIN", cfg.RTPPortMin)
	}
	if v := os.Getenv("CAMERAIO_RTP_PORT_MAX"); v != "" {
		cfg.RTPPortMax = getEnvInt("CAMERAIO_RTP_PORT_MAX", cfg.RTPPortMax)
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
