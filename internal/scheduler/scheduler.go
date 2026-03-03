// internal/scheduler/scheduler.go
// 農產品價差雷達系統 — 排程管理
// 使用 gocron/v2 設定每日自動爬取任務
// 預設 cron 表達式：0 10 * * *（每日 10:00）
// 支援啟動時自動回補歷史資料與每日回補
// 主要功能：
//   - NewScheduler: 建立排程器實例，時區設定為 Asia/Taipei (UTC+8)
//   - Start: 以指定 cron 表達式啟動排程，定期呼叫 CrawlerService.CrawlToday + Backfill
//   - Stop: 優雅關閉排程器，等待執行中任務完成

package scheduler

import (
	"log"
	"time"

	"github.com/go-co-op/gocron/v2"

	"farmer_crawler/internal/config"
	"farmer_crawler/internal/repository"
	"farmer_crawler/internal/service"
)

// Scheduler 排程管理器
// 封裝 gocron.Scheduler、CrawlerService、Repository 與 Config
// 負責定時觸發爬取任務與歷史資料回補
type Scheduler struct {
	s       gocron.Scheduler
	crawler *service.CrawlerService
	repo    *repository.SQLiteRepo
	cfg     *config.Config
}

// NewScheduler 建立新的排程管理器實例
// 時區固定為 Asia/Taipei (UTC+8)，確保排程時間符合台灣時間
func NewScheduler(crawler *service.CrawlerService, repo *repository.SQLiteRepo, cfg *config.Config) (*Scheduler, error) {
	s, err := gocron.NewScheduler(gocron.WithLocation(time.FixedZone("Asia/Taipei", 8*60*60)))
	if err != nil {
		return nil, err
	}
	return &Scheduler{s: s, crawler: crawler, repo: repo, cfg: cfg}, nil
}

// Start 以指定 cron 表達式啟動排程
// cronExpr 範例："0 10 * * *"（每日 10:00 執行）
// 排程觸發時會先 CrawlToday 再 Backfill
// 若 BackfillOnStart 為 true，啟動時以 goroutine 執行一次回補
func (sc *Scheduler) Start(cronExpr string) error {
	_, err := sc.s.NewJob(
		gocron.CronJob(cronExpr, false),
		gocron.NewTask(func() {
			// 每日爬取今日資料
			log.Println("[排程] 開始每日自動爬取...")
			count, err := sc.crawler.CrawlToday()
			if err != nil {
				log.Printf("[排程] 爬取失敗: %v", err)
			} else {
				log.Printf("[排程] 每日爬取完成: %d 筆記錄", count)
			}

			// 每日回補缺漏
			log.Println("[排程] 開始每日回補...")
			backfillCount, backfillErr := sc.crawler.Backfill(
				sc.cfg.Crawler.BackfillDays,
				sc.cfg.Crawler.BatchDays,
				sc.cfg.Crawler.BatchDelay,
				sc.repo,
				nil,
			)
			if backfillErr != nil {
				log.Printf("[排程] 回補部分失敗: %v", backfillErr)
			}
			if backfillCount > 0 {
				log.Printf("[排程] 每日回補完成: %d 筆記錄", backfillCount)
			}
		}),
	)
	if err != nil {
		return err
	}

	sc.s.Start()
	log.Printf("[排程] 已啟動，cron: %s", cronExpr)

	// 啟動時自動回補
	if sc.cfg.Crawler.BackfillOnStart {
		go func() {
			log.Println("[排程] 啟動時自動回補歷史資料...")
			count, err := sc.crawler.Backfill(
				sc.cfg.Crawler.BackfillDays,
				sc.cfg.Crawler.BatchDays,
				sc.cfg.Crawler.BatchDelay,
				sc.repo,
				nil,
			)
			if err != nil {
				log.Printf("[排程] 啟動回補部分失敗: %v", err)
			}
			log.Printf("[排程] 啟動回補完成: %d 筆記錄", count)
		}()
	}

	return nil
}

// Stop 關閉排程器
// 呼叫 gocron.Scheduler.Shutdown 優雅停止所有排程任務
func (sc *Scheduler) Stop() error {
	return sc.s.Shutdown()
}
