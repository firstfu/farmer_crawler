// internal/service/crawler.go
// 農產品價差雷達系統 — 爬蟲服務
// 負責呼叫農糧署開放資料 API，取得批發市場蔬菜交易行情
// 主要功能：
//   - ToMinguoDate: 西元日期轉民國日期字串（YYY.MM.DD）
//   - BuildAPIURL: 組合農糧署 API 查詢 URL
//   - ParseAPIResponse: 解析 API 回傳的中文鍵名 JSON，過濾休市記錄
//   - FetchAndStore: 發送 HTTP GET 請求、解析回應並存入資料庫
//   - CrawlToday / CrawlRange: 高階爬取介面
// HTTP 錯誤處理支援重試機制（retryCount 次，間隔 retryInterval）

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"golang.org/x/time/rate"

	"farmer_crawler/internal/domain"
	"farmer_crawler/internal/repository"
)

// apiRecordJSON 對應農糧署 API 回傳的中文鍵名 JSON 結構
// 每個欄位的 json tag 使用中文名稱以符合 API 回傳格式
type apiRecordJSON struct {
	TradeDate   string      `json:"交易日期"`
	TypeCode    string      `json:"種類代碼"`
	CropCode    string      `json:"作物代號"`
	CropName    string      `json:"作物名稱"`
	MarketCode  json.Number `json:"市場代號"` // API 可能回傳 int 或 string
	MarketName  string      `json:"市場名稱"`
	UpperPrice  float64     `json:"上價"`
	MiddlePrice float64     `json:"中價"`
	LowerPrice  float64     `json:"下價"`
	AvgPrice    float64     `json:"平均價"`
	Volume      float64     `json:"交易量"`
}

// DateBatch 代表一個日期批次範圍（用於分批爬取）
type DateBatch struct {
	From string // 民國日期格式
	To   string // 民國日期格式
}

// CrawlerService 爬蟲服務主結構
// 封裝 API 位址、目標作物、重試策略、分批設定、資料庫存取與 HTTP 客戶端
type CrawlerService struct {
	apiURL        string
	cropName      string
	retryCount    int
	retryInterval time.Duration
	batchDays     int
	batchDelay    time.Duration
	repo          *repository.SQLiteRepo
	client        *http.Client
	limiter       *rate.Limiter  // 全域速率限制
}

// NewCrawlerService 建立新的爬蟲服務實例
// 參數說明：
//   - apiURL: 農糧署 API 基礎 URL
//   - cropName: 目標作物名稱（例如 "茭白筍"）
//   - retryCount: HTTP 請求失敗時的最大重試次數
//   - retryInterval: 重試間隔時間
//   - batchDays: 每批次爬取天數
//   - batchDelay: 批次間等待時間（避免 API 過載）
//   - rateLimit: 每秒最多 N 個請求（Token Bucket 速率）
//   - rateBurst: 突發容許量（Token Bucket 容量）
//   - repo: SQLite 資料庫存取層
func NewCrawlerService(apiURL, cropName string, retryCount int, retryInterval time.Duration, batchDays int, batchDelay time.Duration, rateLimit float64, rateBurst int, repo *repository.SQLiteRepo) *CrawlerService {
	return &CrawlerService{
		apiURL:        apiURL,
		cropName:      cropName,
		retryCount:    retryCount,
		retryInterval: retryInterval,
		batchDays:     batchDays,
		batchDelay:    batchDelay,
		repo:          repo,
		client: &http.Client{
			Timeout:   30 * time.Second,
			Transport: NewDirectTransport(),
		},
		limiter: rate.NewLimiter(rate.Limit(rateLimit), rateBurst),
	}
}

// ToMinguoDate 將西元 time.Time 轉換為民國日期字串
// 格式為 "YYY.MM.DD"，例如 2026-03-03 → "115.03.03"
// 民國年 = 西元年 - 1911
func ToMinguoDate(t time.Time) string {
	year := t.Year() - 1911
	return fmt.Sprintf("%d.%02d.%02d", year, t.Month(), t.Day())
}

// BuildAPIURL 組合完整的農糧署 API 查詢 URL
// 參數：
//   - startDate: 起始日期（民國格式，例如 "114.03.01"）
//   - endDate: 結束日期（民國格式，例如 "114.03.03"）
//
// 回傳包含 Crop、StartDate、EndDate、$top 參數的完整 URL
func (s *CrawlerService) BuildAPIURL(startDate, endDate string) string {
	params := url.Values{}
	params.Set("Crop", s.cropName)
	params.Set("StartDate", startDate)
	params.Set("EndDate", endDate)
	params.Set("$top", "500")

	return s.apiURL + "?" + params.Encode()
}

// buildRequest 建立帶有瀏覽器 Header 的 HTTP 請求
// 避免被目標 API 識別為機器人爬蟲
func (s *CrawlerService) buildRequest(apiURL string) (*http.Request, error) {
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "zh-TW,zh;q=0.9,en-US;q=0.8")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Referer", "https://data.moa.gov.tw/")
	return req, nil
}

// ParseAPIResponse 解析農糧署 API 回傳的 JSON 資料
// 將中文鍵名 JSON 轉換為 domain.PriceRecord 切片
// 過濾規則：排除 CropCode 為 "rest"（休市）或空字串的記錄
func (s *CrawlerService) ParseAPIResponse(data []byte) ([]domain.PriceRecord, error) {
	var rawRecords []apiRecordJSON
	if err := json.Unmarshal(data, &rawRecords); err != nil {
		return nil, fmt.Errorf("JSON 解析失敗: %w", err)
	}

	var records []domain.PriceRecord
	for _, raw := range rawRecords {
		// 過濾休市記錄與空作物代號
		if raw.CropCode == "rest" || raw.CropCode == "" {
			continue
		}

		marketCode, err := parseMarketCode(raw.MarketCode)
		if err != nil {
			log.Printf("[爬蟲] 跳過無效市場代號: %v", raw.MarketCode)
			continue
		}

		records = append(records, domain.PriceRecord{
			TradeDate:   raw.TradeDate,
			CropCode:    raw.CropCode,
			CropName:    raw.CropName,
			MarketCode:  marketCode,
			MarketName:  raw.MarketName,
			UpperPrice:  raw.UpperPrice,
			MiddlePrice: raw.MiddlePrice,
			LowerPrice:  raw.LowerPrice,
			AvgPrice:    raw.AvgPrice,
			Volume:      raw.Volume,
		})
	}

	return records, nil
}

// FetchAndStore 從農糧署 API 取得指定日期範圍的交易資料並存入資料庫
// 具備智慧重試（依錯誤碼分類）、指數退避、速率限制、狀態追蹤
func (s *CrawlerService) FetchAndStore(startDate, endDate string) (int, error) {
	apiURL := s.BuildAPIURL(startDate, endDate)
	log.Printf("[爬蟲] 開始擷取資料: %s ~ %s", startDate, endDate)

	startTime := time.Now()
	var body []byte
	var lastErr error

	// 重試迴圈：最多嘗試 retryCount + 1 次（含首次）
	for attempt := 0; attempt <= s.retryCount; attempt++ {
		if attempt > 0 {
			backoff := s.backoffDuration(attempt-1, s.retryInterval)
			log.Printf("[爬蟲] 第 %d 次重試，等待 %v...", attempt, backoff)
			time.Sleep(backoff)
		}

		// 速率限制：等待 token
		if s.limiter != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			if err := s.limiter.Wait(ctx); err != nil {
				cancel()
				lastErr = fmt.Errorf("速率限制等待失敗: %w", err)
				continue
			}
			cancel()
		}

		// 建立帶 Header 的請求
		req, err := s.buildRequest(apiURL)
		if err != nil {
			lastErr = fmt.Errorf("建立請求失敗: %w", err)
			continue
		}

		resp, err := s.client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("HTTP 請求失敗 (第 %d 次): %w", attempt+1, err)
			continue
		}

		body, err = io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("讀取回應失敗 (第 %d 次): %w", attempt+1, err)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			retryable, category := classifyHTTPError(resp.StatusCode)
			lastErr = fmt.Errorf("API 回傳非 200 狀態碼: %d (%s, 第 %d 次)", resp.StatusCode, category, attempt+1)
			log.Printf("[爬蟲] HTTP %d (%s), retryable=%v", resp.StatusCode, category, retryable)

			if !retryable {
				// 不可重試的錯誤（403、4xx），記錄狀態後直接返回
				s.saveCrawlStatus(startDate, endDate, 0, category, lastErr.Error(), startTime)
				return 0, lastErr
			}

			// 429 時檢查 Retry-After
			if resp.StatusCode == 429 {
				if retryAfter := resp.Header.Get("Retry-After"); retryAfter != "" {
					if seconds, parseErr := strconv.Atoi(retryAfter); parseErr == nil {
						waitTime := time.Duration(seconds) * time.Second
						log.Printf("[爬蟲] 收到 Retry-After: %d 秒", seconds)
						time.Sleep(waitTime)
					}
				}
			}
			continue
		}

		// 成功取得回應，跳出重試迴圈
		lastErr = nil
		break
	}

	if lastErr != nil {
		s.saveCrawlStatus(startDate, endDate, 0, "failed", lastErr.Error(), startTime)
		return 0, fmt.Errorf("HTTP 請求最終失敗（已重試 %d 次）: %w", s.retryCount, lastErr)
	}

	// 解析 JSON 回應
	records, err := s.ParseAPIResponse(body)
	if err != nil {
		s.saveCrawlStatus(startDate, endDate, 0, "failed", err.Error(), startTime)
		return 0, fmt.Errorf("解析回應失敗: %w", err)
	}

	// 空回應處理：記錄警告但不視為錯誤
	if len(records) == 0 {
		log.Printf("[爬蟲] 警告: API 回傳空資料 (%s ~ %s)", startDate, endDate)
		s.saveCrawlStatus(startDate, endDate, 0, "success", "", startTime)
		return 0, nil
	}

	// 批次寫入資料庫
	if err := s.repo.BatchUpsert(records); err != nil {
		s.saveCrawlStatus(startDate, endDate, 0, "failed", err.Error(), startTime)
		return 0, fmt.Errorf("寫入資料庫失敗: %w", err)
	}

	log.Printf("[爬蟲] 成功寫入 %d 筆記錄 (%s ~ %s)", len(records), startDate, endDate)
	s.saveCrawlStatus(startDate, endDate, len(records), "success", "", startTime)
	return len(records), nil
}

// saveCrawlStatus 記錄一次爬取操作的狀態到資料庫
func (s *CrawlerService) saveCrawlStatus(dateFrom, dateTo string, recordCount int, status, errorMsg string, startTime time.Time) {
	duration := time.Since(startTime).Milliseconds()
	crawlStatus := &domain.CrawlStatus{
		DateFrom:    dateFrom,
		DateTo:      dateTo,
		RecordCount: recordCount,
		Status:      status,
		ErrorMsg:    errorMsg,
		DurationMs:  duration,
	}
	if err := s.repo.SaveCrawlStatus(crawlStatus); err != nil {
		log.Printf("[爬蟲] 儲存爬取狀態失敗: %v", err)
	}
}

// CrawlToday 爬取今日的交易資料
// 自動將今日日期轉換為民國格式後呼叫 FetchAndStore
func (s *CrawlerService) CrawlToday() (int, error) {
	today := ToMinguoDate(time.Now())
	return s.FetchAndStore(today, today)
}

// CrawlRange 爬取指定日期範圍的交易資料（自動分批）
// 參數 from、to 皆為民國日期格式（例如 "114.03.01"）
func (s *CrawlerService) CrawlRange(from, to string) (int, error) {
	totalRecords, _, err := s.CrawlRangeWithProgress(from, to, s.batchDays, s.batchDelay, nil)
	return totalRecords, err
}

// ParseMinguoDate 將民國日期字串轉為 time.Time
// 格式 "YYY.MM.DD"，例如 "115.03.03" → 2026-03-03
func ParseMinguoDate(s string) (time.Time, error) {
	parts := splitDotParts(s)
	if len(parts) != 3 {
		return time.Time{}, fmt.Errorf("無效民國日期格式: %q（應為 YYY.MM.DD）", s)
	}
	year, err := strconv.Atoi(parts[0])
	if err != nil {
		return time.Time{}, fmt.Errorf("無效民國年份: %q", parts[0])
	}
	month, err := strconv.Atoi(parts[1])
	if err != nil || month < 1 || month > 12 {
		return time.Time{}, fmt.Errorf("無效月份: %q", parts[1])
	}
	day, err := strconv.Atoi(parts[2])
	if err != nil || day < 1 || day > 31 {
		return time.Time{}, fmt.Errorf("無效日期: %q", parts[2])
	}
	return time.Date(year+1911, time.Month(month), day, 0, 0, 0, 0, time.Local), nil
}

// splitDotParts 以 "." 切分字串為最多 3 段
func splitDotParts(s string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	parts = append(parts, s[start:])
	return parts
}

// SplitDateRange 將大日期範圍拆分為多個 ≤batchDays 天的批次
// from/to 為民國日期格式，回傳 DateBatch 切片
func SplitDateRange(from, to string, batchDays int) ([]DateBatch, error) {
	fromTime, err := ParseMinguoDate(from)
	if err != nil {
		return nil, fmt.Errorf("起始日期錯誤: %w", err)
	}
	toTime, err := ParseMinguoDate(to)
	if err != nil {
		return nil, fmt.Errorf("結束日期錯誤: %w", err)
	}
	if toTime.Before(fromTime) {
		return nil, fmt.Errorf("結束日期 (%s) 早於起始日期 (%s)", to, from)
	}
	if batchDays <= 0 {
		batchDays = 7
	}

	var batches []DateBatch
	cursor := fromTime
	for !cursor.After(toTime) {
		batchEnd := cursor.AddDate(0, 0, batchDays-1)
		if batchEnd.After(toTime) {
			batchEnd = toTime
		}
		batches = append(batches, DateBatch{
			From: ToMinguoDate(cursor),
			To:   ToMinguoDate(batchEnd),
		})
		cursor = batchEnd.AddDate(0, 0, 1)
	}
	return batches, nil
}

// CrawlRangeWithProgress 分批爬取指定日期範圍，透過 progressFn 回報每批進度
// 單批失敗不影響其他批次；全部失敗才回傳 error
// progressFn 可為 nil（靜默模式）
func (s *CrawlerService) CrawlRangeWithProgress(
	from, to string,
	batchDays int,
	batchDelay time.Duration,
	progressFn func(domain.CrawlBatchProgress),
) (totalRecords int, failedBatches int, err error) {
	batches, err := SplitDateRange(from, to, batchDays)
	if err != nil {
		return 0, 0, err
	}

	for i, batch := range batches {
		if i > 0 && batchDelay > 0 {
			time.Sleep(batchDelay)
		}

		count, fetchErr := s.FetchAndStore(batch.From, batch.To)

		progress := domain.CrawlBatchProgress{
			BatchIndex:  i + 1,
			TotalBatch:  len(batches),
			FromDate:    batch.From,
			ToDate:      batch.To,
			RecordCount: count,
		}

		if fetchErr != nil {
			failedBatches++
			progress.Status = "error"
			progress.Error = fetchErr.Error()
			log.Printf("[爬蟲] 批次 %d/%d 失敗 (%s~%s): %v", i+1, len(batches), batch.From, batch.To, fetchErr)
		} else {
			totalRecords += count
			progress.Status = "success"
			log.Printf("[爬蟲] 批次 %d/%d 完成 (%s~%s): %d 筆", i+1, len(batches), batch.From, batch.To, count)
		}

		if progressFn != nil {
			progressFn(progress)
		}
	}

	if failedBatches == len(batches) {
		return totalRecords, failedBatches, fmt.Errorf("所有 %d 個批次皆失敗", len(batches))
	}
	return totalRecords, failedBatches, nil
}

// Backfill 自動回補最近 N 天缺漏的歷史資料
// 排除週日（批發市場休市），找出缺漏的連續日期區間後分批爬取
func (s *CrawlerService) Backfill(
	backfillDays, batchDays int,
	batchDelay time.Duration,
	repo *repository.SQLiteRepo,
	progressFn func(domain.CrawlBatchProgress),
) (int, error) {
	now := time.Now()
	fromTime := now.AddDate(0, 0, -backfillDays)
	toTime := now

	fromDate := ToMinguoDate(fromTime)
	toDate := ToMinguoDate(toTime)

	// 查詢已有資料的日期
	existingDates, err := repo.GetExistingTradeDates(fromDate, toDate)
	if err != nil {
		return 0, fmt.Errorf("查詢已有日期失敗: %w", err)
	}

	// 找出缺漏日期（排除週日）
	var missingDates []time.Time
	cursor := fromTime
	for !cursor.After(toTime) {
		if cursor.Weekday() != time.Sunday {
			minguoDate := ToMinguoDate(cursor)
			if !existingDates[minguoDate] {
				missingDates = append(missingDates, cursor)
			}
		}
		cursor = cursor.AddDate(0, 0, 1)
	}

	if len(missingDates) == 0 {
		log.Println("[回補] 無缺漏資料，跳過回補")
		return 0, nil
	}

	log.Printf("[回補] 發現 %d 天缺漏資料，開始回補...", len(missingDates))

	// 將缺漏日期合併為連續區間
	ranges := mergeDateRanges(missingDates)

	totalRecords := 0
	for _, r := range ranges {
		from := ToMinguoDate(r[0])
		to := ToMinguoDate(r[1])
		count, _, crawlErr := s.CrawlRangeWithProgress(from, to, batchDays, batchDelay, progressFn)
		if crawlErr != nil {
			log.Printf("[回補] 區間 %s~%s 部分失敗: %v", from, to, crawlErr)
		}
		totalRecords += count
	}

	log.Printf("[回補] 完成，共回補 %d 筆記錄", totalRecords)
	return totalRecords, nil
}

// mergeDateRanges 將日期列表合併為連續區間
// 回傳 [][2]time.Time，每個元素為 [起始, 結束]
func mergeDateRanges(dates []time.Time) [][2]time.Time {
	if len(dates) == 0 {
		return nil
	}
	var ranges [][2]time.Time
	start := dates[0]
	prev := dates[0]
	for i := 1; i < len(dates); i++ {
		// 若日期不連續（間隔 > 2 天，跳過週日），開始新區間
		diff := dates[i].Sub(prev).Hours() / 24
		if diff > 2 {
			ranges = append(ranges, [2]time.Time{start, prev})
			start = dates[i]
		}
		prev = dates[i]
	}
	ranges = append(ranges, [2]time.Time{start, prev})
	return ranges
}

// classifyHTTPError 依 HTTP 狀態碼分類錯誤
// 回傳 (是否可重試, 錯誤分類)
func classifyHTTPError(statusCode int) (retryable bool, category string) {
	switch {
	case statusCode == 200:
		return false, "ok"
	case statusCode == 429:
		return true, "rate_limited"
	case statusCode == 403:
		return false, "blocked"
	case statusCode == 408:
		return true, "timeout"
	case statusCode >= 500:
		return true, "server_error"
	default:
		return false, "client_error"
	}
}

// backoffDuration 計算指數退避時間 = base * 2^attempt + random(0, base)
func (s *CrawlerService) backoffDuration(attempt int, base time.Duration) time.Duration {
	backoff := base * time.Duration(1<<uint(attempt))
	jitter := time.Duration(rand.Int63n(int64(base)))
	return backoff + jitter
}

// parseMarketCode 將 json.Number 轉為 int，處理 API 回傳 string 或 int 的情況
func parseMarketCode(n json.Number) (int, error) {
	// 先嘗試直接轉 int64
	if v, err := n.Int64(); err == nil {
		return int(v), nil
	}
	// 若為 string 格式則用 strconv
	return strconv.Atoi(n.String())
}
