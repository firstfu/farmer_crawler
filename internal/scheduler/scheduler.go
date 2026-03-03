// internal/scheduler/scheduler.go
// 農產品價差雷達系統 — 排程管理
// 使用 gocron/v2 設定每日自動爬取任務
// 預設 cron 表達式：0 10 * * *（每日 10:00）
// 主要功能：
//   - NewScheduler: 建立排程器實例，時區設定為 Asia/Taipei (UTC+8)
//   - Start: 以指定 cron 表達式啟動排程，定期呼叫 CrawlerService.CrawlToday
//   - Stop: 優雅關閉排程器，等待執行中任務完成

package scheduler

import (
	"log"
	"time"

	"github.com/go-co-op/gocron/v2"

	"farmer_crawler/internal/service"
)

// Scheduler 排程管理器
// 封裝 gocron.Scheduler 與 CrawlerService，負責定時觸發爬取任務
type Scheduler struct {
	s       gocron.Scheduler
	crawler *service.CrawlerService
}

// NewScheduler 建立新的排程管理器實例
// 時區固定為 Asia/Taipei (UTC+8)，確保排程時間符合台灣時間
func NewScheduler(crawler *service.CrawlerService) (*Scheduler, error) {
	s, err := gocron.NewScheduler(gocron.WithLocation(time.FixedZone("Asia/Taipei", 8*60*60)))
	if err != nil {
		return nil, err
	}
	return &Scheduler{s: s, crawler: crawler}, nil
}

// Start 以指定 cron 表達式啟動排程
// cronExpr 範例："0 10 * * *"（每日 10:00 執行）
// 排程觸發時會呼叫 CrawlerService.CrawlToday 進行當日爬取
func (sc *Scheduler) Start(cronExpr string) error {
	_, err := sc.s.NewJob(
		gocron.CronJob(cronExpr, false),
		gocron.NewTask(func() {
			log.Println("[排程] 開始每日自動爬取...")
			count, err := sc.crawler.CrawlToday()
			if err != nil {
				log.Printf("[排程] 爬取失敗: %v", err)
				return
			}
			log.Printf("[排程] 每日爬取完成: %d 筆記錄", count)
		}),
	)
	if err != nil {
		return err
	}

	sc.s.Start()
	log.Printf("[排程] 已啟動，cron: %s", cronExpr)
	return nil
}

// Stop 關閉排程器
// 呼叫 gocron.Scheduler.Shutdown 優雅停止所有排程任務
func (sc *Scheduler) Stop() error {
	return sc.s.Shutdown()
}
