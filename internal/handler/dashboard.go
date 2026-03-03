// internal/handler/dashboard.go
// 農產品價差雷達系統 — HTTP Handler 層
// 提供 Gin 路由與 HTMX 端點：主儀表板、市場卡片、價差排名表、趨勢圖 JSON
// HTMX 端點回傳 HTML 片段，供前端局部替換
// 主要路由：
//   - GET  /           主儀表板頁面（完整 HTML）
//   - GET  /api/markets 市場卡片 HTML 片段（HTMX 局部更新）
//   - GET  /api/spread  價差排名表 HTML 片段（HTMX 局部更新）
//   - GET  /api/trend   趨勢圖 JSON 資料（供 ECharts 使用）
//   - POST /api/crawl   手動觸發爬取（回傳狀態 HTML 片段）

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

// DashboardHandler 儀表板 HTTP 處理器
// 整合 Repository、AnalyzerService、CrawlerService 與 Config
// 負責渲染 HTML 模板與回傳 JSON 資料
type DashboardHandler struct {
	repo     *repository.SQLiteRepo
	analyzer *service.AnalyzerService
	crawler  *service.CrawlerService
	cfg      *config.Config
	tmpl     *template.Template
}

// NewDashboardHandler 建立新的儀表板處理器
// 載入 web/templates/ 下的所有 HTML 模板，包含 layout、partials、components
// 註冊自訂模板函式：sprintf（格式化輸出）、add（整數加法）
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

// RegisterRoutes 註冊所有路由到 Gin 引擎
// 包含主頁面、HTMX 端點與手動爬取端點
func (h *DashboardHandler) RegisterRoutes(r *gin.Engine) {
	r.GET("/", h.Dashboard)
	r.GET("/api/markets", h.MarketCards)
	r.GET("/api/spread", h.SpreadTable)
	r.GET("/api/trend", h.TrendData)
	r.POST("/api/crawl", h.TriggerCrawl)
}

// Dashboard 主儀表板頁面
// 查詢參數：crop（作物代碼，預設 "SQ1"）
// 回傳完整 HTML 頁面，包含市場卡片、價差排名、趨勢圖
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

// MarketCards 市場卡片 HTMX 端點
// 查詢參數：crop（作物代碼，預設 "SQ1"）
// 回傳 market_cards.html 片段，供 HTMX 局部替換
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
	h.tmpl.ExecuteTemplate(c.Writer, "market_cards", data)
}

// SpreadTable 價差排名表 HTMX 端點
// 查詢參數：crop（作物代碼，預設 "SQ1"）、date（民國日期，預設今日）
// 回傳 spread_table.html 片段，供 HTMX 局部替換
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
	h.tmpl.ExecuteTemplate(c.Writer, "spread_table", data)
}

// TrendData 趨勢圖 JSON 資料端點
// 查詢參數：
//   - crop: 作物代碼（預設 "SQ1"）
//   - days: 查詢天數（預設 30）
//   - market: 市場代碼，逗號分隔（預設為基準市場代碼）
//
// 回傳 JSON 陣列，每個元素包含 market_name、market_code、points
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

// TriggerCrawl 手動觸發爬取端點
// 呼叫 CrawlerService.CrawlToday 立即爬取今日資料
// 回傳 HTML 片段顯示爬取結果（成功/失敗），供 HTMX 局部替換
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

// parseMarketCodes 解析逗號分隔的市場代碼字串
// 例如 "400,200,300" → []int{400, 200, 300}
// 無效的代碼會被忽略
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
