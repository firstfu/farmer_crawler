# 農產品價差雷達系統 — PRD

## 產品概述

茭白筍跨市場價差分析工具，幫助埔里農民判斷最佳銷售市場。

## 核心功能

### 模組 A：爬蟲引擎
- 自動/手動爬取農糧署開放資料 API
- SQLite Upsert 確保 (date, market_id, crop_id) 資料唯一性
- 每日 10:00 自動排程 + CLI 手動補抓

### 模組 B：價差雷達
- 可配置基準市場（預設台中市 400）
- 計算所有市場的絕對價差與溢價百分比
- 自動排序出當日最高溢價市場

### 模組 C：HTMX 儀表板
- 市場卡片即時看板
- 帶殼/去殼篩選（HTMX 局部更新）
- ECharts 雙 Y 軸趨勢圖
- 手動爬取觸發按鈕

## 技術棧

Go + Gin + SQLite + HTMX + ECharts + TailwindCSS CDN

## 資料來源

農糧署開放資料 API：`https://data.moa.gov.tw/Service/OpenData/FromM/FarmTransData.aspx`

## 設計文件

詳見 `docs/plans/2026-03-03-farmer-crawler-design.md`
