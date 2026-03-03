# 農產品價差雷達系統 — Makefile
# Windows 環境使用：需透過 MSYS2 的 make 或 mingw32-make 執行
# 用法：make run / make crawl-today / make build / make test

SHELL := cmd.exe
GO := go
export CGO_ENABLED=1
export PATH := C:\msys64\ucrt64\bin;$(PATH)

# 啟動儀表板伺服器
run:
	$(GO) run ./cmd/server/

# 爬取今日資料
crawl-today:
	$(GO) run ./cmd/server/ crawl --today

# 爬取指定日期區間 (用法: make crawl-range FROM=115.02.01 TO=115.03.03)
crawl-range:
	$(GO) run ./cmd/server/ crawl --from $(FROM) --to $(TO)

# 編譯執行檔
build:
	$(GO) build -o farmer_crawler.exe ./cmd/server/

# 執行所有測試
test:
	$(GO) test ./... -v -count=1

# 清除編譯產物與資料庫
clean:
	del /Q farmer_crawler.exe 2>nul || true
	rmdir /S /Q data 2>nul || true
