# 反爬蟲防護設計文件

> 日期：2026-03-03
> 方案：方案 A（輕量防護）
> 狀態：已核可

## 1. 背景與目標

農產品價差雷達系統的爬蟲目前缺乏反爬蟲防護機制。隨著未來計畫擴充至 10+ 種作物，
請求量將顯著增加。本設計旨在提供全面但輕量的防護，包含：

- HTTP 請求偽裝（避免被識別為機器人）
- 智慧重試策略（依錯誤碼區分處理）
- 全域速率限制（控制請求頻率）
- 爬取狀態追蹤（失敗可見、可追溯）
- 儀表板健康度顯示
- Proxy 介面預留

## 2. HTTP 請求偽裝

### 現況
`crawler.go` 使用裸露的 `http.Client{}`，不帶 User-Agent 或其他 Header。

### 設計
新增 `buildRequest` 方法，為所有 API 請求附加瀏覽器 Header：

```go
func (s *CrawlerService) buildRequest(apiURL string) (*http.Request, error) {
    req, err := http.NewRequest("GET", apiURL, nil)
    if err != nil {
        return nil, err
    }
    req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
    req.Header.Set("Accept", "application/json, text/plain, */*")
    req.Header.Set("Accept-Language", "zh-TW,zh;q=0.9,en-US;q=0.8")
    req.Header.Set("Accept-Encoding", "gzip, deflate, br")
    req.Header.Set("Connection", "keep-alive")
    req.Header.Set("Referer", "https://data.moa.gov.tw/")
    return req, nil
}
```

### 影響範圍
`FetchAndStore` 方法中 `s.client.Get(apiURL)` 改為 `s.client.Do(req)`。

## 3. 智慧重試策略

### 現況
所有 HTTP 錯誤用同一策略重試（固定 30 秒 × 3 次）。

### 設計：依錯誤碼分類

| HTTP 狀態碼 | 分類 | 策略 |
|-------------|------|------|
| 429 | 速率限制 | 讀取 Retry-After，否則指數退避（60s → 120s → 240s） |
| 403 | 被封鎖 | **不重試**，立即記錄錯誤 |
| 5xx | 伺服器錯誤 | 指數退避（30s → 60s → 120s），最多 3 次 |
| 408/超時 | 超時 | 指數退避（15s → 30s → 60s），最多 3 次 |
| 其他 4xx | 客戶端錯誤 | **不重試**，記錄錯誤 |

### 指數退避 + Jitter

```go
func (s *CrawlerService) backoffDuration(attempt int, base time.Duration) time.Duration {
    backoff := base * time.Duration(1<<uint(attempt))
    jitter := time.Duration(rand.Int63n(int64(base)))
    return backoff + jitter
}
```

## 4. 速率限制器

### 設計
使用 `golang.org/x/time/rate`（Token Bucket 演算法），全域控制 HTTP 請求頻率。

```go
type CrawlerService struct {
    // ... 現有欄位
    limiter *rate.Limiter
}

// 初始化：每秒 1 個請求，burst 最多 3 個
limiter: rate.NewLimiter(rate.Every(1*time.Second), 3)
```

### 配置

```yaml
crawler:
  rate_limit: 1    # 每秒最多 N 個請求
  rate_burst: 3    # 突發容許量
```

### 使用方式
在每次 HTTP 請求前呼叫 `s.limiter.Wait(ctx)`，自動控制速率。

## 5. 爬取狀態追蹤

### 資料模型

```go
type CrawlStatus struct {
    ID          int64
    CrawlTime   time.Time  // 爬取時間
    DateFrom    string     // 起始日期（民國）
    DateTo      string     // 結束日期（民國）
    RecordCount int        // 爬取筆數
    Status      string     // "success" | "failed" | "rate_limited" | "blocked"
    ErrorMsg    string     // 錯誤訊息
    Duration    int64      // 耗時（毫秒）
}
```

### 資料庫表

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
```

### Repository 方法
- `SaveCrawlStatus(status *CrawlStatus) error`
- `GetRecentCrawlStatus(limit int) ([]CrawlStatus, error)`
- `GetCrawlHealthSummary() (*CrawlHealth, error)` — 最近 24h 成功率

## 6. 儀表板健康度

### 顯示內容
在首頁新增「爬蟲健康度」區塊：
- 最近一次爬取時間與狀態
- 最近 24 小時成功率（百分比）
- 最近 5 次爬取記錄列表

### 端點
`GET /api/crawl-status` — 回傳爬取狀態（HTMX 片段）

## 7. Proxy 介面預留

### Transport 抽象

```go
// internal/service/transport.go

type HTTPTransport interface {
    RoundTrip(*http.Request) (*http.Response, error)
}

type DirectTransport struct {
    transport *http.Transport
}
```

### 使用方式

```go
client: &http.Client{
    Timeout:   30 * time.Second,
    Transport: NewDirectTransport(),
}
```

### config.yaml 預留

```yaml
crawler:
  # proxy:
  #   enabled: false
  #   url: "socks5://127.0.0.1:1080"
  #   rotate: false
```

## 8. 通知方式

- **Log 檔記錄**：使用結構化日誌記錄所有爬取事件
- **儀表板顯示**：即時顯示爬蟲健康度

## 9. 改動範圍估算

| 檔案 | 改動類型 | 說明 |
|------|----------|------|
| `internal/service/crawler.go` | 修改 | 加入 Header、智慧重試、速率限制 |
| `internal/service/transport.go` | 新增 | Proxy 介面抽象 |
| `internal/domain/model.go` | 修改 | 新增 CrawlStatus 模型 |
| `internal/repository/sqlite.go` | 修改 | 新增 crawl_status 表及 CRUD |
| `internal/handler/dashboard.go` | 修改 | 新增爬取狀態端點及模板 |
| `config.yaml` | 修改 | 新增速率限制設定 |
| `internal/config/config.go` | 修改 | 新增速率限制配置解析 |
| `go.mod` | 修改 | 新增 `golang.org/x/time` 依賴 |
| `templates/` | 修改 | 新增健康度顯示區塊 |

## 10. 不做的事情（YAGNI）

- ❌ 動態 User-Agent 池（單一 UA 已足夠）
- ❌ Circuit Breaker（請求量太低不需要）
- ❌ 指紋隨機化（政府 API 不需要）
- ❌ JavaScript 渲染（API 直接回 JSON）
- ❌ CAPTCHA 處理（政府開放資料不需要）
- ❌ 分散式爬取（單機足夠）
- ❌ 代理池管理（僅預留介面）
