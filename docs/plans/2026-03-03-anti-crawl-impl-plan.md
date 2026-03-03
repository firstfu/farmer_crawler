# 反爬蟲防護 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 為農產品價差雷達系統的爬蟲加入輕量反爬蟲防護：HTTP 偽裝、智慧重試、速率限制、狀態追蹤、儀表板健康度、Proxy 介面預留。

**Architecture:** 在現有 Repository Pattern 分層架構上擴充。新增 `transport.go` 處理 HTTP 傳輸抽象；修改 `crawler.go` 加入 Header 偽裝、錯誤碼分類重試、指數退避、Token Bucket 速率限制；擴充 `sqlite.go` 新增 `crawl_status` 表追蹤爬取歷史；在 Handler 層新增健康度端點與模板。

**Tech Stack:** Go 1.25、`golang.org/x/time/rate`（Token Bucket）、SQLite、Gin、HTMX

**Design doc:** `docs/plans/2026-03-03-anti-crawl-protection-design.md`

---

### Task 1: 新增領域模型 — CrawlStatus + CrawlHealth

**Files:**
- Modify: `internal/domain/model.go:57-65`（在檔案末尾新增）

**Step 1: 新增 CrawlStatus 和 CrawlHealth 模型**

在 `internal/domain/model.go` 末尾新增：

```go
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
```

注意：需要在 `model.go` 頂部的 `package domain` 下加入 `import "time"`。

**Step 2: 驗證編譯**

Run: `cd D:/myCodeProject/farmer_crawler && go build ./internal/domain/`
Expected: 編譯成功，無錯誤

**Step 3: Commit**

```bash
git add internal/domain/model.go
git commit -m "feat: 新增 CrawlStatus 與 CrawlHealth 領域模型"
```

---

### Task 2: 擴充配置 — 速率限制參數

**Files:**
- Modify: `internal/config/config.go:19-29`（CrawlerConfig struct）
- Modify: `config.yaml:5-14`（crawler 區段）

**Step 1: 在 CrawlerConfig 新增速率限制欄位**

在 `internal/config/config.go` 的 `CrawlerConfig` struct 中，`BackfillOnStart` 之後新增：

```go
	RateLimit       float64       `yaml:"rate_limit"`        // 每秒最多 N 個請求
	RateBurst       int           `yaml:"rate_burst"`        // 突發容許量
```

在 `Load` 函式的預設值區段（約 56-64 行），`cfg.Crawler.BackfillDays` 之後新增：

```go
	if cfg.Crawler.RateLimit <= 0 {
		cfg.Crawler.RateLimit = 1.0
	}
	if cfg.Crawler.RateBurst <= 0 {
		cfg.Crawler.RateBurst = 3
	}
```

**Step 2: 更新 config.yaml**

在 `config.yaml` 的 `crawler` 區段末尾（`backfill_on_start: true` 之後）新增：

```yaml
  rate_limit: 1        # 每秒最多 N 個請求
  rate_burst: 3        # 突發容許量
```

**Step 3: 驗證編譯**

Run: `cd D:/myCodeProject/farmer_crawler && go build ./internal/config/`
Expected: 編譯成功

**Step 4: Commit**

```bash
git add internal/config/config.go config.yaml
git commit -m "feat: 新增速率限制配置參數 rate_limit, rate_burst"
```

---

### Task 3: 新增 Transport 層 — Proxy 介面預留

**Files:**
- Create: `internal/service/transport.go`

**Step 1: 建立 transport.go**

```go
// internal/service/transport.go
// 農產品價差雷達系統 — HTTP 傳輸層抽象
// 負責：提供可替換的 HTTP Transport 介面，預留未來 Proxy 擴充點
// 目前實作 DirectTransport（直接連線），未來可新增 ProxyTransport

package service

import (
	"net"
	"net/http"
	"time"
)

// NewDirectTransport 建立直接連線的 HTTP Transport（預設）
// 設定合理的連線池與超時參數
func NewDirectTransport() *http.Transport {
	return &http.Transport{
		MaxIdleConns:        10,
		MaxIdleConnsPerHost: 5,
		IdleConnTimeout:     90 * time.Second,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
	}
}

// 未來擴充：
// func NewProxyTransport(proxyURL string) (*http.Transport, error) { ... }
// func NewRotatingProxyTransport(proxyURLs []string) (*http.Transport, error) { ... }
```

**Step 2: 驗證編譯**

Run: `cd D:/myCodeProject/farmer_crawler && go build ./internal/service/`
Expected: 編譯成功

**Step 3: Commit**

```bash
git add internal/service/transport.go
git commit -m "feat: 新增 HTTP Transport 抽象層，預留 Proxy 擴充介面"
```

---

### Task 4: 擴充 Repository — crawl_status 表與 CRUD

**Files:**
- Modify: `internal/repository/sqlite.go:53-75`（migrate 方法）
- Modify: `internal/repository/sqlite.go`（末尾新增方法）
- Test: `internal/repository/sqlite_test.go`（新增測試）

**Step 1: 寫失敗測試 — SaveCrawlStatus + GetRecentCrawlStatus**

在 `internal/repository/sqlite_test.go` 末尾新增：

```go
func TestSaveCrawlStatus(t *testing.T) {
	repo := setupTestDB(t)

	status := domain.CrawlStatus{
		DateFrom:    "115.03.01",
		DateTo:      "115.03.03",
		RecordCount: 42,
		Status:      "success",
		ErrorMsg:    "",
		DurationMs:  1500,
	}

	err := repo.SaveCrawlStatus(&status)
	if err != nil {
		t.Fatalf("SaveCrawlStatus 失敗: %v", err)
	}

	results, err := repo.GetRecentCrawlStatus(5)
	if err != nil {
		t.Fatalf("GetRecentCrawlStatus 失敗: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("預期 1 筆狀態記錄，得到 %d 筆", len(results))
	}
	if results[0].Status != "success" {
		t.Errorf("預期狀態 success，得到 %s", results[0].Status)
	}
	if results[0].RecordCount != 42 {
		t.Errorf("預期 42 筆記錄，得到 %d", results[0].RecordCount)
	}
}

func TestGetCrawlHealthSummary(t *testing.T) {
	repo := setupTestDB(t)

	// 插入 3 筆狀態：2 成功 1 失敗
	statuses := []domain.CrawlStatus{
		{DateFrom: "115.03.01", DateTo: "115.03.01", RecordCount: 10, Status: "success", DurationMs: 500},
		{DateFrom: "115.03.02", DateTo: "115.03.02", RecordCount: 0, Status: "failed", ErrorMsg: "timeout", DurationMs: 30000},
		{DateFrom: "115.03.03", DateTo: "115.03.03", RecordCount: 15, Status: "success", DurationMs: 800},
	}
	for i := range statuses {
		if err := repo.SaveCrawlStatus(&statuses[i]); err != nil {
			t.Fatalf("SaveCrawlStatus 失敗: %v", err)
		}
	}

	health, err := repo.GetCrawlHealthSummary()
	if err != nil {
		t.Fatalf("GetCrawlHealthSummary 失敗: %v", err)
	}
	if health.TotalCrawls24h != 3 {
		t.Errorf("預期 3 次爬取，得到 %d", health.TotalCrawls24h)
	}
	if health.FailedCrawls24h != 1 {
		t.Errorf("預期 1 次失敗，得到 %d", health.FailedCrawls24h)
	}
	// 成功率 = 2/3 ≈ 66.67
	expectedRate := 66.67
	if health.SuccessRate24h < expectedRate-1 || health.SuccessRate24h > expectedRate+1 {
		t.Errorf("預期成功率約 %.2f%%，得到 %.2f%%", expectedRate, health.SuccessRate24h)
	}
}
```

注意：`sqlite_test.go` 的 import 中已有 `farmer_crawler/internal/domain`，不需額外新增。

**Step 2: 執行測試確認失敗**

Run: `cd D:/myCodeProject/farmer_crawler && go test -v -run "TestSaveCrawlStatus|TestGetCrawlHealthSummary" ./internal/repository/`
Expected: FAIL — `SaveCrawlStatus` 和 `GetCrawlHealthSummary` 方法不存在

**Step 3: 在 migrate 中新增 crawl_status 表**

在 `internal/repository/sqlite.go` 的 `migrate()` 方法中，`CREATE INDEX IF NOT EXISTS idx_market_code` 之後新增：

```sql
	CREATE TABLE IF NOT EXISTS crawl_status (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		crawl_time DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		date_from TEXT NOT NULL,
		date_to TEXT NOT NULL,
		record_count INTEGER DEFAULT 0,
		status TEXT NOT NULL,
		error_msg TEXT DEFAULT '',
		duration_ms INTEGER DEFAULT 0
	);
	CREATE INDEX IF NOT EXISTS idx_crawl_status_time ON crawl_status(crawl_time);
```

**Step 4: 實作 SaveCrawlStatus、GetRecentCrawlStatus、GetCrawlHealthSummary**

在 `internal/repository/sqlite.go` 末尾（`scanRecords` 函式之後）新增：

```go
// SaveCrawlStatus 儲存一筆爬取狀態記錄
func (r *SQLiteRepo) SaveCrawlStatus(status *domain.CrawlStatus) error {
	query := `
	INSERT INTO crawl_status (date_from, date_to, record_count, status, error_msg, duration_ms)
	VALUES (?, ?, ?, ?, ?, ?)
	`
	result, err := r.db.Exec(query,
		status.DateFrom, status.DateTo, status.RecordCount,
		status.Status, status.ErrorMsg, status.DurationMs,
	)
	if err != nil {
		return fmt.Errorf("儲存爬取狀態失敗: %w", err)
	}
	id, _ := result.LastInsertId()
	status.ID = id
	return nil
}

// GetRecentCrawlStatus 取得最近 N 筆爬取狀態（依時間降序）
func (r *SQLiteRepo) GetRecentCrawlStatus(limit int) ([]domain.CrawlStatus, error) {
	query := `
	SELECT id, crawl_time, date_from, date_to, record_count, status, error_msg, duration_ms
	FROM crawl_status
	ORDER BY crawl_time DESC
	LIMIT ?
	`
	rows, err := r.db.Query(query, limit)
	if err != nil {
		return nil, fmt.Errorf("查詢爬取狀態失敗: %w", err)
	}
	defer rows.Close()

	var statuses []domain.CrawlStatus
	for rows.Next() {
		var s domain.CrawlStatus
		if err := rows.Scan(&s.ID, &s.CrawlTime, &s.DateFrom, &s.DateTo,
			&s.RecordCount, &s.Status, &s.ErrorMsg, &s.DurationMs); err != nil {
			return nil, err
		}
		statuses = append(statuses, s)
	}
	return statuses, nil
}

// GetCrawlHealthSummary 取得最近 24 小時的爬蟲健康度摘要
func (r *SQLiteRepo) GetCrawlHealthSummary() (*domain.CrawlHealth, error) {
	health := &domain.CrawlHealth{}

	// 最近 24 小時的統計
	query := `
	SELECT
		COUNT(*) as total,
		SUM(CASE WHEN status != 'success' THEN 1 ELSE 0 END) as failed
	FROM crawl_status
	WHERE crawl_time >= datetime('now', '-24 hours')
	`
	err := r.db.QueryRow(query).Scan(&health.TotalCrawls24h, &health.FailedCrawls24h)
	if err != nil {
		return nil, fmt.Errorf("查詢健康度統計失敗: %w", err)
	}

	if health.TotalCrawls24h > 0 {
		health.SuccessRate24h = float64(health.TotalCrawls24h-health.FailedCrawls24h) / float64(health.TotalCrawls24h) * 100
	}

	// 最近一次爬取
	lastQuery := `
	SELECT crawl_time, status
	FROM crawl_status
	ORDER BY crawl_time DESC
	LIMIT 1
	`
	err = r.db.QueryRow(lastQuery).Scan(&health.LastCrawlTime, &health.LastStatus)
	if err != nil {
		// 無記錄時不回報錯誤
		health.LastStatus = "unknown"
	}

	return health, nil
}
```

**Step 5: 執行測試確認通過**

Run: `cd D:/myCodeProject/farmer_crawler && go test -v -run "TestSaveCrawlStatus|TestGetCrawlHealthSummary" ./internal/repository/`
Expected: PASS

**Step 6: 執行全部 repository 測試確認無回歸**

Run: `cd D:/myCodeProject/farmer_crawler && go test -v ./internal/repository/`
Expected: 全部 PASS（原本 7 個 + 新增 2 個 = 9 個）

**Step 7: Commit**

```bash
git add internal/repository/sqlite.go internal/repository/sqlite_test.go
git commit -m "feat: 新增 crawl_status 表與 CRUD 方法，含測試"
```

---

### Task 5: 改造爬蟲核心 — HTTP 偽裝 + 智慧重試 + 速率限制 + 狀態追蹤

**Files:**
- Modify: `internal/service/crawler.go`（多處修改）
- Test: `internal/service/crawler_test.go`（新增測試）
- Modify: `go.mod`（新增 golang.org/x/time 依賴）

**Step 1: 安裝 golang.org/x/time 依賴**

Run: `cd D:/myCodeProject/farmer_crawler && go get golang.org/x/time/rate`
Expected: go.mod 更新，新增 `golang.org/x/time` 依賴

**Step 2: 寫失敗測試 — backoffDuration + buildRequest**

在 `internal/service/crawler_test.go` 末尾新增：

```go
// TestBackoffDuration 測試指數退避計算
func TestBackoffDuration(t *testing.T) {
	svc := &CrawlerService{}

	// attempt=0, base=30s → 30s + jitter(0~30s) → 30~60s
	d0 := svc.backoffDuration(0, 30*time.Second)
	if d0 < 30*time.Second || d0 > 60*time.Second {
		t.Errorf("attempt=0 預期 30~60s，得到 %v", d0)
	}

	// attempt=1, base=30s → 60s + jitter(0~30s) → 60~90s
	d1 := svc.backoffDuration(1, 30*time.Second)
	if d1 < 60*time.Second || d1 > 90*time.Second {
		t.Errorf("attempt=1 預期 60~90s，得到 %v", d1)
	}

	// attempt=2, base=30s → 120s + jitter(0~30s) → 120~150s
	d2 := svc.backoffDuration(2, 30*time.Second)
	if d2 < 120*time.Second || d2 > 150*time.Second {
		t.Errorf("attempt=2 預期 120~150s，得到 %v", d2)
	}
}

// TestBuildRequest 測試 HTTP 請求包含正確 Header
func TestBuildRequest(t *testing.T) {
	svc := &CrawlerService{
		apiURL:   "https://data.moa.gov.tw/Service/OpenData/FromM/FarmTransData.aspx",
		cropName: "茭白筍",
	}

	apiURL := svc.BuildAPIURL("115.03.01", "115.03.03")
	req, err := svc.buildRequest(apiURL)
	if err != nil {
		t.Fatalf("buildRequest 失敗: %v", err)
	}

	// 檢查 User-Agent
	ua := req.Header.Get("User-Agent")
	if ua == "" {
		t.Error("User-Agent 不應為空")
	}
	if !containsString(ua, "Mozilla") {
		t.Errorf("User-Agent 應包含 Mozilla，得到: %s", ua)
	}

	// 檢查 Accept-Language
	al := req.Header.Get("Accept-Language")
	if !containsString(al, "zh-TW") {
		t.Errorf("Accept-Language 應包含 zh-TW，得到: %s", al)
	}

	// 檢查 Referer
	ref := req.Header.Get("Referer")
	if ref == "" {
		t.Error("Referer 不應為空")
	}
}

// TestClassifyHTTPError 測試 HTTP 錯誤碼分類
func TestClassifyHTTPError(t *testing.T) {
	tests := []struct {
		statusCode    int
		wantRetryable bool
		wantCategory  string
	}{
		{429, true, "rate_limited"},
		{403, false, "blocked"},
		{500, true, "server_error"},
		{502, true, "server_error"},
		{503, true, "server_error"},
		{408, true, "timeout"},
		{400, false, "client_error"},
		{404, false, "client_error"},
		{200, false, "ok"},
	}

	for _, tc := range tests {
		t.Run(fmt.Sprintf("HTTP_%d", tc.statusCode), func(t *testing.T) {
			retryable, category := classifyHTTPError(tc.statusCode)
			if retryable != tc.wantRetryable {
				t.Errorf("HTTP %d: retryable 預期 %v，得到 %v", tc.statusCode, tc.wantRetryable, retryable)
			}
			if category != tc.wantCategory {
				t.Errorf("HTTP %d: category 預期 %q，得到 %q", tc.statusCode, tc.wantCategory, category)
			}
		})
	}
}
```

注意：需要在 `crawler_test.go` 的 import 中加入 `"fmt"` 和 `"time"`（`time` 已存在）。

**Step 3: 執行測試確認失敗**

Run: `cd D:/myCodeProject/farmer_crawler && go test -v -run "TestBackoffDuration|TestBuildRequest|TestClassifyHTTPError" ./internal/service/`
Expected: FAIL — 方法不存在

**Step 4: 修改 crawler.go — 新增 import、修改 struct、新增方法**

4a. 更新 import 區塊，在 `"time"` 之後新增：

```go
	"context"
	"math/rand"

	"golang.org/x/time/rate"
```

4b. 修改 `CrawlerService` struct，在 `client *http.Client` 之後新增：

```go
	limiter *rate.Limiter  // 全域速率限制
```

4c. 修改 `NewCrawlerService` 簽名，新增 `rateLimit float64, rateBurst int` 參數：

```go
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
```

4d. 新增 `buildRequest` 方法（在 `BuildAPIURL` 之後）：

```go
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
```

4e. 新增 `classifyHTTPError` 函式（在 `parseMarketCode` 之前）：

```go
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
```

4f. 新增 `backoffDuration` 方法（在 `classifyHTTPError` 之後）：

```go
// backoffDuration 計算指數退避時間 = base * 2^attempt + random(0, base)
func (s *CrawlerService) backoffDuration(attempt int, base time.Duration) time.Duration {
	backoff := base * time.Duration(1<<uint(attempt))
	jitter := time.Duration(rand.Int63n(int64(base)))
	return backoff + jitter
}
```

4g. 改寫 `FetchAndStore` 方法的重試迴圈，使用智慧重試 + 速率限制 + 狀態追蹤：

```go
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
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		if err := s.limiter.Wait(ctx); err != nil {
			cancel()
			lastErr = fmt.Errorf("速率限制等待失敗: %w", err)
			continue
		}
		cancel()

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
```

4h. 新增 `saveCrawlStatus` 私有方法（在 `FetchAndStore` 之後）：

```go
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
```

注意：需在 `crawler.go` 頂部的 import 加入 `"farmer_crawler/internal/domain"`。

**Step 5: 執行測試確認通過**

Run: `cd D:/myCodeProject/farmer_crawler && go test -v -run "TestBackoffDuration|TestBuildRequest|TestClassifyHTTPError" ./internal/service/`
Expected: PASS

**Step 6: 更新 main.go 中 NewCrawlerService 的呼叫**

`cmd/server/main.go` 中有兩處呼叫 `NewCrawlerService`（約 44 行和 106 行），都需要新增 `rateLimit` 和 `rateBurst` 參數：

```go
// 第 44 行附近（伺服器模式）
crawler := service.NewCrawlerService(
	cfg.Crawler.APIURL,
	cfg.Crawler.CropName,
	cfg.Crawler.RetryCount,
	cfg.Crawler.RetryInterval,
	cfg.Crawler.BatchDays,
	cfg.Crawler.BatchDelay,
	cfg.Crawler.RateLimit,
	cfg.Crawler.RateBurst,
	repo,
)

// 第 106 行附近（CLI 模式）
crawler := service.NewCrawlerService(
	cfg.Crawler.APIURL,
	cfg.Crawler.CropName,
	cfg.Crawler.RetryCount,
	cfg.Crawler.RetryInterval,
	cfg.Crawler.BatchDays,
	cfg.Crawler.BatchDelay,
	cfg.Crawler.RateLimit,
	cfg.Crawler.RateBurst,
	repo,
)
```

**Step 7: 修復 crawler_test.go 中直接建立 CrawlerService 的測試**

測試中有幾處直接用 `&CrawlerService{}` 建立實例（例如 `TestBuildAPIURL`、`TestParseAPIResponse` 等），這些不需要 limiter 所以不用改。但 `TestBackoffDuration` 也是直接建立，也不需要 limiter。確認所有測試仍然通過即可。

**Step 8: 驗證全部編譯和測試**

Run: `cd D:/myCodeProject/farmer_crawler && go build ./...`
Expected: 編譯成功

Run: `cd D:/myCodeProject/farmer_crawler && go test ./internal/service/ ./internal/repository/`
Expected: 全部 PASS

**Step 9: Commit**

```bash
git add internal/service/crawler.go internal/service/crawler_test.go cmd/server/main.go go.mod go.sum
git commit -m "feat: 爬蟲加入 HTTP 偽裝、智慧重試、速率限制、狀態追蹤"
```

---

### Task 6: 新增儀表板健康度端點與模板

**Files:**
- Modify: `internal/handler/dashboard.go`（新增端點）
- Create: `web/templates/components/crawl_health.html`（健康度組件）
- Modify: `web/templates/dashboard.html`（嵌入健康度區塊）

**Step 1: 在 dashboard.go 新增 CrawlStatusAPI 端點**

在 `internal/handler/dashboard.go` 的 `RegisterRoutes` 中新增路由：

```go
r.GET("/api/crawl-status", h.CrawlStatus)
```

在 `TriggerCrawlRange` 方法之後新增 handler 方法：

```go
// CrawlStatus 爬蟲健康度 HTMX 端點
// 回傳最近爬取狀態與健康度摘要 HTML 片段
func (h *DashboardHandler) CrawlStatus(c *gin.Context) {
	health, _ := h.repo.GetCrawlHealthSummary()
	recentStatus, _ := h.repo.GetRecentCrawlStatus(5)

	data := gin.H{
		"Health":       health,
		"RecentStatus": recentStatus,
	}

	c.Header("Content-Type", "text/html; charset=utf-8")
	h.tmpl.ExecuteTemplate(c.Writer, "crawl_health", data)
}
```

同時在 `Dashboard` 方法的 `data := gin.H{...}` 中新增：

```go
	"CrawlHealth": func() *domain.CrawlHealth {
		health, _ := h.repo.GetCrawlHealthSummary()
		return health
	}(),
	"RecentCrawlStatus": func() []domain.CrawlStatus {
		statuses, _ := h.repo.GetRecentCrawlStatus(5)
		return statuses
	}(),
```

注意：`dashboard.go` 的 import 中已有 `farmer_crawler/internal/domain`，不需額外新增。

**Step 2: 建立健康度模板組件**

建立 `web/templates/components/crawl_health.html`：

```html
{{/*
  web/templates/components/crawl_health.html
  農產品價差雷達系統 — 爬蟲健康度組件
  顯示：最近爬取狀態、24h 成功率、最近 5 次爬取記錄
  支援 HTMX 局部更新（hx-get="/api/crawl-status"）
*/}}
{{define "crawl_health"}}
<div class="bg-white rounded-xl shadow-sm border border-gray-100 p-4">
    <div class="flex items-center justify-between mb-3">
        <h3 class="text-sm font-semibold text-gray-600 flex items-center gap-2">
            <svg class="w-4 h-4 text-brand-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z"/>
            </svg>
            爬蟲健康度
        </h3>
        <button hx-get="/api/crawl-status" hx-target="#crawl-health-content" hx-swap="innerHTML"
                class="text-xs text-brand-600 hover:text-brand-700 font-medium">
            重新整理
        </button>
    </div>
    <div id="crawl-health-content">
        {{if .Health}}
        <div class="grid grid-cols-3 gap-3 mb-3">
            <div class="text-center">
                <div class="text-lg font-bold {{if eq .Health.LastStatus "success"}}text-emerald-600{{else if eq .Health.LastStatus "unknown"}}text-gray-400{{else}}text-red-600{{end}}">
                    {{if eq .Health.LastStatus "success"}}正常{{else if eq .Health.LastStatus "unknown"}}無資料{{else}}異常{{end}}
                </div>
                <div class="text-xs text-gray-500">最近狀態</div>
            </div>
            <div class="text-center">
                <div class="text-lg font-bold {{if ge .Health.SuccessRate24h 90.0}}text-emerald-600{{else if ge .Health.SuccessRate24h 50.0}}text-yellow-600{{else}}text-red-600{{end}}">
                    {{sprintf "%.0f%%" .Health.SuccessRate24h}}
                </div>
                <div class="text-xs text-gray-500">24h 成功率</div>
            </div>
            <div class="text-center">
                <div class="text-lg font-bold text-gray-700">{{.Health.TotalCrawls24h}}</div>
                <div class="text-xs text-gray-500">24h 爬取次數</div>
            </div>
        </div>
        {{end}}

        {{if .RecentStatus}}
        <div class="border-t border-gray-100 pt-2">
            <div class="text-xs text-gray-500 mb-1">最近爬取記錄</div>
            <div class="space-y-1">
                {{range .RecentStatus}}
                <div class="flex items-center justify-between text-xs">
                    <span class="text-gray-600">{{.DateFrom}} ~ {{.DateTo}}</span>
                    <span class="flex items-center gap-1">
                        {{if eq .Status "success"}}
                        <span class="w-1.5 h-1.5 rounded-full bg-emerald-500"></span>
                        <span class="text-emerald-600">{{.RecordCount}} 筆</span>
                        {{else}}
                        <span class="w-1.5 h-1.5 rounded-full bg-red-500"></span>
                        <span class="text-red-600">{{.Status}}</span>
                        {{end}}
                    </span>
                </div>
                {{end}}
            </div>
        </div>
        {{end}}
    </div>
</div>
{{end}}
```

**Step 3: 在 dashboard.html 中嵌入健康度區塊**

在 `web/templates/dashboard.html` 中，`{{template "kpi_summary" .}}` 之後、`{{template "filter_bar" .}}` 之前新增：

```html
    <!-- 爬蟲健康度 -->
    <section class="animate-fade-in">
        {{template "crawl_health" dict "Health" .CrawlHealth "RecentStatus" .RecentCrawlStatus}}
    </section>
```

注意：Go 模板原生不支援 `dict` 函式。需要在 `handler.go` 的 `funcMap` 中新增 `dict` 函式：

```go
"dict": func(values ...interface{}) map[string]interface{} {
    dict := make(map[string]interface{})
    for i := 0; i < len(values)-1; i += 2 {
        key, _ := values[i].(string)
        dict[key] = values[i+1]
    }
    return dict
},
```

或者更簡單的做法：直接在 Dashboard handler 中把 `CrawlHealth` 和 `RecentCrawlStatus` 傳入 data（已在 Step 1 完成），然後在模板中直接引用 `.CrawlHealth` 和 `.RecentCrawlStatus`，而不使用嵌套的 `crawl_health` 模板。

**建議的更簡單做法：** 在 `dashboard.html` 中直接寫 inline HTML，或者把 `crawl_health` 模板設計為接收頂層 data（`.CrawlHealth` 和 `.RecentCrawlStatus`）：

修改 `crawl_health.html` 中的引用從 `.Health` → `.CrawlHealth`，`.RecentStatus` → `.RecentCrawlStatus`。

然後在 `dashboard.html` 中：
```html
{{template "crawl_health" .}}
```

而 `/api/crawl-status` 端點回傳時，handler 傳入的 data 用 `CrawlHealth` 和 `RecentCrawlStatus` 作為 key。

**Step 4: 驗證編譯**

Run: `cd D:/myCodeProject/farmer_crawler && go build ./...`
Expected: 編譯成功

**Step 5: Commit**

```bash
git add internal/handler/dashboard.go web/templates/components/crawl_health.html web/templates/dashboard.html
git commit -m "feat: 新增爬蟲健康度儀表板端點與模板"
```

---

### Task 7: 全部測試 + 最終驗證

**Files:**
- 無新檔案

**Step 1: 執行全部測試**

Run: `cd D:/myCodeProject/farmer_crawler && go test -v ./...`
Expected: 全部 PASS（原本 15 個 + 新增約 5 個 = ~20 個）

**Step 2: 編譯完整二進位**

Run: `cd D:/myCodeProject/farmer_crawler && CGO_ENABLED=1 PATH="/c/msys64/ucrt64/bin:$PATH" go build -o farmer_crawler ./cmd/server`
Expected: 編譯成功，產生 `farmer_crawler` 執行檔

**Step 3: 手動冒煙測試（可選）**

Run: `cd D:/myCodeProject/farmer_crawler && ./farmer_crawler crawl --today`
Expected: 正常爬取並印出記錄筆數

**Step 4: 最終 commit**

```bash
git add -A
git commit -m "chore: 反爬蟲防護功能完成 — 全部測試通過"
```

---

### Task 8: 更新 todo.md

**Files:**
- Modify: `docs/todo.md`

**Step 1: 在 todo.md 中新增反爬蟲功能項目並標記完成**

在 `docs/todo.md` 中新增相關項目並使用 ~~刪除線~~ 標記已完成。
