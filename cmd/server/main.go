// cmd/server/main.go
// 農產品價差雷達系統 — 應用入口
// 負責：載入配置 → 初始化 SQLite → 建立 Service/Handler → 啟動排程器 → 啟動 Gin HTTP 伺服器
// 同時支援 CLI 子命令：crawl --today / crawl --from X --to Y

package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/gin-gonic/gin"

	"farmer_crawler/internal/config"
	"farmer_crawler/internal/handler"
	"farmer_crawler/internal/repository"
	"farmer_crawler/internal/scheduler"
	"farmer_crawler/internal/service"
)

func main() {
	// 解析子命令
	if len(os.Args) > 1 && os.Args[1] == "crawl" {
		runCrawlCommand(os.Args[2:])
		return
	}

	// 載入配置
	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatalf("載入配置失敗: %v", err)
	}

	// 初始化 Repository
	repo, err := repository.NewSQLiteRepo(cfg.App.DBPath)
	if err != nil {
		log.Fatalf("初始化資料庫失敗: %v", err)
	}
	defer repo.Close()

	// 初始化 Services
	crawler := service.NewCrawlerService(
		cfg.Crawler.APIURL,
		cfg.Crawler.CropName,
		cfg.Crawler.RetryCount,
		cfg.Crawler.RetryInterval,
		repo,
	)
	analyzer := service.NewAnalyzerService(
		repo,
		cfg.Analyzer.BaseMarketCode,
		cfg.Analyzer.BaseMarketName,
	)

	// 啟動排程器
	sched, err := scheduler.NewScheduler(crawler)
	if err != nil {
		log.Fatalf("建立排程器失敗: %v", err)
	}
	if err := sched.Start(cfg.Crawler.Schedule); err != nil {
		log.Fatalf("啟動排程器失敗: %v", err)
	}
	defer sched.Stop()

	// 初始化 Gin
	r := gin.Default()

	// 註冊路由
	h := handler.NewDashboardHandler(repo, analyzer, crawler, cfg)
	h.RegisterRoutes(r)

	// 啟動 HTTP 伺服器
	addr := fmt.Sprintf(":%d", cfg.App.Port)
	log.Printf("農產品價差雷達啟動中... http://localhost%s", addr)
	if err := r.Run(addr); err != nil {
		log.Fatalf("伺服器啟動失敗: %v", err)
	}
}

// runCrawlCommand 處理 CLI 爬取子命令
// 支援兩種用法：
//   - crawl --today          爬取今日資料
//   - crawl --from X --to Y  爬取指定日期範圍（民國格式）
func runCrawlCommand(args []string) {
	fs := flag.NewFlagSet("crawl", flag.ExitOnError)
	today := fs.Bool("today", false, "爬取今日資料")
	from := fs.String("from", "", "起始日期 (民國格式: 114.01.01)")
	to := fs.String("to", "", "結束日期 (民國格式: 114.03.03)")
	fs.Parse(args)

	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatalf("載入配置失敗: %v", err)
	}

	repo, err := repository.NewSQLiteRepo(cfg.App.DBPath)
	if err != nil {
		log.Fatalf("初始化資料庫失敗: %v", err)
	}
	defer repo.Close()

	crawler := service.NewCrawlerService(
		cfg.Crawler.APIURL,
		cfg.Crawler.CropName,
		cfg.Crawler.RetryCount,
		cfg.Crawler.RetryInterval,
		repo,
	)

	if *today {
		count, err := crawler.CrawlToday()
		if err != nil {
			log.Fatalf("爬取失敗: %v", err)
		}
		fmt.Printf("爬取成功！共 %d 筆記錄\n", count)
		return
	}

	if *from != "" && *to != "" {
		count, err := crawler.CrawlRange(*from, *to)
		if err != nil {
			log.Fatalf("爬取失敗: %v", err)
		}
		fmt.Printf("爬取成功！%s ~ %s 共 %d 筆記錄\n", *from, *to, count)
		return
	}

	fmt.Println("用法:")
	fmt.Println("  farmer_crawler crawl --today")
	fmt.Println("  farmer_crawler crawl --from 114.01.01 --to 114.03.03")
	os.Exit(1)
}
