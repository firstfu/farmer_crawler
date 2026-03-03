// internal/service/analyzer.go
// 農產品價差雷達系統 — 價差計算引擎
// 負責跨市場價差分析，核心功能：
//   - 指定日期與作物，以基準市場為參照計算各市場的絕對價差與溢價百分比
//   - 基準市場當日無資料時，自動回補最新歷史價格
//   - 結果按溢價百分比降序排列，方便快速辨識高價差市場
// 計算公式：
//   - AbsoluteSpread = 目標市場均價 - 基準市場均價
//   - SpreadPercent = (目標市場均價 - 基準市場均價) / 基準市場均價 × 100

package service

import (
	"database/sql"
	"errors"
	"sort"

	"farmer_crawler/internal/domain"
	"farmer_crawler/internal/repository"
)

// AnalyzerService 價差計算服務
// 以指定的基準市場為參照，計算其他市場的價差與溢價百分比
type AnalyzerService struct {
	repo           *repository.SQLiteRepo
	baseMarketCode int    // 基準市場代碼（例如 400 代表台中市）
	baseMarketName string // 基準市場名稱（例如 "台中市"）
}

// NewAnalyzerService 建立新的價差計算服務實例
// 參數：
//   - repo: SQLite 資料庫存取層
//   - baseMarketCode: 基準市場代碼
//   - baseMarketName: 基準市場名稱
func NewAnalyzerService(repo *repository.SQLiteRepo, baseMarketCode int, baseMarketName string) *AnalyzerService {
	return &AnalyzerService{
		repo:           repo,
		baseMarketCode: baseMarketCode,
		baseMarketName: baseMarketName,
	}
}

// CalculateSpread 計算指定日期與作物的跨市場價差
// 邏輯流程：
//  1. 查詢該日期與作物代碼的所有市場交易記錄
//  2. 從結果中尋找基準市場的均價；若當日無資料則回補最新歷史均價
//  3. 若完全無基準價格，回傳空結果（不視為錯誤）
//  4. 針對每個非基準市場（且均價 > 0），計算絕對價差與溢價百分比
//  5. 按溢價百分比降序排列後回傳
func (a *AnalyzerService) CalculateSpread(date, cropCode string) ([]domain.SpreadResult, error) {
	// Step 1: 取得該日所有市場的交易記錄
	records, err := a.repo.GetByDate(date, cropCode)
	if err != nil {
		return nil, err
	}

	// Step 2: 尋找基準市場價格
	var basePrice float64
	var baseFound bool

	for _, rec := range records {
		if rec.MarketCode == a.baseMarketCode && rec.AvgPrice > 0 {
			basePrice = rec.AvgPrice
			baseFound = true
			break
		}
	}

	// Step 2b: 基準市場當日無資料，嘗試回補歷史價格
	if !baseFound {
		histPrice, _, err := a.repo.GetLatestAvgPrice(a.baseMarketCode, cropCode)
		if err != nil {
			// 若為查無資料錯誤，回傳空結果
			if errors.Is(err, sql.ErrNoRows) {
				return nil, nil
			}
			return nil, err
		}
		if histPrice > 0 {
			basePrice = histPrice
			baseFound = true
		}
	}

	// Step 3: 若無基準價格，回傳空結果
	if !baseFound {
		return nil, nil
	}

	// Step 4: 計算各目標市場的價差
	var results []domain.SpreadResult

	for _, rec := range records {
		// 排除基準市場自身
		if rec.MarketCode == a.baseMarketCode {
			continue
		}
		// 排除均價為 0 的記錄
		if rec.AvgPrice <= 0 {
			continue
		}

		absoluteSpread := rec.AvgPrice - basePrice
		spreadPercent := (rec.AvgPrice - basePrice) / basePrice * 100

		results = append(results, domain.SpreadResult{
			TargetMarket:     rec.MarketName,
			TargetMarketCode: rec.MarketCode,
			TargetAvgPrice:   rec.AvgPrice,
			BaseAvgPrice:     basePrice,
			AbsoluteSpread:   absoluteSpread,
			SpreadPercent:    spreadPercent,
			TargetVolume:     rec.Volume,
		})
	}

	// Step 5: 按溢價百分比降序排列
	sort.Slice(results, func(i, j int) bool {
		return results[i].SpreadPercent > results[j].SpreadPercent
	})

	return results, nil
}
