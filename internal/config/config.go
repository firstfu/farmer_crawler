// internal/config/config.go
// 農產品價差雷達系統 — 配置管理
// 負責從 config.yaml 載入應用配置，支援 app、crawler、analyzer 三個區段

package config

import (
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type AppConfig struct {
	Port   int    `yaml:"port"`
	DBPath string `yaml:"db_path"`
}

type CrawlerConfig struct {
	APIURL          string        `yaml:"api_url"`
	CropName        string        `yaml:"crop_name"`
	Schedule        string        `yaml:"schedule"`
	RetryCount      int           `yaml:"retry_count"`
	RetryInterval   time.Duration `yaml:"retry_interval"`
	BatchDays       int           `yaml:"batch_days"`
	BatchDelay      time.Duration `yaml:"batch_delay"`
	BackfillDays    int           `yaml:"backfill_days"`
	BackfillOnStart bool          `yaml:"backfill_on_start"`
	RateLimit       float64       `yaml:"rate_limit"`        // 每秒最多 N 個請求
	RateBurst       int           `yaml:"rate_burst"`        // 突發容許量
}

type AnalyzerConfig struct {
	BaseMarketCode int      `yaml:"base_market_code"`
	BaseMarketName string   `yaml:"base_market_name"`
	CropCodes      []string `yaml:"crop_codes"`
}

type Config struct {
	App      AppConfig      `yaml:"app"`
	Crawler  CrawlerConfig  `yaml:"crawler"`
	Analyzer AnalyzerConfig `yaml:"analyzer"`
}

// Load 從指定路徑載入 YAML 配置檔
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	// 套用預設值
	if cfg.Crawler.BatchDays <= 0 {
		cfg.Crawler.BatchDays = 7
	}
	if cfg.Crawler.BatchDelay <= 0 {
		cfg.Crawler.BatchDelay = 2 * time.Second
	}
	if cfg.Crawler.BackfillDays <= 0 {
		cfg.Crawler.BackfillDays = 30
	}
	if cfg.Crawler.RateLimit <= 0 {
		cfg.Crawler.RateLimit = 1.0
	}
	if cfg.Crawler.RateBurst <= 0 {
		cfg.Crawler.RateBurst = 3
	}

	return &cfg, nil
}
