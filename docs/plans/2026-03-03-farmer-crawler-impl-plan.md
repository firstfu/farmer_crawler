# 農產品價差雷達系統 — 實作計畫

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 建立一個 Go 語言的農產品價差分析工具，自動爬取茭白筍交易行情，計算跨市場價差，並透過 HTMX 儀表板呈現決策資訊。

**Architecture:** 分層架構 (Repository Pattern)。domain 定義資料模型，repository 負責 SQLite 持久化，service 層包含爬蟲引擎與價差計算引擎，handler 層處理 HTTP + HTMX 請求，scheduler 負責定時任務。前端使用 Go html/template + HTMX + ECharts CDN，零建構工具。

**Tech Stack:** Go, Gin, SQLite (go-sqlite3), gocron/v2, HTMX, ECharts, TailwindCSS CDN, gopkg.in/yaml.v3

**Design Doc:** `docs/plans/2026-03-03-farmer-crawler-design.md`

---

## 前置需求

Windows 環境使用 `go-sqlite3` 需要 CGO：
- 確認已安裝 GCC（可透過 `gcc --version` 檢查）
- 若未安裝，建議安裝 [TDM-GCC](https://jmeubank.github.io/tdm-gcc/) 或透過 MSYS2
- 設定環境變數 `CGO_ENABLED=1`

---

### Task 1: 專案骨架與 Go Module 初始化

**Files:**
- Create: `go.mod`
- Create: `config.yaml`
- Create: `internal/domain/model.go`
- Create: `internal/config/config.go`
- Create: `cmd/server/main.go` (最小可運行版本)

**Step 1: 初始化 Go Module 與安裝依賴**

```bash
cd D:/myCodeProject/farmer_crawler
go mod init farmer_crawler
go get github.com/gin-gonic/gin
go get github.com/mattn/go-sqlite3
go get github.com/go-co-op/gocron/v2
go get gopkg.in/yaml.v3
```

**Step 2: 建立目錄結構**

```bash
mkdir -p cmd/server
mkdir -p internal/config
mkdir -p internal/domain
mkdir -p internal/repository
mkdir -p internal/service
mkdir -p internal/handler
mkdir -p internal/scheduler
mkdir -p web/templates/partials
mkdir -p web/templates/components
mkdir -p data
```

**Step 3: 建立配置檔 `config.yaml`**

```yaml
app:
  port: 8080
  db_path: "./data/market.db"

crawler:
  api_url: "https://data.moa.gov.tw/Service/OpenData/FromM/FarmTransData.aspx"
  crop_name: "茭白筍"
  schedule: "0 10 * * *"
  retry_count: 3
  retry_interval: 30s

analyzer:
  base_market_code: 400
  base_market_name: "台中市"
  crop_codes:
    - "SQ1"
    - "SQ3"
```

**Step 4: 建立領域模型 `internal/domain/model.go`**

```go
// internal/domain/model.go
// 農產品價差雷達系統 — 領域模型
// 定義系統核心資料結構：交易行情記錄、價差計算結果、API 回傳格式

package domain

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

// APIRecord 代表農糧署 API 回傳的原始 JSON 欄位（中文鍵名）
type APIRecord struct {
	TradeDate  string  `json:"交易日期"`
	TypeCode   string  `json:"種類代碼"`
	CropCode   string  `json:"作物代號"`
	CropName   string  `json:"作物名稱"`
	MarketCode int     `json:"市場代號"`
	MarketName string  `json:"市場名稱"`
	UpperPrice float64 `json:"上價"`
	MiddlePrice float64 `json:"中價"`
	LowerPrice float64 `json:"下價"`
	AvgPrice   float64 `json:"平均價"`
	Volume     float64 `json:"交易量"`
}

// ToRecord 將 API 回傳格式轉換為領域模型
func (a *APIRecord) ToRecord() PriceRecord {
	return PriceRecord{
		TradeDate:   a.TradeDate,
		CropCode:    a.CropCode,
		CropName:    a.CropName,
		MarketCode:  a.MarketCode,
		MarketName:  a.MarketName,
		UpperPrice:  a.UpperPrice,
		MiddlePrice: a.MiddlePrice,
		LowerPrice:  a.LowerPrice,
		AvgPrice:    a.AvgPrice,
		Volume:      a.Volume,
	}
}

// SpreadResult 代表一個目標市場的價差計算結果
type SpreadResult struct {
	TargetMarket     string  `json:"target_market"`
	TargetMarketCode int     `json:"target_market_code"`
	TargetAvgPrice   float64 `json:"target_avg_price"`
	BaseAvgPrice     float64 `json:"base_avg_price"`
	AbsoluteSpread   float64 `json:"absolute_spread"`  // 絕對價差 (元/公斤)
	SpreadPercent    float64 `json:"spread_percent"`    // 溢價百分比 (%)
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
```

**Step 5: 建立配置載入 `internal/config/config.go`**

```go
// internal/config/config.go
// 農產品價差雷達系統 — 配置管理
// 負責從 config.yaml 載入應用配置，支援 app、crawler、analyzer 三個區段

package config

import (
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type AppConfig struct {
	Port   int    `yaml:"port"`
	DBPath string `yaml:"db_path"`
}

type CrawlerConfig struct {
	APIURL        string        `yaml:"api_url"`
	CropName      string        `yaml:"crop_name"`
	Schedule      string        `yaml:"schedule"`
	RetryCount    int           `yaml:"retry_count"`
	RetryInterval time.Duration `yaml:"retry_interval"`
}

type AnalyzerConfig struct {
	BaseMarketCode int      `yaml:"base_market_code"`
	BaseMarketName string   `yaml:"base_market_name"`
	CropCodes      []string `yaml:"crop_codes"`
}

type Config struct {
	App      AppConfig      `yaml:"app"`
	Crawler  CrawlerConfig  `yaml:"crawler"`
	Analyzer AnalyzerConfig `yaml:"analyzer"`
}

// Load 從指定路徑載入 YAML 配置檔
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}
```

**Step 6: 建立最小入口 `cmd/server/main.go`**

```go
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
```

**Step 7: 驗證專案可編譯執行**

```bash
cd D:/myCodeProject/farmer_crawler
go build ./...
go run cmd/server/main.go
```

Expected: 輸出配置資訊，無錯誤。

**Step 8: Commit**

```bash
git init
git add go.mod go.sum config.yaml CLAUDE.md
git add cmd/ internal/domain/ internal/config/
git add docs/
git commit -m "feat: 專案骨架 — Go module、領域模型、配置載入"
```

---

### Task 2: Repository 層 — SQLite 初始化與 Upsert

**Files:**
- Create: `internal/repository/sqlite.go`
- Create: `internal/repository/sqlite_test.go`

**Step 1: 寫失敗測試 `internal/repository/sqlite_test.go`**

```go
// internal/repository/sqlite_test.go
// 農產品價差雷達系統 — Repository 層測試
// 測試 SQLite 資料表建立、Upsert、查詢等功能

package repository

import (
	"os"
	"testing"

	"farmer_crawler/internal/domain"
)

func setupTestDB(t *testing.T) *SQLiteRepo {
	t.Helper()
	dbPath := "test_market.db"
	t.Cleanup(func() { os.Remove(dbPath) })

	repo, err := NewSQLiteRepo(dbPath)
	if err != nil {
		t.Fatalf("建立測試資料庫失敗: %v", err)
	}
	t.Cleanup(func() { repo.Close() })
	return repo
}

func TestNewSQLiteRepo_CreatesTable(t *testing.T) {
	repo := setupTestDB(t)

	// 確認 price_records 表存在
	var count int
	err := repo.db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='price_records'").Scan(&count)
	if err != nil {
		t.Fatalf("查詢失敗: %v", err)
	}
	if count != 1 {
		t.Errorf("預期 price_records 表存在，但 count=%d", count)
	}
}

func TestUpsert_InsertNewRecord(t *testing.T) {
	repo := setupTestDB(t)

	record := domain.PriceRecord{
		TradeDate:   "114.03.03",
		CropCode:    "SQ1",
		CropName:    "茭白筍-帶殼",
		MarketCode:  400,
		MarketName:  "台中市",
		UpperPrice:  100.0,
		MiddlePrice: 90.0,
		LowerPrice:  80.0,
		AvgPrice:    90.0,
		Volume:      500.0,
	}

	err := repo.Upsert(record)
	if err != nil {
		t.Fatalf("Upsert 失敗: %v", err)
	}

	records, err := repo.GetByDate("114.03.03", "SQ1")
	if err != nil {
		t.Fatalf("查詢失敗: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("預期 1 筆紀錄，得到 %d 筆", len(records))
	}
	if records[0].AvgPrice != 90.0 {
		t.Errorf("預期均價 90.0，得到 %f", records[0].AvgPrice)
	}
}

func TestUpsert_UpdateExistingRecord(t *testing.T) {
	repo := setupTestDB(t)

	record := domain.PriceRecord{
		TradeDate:  "114.03.03",
		CropCode:   "SQ1",
		CropName:   "茭白筍-帶殼",
		MarketCode: 400,
		MarketName: "台中市",
		AvgPrice:   90.0,
		Volume:     500.0,
	}
	_ = repo.Upsert(record)

	// 更新同一筆
	record.AvgPrice = 95.0
	record.Volume = 600.0
	err := repo.Upsert(record)
	if err != nil {
		t.Fatalf("Upsert 更新失敗: %v", err)
	}

	records, err := repo.GetByDate("114.03.03", "SQ1")
	if err != nil {
		t.Fatalf("查詢失敗: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("預期 1 筆紀錄（Upsert 應覆蓋），得到 %d 筆", len(records))
	}
	if records[0].AvgPrice != 95.0 {
		t.Errorf("預期均價 95.0（已更新），得到 %f", records[0].AvgPrice)
	}
}

func TestBatchUpsert(t *testing.T) {
	repo := setupTestDB(t)

	records := []domain.PriceRecord{
		{TradeDate: "114.03.03", CropCode: "SQ1", CropName: "茭白筍-帶殼", MarketCode: 400, MarketName: "台中市", AvgPrice: 90.0, Volume: 500.0},
		{TradeDate: "114.03.03", CropCode: "SQ1", CropName: "茭白筍-帶殼", MarketCode: 109, MarketName: "台北一", AvgPrice: 120.0, Volume: 1000.0},
		{TradeDate: "114.03.03", CropCode: "SQ1", CropName: "茭白筍-帶殼", MarketCode: 800, MarketName: "高雄市", AvgPrice: 110.0, Volume: 800.0},
	}

	err := repo.BatchUpsert(records)
	if err != nil {
		t.Fatalf("BatchUpsert 失敗: %v", err)
	}

	result, err := repo.GetByDate("114.03.03", "SQ1")
	if err != nil {
		t.Fatalf("查詢失敗: %v", err)
	}
	if len(result) != 3 {
		t.Errorf("預期 3 筆紀錄，得到 %d 筆", len(result))
	}
}

func TestGetLatestAvgPrice(t *testing.T) {
	repo := setupTestDB(t)

	records := []domain.PriceRecord{
		{TradeDate: "114.03.01", CropCode: "SQ1", CropName: "茭白筍-帶殼", MarketCode: 400, MarketName: "台中市", AvgPrice: 85.0, Volume: 400.0},
		{TradeDate: "114.03.03", CropCode: "SQ1", CropName: "茭白筍-帶殼", MarketCode: 400, MarketName: "台中市", AvgPrice: 90.0, Volume: 500.0},
	}
	_ = repo.BatchUpsert(records)

	// 查最新日期的價格
	price, date, err := repo.GetLatestAvgPrice(400, "SQ1")
	if err != nil {
		t.Fatalf("GetLatestAvgPrice 失敗: %v", err)
	}
	if price != 90.0 {
		t.Errorf("預期最新均價 90.0，得到 %f", price)
	}
	if date != "114.03.03" {
		t.Errorf("預期最新日期 114.03.03，得到 %s", date)
	}
}

func TestGetTrendData(t *testing.T) {
	repo := setupTestDB(t)

	records := []domain.PriceRecord{
		{TradeDate: "114.03.01", CropCode: "SQ1", CropName: "茭白筍-帶殼", MarketCode: 400, MarketName: "台中市", AvgPrice: 85.0, Volume: 400.0},
		{TradeDate: "114.03.02", CropCode: "SQ1", CropName: "茭白筍-帶殼", MarketCode: 400, MarketName: "台中市", AvgPrice: 88.0, Volume: 450.0},
		{TradeDate: "114.03.03", CropCode: "SQ1", CropName: "茭白筍-帶殼", MarketCode: 400, MarketName: "台中市", AvgPrice: 90.0, Volume: 500.0},
	}
	_ = repo.BatchUpsert(records)

	points, err := repo.GetTrendData(400, "SQ1", 30)
	if err != nil {
		t.Fatalf("GetTrendData 失敗: %v", err)
	}
	if len(points) != 3 {
		t.Errorf("預期 3 筆趨勢資料，得到 %d 筆", len(points))
	}
}
```

**Step 2: 執行測試確認失敗**

```bash
cd D:/myCodeProject/farmer_crawler
CGO_ENABLED=1 go test ./internal/repository/ -v
```

Expected: 編譯失敗 — `NewSQLiteRepo` 未定義。

**Step 3: 實作 `internal/repository/sqlite.go`**

```go
// internal/repository/sqlite.go
// 農產品價差雷達系統 — Repository 層
// 負責 SQLite 資料庫初始化、price_records 表的 CRUD 與 Upsert 操作
// Upsert 基於 (trade_date, market_code, crop_code) 唯一約束

package repository

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"os"

	_ "github.com/mattn/go-sqlite3"

	"farmer_crawler/internal/domain"
)

type SQLiteRepo struct {
	db *sql.DB
}

// NewSQLiteRepo 建立新的 SQLite Repository，自動建表與索引
func NewSQLiteRepo(dbPath string) (*SQLiteRepo, error) {
	dir := filepath.Dir(dbPath)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("建立資料庫目錄失敗: %w", err)
		}
	}

	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL")
	if err != nil {
		return nil, fmt.Errorf("開啟資料庫失敗: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("資料庫連線失敗: %w", err)
	}

	repo := &SQLiteRepo{db: db}
	if err := repo.migrate(); err != nil {
		return nil, fmt.Errorf("資料表遷移失敗: %w", err)
	}

	return repo, nil
}

// migrate 建立資料表與索引
func (r *SQLiteRepo) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS price_records (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		trade_date    TEXT NOT NULL,
		crop_code     TEXT NOT NULL,
		crop_name     TEXT NOT NULL,
		market_code   INTEGER NOT NULL,
		market_name   TEXT NOT NULL,
		upper_price   REAL,
		middle_price  REAL,
		lower_price   REAL,
		avg_price     REAL,
		volume        REAL,
		created_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(trade_date, market_code, crop_code)
	);
	CREATE INDEX IF NOT EXISTS idx_trade_date ON price_records(trade_date);
	CREATE INDEX IF NOT EXISTS idx_market_code ON price_records(market_code);
	`
	_, err := r.db.Exec(schema)
	return err
}

// Close 關閉資料庫連線
func (r *SQLiteRepo) Close() error {
	return r.db.Close()
}

// Upsert 插入或更新一筆交易行情記錄
func (r *SQLiteRepo) Upsert(record domain.PriceRecord) error {
	query := `
	INSERT INTO price_records (trade_date, crop_code, crop_name, market_code, market_name,
		upper_price, middle_price, lower_price, avg_price, volume)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(trade_date, market_code, crop_code)
	DO UPDATE SET
		crop_name = excluded.crop_name,
		upper_price = excluded.upper_price,
		middle_price = excluded.middle_price,
		lower_price = excluded.lower_price,
		avg_price = excluded.avg_price,
		volume = excluded.volume,
		created_at = CURRENT_TIMESTAMP
	`
	_, err := r.db.Exec(query,
		record.TradeDate, record.CropCode, record.CropName,
		record.MarketCode, record.MarketName,
		record.UpperPrice, record.MiddlePrice, record.LowerPrice,
		record.AvgPrice, record.Volume,
	)
	return err
}

// BatchUpsert 批次插入或更新多筆記錄（使用交易包裹）
func (r *SQLiteRepo) BatchUpsert(records []domain.PriceRecord) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("開始交易失敗: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
	INSERT INTO price_records (trade_date, crop_code, crop_name, market_code, market_name,
		upper_price, middle_price, lower_price, avg_price, volume)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(trade_date, market_code, crop_code)
	DO UPDATE SET
		crop_name = excluded.crop_name,
		upper_price = excluded.upper_price,
		middle_price = excluded.middle_price,
		lower_price = excluded.lower_price,
		avg_price = excluded.avg_price,
		volume = excluded.volume,
		created_at = CURRENT_TIMESTAMP
	`)
	if err != nil {
		return fmt.Errorf("準備語句失敗: %w", err)
	}
	defer stmt.Close()

	for _, rec := range records {
		_, err := stmt.Exec(
			rec.TradeDate, rec.CropCode, rec.CropName,
			rec.MarketCode, rec.MarketName,
			rec.UpperPrice, rec.MiddlePrice, rec.LowerPrice,
			rec.AvgPrice, rec.Volume,
		)
		if err != nil {
			return fmt.Errorf("插入記錄失敗 (market=%d, date=%s): %w", rec.MarketCode, rec.TradeDate, err)
		}
	}

	return tx.Commit()
}

// GetByDate 查詢指定日期與作物代號的所有市場行情
func (r *SQLiteRepo) GetByDate(tradeDate, cropCode string) ([]domain.PriceRecord, error) {
	query := `
	SELECT id, trade_date, crop_code, crop_name, market_code, market_name,
		upper_price, middle_price, lower_price, avg_price, volume
	FROM price_records
	WHERE trade_date = ? AND crop_code = ?
	ORDER BY avg_price DESC
	`
	rows, err := r.db.Query(query, tradeDate, cropCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanRecords(rows)
}

// GetLatestAvgPrice 取得指定市場與作物最新日期的均價
// 回傳：均價, 日期, 錯誤
func (r *SQLiteRepo) GetLatestAvgPrice(marketCode int, cropCode string) (float64, string, error) {
	query := `
	SELECT avg_price, trade_date
	FROM price_records
	WHERE market_code = ? AND crop_code = ? AND avg_price > 0
	ORDER BY trade_date DESC
	LIMIT 1
	`
	var price float64
	var date string
	err := r.db.QueryRow(query, marketCode, cropCode).Scan(&price, &date)
	if err != nil {
		return 0, "", err
	}
	return price, date, nil
}

// GetTrendData 取得指定市場的趨勢資料（最近 N 天）
func (r *SQLiteRepo) GetTrendData(marketCode int, cropCode string, days int) ([]domain.TrendPoint, error) {
	query := `
	SELECT trade_date, avg_price, volume
	FROM price_records
	WHERE market_code = ? AND crop_code = ? AND avg_price > 0
	ORDER BY trade_date DESC
	LIMIT ?
	`
	rows, err := r.db.Query(query, marketCode, cropCode, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var points []domain.TrendPoint
	for rows.Next() {
		var p domain.TrendPoint
		if err := rows.Scan(&p.Date, &p.AvgPrice, &p.Volume); err != nil {
			return nil, err
		}
		points = append(points, p)
	}

	// 反轉為時間正序
	for i, j := 0, len(points)-1; i < j; i, j = i+1, j-1 {
		points[i], points[j] = points[j], points[i]
	}

	return points, nil
}

// GetAllMarkets 取得資料庫中所有不重複的市場清單
func (r *SQLiteRepo) GetAllMarkets() ([]struct{ Code int; Name string }, error) {
	query := `SELECT DISTINCT market_code, market_name FROM price_records ORDER BY market_code`
	rows, err := r.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var markets []struct{ Code int; Name string }
	for rows.Next() {
		var m struct{ Code int; Name string }
		if err := rows.Scan(&m.Code, &m.Name); err != nil {
			return nil, err
		}
		markets = append(markets, m)
	}
	return markets, nil
}

func scanRecords(rows *sql.Rows) ([]domain.PriceRecord, error) {
	var records []domain.PriceRecord
	for rows.Next() {
		var r domain.PriceRecord
		err := rows.Scan(
			&r.ID, &r.TradeDate, &r.CropCode, &r.CropName,
			&r.MarketCode, &r.MarketName,
			&r.UpperPrice, &r.MiddlePrice, &r.LowerPrice,
			&r.AvgPrice, &r.Volume,
		)
		if err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	return records, nil
}
```

**Step 4: 執行測試確認通過**

```bash
cd D:/myCodeProject/farmer_crawler
CGO_ENABLED=1 go test ./internal/repository/ -v
```

Expected: 全部 PASS。

**Step 5: Commit**

```bash
git add internal/repository/
git commit -m "feat: Repository 層 — SQLite 初始化、Upsert、查詢"
```

---

### Task 3: Crawler Service — 農糧署 API 爬取

**Files:**
- Create: `internal/service/crawler.go`
- Create: `internal/service/crawler_test.go`

**Step 1: 寫失敗測試 `internal/service/crawler_test.go`**

```go
// internal/service/crawler_test.go
// 農產品價差雷達系統 — 爬蟲引擎測試
// 測試日期轉換、API URL 建構、JSON 解析等功能
// 注意：不測試真實 HTTP 呼叫，僅測試資料處理邏輯

package service

import (
	"encoding/json"
	"testing"
	"time"
)

func TestToMinguoDate(t *testing.T) {
	tests := []struct {
		input    time.Time
		expected string
	}{
		{time.Date(2026, 3, 3, 0, 0, 0, 0, time.UTC), "115.03.03"},
		{time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC), "114.01.15"},
		{time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC), "114.12.01"},
	}

	for _, tt := range tests {
		result := ToMinguoDate(tt.input)
		if result != tt.expected {
			t.Errorf("ToMinguoDate(%v) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestBuildAPIURL(t *testing.T) {
	c := &CrawlerService{
		apiURL:   "https://data.moa.gov.tw/Service/OpenData/FromM/FarmTransData.aspx",
		cropName: "茭白筍",
	}

	url := c.BuildAPIURL("114.03.01", "114.03.03")
	expected := "https://data.moa.gov.tw/Service/OpenData/FromM/FarmTransData.aspx?Crop=%E8%8C%AD%E7%99%BD%E7%AD%8D&StartDate=114.03.01&EndDate=114.03.03&%24top=500"

	if url != expected {
		t.Errorf("BuildAPIURL 結果不符\n got: %s\nwant: %s", url, expected)
	}
}

func TestParseAPIResponse(t *testing.T) {
	// 模擬農糧署 API 回傳的 JSON
	jsonData := `[
		{
			"交易日期": "114.03.03",
			"種類代碼": "N04",
			"作物代號": "SQ1",
			"作物名稱": "茭白筍-帶殼",
			"市場代號": 400,
			"市場名稱": "台中市",
			"上價": 100.0,
			"中價": 90.0,
			"下價": 80.0,
			"平均價": 90.0,
			"交易量": 500.0
		},
		{
			"交易日期": "114.03.03",
			"種類代碼": "N04",
			"作物代號": "rest",
			"作物名稱": "休市",
			"市場代號": 104,
			"市場名稱": "台北二",
			"上價": 0,
			"中價": 0,
			"下價": 0,
			"平均價": 0,
			"交易量": 0
		}
	]`

	records, err := ParseAPIResponse([]byte(jsonData))
	if err != nil {
		t.Fatalf("ParseAPIResponse 失敗: %v", err)
	}

	// 應過濾掉休市記錄（crop_code = "rest"）
	if len(records) != 1 {
		t.Fatalf("預期 1 筆有效記錄（過濾休市），得到 %d 筆", len(records))
	}

	if records[0].MarketCode != 400 {
		t.Errorf("預期市場代號 400，得到 %d", records[0].MarketCode)
	}
	if records[0].AvgPrice != 90.0 {
		t.Errorf("預期均價 90.0，得到 %f", records[0].AvgPrice)
	}
}

func TestParseAPIResponse_EmptyArray(t *testing.T) {
	records, err := ParseAPIResponse([]byte("[]"))
	if err != nil {
		t.Fatalf("空陣列不應回傳錯誤: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("預期 0 筆紀錄，得到 %d 筆", len(records))
	}
}

// 確認 APIRecord JSON 中文鍵名能正確解析
func TestAPIRecordJSONMapping(t *testing.T) {
	jsonStr := `{"交易日期":"114.03.03","種類代碼":"N04","作物代號":"SQ1","作物名稱":"茭白筍-帶殼","市場代號":400,"市場名稱":"台中市","上價":100,"中價":90,"下價":80,"平均價":90,"交易量":500}`

	var rec apiRecordJSON
	if err := json.Unmarshal([]byte(jsonStr), &rec); err != nil {
		t.Fatalf("JSON 解析失敗: %v", err)
	}
	if rec.CropCode != "SQ1" {
		t.Errorf("作物代號解析錯誤: got %q", rec.CropCode)
	}
}
```

**Step 2: 執行測試確認失敗**

```bash
CGO_ENABLED=1 go test ./internal/service/ -v -run TestToMinguo
```

Expected: 編譯失敗 — `ToMinguoDate` 未定義。

**Step 3: 實作 `internal/service/crawler.go`**

```go
// internal/service/crawler.go
// 農產品價差雷達系統 — 爬蟲引擎
// 負責呼叫農糧署開放資料 API，取得茭白筍交易行情，並將結果存入 SQLite
// API: https://data.moa.gov.tw/Service/OpenData/FromM/FarmTransData.aspx
// 日期格式：民國年.月.日（例：114.03.03），轉換公式：民國年 = 西元年 - 1911

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

// apiRecordJSON 對應農糧署 API 回傳的中文鍵名 JSON
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

type CrawlerService struct {
	apiURL        string
	cropName      string
	retryCount    int
	retryInterval time.Duration
	repo          *repository.SQLiteRepo
	client        *http.Client
}

// NewCrawlerService 建立爬蟲服務實例
func NewCrawlerService(apiURL, cropName string, retryCount int, retryInterval time.Duration, repo *repository.SQLiteRepo) *CrawlerService {
	return &CrawlerService{
		apiURL:        apiURL,
		cropName:      cropName,
		retryCount:    retryCount,
		retryInterval: retryInterval,
		repo:          repo,
		client:        &http.Client{Timeout: 30 * time.Second},
	}
}

// ToMinguoDate 將西元時間轉換為民國日期字串 "YYY.MM.DD"
func ToMinguoDate(t time.Time) string {
	year := t.Year() - 1911
	return fmt.Sprintf("%d.%02d.%02d", year, t.Month(), t.Day())
}

// BuildAPIURL 建構農糧署 API 查詢 URL
func (c *CrawlerService) BuildAPIURL(startDate, endDate string) string {
	params := url.Values{}
	params.Set("Crop", c.cropName)
	params.Set("StartDate", startDate)
	params.Set("EndDate", endDate)
	params.Set("$top", "500")
	return c.apiURL + "?" + params.Encode()
}

// ParseAPIResponse 解析 API 回傳的 JSON，過濾掉休市記錄
func ParseAPIResponse(data []byte) ([]domain.PriceRecord, error) {
	var apiRecords []apiRecordJSON
	if err := json.Unmarshal(data, &apiRecords); err != nil {
		return nil, fmt.Errorf("JSON 解析失敗: %w", err)
	}

	var records []domain.PriceRecord
	for _, ar := range apiRecords {
		// 過濾休市記錄
		if ar.CropCode == "rest" || ar.CropCode == "" {
			continue
		}
		records = append(records, domain.PriceRecord{
			TradeDate:   ar.TradeDate,
			CropCode:    ar.CropCode,
			CropName:    ar.CropName,
			MarketCode:  ar.MarketCode,
			MarketName:  ar.MarketName,
			UpperPrice:  ar.UpperPrice,
			MiddlePrice: ar.MiddlePrice,
			LowerPrice:  ar.LowerPrice,
			AvgPrice:    ar.AvgPrice,
			Volume:      ar.Volume,
		})
	}
	return records, nil
}

// FetchAndStore 爬取指定日期區間的資料並存入資料庫
func (c *CrawlerService) FetchAndStore(startDate, endDate string) (int, error) {
	apiURL := c.BuildAPIURL(startDate, endDate)
	log.Printf("[爬蟲] 開始爬取: %s ~ %s", startDate, endDate)

	var body []byte
	var lastErr error

	for attempt := 0; attempt <= c.retryCount; attempt++ {
		if attempt > 0 {
			log.Printf("[爬蟲] 第 %d 次重試...", attempt)
			time.Sleep(c.retryInterval)
		}

		resp, err := c.client.Get(apiURL)
		if err != nil {
			lastErr = fmt.Errorf("HTTP 請求失敗: %w", err)
			continue
		}

		body, err = io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("讀取回應失敗: %w", err)
			continue
		}

		lastErr = nil
		break
	}

	if lastErr != nil {
		return 0, lastErr
	}

	records, err := ParseAPIResponse(body)
	if err != nil {
		return 0, err
	}

	if len(records) == 0 {
		log.Printf("[爬蟲] 警告: %s ~ %s 無有效資料", startDate, endDate)
		return 0, nil
	}

	if err := c.repo.BatchUpsert(records); err != nil {
		return 0, fmt.Errorf("寫入資料庫失敗: %w", err)
	}

	log.Printf("[爬蟲] 成功: 寫入 %d 筆記錄", len(records))
	return len(records), nil
}

// CrawlToday 爬取今日資料
func (c *CrawlerService) CrawlToday() (int, error) {
	today := ToMinguoDate(time.Now())
	return c.FetchAndStore(today, today)
}

// CrawlRange 爬取指定日期區間（民國日期字串）
func (c *CrawlerService) CrawlRange(from, to string) (int, error) {
	return c.FetchAndStore(from, to)
}
```

**Step 4: 執行測試確認通過**

```bash
CGO_ENABLED=1 go test ./internal/service/ -v
```

Expected: 全部 PASS。

**Step 5: Commit**

```bash
git add internal/service/crawler.go internal/service/crawler_test.go
git commit -m "feat: 爬蟲引擎 — API 呼叫、JSON 解析、日期轉換"
```

---

### Task 4: Analyzer Service — 價差計算引擎

**Files:**
- Create: `internal/service/analyzer.go`
- Create: `internal/service/analyzer_test.go`

**Step 1: 寫失敗測試 `internal/service/analyzer_test.go`**

```go
// internal/service/analyzer_test.go
// 農產品價差雷達系統 — 價差計算引擎測試
// 測試價差計算邏輯、排序、邊界情況處理

package service

import (
	"os"
	"testing"

	"farmer_crawler/internal/domain"
	"farmer_crawler/internal/repository"
)

func setupAnalyzerTest(t *testing.T) (*AnalyzerService, *repository.SQLiteRepo) {
	t.Helper()
	dbPath := "test_analyzer.db"
	t.Cleanup(func() { os.Remove(dbPath) })

	repo, err := repository.NewSQLiteRepo(dbPath)
	if err != nil {
		t.Fatalf("建立測試資料庫失敗: %v", err)
	}
	t.Cleanup(func() { repo.Close() })

	analyzer := NewAnalyzerService(repo, 400, "台中市")
	return analyzer, repo
}

func TestCalculateSpread_Basic(t *testing.T) {
	analyzer, repo := setupAnalyzerTest(t)

	// 寫入測試資料
	records := []domain.PriceRecord{
		{TradeDate: "114.03.03", CropCode: "SQ1", CropName: "茭白筍-帶殼", MarketCode: 400, MarketName: "台中市", AvgPrice: 85.0, Volume: 500.0},
		{TradeDate: "114.03.03", CropCode: "SQ1", CropName: "茭白筍-帶殼", MarketCode: 109, MarketName: "台北一", AvgPrice: 120.0, Volume: 1000.0},
		{TradeDate: "114.03.03", CropCode: "SQ1", CropName: "茭白筍-帶殼", MarketCode: 800, MarketName: "高雄市", AvgPrice: 110.0, Volume: 800.0},
	}
	_ = repo.BatchUpsert(records)

	results, err := analyzer.CalculateSpread("114.03.03", "SQ1")
	if err != nil {
		t.Fatalf("CalculateSpread 失敗: %v", err)
	}

	// 應有 2 筆（排除基準市場自身）
	if len(results) != 2 {
		t.Fatalf("預期 2 筆價差結果，得到 %d 筆", len(results))
	}

	// 台北一應排第一（溢價最高）
	if results[0].TargetMarket != "台北一" {
		t.Errorf("預期第一名為台北一，得到 %s", results[0].TargetMarket)
	}

	// 驗算：(120 - 85) / 85 * 100 ≈ 41.18
	expectedPercent := (120.0 - 85.0) / 85.0 * 100
	if abs(results[0].SpreadPercent-expectedPercent) > 0.01 {
		t.Errorf("台北一溢價百分比預期 %.2f%%，得到 %.2f%%", expectedPercent, results[0].SpreadPercent)
	}

	// 絕對價差
	if results[0].AbsoluteSpread != 35.0 {
		t.Errorf("台北一絕對價差預期 35.0，得到 %f", results[0].AbsoluteSpread)
	}
}

func TestCalculateSpread_BaseMarketMissing(t *testing.T) {
	analyzer, repo := setupAnalyzerTest(t)

	// 基準市場 114.03.03 當日無資料，但 114.03.01 有
	records := []domain.PriceRecord{
		{TradeDate: "114.03.01", CropCode: "SQ1", CropName: "茭白筍-帶殼", MarketCode: 400, MarketName: "台中市", AvgPrice: 85.0, Volume: 500.0},
		{TradeDate: "114.03.03", CropCode: "SQ1", CropName: "茭白筍-帶殼", MarketCode: 109, MarketName: "台北一", AvgPrice: 120.0, Volume: 1000.0},
	}
	_ = repo.BatchUpsert(records)

	results, err := analyzer.CalculateSpread("114.03.03", "SQ1")
	if err != nil {
		t.Fatalf("基準市場休市時不應回傳錯誤: %v", err)
	}

	// 應 fallback 到歷史價格
	if len(results) != 1 {
		t.Fatalf("預期 1 筆價差結果，得到 %d 筆", len(results))
	}
	if results[0].BaseAvgPrice != 85.0 {
		t.Errorf("應使用歷史基準價 85.0，得到 %f", results[0].BaseAvgPrice)
	}
}

func TestCalculateSpread_NoData(t *testing.T) {
	analyzer, _ := setupAnalyzerTest(t)

	results, err := analyzer.CalculateSpread("114.03.03", "SQ1")
	if err != nil {
		t.Fatalf("無資料時不應回傳錯誤: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("無資料時預期空結果，得到 %d 筆", len(results))
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
```

**Step 2: 執行測試確認失敗**

```bash
CGO_ENABLED=1 go test ./internal/service/ -v -run TestCalculateSpread
```

Expected: 編譯失敗 — `NewAnalyzerService` 未定義。

**Step 3: 實作 `internal/service/analyzer.go`**

```go
// internal/service/analyzer.go
// 農產品價差雷達系統 — 價差計算引擎
// 以可配置的基準市場為成本參考，計算所有目標市場的絕對價差與溢價百分比
// 公式：溢價% = (目標市場均價 - 基準市場均價) / 基準市場均價 × 100

package service

import (
	"log"
	"sort"

	"farmer_crawler/internal/domain"
	"farmer_crawler/internal/repository"
)

type AnalyzerService struct {
	repo           *repository.SQLiteRepo
	baseMarketCode int
	baseMarketName string
}

// NewAnalyzerService 建立價差計算引擎
func NewAnalyzerService(repo *repository.SQLiteRepo, baseMarketCode int, baseMarketName string) *AnalyzerService {
	return &AnalyzerService{
		repo:           repo,
		baseMarketCode: baseMarketCode,
		baseMarketName: baseMarketName,
	}
}

// CalculateSpread 計算指定日期與作物的跨市場價差
// 回傳按溢價百分比降序排列的結果
func (a *AnalyzerService) CalculateSpread(date, cropCode string) ([]domain.SpreadResult, error) {
	// 1. 取得基準市場當日均價
	allRecords, err := a.repo.GetByDate(date, cropCode)
	if err != nil {
		return nil, err
	}

	var basePrice float64
	var baseFound bool

	for _, r := range allRecords {
		if r.MarketCode == a.baseMarketCode && r.AvgPrice > 0 {
			basePrice = r.AvgPrice
			baseFound = true
			break
		}
	}

	// 2. 基準市場當日無資料 → fallback 到歷史最新
	if !baseFound {
		historicalPrice, histDate, err := a.repo.GetLatestAvgPrice(a.baseMarketCode, cropCode)
		if err != nil {
			log.Printf("[分析] 基準市場 %s 無任何歷史資料", a.baseMarketName)
			return nil, nil
		}
		basePrice = historicalPrice
		log.Printf("[分析] 基準市場 %s 當日休市，使用 %s 的歷史價格 %.1f", a.baseMarketName, histDate, basePrice)
	}

	if basePrice <= 0 {
		return nil, nil
	}

	// 3. 計算各市場價差
	var results []domain.SpreadResult
	for _, r := range allRecords {
		if r.MarketCode == a.baseMarketCode {
			continue
		}
		if r.AvgPrice <= 0 {
			continue
		}

		spread := domain.SpreadResult{
			TargetMarket:     r.MarketName,
			TargetMarketCode: r.MarketCode,
			TargetAvgPrice:   r.AvgPrice,
			BaseAvgPrice:     basePrice,
			AbsoluteSpread:   r.AvgPrice - basePrice,
			SpreadPercent:    (r.AvgPrice - basePrice) / basePrice * 100,
			TargetVolume:     r.Volume,
		}
		results = append(results, spread)
	}

	// 4. 按溢價百分比降序排列
	sort.Slice(results, func(i, j int) bool {
		return results[i].SpreadPercent > results[j].SpreadPercent
	})

	return results, nil
}
```

**Step 4: 執行測試確認通過**

```bash
CGO_ENABLED=1 go test ./internal/service/ -v
```

Expected: 全部 PASS。

**Step 5: Commit**

```bash
git add internal/service/analyzer.go internal/service/analyzer_test.go
git commit -m "feat: 價差計算引擎 — 跨市場價差與溢價百分比"
```

---

### Task 5: Scheduler — 排程管理

**Files:**
- Create: `internal/scheduler/scheduler.go`

**Step 1: 實作 `internal/scheduler/scheduler.go`**

```go
// internal/scheduler/scheduler.go
// 農產品價差雷達系統 — 排程管理
// 使用 gocron/v2 設定每日自動爬取任務
// 預設 cron 表達式：0 10 * * *（每日 10:00）

package scheduler

import (
	"log"
	"time"

	"github.com/go-co-op/gocron/v2"

	"farmer_crawler/internal/service"
)

type Scheduler struct {
	s       gocron.Scheduler
	crawler *service.CrawlerService
}

// NewScheduler 建立排程器
func NewScheduler(crawler *service.CrawlerService) (*Scheduler, error) {
	s, err := gocron.NewScheduler(gocron.WithLocation(time.FixedZone("Asia/Taipei", 8*60*60)))
	if err != nil {
		return nil, err
	}
	return &Scheduler{s: s, crawler: crawler}, nil
}

// Start 啟動排程器，設定每日爬取任務
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

// Stop 停止排程器
func (sc *Scheduler) Stop() error {
	return sc.s.Shutdown()
}
```

**Step 2: 確認編譯通過**

```bash
CGO_ENABLED=1 go build ./internal/scheduler/
```

Expected: 無錯誤。

**Step 3: Commit**

```bash
git add internal/scheduler/
git commit -m "feat: 排程管理 — gocron 每日自動爬取"
```

---

### Task 6: Handler 層 — Gin HTTP + HTMX Endpoints

**Files:**
- Create: `internal/handler/dashboard.go`

**Step 1: 實作 `internal/handler/dashboard.go`**

```go
// internal/handler/dashboard.go
// 農產品價差雷達系統 — HTTP Handler 層
// 提供 Gin 路由與 HTMX 端點：主儀表板、市場卡片、價差排名表、趨勢圖 JSON
// HTMX 端點回傳 HTML 片段，供前端局部替換

package handler

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"farmer_crawler/internal/config"
	"farmer_crawler/internal/repository"
	"farmer_crawler/internal/service"
)

type DashboardHandler struct {
	repo     *repository.SQLiteRepo
	analyzer *service.AnalyzerService
	crawler  *service.CrawlerService
	cfg      *config.Config
	tmpl     *template.Template
}

// NewDashboardHandler 建立 Dashboard Handler
func NewDashboardHandler(repo *repository.SQLiteRepo, analyzer *service.AnalyzerService, crawler *service.CrawlerService, cfg *config.Config) *DashboardHandler {
	funcMap := template.FuncMap{
		"sprintf": fmt.Sprintf,
		"add":     func(a, b int) int { return a + b },
	}

	tmpl := template.Must(template.New("").Funcs(funcMap).ParseGlob("web/templates/*.html"))
	template.Must(tmpl.ParseGlob("web/templates/partials/*.html"))
	template.Must(tmpl.ParseGlob("web/templates/components/*.html"))

	return &DashboardHandler{
		repo:     repo,
		analyzer: analyzer,
		crawler:  crawler,
		cfg:      cfg,
		tmpl:     tmpl,
	}
}

// RegisterRoutes 註冊所有路由
func (h *DashboardHandler) RegisterRoutes(r *gin.Engine) {
	r.GET("/", h.Dashboard)
	r.GET("/api/markets", h.MarketCards)
	r.GET("/api/spread", h.SpreadTable)
	r.GET("/api/trend", h.TrendData)
	r.POST("/api/crawl", h.TriggerCrawl)
}

// Dashboard 主儀表板（完整頁面）
func (h *DashboardHandler) Dashboard(c *gin.Context) {
	cropCode := c.DefaultQuery("crop", "SQ1")
	today := service.ToMinguoDate(timeNow())

	spreads, _ := h.analyzer.CalculateSpread(today, cropCode)
	records, _ := h.repo.GetByDate(today, cropCode)

	data := gin.H{
		"Title":          "茭白筍價差雷達",
		"Date":           today,
		"CropCode":       cropCode,
		"BaseMarketName": h.cfg.Analyzer.BaseMarketName,
		"BaseMarketCode": h.cfg.Analyzer.BaseMarketCode,
		"Spreads":        spreads,
		"Records":        records,
		"CropCodes":      h.cfg.Analyzer.CropCodes,
	}

	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := h.tmpl.ExecuteTemplate(c.Writer, "dashboard.html", data); err != nil {
		log.Printf("[Handler] 渲染失敗: %v", err)
		c.String(http.StatusInternalServerError, "渲染失敗")
	}
}

// MarketCards 市場卡片（HTMX partial）
func (h *DashboardHandler) MarketCards(c *gin.Context) {
	cropCode := c.DefaultQuery("crop", "SQ1")
	today := service.ToMinguoDate(timeNow())

	records, _ := h.repo.GetByDate(today, cropCode)
	spreads, _ := h.analyzer.CalculateSpread(today, cropCode)

	data := gin.H{
		"Records":        records,
		"Spreads":        spreads,
		"BaseMarketCode": h.cfg.Analyzer.BaseMarketCode,
	}

	c.Header("Content-Type", "text/html; charset=utf-8")
	h.tmpl.ExecuteTemplate(c.Writer, "market_cards.html", data)
}

// SpreadTable 價差排名表（HTMX partial）
func (h *DashboardHandler) SpreadTable(c *gin.Context) {
	cropCode := c.DefaultQuery("crop", "SQ1")
	date := c.DefaultQuery("date", service.ToMinguoDate(timeNow()))

	spreads, _ := h.analyzer.CalculateSpread(date, cropCode)

	data := gin.H{
		"Spreads":        spreads,
		"Date":           date,
		"CropCode":       cropCode,
		"BaseMarketName": h.cfg.Analyzer.BaseMarketName,
	}

	c.Header("Content-Type", "text/html; charset=utf-8")
	h.tmpl.ExecuteTemplate(c.Writer, "spread_table.html", data)
}

// TrendData 趨勢圖 JSON
func (h *DashboardHandler) TrendData(c *gin.Context) {
	cropCode := c.DefaultQuery("crop", "SQ1")
	daysStr := c.DefaultQuery("days", "30")
	marketStr := c.DefaultQuery("market", fmt.Sprintf("%d", h.cfg.Analyzer.BaseMarketCode))

	days, _ := strconv.Atoi(daysStr)
	if days <= 0 {
		days = 30
	}

	marketCodes := parseMarketCodes(marketStr)
	var trends []gin.H

	for _, code := range marketCodes {
		points, err := h.repo.GetTrendData(code, cropCode, days)
		if err != nil {
			continue
		}
		markets, _ := h.repo.GetAllMarkets()
		name := ""
		for _, m := range markets {
			if m.Code == code {
				name = m.Name
				break
			}
		}
		trends = append(trends, gin.H{
			"market_name": name,
			"market_code": code,
			"points":      points,
		})
	}

	c.JSON(http.StatusOK, trends)
}

// TriggerCrawl 手動觸發爬取（HTMX）
func (h *DashboardHandler) TriggerCrawl(c *gin.Context) {
	count, err := h.crawler.CrawlToday()
	if err != nil {
		c.String(http.StatusInternalServerError,
			`<div class="text-red-600 p-2">爬取失敗: %s</div>`, err.Error())
		return
	}
	c.String(http.StatusOK,
		`<div class="text-green-600 p-2">爬取成功！共 %d 筆記錄</div>`, count)
}

func parseMarketCodes(s string) []int {
	parts := strings.Split(s, ",")
	var codes []int
	for _, p := range parts {
		code, err := strconv.Atoi(strings.TrimSpace(p))
		if err == nil {
			codes = append(codes, code)
		}
	}
	return codes
}

// timeNow 方便測試時 mock（目前直接使用 time.Now）
var timeNow = func() interface{ Year() int; Month() interface{ String() string }; Day() int } {
	return nil
}

func init() {
	// 使用正常的 time.Now
	import "time"
}
```

等等，上面的 `timeNow` 處理有問題。讓我修正：

```go
// internal/handler/dashboard.go
// 農產品價差雷達系統 — HTTP Handler 層
// 提供 Gin 路由與 HTMX 端點：主儀表板、市場卡片、價差排名表、趨勢圖 JSON
// HTMX 端點回傳 HTML 片段，供前端局部替換

package handler

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"farmer_crawler/internal/config"
	"farmer_crawler/internal/repository"
	"farmer_crawler/internal/service"
)

type DashboardHandler struct {
	repo     *repository.SQLiteRepo
	analyzer *service.AnalyzerService
	crawler  *service.CrawlerService
	cfg      *config.Config
	tmpl     *template.Template
}

// NewDashboardHandler 建立 Dashboard Handler
func NewDashboardHandler(repo *repository.SQLiteRepo, analyzer *service.AnalyzerService, crawler *service.CrawlerService, cfg *config.Config) *DashboardHandler {
	funcMap := template.FuncMap{
		"sprintf": fmt.Sprintf,
		"add":     func(a, b int) int { return a + b },
	}

	tmpl := template.Must(template.New("").Funcs(funcMap).ParseGlob("web/templates/*.html"))
	template.Must(tmpl.ParseGlob("web/templates/partials/*.html"))
	template.Must(tmpl.ParseGlob("web/templates/components/*.html"))

	return &DashboardHandler{
		repo:     repo,
		analyzer: analyzer,
		crawler:  crawler,
		cfg:      cfg,
		tmpl:     tmpl,
	}
}

// RegisterRoutes 註冊所有路由
func (h *DashboardHandler) RegisterRoutes(r *gin.Engine) {
	r.GET("/", h.Dashboard)
	r.GET("/api/markets", h.MarketCards)
	r.GET("/api/spread", h.SpreadTable)
	r.GET("/api/trend", h.TrendData)
	r.POST("/api/crawl", h.TriggerCrawl)
}

// Dashboard 主儀表板（完整頁面）
func (h *DashboardHandler) Dashboard(c *gin.Context) {
	cropCode := c.DefaultQuery("crop", "SQ1")
	today := service.ToMinguoDate(time.Now())

	spreads, _ := h.analyzer.CalculateSpread(today, cropCode)
	records, _ := h.repo.GetByDate(today, cropCode)

	data := gin.H{
		"Title":          "茭白筍價差雷達",
		"Date":           today,
		"CropCode":       cropCode,
		"BaseMarketName": h.cfg.Analyzer.BaseMarketName,
		"BaseMarketCode": h.cfg.Analyzer.BaseMarketCode,
		"Spreads":        spreads,
		"Records":        records,
		"CropCodes":      h.cfg.Analyzer.CropCodes,
	}

	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := h.tmpl.ExecuteTemplate(c.Writer, "dashboard.html", data); err != nil {
		log.Printf("[Handler] 渲染失敗: %v", err)
		c.String(http.StatusInternalServerError, "渲染失敗")
	}
}

// MarketCards 市場卡片（HTMX partial）
func (h *DashboardHandler) MarketCards(c *gin.Context) {
	cropCode := c.DefaultQuery("crop", "SQ1")
	today := service.ToMinguoDate(time.Now())

	records, _ := h.repo.GetByDate(today, cropCode)
	spreads, _ := h.analyzer.CalculateSpread(today, cropCode)

	data := gin.H{
		"Records":        records,
		"Spreads":        spreads,
		"BaseMarketCode": h.cfg.Analyzer.BaseMarketCode,
	}

	c.Header("Content-Type", "text/html; charset=utf-8")
	h.tmpl.ExecuteTemplate(c.Writer, "market_cards.html", data)
}

// SpreadTable 價差排名表（HTMX partial）
func (h *DashboardHandler) SpreadTable(c *gin.Context) {
	cropCode := c.DefaultQuery("crop", "SQ1")
	date := c.DefaultQuery("date", service.ToMinguoDate(time.Now()))

	spreads, _ := h.analyzer.CalculateSpread(date, cropCode)

	data := gin.H{
		"Spreads":        spreads,
		"Date":           date,
		"CropCode":       cropCode,
		"BaseMarketName": h.cfg.Analyzer.BaseMarketName,
	}

	c.Header("Content-Type", "text/html; charset=utf-8")
	h.tmpl.ExecuteTemplate(c.Writer, "spread_table.html", data)
}

// TrendData 趨勢圖 JSON
func (h *DashboardHandler) TrendData(c *gin.Context) {
	cropCode := c.DefaultQuery("crop", "SQ1")
	daysStr := c.DefaultQuery("days", "30")
	marketStr := c.DefaultQuery("market", fmt.Sprintf("%d", h.cfg.Analyzer.BaseMarketCode))

	days, _ := strconv.Atoi(daysStr)
	if days <= 0 {
		days = 30
	}

	marketCodes := parseMarketCodes(marketStr)
	var trends []gin.H

	for _, code := range marketCodes {
		points, err := h.repo.GetTrendData(code, cropCode, days)
		if err != nil {
			continue
		}
		markets, _ := h.repo.GetAllMarkets()
		name := ""
		for _, m := range markets {
			if m.Code == code {
				name = m.Name
				break
			}
		}
		trends = append(trends, gin.H{
			"market_name": name,
			"market_code": code,
			"points":      points,
		})
	}

	c.JSON(http.StatusOK, trends)
}

// TriggerCrawl 手動觸發爬取（HTMX）
func (h *DashboardHandler) TriggerCrawl(c *gin.Context) {
	count, err := h.crawler.CrawlToday()
	if err != nil {
		c.String(http.StatusInternalServerError,
			`<div class="text-red-600 p-2">爬取失敗: %s</div>`, err.Error())
		return
	}
	c.String(http.StatusOK,
		`<div class="text-green-600 p-2">爬取成功！共 %d 筆記錄</div>`, count)
}

func parseMarketCodes(s string) []int {
	parts := strings.Split(s, ",")
	var codes []int
	for _, p := range parts {
		code, err := strconv.Atoi(strings.TrimSpace(p))
		if err == nil {
			codes = append(codes, code)
		}
	}
	return codes
}
```

**Step 2: 確認編譯通過**

```bash
CGO_ENABLED=1 go build ./internal/handler/
```

Expected: 無錯誤。

**Step 3: Commit**

```bash
git add internal/handler/
git commit -m "feat: HTTP Handler 層 — 儀表板、HTMX 端點、趨勢圖 JSON"
```

---

### Task 7: HTML 模板 — HTMX + ECharts 儀表板

**Files:**
- Create: `web/templates/layout.html`
- Create: `web/templates/dashboard.html`
- Create: `web/templates/partials/market_cards.html`
- Create: `web/templates/partials/spread_table.html`
- Create: `web/templates/partials/trend_chart.html`
- Create: `web/templates/components/filter_bar.html`

**Step 1: 建立基底模板 `web/templates/layout.html`**

```html
{{/* web/templates/layout.html */}}
{{/* 農產品價差雷達系統 — 基底模板 */}}
{{/* 載入 HTMX、ECharts、TailwindCSS CDN */}}
{{define "layout"}}
<!DOCTYPE html>
<html lang="zh-TW">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>{{.Title}}</title>
    <script src="https://cdn.tailwindcss.com"></script>
    <script src="https://unpkg.com/htmx.org@2.0.4"></script>
    <script src="https://cdn.jsdelivr.net/npm/echarts@5.5.1/dist/echarts.min.js"></script>
</head>
<body class="bg-gray-50 min-h-screen">
    <nav class="bg-green-700 text-white p-4 shadow-lg">
        <div class="container mx-auto flex justify-between items-center">
            <h1 class="text-xl font-bold">茭白筍價差雷達</h1>
            <span class="text-green-200 text-sm">{{.Date}}</span>
        </div>
    </nav>
    <main class="container mx-auto p-4">
        {{template "content" .}}
    </main>
</body>
</html>
{{end}}
```

**Step 2: 建立主儀表板 `web/templates/dashboard.html`**

```html
{{/* web/templates/dashboard.html */}}
{{/* 農產品價差雷達系統 — 主儀表板頁面 */}}
{{/* 組合：篩選標籤 + 市場卡片 + 價差排名 + 趨勢圖 + 手動爬取按鈕 */}}
{{template "layout" .}}
{{define "content"}}
<div class="space-y-6">

    {{/* 篩選列 */}}
    {{template "filter_bar" .}}

    {{/* 手動爬取按鈕 */}}
    <div class="flex justify-end">
        <button hx-post="/api/crawl"
                hx-target="#crawl-status"
                hx-swap="innerHTML"
                class="bg-green-600 hover:bg-green-700 text-white px-4 py-2 rounded-lg text-sm transition">
            手動爬取今日資料
        </button>
        <div id="crawl-status" class="ml-3 flex items-center"></div>
    </div>

    {{/* 市場卡片區 */}}
    <section>
        <h2 class="text-lg font-semibold text-gray-700 mb-3">各市場今日行情</h2>
        <div id="market-cards">
            {{template "market_cards" .}}
        </div>
    </section>

    {{/* 價差排名表 */}}
    <section>
        <h2 class="text-lg font-semibold text-gray-700 mb-3">
            價差排名（基準：{{.BaseMarketName}}）
        </h2>
        <div id="spread-table">
            {{template "spread_table" .}}
        </div>
    </section>

    {{/* 趨勢圖 */}}
    <section>
        <h2 class="text-lg font-semibold text-gray-700 mb-3">近30日趨勢</h2>
        {{template "trend_chart" .}}
    </section>

</div>
{{end}}
```

**Step 3: 建立市場卡片 `web/templates/partials/market_cards.html`**

```html
{{/* web/templates/partials/market_cards.html */}}
{{/* 農產品價差雷達系統 — 市場卡片區 */}}
{{/* 以卡片形式呈現各市場的均價、交易量，基準市場特別標示 */}}
{{define "market_cards"}}
<div class="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-4 gap-4">
    {{range .Records}}
    <div class="bg-white rounded-xl shadow p-4 border-l-4
        {{if eq .MarketCode $.BaseMarketCode}}border-blue-500{{else}}border-green-500{{end}}">
        <div class="flex justify-between items-start">
            <h3 class="font-semibold text-gray-800">{{.MarketName}}</h3>
            {{if eq .MarketCode $.BaseMarketCode}}
            <span class="text-xs bg-blue-100 text-blue-700 px-2 py-0.5 rounded">基準</span>
            {{end}}
        </div>
        <div class="mt-2">
            <span class="text-2xl font-bold text-gray-900">{{sprintf "%.1f" .AvgPrice}}</span>
            <span class="text-sm text-gray-500 ml-1">元/公斤</span>
        </div>
        <div class="mt-1 text-sm text-gray-500">
            交易量：{{sprintf "%.0f" .Volume}} 公斤
        </div>
        <div class="mt-1 text-xs text-gray-400">
            {{sprintf "%.1f" .LowerPrice}} ~ {{sprintf "%.1f" .UpperPrice}}
        </div>
    </div>
    {{end}}
    {{if not .Records}}
    <div class="col-span-full text-center text-gray-400 py-8">
        今日尚無交易資料，請先爬取資料
    </div>
    {{end}}
</div>
{{end}}
```

**Step 4: 建立價差排名表 `web/templates/partials/spread_table.html`**

```html
{{/* web/templates/partials/spread_table.html */}}
{{/* 農產品價差雷達系統 — 價差排名表格 */}}
{{/* 按溢價百分比降序排列，前三名特別標示 */}}
{{define "spread_table"}}
<div class="overflow-x-auto">
    <table class="w-full bg-white rounded-xl shadow">
        <thead class="bg-gray-50">
            <tr>
                <th class="px-4 py-3 text-left text-sm font-medium text-gray-600">排名</th>
                <th class="px-4 py-3 text-left text-sm font-medium text-gray-600">市場</th>
                <th class="px-4 py-3 text-right text-sm font-medium text-gray-600">均價</th>
                <th class="px-4 py-3 text-right text-sm font-medium text-gray-600">價差</th>
                <th class="px-4 py-3 text-right text-sm font-medium text-gray-600">溢價%</th>
                <th class="px-4 py-3 text-right text-sm font-medium text-gray-600">交易量</th>
            </tr>
        </thead>
        <tbody class="divide-y divide-gray-100">
            {{range $i, $s := .Spreads}}
            <tr class="hover:bg-gray-50 transition">
                <td class="px-4 py-3 text-sm">
                    {{if eq $i 0}}🥇{{else if eq $i 1}}🥈{{else if eq $i 2}}🥉{{else}}{{add $i 1}}{{end}}
                </td>
                <td class="px-4 py-3 text-sm font-medium text-gray-800">{{$s.TargetMarket}}</td>
                <td class="px-4 py-3 text-sm text-right">{{sprintf "%.1f" $s.TargetAvgPrice}}</td>
                <td class="px-4 py-3 text-sm text-right
                    {{if gt $s.AbsoluteSpread 0.0}}text-green-600{{else}}text-red-600{{end}}">
                    {{if gt $s.AbsoluteSpread 0.0}}+{{end}}{{sprintf "%.1f" $s.AbsoluteSpread}}
                </td>
                <td class="px-4 py-3 text-sm text-right font-semibold
                    {{if gt $s.SpreadPercent 0.0}}text-green-600{{else}}text-red-600{{end}}">
                    {{if gt $s.SpreadPercent 0.0}}+{{end}}{{sprintf "%.1f" $s.SpreadPercent}}%
                </td>
                <td class="px-4 py-3 text-sm text-right text-gray-500">{{sprintf "%.0f" $s.TargetVolume}}</td>
            </tr>
            {{end}}
            {{if not .Spreads}}
            <tr>
                <td colspan="6" class="px-4 py-8 text-center text-gray-400">
                    尚無價差資料
                </td>
            </tr>
            {{end}}
        </tbody>
    </table>
    {{if .Spreads}}
    <p class="text-xs text-gray-400 mt-2">
        基準市場：{{.BaseMarketName}}（均價 {{sprintf "%.1f" (index .Spreads 0).BaseAvgPrice}} 元/公斤）
    </p>
    {{end}}
</div>
{{end}}
```

**Step 5: 建立趨勢圖容器 `web/templates/partials/trend_chart.html`**

```html
{{/* web/templates/partials/trend_chart.html */}}
{{/* 農產品價差雷達系統 — 趨勢圖容器 */}}
{{/* 後端提供 JSON API，前端用 Vanilla JS 初始化 ECharts 雙 Y 軸圖表 */}}
{{define "trend_chart"}}
<div class="bg-white rounded-xl shadow p-4">
    <div id="trend-chart" style="height: 400px;"></div>
</div>

<script>
(function() {
    const chartDom = document.getElementById('trend-chart');
    const chart = echarts.init(chartDom);

    // 預設載入基準市場 + 前 3 大市場
    const defaultMarkets = '{{.BaseMarketCode}}';
    const cropCode = '{{.CropCode}}';

    fetch(`/api/trend?crop=${cropCode}&days=30&market=${defaultMarkets}`)
        .then(r => r.json())
        .then(data => {
            if (!data || data.length === 0) {
                chartDom.innerHTML = '<p class="text-center text-gray-400 py-16">尚無趨勢資料</p>';
                return;
            }

            const series = [];
            const legendData = [];

            data.forEach((market, idx) => {
                legendData.push(market.market_name);

                // 均價折線
                series.push({
                    name: market.market_name,
                    type: 'line',
                    yAxisIndex: 0,
                    smooth: true,
                    data: market.points.map(p => [p.date, p.avg_price]),
                });

                // 僅第一個市場顯示交易量柱狀圖
                if (idx === 0) {
                    series.push({
                        name: market.market_name + '交易量',
                        type: 'bar',
                        yAxisIndex: 1,
                        barWidth: '40%',
                        itemStyle: { opacity: 0.3 },
                        data: market.points.map(p => [p.date, p.volume]),
                    });
                }
            });

            chart.setOption({
                tooltip: { trigger: 'axis' },
                legend: { data: legendData, bottom: 0 },
                grid: { left: '3%', right: '4%', bottom: '15%', containLabel: true },
                xAxis: { type: 'category' },
                yAxis: [
                    { type: 'value', name: '均價 (元/公斤)', position: 'left' },
                    { type: 'value', name: '交易量 (公斤)', position: 'right' },
                ],
                series: series,
            });
        })
        .catch(err => {
            chartDom.innerHTML = '<p class="text-center text-red-400 py-16">載入趨勢圖失敗</p>';
        });

    window.addEventListener('resize', () => chart.resize());
})();
</script>
{{end}}
```

**Step 6: 建立篩選標籤 `web/templates/components/filter_bar.html`**

```html
{{/* web/templates/components/filter_bar.html */}}
{{/* 農產品價差雷達系統 — 篩選標籤列 */}}
{{/* HTMX 點擊標籤 → hx-get 請求 → hx-swap 局部替換 → hx-push-url 同步網址 */}}
{{define "filter_bar"}}
<div class="flex gap-2 flex-wrap">
    {{range .CropCodes}}
    <button hx-get="/api/spread?crop={{.}}"
            hx-target="#spread-table"
            hx-swap="innerHTML"
            hx-push-url="?crop={{.}}"
            class="px-4 py-2 rounded-full text-sm font-medium transition
                {{if eq . $.CropCode}}
                bg-green-600 text-white shadow
                {{else}}
                bg-white text-gray-600 border border-gray-300 hover:bg-green-50
                {{end}}"
            onclick="document.querySelectorAll('[hx-get^=\x27/api/spread\x27]').forEach(b=>b.className=b.className.replace(/bg-green-600 text-white shadow/,'bg-white text-gray-600 border border-gray-300 hover:bg-green-50'));this.className=this.className.replace(/bg-white text-gray-600 border border-gray-300 hover:bg-green-50/,'bg-green-600 text-white shadow')">
        {{if eq . "SQ1"}}帶殼{{else if eq . "SQ3"}}去殼{{else}}{{.}}{{end}}
    </button>
    {{end}}
</div>
{{end}}
```

**Step 7: 確認編譯通過**

```bash
CGO_ENABLED=1 go build ./...
```

Expected: 無錯誤。

**Step 8: Commit**

```bash
git add web/
git commit -m "feat: HTMX 儀表板模板 — 市場卡片、價差排名、趨勢圖"
```

---

### Task 8: Main 入口 — 完整接線與啟動

**Files:**
- Modify: `cmd/server/main.go`

**Step 1: 更新 `cmd/server/main.go` 為完整版本**

```go
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
```

**Step 2: 編譯完整專案**

```bash
CGO_ENABLED=1 go build -o farmer_crawler.exe ./cmd/server/
```

Expected: 編譯成功，產生 `farmer_crawler.exe`。

**Step 3: 測試 CLI 爬取**

```bash
./farmer_crawler.exe crawl --today
```

Expected: 輸出「爬取成功！共 N 筆記錄」。

**Step 4: 測試 HTTP 伺服器**

```bash
./farmer_crawler.exe
# 另開終端訪問 http://localhost:8080
```

Expected: 瀏覽器顯示儀表板頁面。

**Step 5: Commit**

```bash
git add cmd/server/main.go
git commit -m "feat: 完整入口 — CLI 爬取 + HTTP 伺服器 + 排程器接線"
```

---

### Task 9: 端對端驗證 — 爬取 + 儀表板展示

**Step 1: 爬取歷史資料（近一週）**

```bash
./farmer_crawler.exe crawl --from 114.02.24 --to 114.03.03
```

Expected: 輸出筆數。

**Step 2: 啟動伺服器並驗證儀表板**

```bash
./farmer_crawler.exe
```

在瀏覽器開啟 `http://localhost:8080`，驗證：
- [ ] 市場卡片正確顯示各市場均價
- [ ] 價差排名表按溢價%降序排列
- [ ] 篩選標籤切換帶殼/去殼時，表格局部更新（無整頁刷新）
- [ ] 趨勢圖正確渲染
- [ ] 手動爬取按鈕可用

**Step 3: 加入 .gitignore**

```
# .gitignore
data/
*.db
*.exe
```

**Step 4: Final Commit**

```bash
git add .gitignore
git commit -m "chore: 加入 .gitignore，排除資料庫與執行檔"
```

---

### Task 10: 文件收尾 — PRD 與 TODO

**Files:**
- Create: `docs/prd.md`
- Create: `docs/todo.md`

**Step 1: 建立 `docs/prd.md`**

從設計文件 `docs/plans/2026-03-03-farmer-crawler-design.md` 精簡而來。

**Step 2: 建立 `docs/todo.md`**

```markdown
# 農產品價差雷達 — TODO

## 核心功能
- [ ] 模組 A：爬蟲引擎（API 爬取 + SQLite Upsert）
- [ ] 模組 B：價差計算引擎（跨市場價差 + 溢價百分比）
- [ ] 模組 C：HTMX 儀表板（市場卡片 + 價差排名 + 趨勢圖）
- [ ] CLI 子命令（crawl --today / crawl --from --to）
- [ ] 每日自動排程（gocron）

## 未來擴展
- [ ] Docker 容器化部署
- [ ] 支援多種作物配置
- [ ] 運費成本模型
- [ ] Line Notify 價差提醒
```

**Step 3: Commit**

```bash
git add docs/prd.md docs/todo.md
git commit -m "docs: 加入 PRD 與 TODO 清單"
```
