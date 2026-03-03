// cmd/server/main.go
// 農產品價差雷達系統 — 應用入口
// 負責載入配置、初始化各層元件、啟動 HTTP 伺服器與排程器

package main

import (
	"fmt"
	"log"

	"farmer_crawler/internal/config"
)

func main() {
	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatalf("載入配置失敗: %v", err)
	}

	fmt.Printf("農產品價差雷達啟動中... 埠號: %d\n", cfg.App.Port)
	fmt.Printf("資料庫路徑: %s\n", cfg.App.DBPath)
	fmt.Printf("目標作物: %s\n", cfg.Crawler.CropName)
	fmt.Printf("基準市場: %s (%d)\n", cfg.Analyzer.BaseMarketName, cfg.Analyzer.BaseMarketCode)
}
