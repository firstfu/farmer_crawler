# Migration 版本管理機制設計

## 日期：2026-03-03

## 背景

目前 `migrate()` 使用 `CREATE TABLE IF NOT EXISTS`，只能建立不存在的表，無法修改已存在的表結構。
隨著功能持續擴充，需要一個輕量的版本化 migration 機制來管理 schema 變更。

## 方案選擇

選擇自建版本號 migration，理由：
- 零新增依賴，保持專案輕量
- 專案規模小（2 張表），不需重型工具
- 邏輯透明，全在 sqlite.go 內

## 設計

### schema_migrations 表

```sql
CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    description TEXT NOT NULL,
    applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

### Migration 結構

```go
type Migration struct {
    Version     int
    Description string
    Up          string
}
```

以有序 slice 註冊，版本號遞增，每個 migration 包含 Up SQL。

### 執行流程

1. 建立 schema_migrations 表（如不存在）
2. 查詢目前最大 version
3. 遍歷 migrations，跳過已套用的
4. 每個未套用的 migration 在 transaction 中執行 Up SQL + 記錄版本
5. 輸出 log

### 既有資料庫相容

- Migration 1、2 使用 `CREATE TABLE IF NOT EXISTS`，對已有表不會報錯
- 首次執行時自動建立 schema_migrations 並標記已套用

### 檔案變動

- `internal/repository/sqlite.go` — 改寫 migrate() 為版本化機制
- `internal/repository/sqlite_test.go` — 新增 migration 測試
