# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 專案概述

農產品價差雷達系統 — 茭白筍跨市場價差分析工具（Go + Gin + SQLite + HTMX + ECharts）。
幫助埔里農民即時掌握全台 16 個批發市場行情，找出最佳銷售市場。MVP 已完成。

## 語言與回應規範

- 一律使用繁體中文回應。
- 每個檔案頂部加上詳細註解，格式：`// 檔案路徑` → `// 系統名稱 — 模組名稱` → `// 負責：...`。

## 文件管理

- 規劃書：`docs/prd.md`
- 開發進度：`docs/todo.md`（完成項目使用 ~~刪除線~~）
- 設計文件：`docs/plans/`

## 開發環境（Windows 必須）

```bash
export CGO_ENABLED=1
export PATH="/c/msys64/ucrt64/bin:$PATH"
```

SQLite 需要 CGO，缺少 GCC 會編譯失敗。需安裝 MSYS2 + `pacman -S mingw-w64-ucrt-x86_64-gcc`。

## 常用命令

```bash
# 開發模式
go run ./cmd/server

# 編譯
go build -o farmer_crawler ./cmd/server

# 測試（全部 15 個）
go test ./...
go test -v ./internal/repository   # 單一套件
go test -run TestUpsert ./internal/repository  # 單一測試

# 爬蟲 CLI
go run ./cmd/server crawl --today
go run ./cmd/server crawl --from 114.01.01 --to 114.03.03
```

## 架構（Repository Pattern 分層）

```
Scheduler (gocron, 每日 10:00)
   ↓
Handler (Gin) → Service → Repository → SQLite
   ↓               ↑
 Template        Config (YAML)
 (HTMX)
```

- **Config** (`internal/config/`) — 從 `config.yaml` 載入配置
- **Domain** (`internal/domain/`) — 領域模型：PriceRecord、SpreadResult、TrendPoint
- **Repository** (`internal/repository/`) — SQLite CRUD，WAL 模式，Upsert 基於 `UNIQUE(trade_date, market_code, crop_code)`
- **Service** (`internal/service/`) — CrawlerService（API 爬蟲 + 重試）、AnalyzerService（價差計算 + 基準市場回補）
- **Handler** (`internal/handler/`) — Gin 路由 + HTMX 端點 + Go template 渲染
- **Scheduler** (`internal/scheduler/`) — gocron 每日排程呼叫 CrawlerService

### HTTP 端點

```
GET  /              主儀表板
GET  /api/markets   市場卡片（HTMX 片段）
GET  /api/spread    價差排名表（HTMX 片段）
GET  /api/trend     趨勢圖資料（JSON）
POST /api/crawl     手動爬取
```

## 關鍵技術細節

### 日期格式（民國年）
格式 `YYY.MM.DD`（例：`114.03.03` = 西元 2025/03/03）。轉換：西元 - 1911 = 民國。
核心函式：`service.ToMinguoDate(time.Time) string`。所有 API 參數和資料庫儲存皆用民國格式。

### API 市場代號雙型態
農糧署 API 的「市場代號」可能回傳 int 或 string，必須用 `json.Number` 處理：
```go
type apiRecordJSON struct {
    MarketCode json.Number `json:"市場代號"`
}
```

### 基準市場價格回補
AnalyzerService 計算價差時，若基準市場當日無資料，自動查詢最新歷史均價；若無任何歷史資料則返回空結果（不報錯）。

### API 中文鍵名
農糧署 API 回傳 JSON 鍵名全部是中文（`交易日期`、`市場代號`、`平均價`等），struct tag 直接映射。

## 測試模式

- **Repository 測試**：暫存 `.db` 檔，`t.Cleanup` 自動刪除
- **Analyzer 測試**：`os.MkdirTemp` 隔離暫存目錄
- **Crawler 測試**：純單元測試，不發真實 HTTP 請求
- **浮點比較**：自訂 `abs()` + 容差 0.01

## 資料來源

農糧署開放資料 API：
```
https://data.moa.gov.tw/Service/OpenData/FromM/FarmTransData.aspx
  ?Crop=茭白筍&StartDate={民國日期}&EndDate={民國日期}&$top=500
```
