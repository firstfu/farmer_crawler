# 農產品價差雷達 — TODO

## 核心功能
- ~~模組 A：爬蟲引擎（API 爬取 + SQLite Upsert）~~
- ~~模組 B：價差計算引擎（跨市場價差 + 溢價百分比）~~
- ~~模組 C：HTMX 儀表板（市場卡片 + 價差排名 + 趨勢圖）~~
- ~~CLI 子命令（crawl --today / crawl --from --to）~~
- ~~每日自動排程（gocron）~~

## Web UI 日期範圍爬取 + 自動回補
- ~~Config 擴展（batch_days, batch_delay, backfill_days, backfill_on_start）~~
- ~~Domain 模型新增 CrawlBatchProgress / CrawlBatchResult~~
- ~~Repository 新增 GetExistingTradeDates~~
- ~~CrawlerService 核心：ParseMinguoDate、SplitDateRange、CrawlRangeWithProgress、Backfill~~
- ~~Handler 新增 SSE 端點 GET /api/crawl/range~~
- ~~前端 UI 爬取日期範圍按鈕 + 進度面板 + SSE JS~~
- ~~Scheduler 整合啟動回補 + 每日回補~~
- ~~入口點 main.go 更新參數~~
- ~~新增 ParseMinguoDate / SplitDateRange 測試（5 個測試全部通過）~~

## 反爬蟲防護
- ~~HTTP 請求偽裝（User-Agent、Accept-Language、Referer 等瀏覽器 Header）~~
- ~~智慧重試策略（依 HTTP 錯誤碼分類：429 退避、403 停止、5xx 重試）~~
- ~~指數退避 + Jitter（避免固定間隔的機器人特徵）~~
- ~~Token Bucket 速率限制器（golang.org/x/time/rate）~~
- ~~爬取狀態追蹤（crawl_status 表 + Repository CRUD）~~
- ~~儀表板爬蟲健康度顯示（成功率、最近記錄、HTMX 局部更新）~~
- ~~HTTP Transport 抽象層（預留 Proxy 擴充介面）~~
- ~~速率限制配置化（rate_limit、rate_burst in config.yaml）~~

## 未來擴展
- [ ] Docker 容器化部署
- [ ] 支援多種作物配置
- [ ] 運費成本模型
- [ ] Line Notify 價差提醒
- ~~歷史價差趨勢分析~~
