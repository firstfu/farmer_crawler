# Migration 版本管理機制 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 將現有的 `CREATE TABLE IF NOT EXISTS` 改為版本化 migration 機制，支援增量 schema 變更。

**Architecture:** 在 `sqlite.go` 內新增 `Migration` 結構與 `schema_migrations` 表，程式啟動時自動比對版本號並執行未套用的 migration。每個 migration 在 transaction 中執行確保原子性。

**Tech Stack:** Go `database/sql` + `mattn/go-sqlite3`，零新增依賴。

---

### Task 1: 寫 migration 機制的失敗測試

**Files:**
- Modify: `internal/repository/sqlite_test.go`

**Step 1: 寫 TestMigration_CreatesSchemaTable 測試**

在 `sqlite_test.go` 尾部新增：

```go
func TestMigration_CreatesSchemaTable(t *testing.T) {
	repo := setupTestDB(t)

	// schema_migrations 表應該存在
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

	// 應至少有 2 個已套用的 migration（price_records + crawl_status）
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

	// 第一次建立
	repo1, err := NewSQLiteRepo(dbPath)
	if err != nil {
		t.Fatalf("第一次建立失敗: %v", err)
	}
	repo1.Close()

	// 第二次開啟（應該不會重複執行 migration）
	repo2, err := NewSQLiteRepo(dbPath)
	if err != nil {
		t.Fatalf("第二次開啟失敗: %v", err)
	}
	defer repo2.Close()

	// 版本號不應重複
	var count int
	err = repo2.db.QueryRow("SELECT count(*) FROM schema_migrations").Scan(&count)
	if err != nil {
		t.Fatalf("查詢失敗: %v", err)
	}
	if count != len(migrations) {
		t.Errorf("預期 %d 筆 migration 紀錄，得到 %d（有重複執行）", len(migrations), count)
	}
}
```

**Step 2: 執行測試確認失敗**

```bash
export CGO_ENABLED=1 && export PATH="/c/msys64/ucrt64/bin:$PATH" && go test -v -run "TestMigration" ./internal/repository
```

預期：FAIL — `schema_migrations` 表不存在、`migrations` 變數未定義。

---

### Task 2: 實作 Migration 結構與 migrations 清單

**Files:**
- Modify: `internal/repository/sqlite.go`

**Step 1: 在 `SQLiteRepo` struct 下方新增 Migration 結構與清單**

在 `sqlite.go` 的 `SQLiteRepo` struct 之後，`NewSQLiteRepo` 之前，新增：

```go
// Migration 定義一個資料庫結構變更
type Migration struct {
	Version     int
	Description string
	Up          string
}

// migrations 所有的資料庫結構變更，按版本號遞增排列
// 新增 migration 時，在此 slice 末尾追加即可
var migrations = []Migration{
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
```

---

### Task 3: 改寫 migrate() 為版本化機制

**Files:**
- Modify: `internal/repository/sqlite.go`

**Step 1: 替換整個 migrate() 函式**

將原本的 `migrate()` 替換為：

```go
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

		fmt.Printf("[Migration] v%d: %s ✓\n", m.Version, m.Description)
	}

	return nil
}
```

**Step 2: 移除 `migrate()` 上方舊的 import（如有多餘的）**

確認 import 區塊只包含需要的套件（`database/sql`、`fmt`、`os`、`path/filepath`）。

**Step 3: 執行測試確認全部通過**

```bash
export CGO_ENABLED=1 && export PATH="/c/msys64/ucrt64/bin:$PATH" && go test -v ./internal/repository
```

預期：全部 PASS（包含原有的 7 個測試 + 新增的 3 個 migration 測試）。

---

### Task 4: 新增 GetCurrentVersion 查詢方法與測試

**Files:**
- Modify: `internal/repository/sqlite.go`
- Modify: `internal/repository/sqlite_test.go`

**Step 1: 在 sqlite_test.go 新增測試**

```go
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
```

**Step 2: 執行測試確認失敗**

```bash
export CGO_ENABLED=1 && export PATH="/c/msys64/ucrt64/bin:$PATH" && go test -v -run "TestGetCurrentVersion" ./internal/repository
```

預期：FAIL — `GetCurrentVersion` 未定義。

**Step 3: 在 sqlite.go 的 Close() 之後新增方法**

```go
// GetCurrentVersion 取得目前資料庫的 schema 版本號
func (r *SQLiteRepo) GetCurrentVersion() (int, error) {
	var version int
	err := r.db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&version)
	if err != nil {
		return 0, fmt.Errorf("查詢 schema 版本失敗: %w", err)
	}
	return version, nil
}
```

**Step 4: 執行全部測試確認通過**

```bash
export CGO_ENABLED=1 && export PATH="/c/msys64/ucrt64/bin:$PATH" && go test -v ./internal/repository
```

預期：全部 PASS。

---

### Task 5: 更新 TestNewSQLiteRepo_CreatesTable 與提交

**Files:**
- Modify: `internal/repository/sqlite_test.go`

**Step 1: 更新既有測試，增加 schema_migrations 表檢查**

將 `TestNewSQLiteRepo_CreatesTable` 改為同時檢查三張表：

```go
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
```

**Step 2: 執行全部測試確認通過**

```bash
export CGO_ENABLED=1 && export PATH="/c/msys64/ucrt64/bin:$PATH" && go test -v ./internal/repository
```

預期：全部 PASS。

**Step 3: 更新檔案頂部註解**

`sqlite.go` 頂部註解更新為：

```go
// internal/repository/sqlite.go
// 農產品價差雷達系統 — Repository 層
// 負責 SQLite 資料庫初始化、版本化 migration、price_records 表的 CRUD 與 Upsert 操作
// 使用 schema_migrations 表追蹤版本，支援增量 schema 變更
// Upsert 基於 (trade_date, market_code, crop_code) 唯一約束
// 使用 WAL 模式提升並發讀寫效能
```

**Step 4: 提交**

```bash
git add internal/repository/sqlite.go internal/repository/sqlite_test.go docs/plans/2026-03-03-migration-versioning-design.md docs/plans/2026-03-03-migration-versioning-impl-plan.md
git commit -m "feat: 加入版本化 migration 機制，替換原有 CREATE TABLE IF NOT EXISTS"
```
