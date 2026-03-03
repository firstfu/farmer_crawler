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

// SpreadResult 代表一個目標市場的價差計算結果
type SpreadResult struct {
	TargetMarket     string  `json:"target_market"`
	TargetMarketCode int     `json:"target_market_code"`
	TargetAvgPrice   float64 `json:"target_avg_price"`
	BaseAvgPrice     float64 `json:"base_avg_price"`
	AbsoluteSpread   float64 `json:"absolute_spread"` // 絕對價差 (元/公斤)
	SpreadPercent    float64 `json:"spread_percent"`   // 溢價百分比 (%)
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
