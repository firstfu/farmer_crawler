# 農產品價差雷達系統

茭白筍跨市場價差分析工具，幫助埔里農民即時掌握全台批發市場行情，找出最佳銷售市場。

## 功能特色

### 爬蟲引擎（模組 A）
- 自動爬取農糧署開放資料 API，涵蓋全台 16 個批發市場
- SQLite Upsert 確保資料不重複（以日期 + 市場 + 作物為唯一鍵）
- 每日 10:00 自動排程 + CLI 手動補抓歷史資料

### 價差雷達（模組 B）
- 可配置基準市場（預設台中市），計算所有市場的絕對價差與溢價百分比
- 自動排序出當日最高溢價市場，一眼看出哪裡賣最好

### HTMX 儀表板（模組 C）
- 市場卡片即時看板，支援帶殼（SQ1）/ 去殼（SQ3）篩選
- HTMX 局部更新，操作流暢不跳頁
- ECharts 趨勢圖，可選擇多市場疊加比較
- 一鍵手動爬取按鈕

## 技術棧

| 類別 | 技術 |
|------|------|
| 語言 | Go 1.25 |
| Web 框架 | Gin |
| 資料庫 | SQLite（mattn/go-sqlite3） |
| 前端渲染 | HTMX + Go template |
| 圖表 | ECharts |
| 樣式 | TailwindCSS（CDN） |
| 排程 | gocron v2 |
| 配置 | YAML（gopkg.in/yaml.v3） |

## 專案結構

```
farmer_crawler/
├── cmd/server/
│   └── main.go              # 應用入口（HTTP 伺服器 + CLI 子命令）
├── internal/
│   ├── config/
│   │   └── config.go         # YAML 配置載入
│   ├── domain/
│   │   └── model.go          # 領域模型（PriceRecord、SpreadResult 等）
│   ├── handler/
│   │   └── dashboard.go      # HTTP Handler（儀表板 + HTMX 端點）
│   ├── repository/
│   │   ├── sqlite.go         # SQLite Repository（CRUD + 趨勢查詢）
│   │   └── sqlite_test.go    # Repository 測試
│   ├── scheduler/
│   │   └── scheduler.go      # gocron 每日排程器
│   └── service/
│       ├── analyzer.go       # 價差計算引擎
│       ├── analyzer_test.go  # 價差計算測試
│       ├── crawler.go        # API 爬蟲服務
│       └── crawler_test.go   # 爬蟲測試
├── web/templates/
│   ├── layout.html           # 主版面（HTML head、TailwindCSS、ECharts）
│   ├── dashboard.html        # 儀表板主頁
│   ├── partials/             # HTMX 局部更新片段
│   │   ├── market_cards.html
│   │   ├── spread_table.html
│   │   └── trend_chart.html
│   └── components/           # 可重用元件
│       ├── kpi_summary.html
│       └── filter_bar.html
├── data/
│   └── market.db             # SQLite 資料庫檔案（自動建立）
├── config.yaml               # 應用配置檔
├── docs/
│   ├── prd.md                # 產品需求文件
│   ├── todo.md               # 開發進度追蹤
│   └── plans/                # 設計與實作計畫
├── go.mod
└── go.sum
```

## 前置需求

- **Go 1.25+**
- **GCC**（SQLite 需要 CGO）
  - Windows：安裝 [MSYS2](https://www.msys2.org/) 後執行 `pacman -S mingw-w64-ucrt-x86_64-gcc`
  - Linux/macOS：通常已內建

### Windows 環境設定

```bash
# 確認 GCC 可用（MSYS2 ucrt64）
export PATH="/c/msys64/ucrt64/bin:$PATH"
export CGO_ENABLED=1
```

## 快速開始

```bash
# 1. 取得原始碼
git clone <repository-url>
cd farmer_crawler

# 2. 安裝依賴
go mod download

# 3. 啟動伺服器
go run ./cmd/server

# 4. 開啟瀏覽器
# http://localhost:8080
```

伺服器啟動後會自動建立 SQLite 資料庫，並在每日 10:00 排程爬取最新資料。

## 配置說明

編輯 `config.yaml` 調整系統參數：

```yaml
app:
  port: 8080                # HTTP 伺服器埠號
  db_path: "./data/market.db"  # SQLite 資料庫路徑

crawler:
  api_url: "https://data.moa.gov.tw/Service/OpenData/FromM/FarmTransData.aspx"
  crop_name: "茭白筍"       # 爬取的作物名稱
  schedule: "0 10 * * *"    # Cron 排程（每日 10:00）
  retry_count: 3            # API 重試次數
  retry_interval: 30s       # 重試間隔

analyzer:
  base_market_code: 400     # 基準市場代碼（台中市）
  base_market_name: "台中市" # 基準市場名稱
  crop_codes:               # 支援的作物代碼
    - "SQ1"                 # 帶殼茭白筍
    - "SQ3"                 # 去殼茭白筍
```

### 市場代碼對照

| 代碼 | 市場 | 代碼 | 市場 |
|------|------|------|------|
| 104 | 台北二 | 514 | 溪湖鎮 |
| 109 | 台北一 | 540 | 南投市 |
| 220 | 板橋區 | 600 | 嘉義市 |
| 241 | 三重區 | 800 | 高雄市 |
| 260 | 宜蘭市 | 830 | 鳳山區 |
| 338 | 桃農 | 900 | 屏東市 |
| 400 | 台中市 | 930 | 台東市 |
| 420 | 豐原區 | 950 | 花蓮市 |

## 使用方式

### Web 儀表板

啟動伺服器後開啟 `http://localhost:8080`，即可使用：

- **市場卡片**：顯示各市場今日均價，基準市場以特殊色標示
- **帶殼/去殼切換**：篩選不同作物代碼
- **價差排名表**：依溢價百分比排序，快速找到最高價市場
- **趨勢圖**：選擇市場後顯示歷史價格走勢
- **手動爬取**：點擊按鈕立即抓取今日最新資料

### CLI 命令

```bash
# 爬取今日資料
go run ./cmd/server crawl --today

# 爬取指定日期範圍（民國格式，西元年 - 1911 = 民國年）
go run ./cmd/server crawl --from 114.01.01 --to 114.03.03
```

編譯後也可直接使用二進制檔：

```bash
go build -o farmer_crawler ./cmd/server
./farmer_crawler crawl --today
```

## API 端點

| 方法 | 路徑 | 說明 | 回傳格式 |
|------|------|------|----------|
| GET | `/` | 主儀表板 | HTML |
| GET | `/api/markets?crop=SQ1&markets=400,109` | 市場卡片（可選市場過濾） | HTML 片段 |
| GET | `/api/spread?crop=SQ1&date=114.03.03&markets=400,109` | 價差排名表（可選市場過濾） | HTML 片段 |
| GET | `/api/trend?crop=SQ1&days=30&market=400,109&from=114.01.01&to=114.03.03` | 趨勢圖資料 | JSON |
| POST | `/api/crawl` | 手動觸發爬取 | HTML 片段 |

### 趨勢資料 JSON 格式

```json
[
  {
    "market_name": "台中市",
    "market_code": 400,
    "points": [
      { "date": "114.03.01", "avg_price": 25.5, "volume": 1200 }
    ]
  }
]
```

## 開發指南

### 執行測試

```bash
# Windows 需先設定環境變數
export CGO_ENABLED=1
export PATH="/c/msys64/ucrt64/bin:$PATH"

# 執行全部測試（15 個測試）
go test ./...
```

### 架構說明

系統採用 **Repository Pattern** 分層架構：

```
Scheduler（gocron 每日排程）
   ↓
Handler → Service → Repository → SQLite
   ↓         ↑
 Template   Config（YAML 配置）
（HTMX 局部更新）
```

- **Config**：從 config.yaml 載入應用配置
- **Scheduler**：gocron 每日排程，自動呼叫 CrawlerService
- **Handler**：接收 HTTP 請求，呼叫 Service，渲染模板
- **Service**：商業邏輯（爬蟲、價差計算）
- **Repository**：資料存取層（SQLite CRUD）
- **Domain**：領域模型定義

### 資料來源

農糧署開放資料 API：
```
https://data.moa.gov.tw/Service/OpenData/FromM/FarmTransData.aspx
  ?Crop=茭白筍&StartDate={民國日期}&EndDate={民國日期}&$top=500
```

日期格式為民國年.月.日（例：`114.03.03` 即西元 2025 年 3 月 3 日），API 欄位名稱為中文（交易日期、市場代號、平均價等）。西元年 - 1911 = 民國年。

## 未來規劃

- [ ] Docker 容器化部署
- [ ] 支援多種作物配置
- [ ] 運費成本模型
- [ ] Line Notify 價差提醒
- [ ] 歷史價差趨勢分析

## 授權

MIT License
