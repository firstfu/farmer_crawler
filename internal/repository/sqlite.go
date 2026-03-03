// internal/repository/sqlite.go
// 農產品價差雷達系統 — Repository 層
// 負責 SQLite 資料庫初始化、版本化 migration、price_records 表的 CRUD 與 Upsert 操作
// 使用 schema_migrations 表追蹤版本，支援增量 schema 變更
// Upsert 基於 (trade_date, market_code, crop_code) 唯一約束
// 使用 WAL 模式提升並發讀寫效能

package repository

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"

	"farmer_crawler/internal/domain"
)

// SQLiteRepo 封裝 SQLite 資料庫連線與操作方法
type SQLiteRepo struct {
	db *sql.DB
}

// migration 定義一個資料庫結構變更
type migration struct {
	Version     int
	Description string
	Up          string
}

// migrations 所有的資料庫結構變更，按版本號遞增排列
// 新增 migration 時，在此 slice 末尾追加即可
var migrations = []migration{
	{
		Version:     1,
		Description: "建立 price_records 表與索引",
		Up: `
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
		`,
	},
	{
		Version:     2,
		Description: "建立 crawl_status 表與索引",
		Up: `
		CREATE TABLE IF NOT EXISTS crawl_status (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			crawl_time DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			date_from TEXT NOT NULL,
			date_to TEXT NOT NULL,
			record_count INTEGER DEFAULT 0,
			status TEXT NOT NULL,
			error_msg TEXT DEFAULT '',
			duration_ms INTEGER DEFAULT 0
		);
		CREATE INDEX IF NOT EXISTS idx_crawl_status_time ON crawl_status(crawl_time);
		`,
	},
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

// migrate 執行版本化資料庫遷移
// 1. 建立 schema_migrations 表（如不存在）
// 2. 查詢目前已套用的最大版本號
// 3. 依序執行尚未套用的 migration（每個在 transaction 中執行）
func (r *SQLiteRepo) migrate() error {
	// 建立 migration 追蹤表
	_, err := r.db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version     INTEGER PRIMARY KEY,
			description TEXT NOT NULL,
			applied_at  DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("建立 schema_migrations 表失敗: %w", err)
	}

	// 查詢目前版本
	var currentVersion int
	err = r.db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&currentVersion)
	if err != nil {
		return fmt.Errorf("查詢目前版本失敗: %w", err)
	}

	// 執行尚未套用的 migration
	for _, m := range migrations {
		if m.Version <= currentVersion {
			continue
		}

		tx, err := r.db.Begin()
		if err != nil {
			return fmt.Errorf("開始 migration v%d 交易失敗: %w", m.Version, err)
		}

		if _, err := tx.Exec(m.Up); err != nil {
			tx.Rollback()
			return fmt.Errorf("執行 migration v%d (%s) 失敗: %w", m.Version, m.Description, err)
		}

		if _, err := tx.Exec(
			"INSERT INTO schema_migrations (version, description) VALUES (?, ?)",
			m.Version, m.Description,
		); err != nil {
			tx.Rollback()
			return fmt.Errorf("記錄 migration v%d 版本失敗: %w", m.Version, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("提交 migration v%d 失敗: %w", m.Version, err)
		}

		log.Printf("[Migration] v%d: %s", m.Version, m.Description)
	}

	return nil
}

// Close 關閉資料庫連線
func (r *SQLiteRepo) Close() error {
	return r.db.Close()
}

// GetCurrentVersion 取得目前資料庫的 schema 版本號
func (r *SQLiteRepo) GetCurrentVersion() (int, error) {
	var version int
	err := r.db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&version)
	if err != nil {
		return 0, fmt.Errorf("查詢 schema 版本失敗: %w", err)
	}
	return version, nil
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
func (r *SQLiteRepo) GetAllMarkets() ([]struct {
	Code int
	Name string
}, error) {
	query := `SELECT DISTINCT market_code, market_name FROM price_records ORDER BY market_code`
	rows, err := r.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var markets []struct {
		Code int
		Name string
	}
	for rows.Next() {
		var m struct {
			Code int
			Name string
		}
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

// GetExistingTradeDates 查詢指定日期範圍內已有資料的交易日期集合
// fromDate 與 toDate 皆為民國日期格式（例："115.02.01"）
// 回傳 map[string]bool，key 為已存在的交易日期
func (r *SQLiteRepo) GetExistingTradeDates(fromDate, toDate string) (map[string]bool, error) {
	query := `SELECT DISTINCT trade_date FROM price_records WHERE trade_date >= ? AND trade_date <= ?`
	rows, err := r.db.Query(query, fromDate, toDate)
	if err != nil {
		return nil, fmt.Errorf("查詢已有交易日期失敗: %w", err)
	}
	defer rows.Close()

	dates := make(map[string]bool)
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		dates[d] = true
	}
	return dates, nil
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

// SaveCrawlStatus 儲存一筆爬取狀態記錄
func (r *SQLiteRepo) SaveCrawlStatus(status *domain.CrawlStatus) error {
	query := `
	INSERT INTO crawl_status (date_from, date_to, record_count, status, error_msg, duration_ms)
	VALUES (?, ?, ?, ?, ?, ?)
	`
	result, err := r.db.Exec(query,
		status.DateFrom, status.DateTo, status.RecordCount,
		status.Status, status.ErrorMsg, status.DurationMs,
	)
	if err != nil {
		return fmt.Errorf("儲存爬取狀態失敗: %w", err)
	}
	id, _ := result.LastInsertId()
	status.ID = id
	return nil
}

// GetRecentCrawlStatus 取得最近 N 筆爬取狀態（依時間降序）
func (r *SQLiteRepo) GetRecentCrawlStatus(limit int) ([]domain.CrawlStatus, error) {
	query := `
	SELECT id, crawl_time, date_from, date_to, record_count, status, error_msg, duration_ms
	FROM crawl_status
	ORDER BY crawl_time DESC
	LIMIT ?
	`
	rows, err := r.db.Query(query, limit)
	if err != nil {
		return nil, fmt.Errorf("查詢爬取狀態失敗: %w", err)
	}
	defer rows.Close()

	var statuses []domain.CrawlStatus
	for rows.Next() {
		var s domain.CrawlStatus
		if err := rows.Scan(&s.ID, &s.CrawlTime, &s.DateFrom, &s.DateTo,
			&s.RecordCount, &s.Status, &s.ErrorMsg, &s.DurationMs); err != nil {
			return nil, err
		}
		statuses = append(statuses, s)
	}
	return statuses, nil
}

// GetCrawlHealthSummary 取得最近 24 小時的爬蟲健康度摘要
func (r *SQLiteRepo) GetCrawlHealthSummary() (*domain.CrawlHealth, error) {
	health := &domain.CrawlHealth{}

	// 最近 24 小時的統計
	query := `
	SELECT
		COUNT(*) as total,
		SUM(CASE WHEN status != 'success' THEN 1 ELSE 0 END) as failed
	FROM crawl_status
	WHERE crawl_time >= datetime('now', '-24 hours')
	`
	err := r.db.QueryRow(query).Scan(&health.TotalCrawls24h, &health.FailedCrawls24h)
	if err != nil {
		return nil, fmt.Errorf("查詢健康度統計失敗: %w", err)
	}

	if health.TotalCrawls24h > 0 {
		health.SuccessRate24h = float64(health.TotalCrawls24h-health.FailedCrawls24h) / float64(health.TotalCrawls24h) * 100
	}

	// 最近一次爬取
	lastQuery := `
	SELECT crawl_time, status
	FROM crawl_status
	ORDER BY crawl_time DESC
	LIMIT 1
	`
	err = r.db.QueryRow(lastQuery).Scan(&health.LastCrawlTime, &health.LastStatus)
	if err != nil {
		// 無記錄時不回報錯誤
		health.LastStatus = "unknown"
	}

	return health, nil
}
