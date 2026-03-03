# 農產品價差雷達系統設計文件

> 日期：2026-03-03
> 狀態：已核准

## 1. 專案概述

### 目標
建立一個以 Go 語言開發的農產品價差分析工具，專注於**茭白筍**（埔里產地）。系統自動爬取農糧署開放資料 API 的批發市場交易行情，計算跨市場價差，並透過 HTMX 儀表板呈現決策資訊，幫助農民判斷最佳銷售市場。

### 核心功能
- **模組 A：爬蟲引擎** — 自動/手動爬取農糧署 API，SQLite Upsert 確保資料唯一性
- **模組 B：價差雷達** — 以可配置的基準市場計算跨市場價差與溢價百分比
- **模組 C：HTMX 儀表板** — 即時看板、動態篩選、多市場趨勢圖

### 關鍵決策
| 決策項目 | 選擇 | 理由 |
|---------|------|------|
| 資料來源 | 農糧署開放資料 API | 官方結構化 JSON，穩定可靠 |
| 目標作物 | 僅茭白筍（SQ1 帶殼 / SQ3 去殼） | 專注單一作物，降低複雜度 |
| 基準市場 | 可配置，預設台中市 (400) | 中部最大批發市場，資料穩定 |
| 架構方案 | 方案 B 分層架構 (Repository Pattern) | 關注點分離、易測試 |
| 爬取頻率 | 每日自動 + CLI 手動補抓 | 兼顧日常運行與歷史資料回填 |
| 部署環境 | 本機開發（先跑起來） | MVP 階段優先 |

---

## 2. 技術棧

| 層級 | 技術 | 說明 |
|------|------|------|
| 語言 | Go | 主要開發語言 |
| Web 框架 | Gin | 輕量高效 HTTP 框架 |
| 資料庫 | SQLite (go-sqlite3) | 零配置、單檔部署 |
| 排程 | gocron | Go 原生 cron 排程器 |
| 前端互動 | HTMX (CDN) | 局部更新，SPA 般體驗 |
| 圖表 | ECharts (CDN) | 雙 Y 軸趨勢圖 |
| CSS | TailwindCSS (CDN) | 開發階段免建構 |
| 模板 | Go html/template | 伺服器端渲染 |

**零前端建構工具** — 無需 Node.js / npm / webpack。

---

## 3. 專案目錄結構

```
farmer_crawler/
├── cmd/
│   └── server/
│       └── main.go                # 應用入口：初始化 DI、啟動 Gin + 排程
├── internal/
│   ├── config/
│   │   └── config.go              # YAML 配置載入
│   ├── domain/
│   │   └── model.go               # 領域模型：Market, Crop, PriceRecord, SpreadResult
│   ├── repository/
│   │   └── sqlite.go              # SQLite CRUD + Upsert (date, market_id, crop_id)
│   ├── service/
│   │   ├── crawler.go             # 農糧署 API 爬取 + 資料正規化
│   │   └── analyzer.go            # 價差計算引擎
│   ├── handler/
│   │   └── dashboard.go           # Gin HTTP handlers (HTMX endpoints)
│   └── scheduler/
│       └── scheduler.go           # gocron 排程管理
├── web/
│   └── templates/
│       ├── layout.html            # 基底模板 (HTMX + ECharts CDN)
│       ├── dashboard.html         # 主儀表板
│       ├── partials/
│       │   ├── market_cards.html  # 市場卡片（hx-swap 目標）
│       │   ├── spread_table.html  # 價差排名表格
│       │   └── trend_chart.html   # 趨勢圖容器
│       └── components/
│           └── filter_bar.html    # 篩選標籤列
├── config.yaml                    # 應用配置
├── go.mod
├── go.sum
└── data/                          # SQLite DB 自動建立於此
```

---

## 4. 領域模型

### PriceRecord — 單筆交易行情記錄

```go
type PriceRecord struct {
    TradeDate   string  // "114.03.03" (民國日期)
    CropCode    string  // "SQ1" 帶殼 / "SQ3" 去殼
    CropName    string  // "茭白筍-帶殼"
    MarketCode  int     // 400 (台中市)
    MarketName  string  // "台中市"
    UpperPrice  float64 // 上價 (元/公斤)
    MiddlePrice float64 // 中價
    LowerPrice  float64 // 下價
    AvgPrice    float64 // 平均價
    Volume      float64 // 交易量 (公斤)
}
```

### SpreadResult — 價差計算結果

```go
type SpreadResult struct {
    TargetMarket    string
    TargetMarketCode int
    TargetAvgPrice  float64
    BaseAvgPrice    float64
    AbsoluteSpread  float64 // 絕對價差 (元/公斤)
    SpreadPercent   float64 // 溢價百分比 (%)
    TargetVolume    float64
}
```

---

## 5. SQLite Schema

```sql
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

CREATE INDEX idx_trade_date ON price_records(trade_date);
CREATE INDEX idx_market_code ON price_records(market_code);
```

**Upsert 策略：** `INSERT OR REPLACE` 基於 `(trade_date, market_code, crop_code)` 唯一約束。

---

## 6. 模組 A：爬蟲引擎

### API 規格

- **Endpoint:** `https://data.moa.gov.tw/Service/OpenData/FromM/FarmTransData.aspx`
- **參數:** `Crop=茭白筍&StartDate={民國日期}&EndDate={民國日期}&$top=500`
- **日期格式:** 民國年.月.日（例：114.03.03）
- **轉換公式:** 民國年 = 西元年 - 1911

### API 回傳欄位對應

| API 欄位 | 模型欄位 | 類型 |
|---------|---------|------|
| 交易日期 | TradeDate | string |
| 種類代碼 | (分類用) | string |
| 作物代號 | CropCode | string |
| 作物名稱 | CropName | string |
| 市場代號 | MarketCode | int |
| 市場名稱 | MarketName | string |
| 上價 | UpperPrice | float64 |
| 中價 | MiddlePrice | float64 |
| 下價 | LowerPrice | float64 |
| 平均價 | AvgPrice | float64 |
| 交易量 | Volume | float64 |

### 爬取排程

- 預設每日 **10:00** 自動爬取（農糧署 10 點更新完畢）
- cron 表達式可透過 config.yaml 配置

### 錯誤處理

- HTTP 超時：重試 3 次，間隔 30 秒
- API 回傳空資料：記錄 warning log，不覆蓋既有資料

### CLI 補抓模式

```bash
# 補抓歷史區間
./farmer_crawler crawl --from 114.01.01 --to 114.03.03

# 僅爬取今日
./farmer_crawler crawl --today
```

---

## 7. 模組 B：價差計算引擎

### 計算邏輯

1. 查詢基準市場（預設台中市 400）當日該品項均價
2. 查詢所有其他市場當日同品項均價
3. 計算每個市場的絕對價差與溢價百分比
4. 按溢價百分比降序排列

### 公式

```
絕對價差 = 目標市場均價 - 基準市場均價
溢價百分比 = (目標市場均價 - 基準市場均價) / 基準市場均價 × 100
```

### 邊界情況

| 情況 | 處理方式 |
|------|---------|
| 基準市場當日休市 | 使用最近一個交易日均價，UI 標示「使用歷史價格」 |
| 目標市場無資料 | 跳過該市場，不計算 |
| 帶殼 vs 去殼 | 分開計算，UI 可切換 |

---

## 8. 模組 C：HTMX 決策儀表板

### 路由設計

| 方法 | 路由 | 用途 | 回傳 |
|------|------|------|------|
| GET | `/` | 主儀表板（完整頁面） | 完整 HTML |
| GET | `/api/markets` | 市場卡片列表 | HTML 片段 |
| GET | `/api/spread?crop=SQ1` | 價差排名表格 | HTML 片段 |
| GET | `/api/trend?days=30&market=400,109` | 趨勢圖資料 | JSON |
| POST | `/api/crawl` | 手動觸發爬取 | HTML 狀態訊息 |

### HTMX 互動模式

1. **市場卡片區** — 每張卡片顯示：市場名稱、均價、交易量、與基準的價差
2. **篩選標籤** — 點擊 `hx-get` → 伺服器回傳新片段 → `hx-swap="innerHTML"` 局部替換 → `hx-push-url="true"` 同步網址
3. **價差排名表** — 按溢價%降序，標示前三名，基準市場特別標示
4. **趨勢圖** — 後端 JSON → `data-chart` 屬性 → Vanilla JS 初始化 ECharts 雙 Y 軸

### ECharts 圖表規格

- **左 Y 軸：** 均價折線圖（元/公斤）
- **右 Y 軸：** 交易量柱狀圖（公斤）
- **X 軸：** 日期
- **預設顯示：** 基準市場 + 溢價前 3 名市場
- **互動：** 可勾選/取消市場顯示

---

## 9. 配置檔 (config.yaml)

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

---

## 10. Go 套件依賴

| 套件 | 用途 |
|------|------|
| `github.com/gin-gonic/gin` | HTTP 框架 |
| `github.com/mattn/go-sqlite3` | SQLite 驅動 |
| `github.com/go-co-op/gocron/v2` | 排程管理 |
| `gopkg.in/yaml.v3` | YAML 配置解析 |

---

## 11. 已知市場代碼參考

| 代號 | 市場名稱 |
|------|---------|
| 104 | 台北二 |
| 109 | 台北一 |
| 220 | 板橋區 |
| 241 | 三重區 |
| 260 | 宜蘭市 |
| 338 | 桃農 |
| 400 | 台中市 |
| 420 | 豐原區 |
| 514 | 溪湖鎮 |
| 540 | 南投市 |
| 600 | 嘉義市 |
| 800 | 高雄市 |
| 830 | 鳳山區 |
| 900 | 屏東市 |
| 930 | 台東市 |
| 950 | 花蓮市 |
