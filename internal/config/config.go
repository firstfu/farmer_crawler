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
	APIURL        string        `yaml:"api_url"`
	CropName      string        `yaml:"crop_name"`
	Schedule      string        `yaml:"schedule"`
	RetryCount    int           `yaml:"retry_count"`
	RetryInterval time.Duration `yaml:"retry_interval"`
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

	return &cfg, nil
}
