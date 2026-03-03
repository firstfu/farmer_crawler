// internal/service/analyzer_test.go
// 農產品價差雷達系統 — 價差計算引擎單元測試
// 測試跨市場價差計算邏輯，包含：
//   - TestCalculateSpread_Basic: 基本三市場價差計算，驗證排序與百分比精度
//   - TestCalculateSpread_BaseMarketMissing: 基準市場當日無資料，驗證歷史價格回補
//   - TestCalculateSpread_NoData: 空資料庫，驗證無資料時不產生錯誤
// 每個測試使用獨立的暫存 SQLite 資料庫，避免與其他測試互相干擾

package service

import (
	"os"
	"path/filepath"
	"testing"

	"farmer_crawler/internal/domain"
	"farmer_crawler/internal/repository"
)

// abs 計算浮點數絕對值（避免引入 math 套件）
func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// setupAnalyzerTest 建立測試用暫存 SQLite 資料庫，回傳 AnalyzerService 與 SQLiteRepo
// 測試結束後自動清除暫存目錄
func setupAnalyzerTest(t *testing.T) (*AnalyzerService, *repository.SQLiteRepo) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "analyzer_test_*")
	if err != nil {
		t.Fatalf("建立暫存目錄失敗: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	dbPath := filepath.Join(tmpDir, "test.db")
	repo, err := repository.NewSQLiteRepo(dbPath)
	if err != nil {
		t.Fatalf("建立測試資料庫失敗: %v", err)
	}
	t.Cleanup(func() { repo.Close() })

	// 基準市場：台中市（市場代碼 400）
	svc := NewAnalyzerService(repo, 400, "台中市")
	return svc, repo
}

// TestCalculateSpread_Basic 測試基本價差計算
// 場景：三個市場（台中400 avgPrice=85, 台北一109=120, 高雄800=110）
// 預期：排除基準市場後回傳 2 筆結果，按 SpreadPercent 降序排列
//   - 台北一: spread=35, percent≈41.18%
//   - 高雄: spread=25, percent≈29.41%
func TestCalculateSpread_Basic(t *testing.T) {
	svc, repo := setupAnalyzerTest(t)

	// 插入測試資料：三個市場同日同作物
	testRecords := []domain.PriceRecord{
		{
			TradeDate:  "114.03.03",
			CropCode:   "SQ1",
			CropName:   "茭白筍-帶殼",
			MarketCode: 400,
			MarketName: "台中市",
			AvgPrice:   85,
			Volume:     500,
		},
		{
			TradeDate:  "114.03.03",
			CropCode:   "SQ1",
			CropName:   "茭白筍-帶殼",
			MarketCode: 109,
			MarketName: "台北一",
			AvgPrice:   120,
			Volume:     300,
		},
		{
			TradeDate:  "114.03.03",
			CropCode:   "SQ1",
			CropName:   "茭白筍-帶殼",
			MarketCode: 800,
			MarketName: "高雄市",
			AvgPrice:   110,
			Volume:     200,
		},
	}

	if err := repo.BatchUpsert(testRecords); err != nil {
		t.Fatalf("插入測試資料失敗: %v", err)
	}

	results, err := svc.CalculateSpread("114.03.03", "SQ1")
	if err != nil {
		t.Fatalf("CalculateSpread 回傳錯誤: %v", err)
	}

	// 應有 2 筆結果（排除基準市場台中）
	if len(results) != 2 {
		t.Fatalf("預期 2 筆結果，得到 %d 筆", len(results))
	}

	// 第一筆應是台北一（SpreadPercent 最高）
	if results[0].TargetMarketCode != 109 {
		t.Errorf("第一筆市場代碼預期 109（台北一），得到 %d", results[0].TargetMarketCode)
	}
	if results[0].TargetMarket != "台北一" {
		t.Errorf("第一筆市場名稱預期 台北一，得到 %s", results[0].TargetMarket)
	}
	if abs(results[0].AbsoluteSpread-35) > 0.01 {
		t.Errorf("第一筆絕對價差預期 35，得到 %f", results[0].AbsoluteSpread)
	}
	// 35/85*100 ≈ 41.176...%
	if abs(results[0].SpreadPercent-41.18) > 0.01 {
		t.Errorf("第一筆溢價百分比預期 ≈41.18%%，得到 %f%%", results[0].SpreadPercent)
	}
	if abs(results[0].BaseAvgPrice-85) > 0.01 {
		t.Errorf("第一筆基準價格預期 85，得到 %f", results[0].BaseAvgPrice)
	}

	// 第二筆應是高雄市
	if results[1].TargetMarketCode != 800 {
		t.Errorf("第二筆市場代碼預期 800（高雄市），得到 %d", results[1].TargetMarketCode)
	}
	if abs(results[1].AbsoluteSpread-25) > 0.01 {
		t.Errorf("第二筆絕對價差預期 25，得到 %f", results[1].AbsoluteSpread)
	}
	// 25/85*100 ≈ 29.411...%
	if abs(results[1].SpreadPercent-29.41) > 0.01 {
		t.Errorf("第二筆溢價百分比預期 ≈29.41%%，得到 %f%%", results[1].SpreadPercent)
	}
}

// TestCalculateSpread_BaseMarketMissing 測試基準市場當日無資料時的歷史回補
// 場景：基準市場（台中400）在 114.03.03 無資料，但在 114.03.01 有 avgPrice=85
//
//	另一市場（台北一109）在 114.03.03 有 avgPrice=120
//
// 預期：應使用歷史價格 85 作為基準價，正確計算價差
func TestCalculateSpread_BaseMarketMissing(t *testing.T) {
	svc, repo := setupAnalyzerTest(t)

	// 基準市場只有歷史資料（114.03.01）
	historicalRecord := domain.PriceRecord{
		TradeDate:  "114.03.01",
		CropCode:   "SQ1",
		CropName:   "茭白筍-帶殼",
		MarketCode: 400,
		MarketName: "台中市",
		AvgPrice:   85,
		Volume:     500,
	}
	if err := repo.Upsert(historicalRecord); err != nil {
		t.Fatalf("插入歷史記錄失敗: %v", err)
	}

	// 目標市場有當日資料（114.03.03）
	targetRecord := domain.PriceRecord{
		TradeDate:  "114.03.03",
		CropCode:   "SQ1",
		CropName:   "茭白筍-帶殼",
		MarketCode: 109,
		MarketName: "台北一",
		AvgPrice:   120,
		Volume:     300,
	}
	if err := repo.Upsert(targetRecord); err != nil {
		t.Fatalf("插入目標記錄失敗: %v", err)
	}

	results, err := svc.CalculateSpread("114.03.03", "SQ1")
	if err != nil {
		t.Fatalf("CalculateSpread 回傳錯誤: %v", err)
	}

	// 應有 1 筆結果
	if len(results) != 1 {
		t.Fatalf("預期 1 筆結果，得到 %d 筆", len(results))
	}

	// 基準價格應使用歷史價 85
	if abs(results[0].BaseAvgPrice-85) > 0.01 {
		t.Errorf("基準價格預期 85（歷史回補），得到 %f", results[0].BaseAvgPrice)
	}
	if abs(results[0].AbsoluteSpread-35) > 0.01 {
		t.Errorf("絕對價差預期 35，得到 %f", results[0].AbsoluteSpread)
	}
	if abs(results[0].SpreadPercent-41.18) > 0.01 {
		t.Errorf("溢價百分比預期 ≈41.18%%，得到 %f%%", results[0].SpreadPercent)
	}
}

// TestCalculateSpread_NoData 測試空資料庫情境
// 預期：回傳空結果且不產生錯誤
func TestCalculateSpread_NoData(t *testing.T) {
	svc, _ := setupAnalyzerTest(t)

	results, err := svc.CalculateSpread("114.03.03", "SQ1")
	if err != nil {
		t.Fatalf("空資料庫不應回傳錯誤，得到: %v", err)
	}

	if len(results) != 0 {
		t.Errorf("空資料庫應回傳 0 筆結果，得到 %d 筆", len(results))
	}
}
