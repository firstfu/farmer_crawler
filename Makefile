# =============================================================================
# 農產品價差雷達系統 — Makefile
# Windows 環境使用：需透過 GnuWin32 make / MSYS2 make 執行
# CGO 需求：SQLite 驅動需要 GCC（MSYS2 ucrt64）
# =============================================================================
# 用法：
#   make help            — 顯示所有可用指令
#   make run             — 啟動儀表板伺服器
#   make build           — 編譯執行檔
#   make test            — 執行所有測試
#   make crawl-today     — 爬取今日資料
#   make lint            — 執行靜態分析
#   make clean           — 清除編譯產物
# =============================================================================

BINARY     := farmer_crawler.exe
MODULE     := farmer_crawler
MAIN_PKG   := ./cmd/server/
CONFIG     := config.yaml
DB_DIR     := data
PORT       := 8080

# CGO 環境（SQLite 需要）
export CGO_ENABLED := 1
export PATH := C:\msys64\ucrt64\bin;$(PATH)

# Go 指令（直接使用 go，需確保 Go 在系統 PATH 中）
GO := go

# 版本資訊（從 git 取得）
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
GIT_BRANCH := $(shell git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "unknown")

# Go 編譯旗標
LDFLAGS := -ldflags "-s -w -X main.version=$(GIT_COMMIT) -X main.branch=$(GIT_BRANCH)"

# =============================================================================
# 預設目標
# =============================================================================

.DEFAULT_GOAL := help

.PHONY: help run build test test-v test-cover test-race \
        crawl-today crawl-range \
        lint vet fmt \
        clean clean-all deps tidy \
        dev check info db-reset

# =============================================================================
# 說明
# =============================================================================

## help: 顯示所有可用指令
help:
	@echo ""
	@echo "============================================"
	@echo " 農產品價差雷達系統 — Make 指令集"
	@echo "============================================"
	@echo ""
	@echo " --- 執行 ---"
	@echo "  make run             啟動儀表板伺服器 (port $(PORT))"
	@echo "  make dev             開發模式（同 run）"
	@echo ""
	@echo " --- 編譯 ---"
	@echo "  make build           編譯執行檔 $(BINARY)"
	@echo "  make clean           清除編譯產物"
	@echo "  make clean-all       清除編譯產物 + 資料庫"
	@echo ""
	@echo " --- 測試 ---"
	@echo "  make test            執行所有測試"
	@echo "  make test-v          執行測試（詳細輸出）"
	@echo "  make test-cover      執行測試 + 覆蓋率報告"
	@echo "  make test-race       執行測試 + 競態偵測"
	@echo ""
	@echo " --- 爬蟲 ---"
	@echo "  make crawl-today     爬取今日資料"
	@echo "  make crawl-range FROM=115.02.01 TO=115.03.03"
	@echo ""
	@echo " --- 程式碼品質 ---"
	@echo "  make fmt             格式化程式碼"
	@echo "  make vet             靜態分析"
	@echo "  make lint            fmt + vet 全套檢查"
	@echo "  make check           lint + test 完整檢查"
	@echo ""
	@echo " --- 依賴管理 ---"
	@echo "  make deps            下載依賴套件"
	@echo "  make tidy            整理 go.mod"
	@echo ""
	@echo " --- 工具 ---"
	@echo "  make info            顯示專案環境資訊"
	@echo "  make db-reset        重置資料庫"
	@echo ""

# =============================================================================
# 執行
# =============================================================================

## run: 啟動儀表板伺服器
run:
	$(GO) run $(MAIN_PKG)

## dev: 開發模式啟動（同 run，語意更明確）
dev: run

# =============================================================================
# 編譯
# =============================================================================

## build: 編譯執行檔
build:
	@echo "[BUILD] 編譯 $(BINARY) ..."
	$(GO) build $(LDFLAGS) -o $(BINARY) $(MAIN_PKG)
	@echo "[BUILD] 完成！"

# =============================================================================
# 測試
# =============================================================================

## test: 執行所有測試
test:
	$(GO) test ./... -count=1

## test-v: 執行所有測試（詳細輸出）
test-v:
	$(GO) test ./... -v -count=1

## test-cover: 執行測試並產生覆蓋率報告
test-cover:
	$(GO) test ./... -count=1 -coverprofile=coverage.out
	$(GO) tool cover -func=coverage.out
	@echo ""
	@echo "[COVER] 報告已產生：coverage.out"
	@echo "[COVER] 瀏覽器檢視：go tool cover -html=coverage.out"

## test-race: 執行測試並啟用競態偵測器
test-race:
	$(GO) test ./... -count=1 -race

# =============================================================================
# 爬蟲指令
# =============================================================================

## crawl-today: 爬取今日資料
crawl-today:
	$(GO) run $(MAIN_PKG) crawl --today

## crawl-range: 爬取指定日期區間 (用法: make crawl-range FROM=115.02.01 TO=115.03.03)
crawl-range:
ifndef FROM
	$(error 請指定 FROM，例: make crawl-range FROM=115.02.01 TO=115.03.03)
endif
ifndef TO
	$(error 請指定 TO，例: make crawl-range FROM=115.02.01 TO=115.03.03)
endif
	$(GO) run $(MAIN_PKG) crawl --from $(FROM) --to $(TO)

# =============================================================================
# 程式碼品質
# =============================================================================

## fmt: 格式化所有 Go 原始碼
fmt:
	@echo "[FMT] 格式化程式碼..."
	$(GO) fmt ./...

## vet: 靜態分析
vet:
	@echo "[VET] 靜態分析中..."
	$(GO) vet ./...

## lint: fmt + vet 完整程式碼檢查
lint: fmt vet
	@echo "[LINT] 檢查通過！"

## check: 完整檢查（lint + test）
check: lint test
	@echo ""
	@echo "[CHECK] 全部通過！可安心提交。"

# =============================================================================
# 依賴管理
# =============================================================================

## deps: 下載所有依賴套件
deps:
	@echo "[DEPS] 下載依賴..."
	$(GO) mod download

## tidy: 整理 go.mod / go.sum
tidy:
	@echo "[TIDY] 整理模組依賴..."
	$(GO) mod tidy

# =============================================================================
# 清除
# =============================================================================

## clean: 清除編譯產物
clean:
	@echo "[CLEAN] 清除編譯產物..."
	-rm -f $(BINARY)
	-rm -f coverage.out
	@echo "[CLEAN] 完成。"

## clean-all: 清除編譯產物 + 資料庫
clean-all: clean
	@echo "[CLEAN] 清除資料庫目錄..."
	-rm -rf $(DB_DIR)
	@echo "[CLEAN-ALL] 完成。"

## db-reset: 重置資料庫（刪除後重建目錄）
db-reset:
	@echo "[DB] 重置資料庫..."
	-rm -rf $(DB_DIR)
	mkdir -p $(DB_DIR)
	@echo "[DB] 資料庫已重置，下次啟動時會自動建立。"

# =============================================================================
# 工具
# =============================================================================

## info: 顯示專案環境資訊
info:
	@echo ""
	@echo "=== 專案環境資訊 ==="
	@echo "Module:     $(MODULE)"
	@echo "Binary:     $(BINARY)"
	@echo "Git Commit: $(GIT_COMMIT)"
	@echo "Git Branch: $(GIT_BRANCH)"
	@echo ""
	@$(GO) version
	@$(GO) env GOPATH GOROOT CGO_ENABLED CC
	@echo ""
