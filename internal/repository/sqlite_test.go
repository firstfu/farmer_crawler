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

	tables := []string{"price_records", "crawl_status", "schema_migrations"}
	for _, table := range tables {
		var count int
		err := repo.db.QueryRow(
			"SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", table,
		).Scan(&count)
		if err != nil {
			t.Fatalf("查詢 %s 失敗: %v", table, err)
		}
		if count != 1 {
			t.Errorf("預期 %s 表存在，但 count=%d", table, count)
		}
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

func TestSaveCrawlStatus(t *testing.T) {
	repo := setupTestDB(t)

	status := domain.CrawlStatus{
		DateFrom:    "115.03.01",
		DateTo:      "115.03.03",
		RecordCount: 42,
		Status:      "success",
		ErrorMsg:    "",
		DurationMs:  1500,
	}

	err := repo.SaveCrawlStatus(&status)
	if err != nil {
		t.Fatalf("SaveCrawlStatus 失敗: %v", err)
	}

	results, err := repo.GetRecentCrawlStatus(5)
	if err != nil {
		t.Fatalf("GetRecentCrawlStatus 失敗: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("預期 1 筆狀態記錄，得到 %d 筆", len(results))
	}
	if results[0].Status != "success" {
		t.Errorf("預期狀態 success，得到 %s", results[0].Status)
	}
	if results[0].RecordCount != 42 {
		t.Errorf("預期 42 筆記錄，得到 %d", results[0].RecordCount)
	}
}

func TestGetCrawlHealthSummary(t *testing.T) {
	repo := setupTestDB(t)

	// 插入 3 筆狀態：2 成功 1 失敗
	statuses := []domain.CrawlStatus{
		{DateFrom: "115.03.01", DateTo: "115.03.01", RecordCount: 10, Status: "success", DurationMs: 500},
		{DateFrom: "115.03.02", DateTo: "115.03.02", RecordCount: 0, Status: "failed", ErrorMsg: "timeout", DurationMs: 30000},
		{DateFrom: "115.03.03", DateTo: "115.03.03", RecordCount: 15, Status: "success", DurationMs: 800},
	}
	for i := range statuses {
		if err := repo.SaveCrawlStatus(&statuses[i]); err != nil {
			t.Fatalf("SaveCrawlStatus 失敗: %v", err)
		}
	}

	health, err := repo.GetCrawlHealthSummary()
	if err != nil {
		t.Fatalf("GetCrawlHealthSummary 失敗: %v", err)
	}
	if health.TotalCrawls24h != 3 {
		t.Errorf("預期 3 次爬取，得到 %d", health.TotalCrawls24h)
	}
	if health.FailedCrawls24h != 1 {
		t.Errorf("預期 1 次失敗，得到 %d", health.FailedCrawls24h)
	}
	// 成功率 = 2/3 ≈ 66.67
	expectedRate := 66.67
	if health.SuccessRate24h < expectedRate-1 || health.SuccessRate24h > expectedRate+1 {
		t.Errorf("預期成功率約 %.2f%%，得到 %.2f%%", expectedRate, health.SuccessRate24h)
	}
}

func TestMigration_CreatesSchemaTable(t *testing.T) {
	repo := setupTestDB(t)

	var count int
	err := repo.db.QueryRow(
		"SELECT count(*) FROM sqlite_master WHERE type='table' AND name='schema_migrations'",
	).Scan(&count)
	if err != nil {
		t.Fatalf("查詢失敗: %v", err)
	}
	if count != 1 {
		t.Errorf("預期 schema_migrations 表存在，但 count=%d", count)
	}
}

func TestMigration_RecordsVersion(t *testing.T) {
	repo := setupTestDB(t)

	var maxVersion int
	err := repo.db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&maxVersion)
	if err != nil {
		t.Fatalf("查詢版本失敗: %v", err)
	}
	if maxVersion < 2 {
		t.Errorf("預期至少 2 個 migration 版本，得到 %d", maxVersion)
	}
}

func TestMigration_Idempotent(t *testing.T) {
	dbPath := "test_idempotent.db"
	t.Cleanup(func() { os.Remove(dbPath) })

	repo1, err := NewSQLiteRepo(dbPath)
	if err != nil {
		t.Fatalf("第一次建立失敗: %v", err)
	}
	repo1.Close()

	repo2, err := NewSQLiteRepo(dbPath)
	if err != nil {
		t.Fatalf("第二次開啟失敗: %v", err)
	}
	defer repo2.Close()

	var count int
	err = repo2.db.QueryRow("SELECT count(*) FROM schema_migrations").Scan(&count)
	if err != nil {
		t.Fatalf("查詢失敗: %v", err)
	}
	if count != len(migrations) {
		t.Errorf("預期 %d 筆 migration 紀錄，得到 %d（有重複執行）", len(migrations), count)
	}
}

func TestGetCurrentVersion(t *testing.T) {
	repo := setupTestDB(t)

	version, err := repo.GetCurrentVersion()
	if err != nil {
		t.Fatalf("GetCurrentVersion 失敗: %v", err)
	}
	if version != len(migrations) {
		t.Errorf("預期版本 %d，得到 %d", len(migrations), version)
	}
}
