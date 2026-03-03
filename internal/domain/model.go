// internal/domain/model.go
// 農產品價差雷達系統 — 領域模型
// 定義系統核心資料結構：交易行情記錄、價差計算結果、API 回傳格式

package domain

import "time"

// PriceRecord 代表一筆批發市場交易行情記錄
type PriceRecord struct {
	ID          int64   `json:"id"`
	TradeDate   string  `json:"trade_date"`   // 民國日期 "114.03.03"
	CropCode    string  `json:"crop_code"`    // "SQ1" 帶殼 / "SQ3" 去殼
	CropName    string  `json:"crop_name"`    // "茭白筍-帶殼"
	MarketCode  int     `json:"market_code"`  // 400 (台中市)
	MarketName  string  `json:"market_name"`  // "台中市"
	UpperPrice  float64 `json:"upper_price"`  // 上價 (元/公斤)
	MiddlePrice float64 `json:"middle_price"` // 中價
	LowerPrice  float64 `json:"lower_price"`  // 下價
	AvgPrice    float64 `json:"avg_price"`    // 平均價
	Volume      float64 `json:"volume"`       // 交易量 (公斤)
}

// SpreadResult 代表一個目標市場的價差計算結果
type SpreadResult struct {
	TargetMarket     string  `json:"target_market"`
	TargetMarketCode int     `json:"target_market_code"`
	TargetAvgPrice   float64 `json:"target_avg_price"`
	BaseAvgPrice     float64 `json:"base_avg_price"`
	AbsoluteSpread   float64 `json:"absolute_spread"` // 絕對價差 (元/公斤)
	SpreadPercent    float64 `json:"spread_percent"`  // 溢價百分比 (%)
	TargetVolume     float64 `json:"target_volume"`
}

// TrendPoint 代表趨勢圖上的一個資料點
type TrendPoint struct {
	Date     string  `json:"date"`
	AvgPrice float64 `json:"avg_price"`
	Volume   float64 `json:"volume"`
}

// MarketTrend 代表單一市場的趨勢資料
type MarketTrend struct {
	MarketName string       `json:"market_name"`
	MarketCode int          `json:"market_code"`
	Points     []TrendPoint `json:"points"`
}

// CrawlBatchProgress 代表一個爬取批次的進度資訊（用於 SSE 推送）
type CrawlBatchProgress struct {
	BatchIndex  int    `json:"batch_index"`   // 當前批次索引（從 1 開始）
	TotalBatch  int    `json:"total_batch"`   // 總批次數
	FromDate    string `json:"from_date"`     // 此批次起始日期（民國格式）
	ToDate      string `json:"to_date"`       // 此批次結束日期（民國格式）
	RecordCount int    `json:"record_count"`  // 此批次寫入筆數
	Status      string `json:"status"`        // "success" / "error"
	Error       string `json:"error,omitempty"`
}

// CrawlBatchResult 代表分批爬取的最終結果
type CrawlBatchResult struct {
	TotalRecords  int    `json:"total_records"`
	TotalBatches  int    `json:"total_batches"`
	FailedBatches int    `json:"failed_batches"`
	Status        string `json:"status"` // "completed" / "partial" / "failed"
}

// CrawlStatus 代表一次爬取操作的狀態記錄
type CrawlStatus struct {
	ID          int64     `json:"id"`
	CrawlTime   time.Time `json:"crawl_time"`    // 爬取時間
	DateFrom    string    `json:"date_from"`      // 起始日期（民國）
	DateTo      string    `json:"date_to"`        // 結束日期（民國）
	RecordCount int       `json:"record_count"`   // 爬取筆數
	Status      string    `json:"status"`         // "success" | "failed" | "rate_limited" | "blocked"
	ErrorMsg    string    `json:"error_msg"`       // 錯誤訊息
	DurationMs  int64     `json:"duration_ms"`    // 耗時（毫秒）
}

// CrawlHealth 代表爬蟲健康度摘要
type CrawlHealth struct {
	LastCrawlTime   time.Time `json:"last_crawl_time"`
	LastStatus      string    `json:"last_status"`
	SuccessRate24h  float64   `json:"success_rate_24h"`  // 最近 24h 成功率 (0-100)
	TotalCrawls24h  int       `json:"total_crawls_24h"`
	FailedCrawls24h int       `json:"failed_crawls_24h"`
}
