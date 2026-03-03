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
