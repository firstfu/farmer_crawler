// internal/repository/sqlite.go
// 農產品價差雷達系統 — Repository 層
// 負責 SQLite 資料庫初始化、price_records 表的 CRUD 與 Upsert 操作
// Upsert 基於 (trade_date, market_code, crop_code) 唯一約束
// 使用 WAL 模式提升並發讀寫效能

package repository

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"

	"farmer_crawler/internal/domain"
)

// SQLiteRepo 封裝 SQLite 資料庫連線與操作方法
type SQLiteRepo struct {
	db *sql.DB
}

// NewSQLiteRepo 建立新的 SQLite Repository 實例
// 自動建立資料庫目錄、開啟連線、執行資料表遷移
func NewSQLiteRepo(dbPath string) (*SQLiteRepo, error) {
	dir := filepath.Dir(dbPath)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("建立資料庫目錄失敗: %w", err)
		}
	}

	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL")
	if err != nil {
		return nil, fmt.Errorf("開啟資料庫失敗: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("資料庫連線失敗: %w", err)
	}

	repo := &SQLiteRepo{db: db}
	if err := repo.migrate(); err != nil {
		return nil, fmt.Errorf("資料表遷移失敗: %w", err)
	}

	return repo, nil
}

// migrate 執行資料表建立與索引建立
func (r *SQLiteRepo) migrate() error {
	schema := `
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
	CREATE INDEX IF NOT EXISTS idx_trade_date ON price_records(trade_date);
	CREATE INDEX IF NOT EXISTS idx_market_code ON price_records(market_code);
	`
	_, err := r.db.Exec(schema)
	return err
}

// Close 關閉資料庫連線
func (r *SQLiteRepo) Close() error {
	return r.db.Close()
}

// Upsert 插入或更新一筆交易記錄
// 當 (trade_date, market_code, crop_code) 衝突時，更新現有記錄
func (r *SQLiteRepo) Upsert(record domain.PriceRecord) error {
	query := `
	INSERT INTO price_records (trade_date, crop_code, crop_name, market_code, market_name,
		upper_price, middle_price, lower_price, avg_price, volume)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(trade_date, market_code, crop_code)
	DO UPDATE SET
		crop_name = excluded.crop_name,
		upper_price = excluded.upper_price,
		middle_price = excluded.middle_price,
		lower_price = excluded.lower_price,
		avg_price = excluded.avg_price,
		volume = excluded.volume,
		created_at = CURRENT_TIMESTAMP
	`
	_, err := r.db.Exec(query,
		record.TradeDate, record.CropCode, record.CropName,
		record.MarketCode, record.MarketName,
		record.UpperPrice, record.MiddlePrice, record.LowerPrice,
		record.AvgPrice, record.Volume,
	)
	return err
}

// BatchUpsert 批次插入或更新多筆交易記錄
// 使用交易 (Transaction) 確保原子性，全部成功或全部回滾
func (r *SQLiteRepo) BatchUpsert(records []domain.PriceRecord) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("開始交易失敗: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
	INSERT INTO price_records (trade_date, crop_code, crop_name, market_code, market_name,
		upper_price, middle_price, lower_price, avg_price, volume)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(trade_date, market_code, crop_code)
	DO UPDATE SET
		crop_name = excluded.crop_name,
		upper_price = excluded.upper_price,
		middle_price = excluded.middle_price,
		lower_price = excluded.lower_price,
		avg_price = excluded.avg_price,
		volume = excluded.volume,
		created_at = CURRENT_TIMESTAMP
	`)
	if err != nil {
		return fmt.Errorf("準備語句失敗: %w", err)
	}
	defer stmt.Close()

	for _, rec := range records {
		_, err := stmt.Exec(
			rec.TradeDate, rec.CropCode, rec.CropName,
			rec.MarketCode, rec.MarketName,
			rec.UpperPrice, rec.MiddlePrice, rec.LowerPrice,
			rec.AvgPrice, rec.Volume,
		)
		if err != nil {
			return fmt.Errorf("插入記錄失敗 (market=%d, date=%s): %w", rec.MarketCode, rec.TradeDate, err)
		}
	}

	return tx.Commit()
}

// GetByDate 依交易日期與作物代碼查詢所有市場的交易記錄
// 結果按平均價格降序排列
func (r *SQLiteRepo) GetByDate(tradeDate, cropCode string) ([]domain.PriceRecord, error) {
	query := `
	SELECT id, trade_date, crop_code, crop_name, market_code, market_name,
		upper_price, middle_price, lower_price, avg_price, volume
	FROM price_records
	WHERE trade_date = ? AND crop_code = ?
	ORDER BY avg_price DESC
	`
	rows, err := r.db.Query(query, tradeDate, cropCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanRecords(rows)
}

// GetLatestAvgPrice 取得指定市場與作物的最新平均價格與日期
func (r *SQLiteRepo) GetLatestAvgPrice(marketCode int, cropCode string) (float64, string, error) {
	query := `
	SELECT avg_price, trade_date
	FROM price_records
	WHERE market_code = ? AND crop_code = ? AND avg_price > 0
	ORDER BY trade_date DESC
	LIMIT 1
	`
	var price float64
	var date string
	err := r.db.QueryRow(query, marketCode, cropCode).Scan(&price, &date)
	if err != nil {
		return 0, "", err
	}
	return price, date, nil
}

// GetTrendData 取得指定市場與作物的趨勢資料（最近 N 天）
// 結果按日期升序排列，適合繪製趨勢圖
func (r *SQLiteRepo) GetTrendData(marketCode int, cropCode string, days int) ([]domain.TrendPoint, error) {
	query := `
	SELECT trade_date, avg_price, volume
	FROM price_records
	WHERE market_code = ? AND crop_code = ? AND avg_price > 0
	ORDER BY trade_date DESC
	LIMIT ?
	`
	rows, err := r.db.Query(query, marketCode, cropCode, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var points []domain.TrendPoint
	for rows.Next() {
		var p domain.TrendPoint
		if err := rows.Scan(&p.Date, &p.AvgPrice, &p.Volume); err != nil {
			return nil, err
		}
		points = append(points, p)
	}

	// 反轉為日期升序
	for i, j := 0, len(points)-1; i < j; i, j = i+1, j-1 {
		points[i], points[j] = points[j], points[i]
	}

	return points, nil
}

// GetAllMarkets 取得所有不重複的市場代碼與名稱
func (r *SQLiteRepo) GetAllMarkets() ([]struct{ Code int; Name string }, error) {
	query := `SELECT DISTINCT market_code, market_name FROM price_records ORDER BY market_code`
	rows, err := r.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var markets []struct{ Code int; Name string }
	for rows.Next() {
		var m struct{ Code int; Name string }
		if err := rows.Scan(&m.Code, &m.Name); err != nil {
			return nil, err
		}
		markets = append(markets, m)
	}
	return markets, nil
}

// GetTrendDataByDateRange 取得指定市場與作物在日期範圍內的趨勢資料
// fromDate 與 toDate 皆為民國日期格式（例："115.02.01"）
// 結果按日期升序排列，適合繪製趨勢圖
func (r *SQLiteRepo) GetTrendDataByDateRange(marketCode int, cropCode, fromDate, toDate string) ([]domain.TrendPoint, error) {
	query := `
	SELECT trade_date, avg_price, volume
	FROM price_records
	WHERE market_code = ? AND crop_code = ? AND avg_price > 0
		AND trade_date >= ? AND trade_date <= ?
	ORDER BY trade_date ASC
	`
	rows, err := r.db.Query(query, marketCode, cropCode, fromDate, toDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var points []domain.TrendPoint
	for rows.Next() {
		var p domain.TrendPoint
		if err := rows.Scan(&p.Date, &p.AvgPrice, &p.Volume); err != nil {
			return nil, err
		}
		points = append(points, p)
	}

	return points, nil
}

// scanRecords 將 sql.Rows 掃描為 PriceRecord 切片
func scanRecords(rows *sql.Rows) ([]domain.PriceRecord, error) {
	var records []domain.PriceRecord
	for rows.Next() {
		var r domain.PriceRecord
		err := rows.Scan(
			&r.ID, &r.TradeDate, &r.CropCode, &r.CropName,
			&r.MarketCode, &r.MarketName,
			&r.UpperPrice, &r.MiddlePrice, &r.LowerPrice,
			&r.AvgPrice, &r.Volume,
		)
		if err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	return records, nil
}
