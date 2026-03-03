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
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"time"

	"farmer_crawler/internal/domain"
	"farmer_crawler/internal/repository"
)

// apiRecordJSON 對應農糧署 API 回傳的中文鍵名 JSON 結構
// 每個欄位的 json tag 使用中文名稱以符合 API 回傳格式
type apiRecordJSON struct {
	TradeDate   string  `json:"交易日期"`
	TypeCode    string  `json:"種類代碼"`
	CropCode    string  `json:"作物代號"`
	CropName    string  `json:"作物名稱"`
	MarketCode  int     `json:"市場代號"`
	MarketName  string  `json:"市場名稱"`
	UpperPrice  float64 `json:"上價"`
	MiddlePrice float64 `json:"中價"`
	LowerPrice  float64 `json:"下價"`
	AvgPrice    float64 `json:"平均價"`
	Volume      float64 `json:"交易量"`
}

// CrawlerService 爬蟲服務主結構
// 封裝 API 位址、目標作物、重試策略、資料庫存取與 HTTP 客戶端
type CrawlerService struct {
	apiURL        string
	cropName      string
	retryCount    int
	retryInterval time.Duration
	repo          *repository.SQLiteRepo
	client        *http.Client
}

// NewCrawlerService 建立新的爬蟲服務實例
// 參數說明：
//   - apiURL: 農糧署 API 基礎 URL
//   - cropName: 目標作物名稱（例如 "茭白筍"）
//   - retryCount: HTTP 請求失敗時的最大重試次數
//   - retryInterval: 重試間隔時間
//   - repo: SQLite 資料庫存取層
func NewCrawlerService(apiURL, cropName string, retryCount int, retryInterval time.Duration, repo *repository.SQLiteRepo) *CrawlerService {
	return &CrawlerService{
		apiURL:        apiURL,
		cropName:      cropName,
		retryCount:    retryCount,
		retryInterval: retryInterval,
		repo:          repo,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
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

		records = append(records, domain.PriceRecord{
			TradeDate:   raw.TradeDate,
			CropCode:    raw.CropCode,
			CropName:    raw.CropName,
			MarketCode:  raw.MarketCode,
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
// 具備重試機制：HTTP 請求失敗時最多重試 retryCount 次
// 回傳成功寫入的記錄筆數與錯誤
func (s *CrawlerService) FetchAndStore(startDate, endDate string) (int, error) {
	apiURL := s.BuildAPIURL(startDate, endDate)
	log.Printf("[爬蟲] 開始擷取資料: %s ~ %s", startDate, endDate)

	var body []byte
	var lastErr error

	// 重試迴圈：最多嘗試 retryCount + 1 次（含首次）
	for attempt := 0; attempt <= s.retryCount; attempt++ {
		if attempt > 0 {
			log.Printf("[爬蟲] 第 %d 次重試...", attempt)
			time.Sleep(s.retryInterval)
		}

		resp, err := s.client.Get(apiURL)
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
			lastErr = fmt.Errorf("API 回傳非 200 狀態碼: %d (第 %d 次)", resp.StatusCode, attempt+1)
			continue
		}

		// 成功取得回應，跳出重試迴圈
		lastErr = nil
		break
	}

	if lastErr != nil {
		return 0, fmt.Errorf("HTTP 請求最終失敗（已重試 %d 次）: %w", s.retryCount, lastErr)
	}

	// 解析 JSON 回應
	records, err := s.ParseAPIResponse(body)
	if err != nil {
		return 0, fmt.Errorf("解析回應失敗: %w", err)
	}

	// 空回應處理：記錄警告但不視為錯誤
	if len(records) == 0 {
		log.Printf("[爬蟲] 警告: API 回傳空資料 (%s ~ %s)", startDate, endDate)
		return 0, nil
	}

	// 批次寫入資料庫
	if err := s.repo.BatchUpsert(records); err != nil {
		return 0, fmt.Errorf("寫入資料庫失敗: %w", err)
	}

	log.Printf("[爬蟲] 成功寫入 %d 筆記錄 (%s ~ %s)", len(records), startDate, endDate)
	return len(records), nil
}

// CrawlToday 爬取今日的交易資料
// 自動將今日日期轉換為民國格式後呼叫 FetchAndStore
func (s *CrawlerService) CrawlToday() (int, error) {
	today := ToMinguoDate(time.Now())
	return s.FetchAndStore(today, today)
}

// CrawlRange 爬取指定日期範圍的交易資料
// 參數 from、to 皆為民國日期格式（例如 "114.03.01"）
func (s *CrawlerService) CrawlRange(from, to string) (int, error) {
	return s.FetchAndStore(from, to)
}
