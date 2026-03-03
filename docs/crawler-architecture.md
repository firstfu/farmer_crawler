# 爬蟲系統架構文件

> 農產品價差雷達系統 — 爬蟲模組技術文件
> 對象：開發者 / 接手維護者
> 最後更新：2026-03-03

---

## 目錄

1. [系統概覽](#1-系統概覽)
2. [資料來源：農糧署 API](#2-資料來源農糧署-api)
3. [核心模組：CrawlerService](#3-核心模組crawlerservice)
4. [排程器：Scheduler](#4-排程器scheduler)
5. [歷史資料回補：Backfill](#5-歷史資料回補backfill)
6. [觸發方式一覽](#6-觸發方式一覽)
7. [資料儲存：Repository 層](#7-資料儲存repository-層)
8. [配置參數](#8-配置參數)
9. [資料流程圖](#9-資料流程圖)
10. [已知注意事項與陷阱](#10-已知注意事項與陷阱)

---

## 1. 系統概覽

爬蟲系統負責從農糧署開放資料 API 擷取全台 16 個批發市場的茭白筍交易行情，並存入本地 SQLite 資料庫。系統支援三種觸發方式：每日自動排程、CLI 手動執行、Web API 手動觸發。

### 涉及檔案

| 檔案路徑 | 職責 |
|---|---|
| `internal/service/crawler.go` | 爬蟲核心邏輯：API 呼叫、JSON 解析、分批爬取、重試、回補 |
| `internal/service/crawler_test.go` | 單元測試（15 個測試案例） |
| `internal/scheduler/scheduler.go` | gocron 排程管理，每日 10:00 自動觸發 |
| `internal/repository/sqlite.go` | SQLite 資料存取，BatchUpsert 批次寫入 |
| `internal/domain/model.go` | 領域模型定義（PriceRecord、CrawlBatchProgress 等） |
| `internal/handler/dashboard.go` | HTTP 端點：手動爬取 API（`POST /api/crawl`、`GET /api/crawl/range`） |
| `cmd/server/main.go` | 應用入口，CLI 子命令處理 |
| `config.yaml` | 爬蟲配置參數 |

---

## 2. 資料來源：農糧署 API

### 端點

```
https://data.moa.gov.tw/Service/OpenData/FromM/FarmTransData.aspx
```

### 查詢參數

| 參數 | 說明 | 範例值 |
|---|---|---|
| `Crop` | 作物名稱 | `茭白筍` |
| `StartDate` | 起始日期（民國格式） | `115.03.01` |
| `EndDate` | 結束日期（民國格式） | `115.03.03` |
| `$top` | 回傳筆數上限 | `500` |

### 回傳格式

API 回傳 JSON 陣列，**鍵名全部為中文**：

```json
[
  {
    "交易日期": "115.03.03",
    "種類代碼": "V",
    "作物代號": "SQ1",
    "作物名稱": "茭白筍-帶殼",
    "市場代號": 400,
    "市場名稱": "台中市",
    "上價": 100.0,
    "中價": 90.0,
    "下價": 80.0,
    "平均價": 90.0,
    "交易量": 500.0
  }
]
```

### 特殊記錄類型

- **休市記錄**：`作物代號 = "rest"`，`作物名稱 = "休市"` — 解析時必須過濾
- **空記錄**：`作物代號 = ""`（空字串）— 解析時必須過濾
- **空回傳**：整個 API 回傳 `[]` — 不視為錯誤，僅記錄警告

### 日期格式：民國年

所有 API 參數與資料庫儲存一律使用**民國日期格式** `YYY.MM.DD`：

```
西元 2026-03-03  →  民國 115.03.03
轉換公式：民國年 = 西元年 - 1911
```

相關函式：
- `service.ToMinguoDate(time.Time) string` — 西元轉民國
- `service.ParseMinguoDate(string) (time.Time, error)` — 民國轉西元

---

## 3. 核心模組：CrawlerService

位置：`internal/service/crawler.go`

### 結構體

```go
type CrawlerService struct {
    apiURL        string               // 農糧署 API 基礎 URL
    cropName      string               // 目標作物名稱（"茭白筍"）
    retryCount    int                  // HTTP 失敗最大重試次數
    retryInterval time.Duration        // 重試間隔
    batchDays     int                  // 每批次天數
    batchDelay    time.Duration        // 批次間等待時間
    repo          *repository.SQLiteRepo
    client        *http.Client         // Timeout: 30 秒
}
```

### 核心函式

#### `BuildAPIURL(startDate, endDate string) string`

組合完整的 API 查詢 URL，自動 URL-encode 中文作物名稱。

#### `ParseAPIResponse(data []byte) ([]domain.PriceRecord, error)`

解析 API 回傳的中文鍵名 JSON，轉換為 `domain.PriceRecord` 切片。

**過濾規則**：
- 跳過 `CropCode == "rest"`（休市）
- 跳過 `CropCode == ""`（空記錄）
- 跳過市場代號解析失敗的記錄（記錄 log 警告）

**市場代號雙型態處理**：

農糧署 API 的「市場代號」欄位可能回傳 `int`（如 `400`）或 `string`（如 `"400"`），使用 `json.Number` 型別接收，再透過 `parseMarketCode()` 統一轉為 `int`：

```go
type apiRecordJSON struct {
    MarketCode json.Number `json:"市場代號"` // 可能是 int 或 string
}
```

#### `FetchAndStore(startDate, endDate string) (int, error)`

完整的單次爬取流程：**發送 HTTP → 重試機制 → 解析 JSON → 寫入資料庫**。

**重試機制**：
1. 首次請求 + 最多 `retryCount` 次重試（共 `retryCount + 1` 次嘗試）
2. 每次重試前等待 `retryInterval`
3. 觸發重試的條件：HTTP 連線失敗、讀取回應失敗、HTTP 狀態碼非 200
4. 全部重試失敗才回傳 error

**空資料處理**：
API 回傳空陣列時，記錄警告但回傳 `(0, nil)` — 不視為錯誤。

#### `CrawlToday() (int, error)`

爬取今日資料的快捷方法，自動用 `time.Now()` 轉民國日期。

#### `CrawlRange(from, to string) (int, error)`

爬取指定日期範圍，內部呼叫 `CrawlRangeWithProgress`。

#### `CrawlRangeWithProgress(from, to, batchDays, batchDelay, progressFn) (totalRecords, failedBatches, error)`

分批爬取的核心實作：

1. 呼叫 `SplitDateRange()` 將大範圍切分為 ≤ `batchDays` 天的批次
2. 依序執行每個批次的 `FetchAndStore()`
3. 批次間等待 `batchDelay`（避免 API 過載）
4. 透過 `progressFn` 回調即時回報每批進度
5. **單批失敗不影響其他批次**，僅累計 `failedBatches`
6. 全部批次都失敗才回傳 error

#### `SplitDateRange(from, to string, batchDays int) ([]DateBatch, error)`

將大日期範圍拆分為多個批次：

```
範圍 115.01.01 ~ 115.01.21 (21 天), batchDays=7
→ 批次 1: 115.01.01 ~ 115.01.07
→ 批次 2: 115.01.08 ~ 115.01.14
→ 批次 3: 115.01.15 ~ 115.01.21
```

最後一批可能不足 `batchDays` 天（自動截止到結束日期）。

---

## 4. 排程器：Scheduler

位置：`internal/scheduler/scheduler.go`

使用 `gocron/v2`，時區固定為 `Asia/Taipei` (UTC+8)。

### 每日排程任務

Cron 表達式預設 `0 10 * * *`（每日上午 10:00 執行）。

每次觸發執行兩個步驟：

```
Step 1: CrawlToday()    → 爬取今日資料
Step 2: Backfill()       → 回補最近 N 天缺漏的歷史資料
```

### 啟動時回補

若 `backfill_on_start: true`（預設啟用），伺服器啟動時會以 **goroutine** 非同步執行一次 `Backfill()`，不阻塞主程序啟動。

---

## 5. 歷史資料回補：Backfill

位置：`internal/service/crawler.go` → `Backfill()` 方法

### 回補流程

```
1. 計算回補範圍：today - backfillDays ~ today
2. 查詢資料庫已有哪些日期的資料 → existingDates (map)
3. 逐日檢查：
   - 排除週日（批發市場固定休市）
   - 若該日期不在 existingDates 中 → 標記為缺漏
4. 將缺漏日期合併為連續區間（mergeDateRanges）
5. 對每個連續區間呼叫 CrawlRangeWithProgress() 分批爬取
```

### 日期合併邏輯

`mergeDateRanges()` 將離散的缺漏日期合併為連續區間，減少 API 請求次數：

```
缺漏日期: [3/1, 3/2, 3/3, 3/5, 3/6]
合併結果: [3/1~3/3, 3/5~3/6] → 2 個區間，而非 5 次請求
```

判斷不連續的閾值：日期間隔 > 2 天（考慮週日跳過的情況）。

---

## 6. 觸發方式一覽

### 方式 1：每日自動排程

```
gocron → CrawlToday() + Backfill()
觸發時間：每日 10:00 (Asia/Taipei)
```

### 方式 2：CLI 命令列

```bash
# 爬取今日資料
go run ./cmd/server crawl --today

# 爬取指定日期範圍
go run ./cmd/server crawl --from 114.01.01 --to 114.03.03
```

CLI 模式不啟動 HTTP 伺服器，執行完畢後直接退出。

### 方式 3：Web API 手動觸發

```
POST /api/crawl          → CrawlToday()，回傳 HTML 片段
GET  /api/crawl/range    → CrawlRangeWithProgress()，透過 SSE 即時推送進度
    ?from=114.01.01
    &to=114.03.03
```

#### SSE 進度推送格式

`GET /api/crawl/range` 使用 Server-Sent Events 即時回報每批次進度：

**進度事件** (`event: progress`)：
```json
{
  "batch_index": 1,
  "total_batch": 3,
  "from_date": "114.01.01",
  "to_date": "114.01.07",
  "record_count": 48,
  "status": "success"
}
```

**完成事件** (`event: done`)：
```json
{
  "total_records": 144,
  "total_batches": 3,
  "failed_batches": 0,
  "status": "completed"
}
```

`status` 值：`"completed"` / `"partial"`（部分批次失敗）/ `"failed"`（全部失敗）

---

## 7. 資料儲存：Repository 層

位置：`internal/repository/sqlite.go`

### 資料表結構

```sql
CREATE TABLE price_records (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    trade_date    TEXT NOT NULL,        -- 民國日期 "115.03.03"
    crop_code     TEXT NOT NULL,        -- "SQ1" 帶殼 / "SQ3" 去殼
    crop_name     TEXT NOT NULL,        -- "茭白筍-帶殼"
    market_code   INTEGER NOT NULL,     -- 400 (台中市)
    market_name   TEXT NOT NULL,        -- "台中市"
    upper_price   REAL,                 -- 上價 (元/公斤)
    middle_price  REAL,                 -- 中價
    lower_price   REAL,                 -- 下價
    avg_price     REAL,                 -- 平均價
    volume        REAL,                 -- 交易量 (公斤)
    created_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(trade_date, market_code, crop_code)
);
```

### Upsert 策略

使用 SQLite 的 `ON CONFLICT ... DO UPDATE` 語法，以 `(trade_date, market_code, crop_code)` 三欄位組合作為唯一約束：

- **新記錄**：直接 INSERT
- **重複記錄**：覆蓋更新價格、交易量、時間戳

### BatchUpsert

爬蟲每次呼叫 `BatchUpsert()` 批次寫入，使用 `sql.Tx` 交易確保原子性（全部成功或全部回滾）。

### 索引

```sql
CREATE INDEX idx_trade_date ON price_records(trade_date);
CREATE INDEX idx_market_code ON price_records(market_code);
```

### WAL 模式

資料庫以 `?_journal_mode=WAL` 開啟，提升並發讀寫效能。

---

## 8. 配置參數

檔案：`config.yaml`

```yaml
crawler:
  api_url: "https://data.moa.gov.tw/Service/OpenData/FromM/FarmTransData.aspx"
  crop_name: "茭白筍"
  schedule: "0 10 * * *"      # 每日 10:00 (cron 表達式)
  retry_count: 3               # HTTP 失敗最大重試次數
  retry_interval: 30s          # 重試間隔
  batch_days: 7                # 每批次天數
  batch_delay: 2s              # 批次間等待（避免 API 過載）
  backfill_days: 30            # 回補最近 N 天
  backfill_on_start: true      # 啟動時自動回補
```

### 預設值（程式碼內建）

| 參數 | 預設值 | 觸發條件 |
|---|---|---|
| `batch_days` | 7 | 值 ≤ 0 時 |
| `batch_delay` | 2s | 值 ≤ 0 時 |
| `backfill_days` | 30 | 值 ≤ 0 時 |
| HTTP Client Timeout | 30s | 固定值（不可配置） |

---

## 9. 資料流程圖

### 完整資料流

```
                        ┌────────────────┐
                        │  農糧署 API    │
                        │  (JSON, 中文鍵) │
                        └───────┬────────┘
                                │ HTTP GET
                                ▼
┌─────────┐         ┌──────────────────────┐
│ 觸發源   │────────▶│   CrawlerService     │
│         │         │                      │
│ • 排程器 │         │  BuildAPIURL()       │
│ • CLI   │         │  ↓                   │
│ • Web   │         │  FetchAndStore()     │
│   API   │         │  ├─ HTTP GET + 重試  │
└─────────┘         │  ├─ ParseAPIResponse │
                    │  │  ├─ 過濾 rest/空  │
                    │  │  └─ json.Number   │
                    │  └─ BatchUpsert()    │
                    └──────────┬───────────┘
                               │
                               ▼
                    ┌──────────────────────┐
                    │   SQLite (WAL)       │
                    │   price_records      │
                    │   UPSERT on          │
                    │   (date,market,crop) │
                    └──────────────────────┘
```

### 分批爬取流程

```
CrawlRange("114.01.01", "114.01.21")
  │
  ▼
SplitDateRange() → 3 batches (7天/批)
  │
  ├─ Batch 1: FetchAndStore("114.01.01", "114.01.07")
  │  └─ sleep(batchDelay)
  ├─ Batch 2: FetchAndStore("114.01.08", "114.01.14")
  │  └─ sleep(batchDelay)
  └─ Batch 3: FetchAndStore("114.01.15", "114.01.21")
```

### 回補流程

```
Backfill(30 天)
  │
  ├─ 查詢 DB 已有日期 → existingDates
  ├─ 逐日檢查（排除週日）→ missingDates
  ├─ mergeDateRanges() → 連續區間
  └─ 每個區間 → CrawlRangeWithProgress()
```

---

## 10. 已知注意事項與陷阱

### API 市場代號雙型態

農糧署 API 的「市場代號」欄位**不保證回傳固定型態**，可能是 JSON number（`400`）或 JSON string（`"400"`）。已使用 `json.Number` 解決，修改時**切勿改為** `int` 或 `string`。

### 民國日期字串比較

資料庫中 `trade_date` 存為文字 `"115.03.03"`，SQLite 的字串比較（`>=`、`<=`）在民國日期格式下剛好能正確排序（因年份位數固定為 3 位），但若未來年份達到 4 位（民國 1000 年 = 西元 2911 年）則需調整。

### 週日休市

批發市場**每週日固定休市**，回補邏輯已排除週日。但**國定假日**和**臨時休市**未處理 — API 會回傳空資料或 rest 記錄，系統會正確忽略但缺漏日期清單中可能包含這些假日。

### 批次間延遲

`batch_delay`（預設 2 秒）是為了避免農糧署 API 被過度請求而設。**請勿設為 0**，否則短時間大量請求可能導致 API 暫時拒絕服務。

### HTTP Client Timeout

固定 30 秒，不可透過 config.yaml 配置。若 API 回應緩慢，需修改 `NewCrawlerService()` 中的 `http.Client{Timeout}` 值。

### 空回應不是錯誤

某些日期（如假日、非產季）API 合法地回傳 `[]`，系統記錄 log 警告但不報錯。這是預期行為。

### CGO 編譯需求

SQLite driver (`go-sqlite3`) 需要 CGO，Windows 環境必須：
```bash
export CGO_ENABLED=1
export PATH="/c/msys64/ucrt64/bin:$PATH"
```

### 測試策略

所有爬蟲測試為**純單元測試**，不發送真實 HTTP 請求：
- 日期轉換測試（西元 ↔ 民國）
- API URL 組合測試
- JSON 解析與過濾測試（含 string/int 市場代號）
- 日期範圍分批測試（整除/非整除/單日/反向範圍）
