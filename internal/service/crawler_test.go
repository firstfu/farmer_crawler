// internal/service/crawler_test.go
// 農產品價差雷達系統 — 爬蟲服務單元測試
// 測試日期轉換（西元→民國）、API URL 組合、JSON 回應解析等功能
// 所有測試皆為單元測試，不發送真實 HTTP 請求

package service

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// TestToMinguoDate 測試西元 time.Time 轉民國日期字串
// 民國年 = 西元年 - 1911，格式 "YYY.MM.DD"
func TestToMinguoDate(t *testing.T) {
	tests := []struct {
		name     string
		input    time.Time
		expected string
	}{
		{
			name:     "2026-03-03 → 115.03.03",
			input:    time.Date(2026, 3, 3, 0, 0, 0, 0, time.UTC),
			expected: "115.03.03",
		},
		{
			name:     "2025-01-15 → 114.01.15",
			input:    time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC),
			expected: "114.01.15",
		},
		{
			name:     "2025-12-01 → 114.12.01",
			input:    time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC),
			expected: "114.12.01",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ToMinguoDate(tc.input)
			if got != tc.expected {
				t.Errorf("ToMinguoDate(%v) = %q, want %q", tc.input, got, tc.expected)
			}
		})
	}
}

// TestBuildAPIURL 測試 API URL 正確組合 Crop、StartDate、EndDate、$top 參數
func TestBuildAPIURL(t *testing.T) {
	svc := &CrawlerService{
		apiURL:   "https://data.moa.gov.tw/Service/OpenData/FromM/FarmTransData.aspx",
		cropName: "茭白筍",
	}

	url := svc.BuildAPIURL("114.03.01", "114.03.03")

	// 檢查 URL 包含必要參數
	checks := []struct {
		name    string
		contain string
	}{
		{"基礎 URL", "https://data.moa.gov.tw/Service/OpenData/FromM/FarmTransData.aspx"},
		{"作物參數", "Crop=%E8%8C%AD%E7%99%BD%E7%AD%8D"}, // 茭白筍 URL encoded
		{"起始日期", "StartDate=114.03.01"},
		{"結束日期", "EndDate=114.03.03"},
		{"筆數上限", "%24top=500"}, // $top URL encoded
	}

	for _, c := range checks {
		t.Run(c.name, func(t *testing.T) {
			if !containsString(url, c.contain) {
				t.Errorf("BuildAPIURL 結果缺少 %s\nURL: %s", c.name, url)
			}
		})
	}
}

// TestParseAPIResponse 測試 JSON 解析與過濾休市記錄
func TestParseAPIResponse(t *testing.T) {
	// 模擬農糧署 API 回傳的 JSON（含中文鍵名）
	mockJSON := `[
		{
			"交易日期": "114.03.03",
			"種類代碼": "V",
			"作物代號": "SQ1",
			"作物名稱": "茭白筍-帶殼",
			"市場代號": 400,
			"市場名稱": "台中市",
			"上價": 100.0,
			"中價": 90.0,
			"下價": 80.0,
			"平均價": 90.0,
			"交易量": 500.0
		},
		{
			"交易日期": "114.03.03",
			"種類代碼": "V",
			"作物代號": "rest",
			"作物名稱": "休市",
			"市場代號": 109,
			"市場名稱": "台北一",
			"上價": 0,
			"中價": 0,
			"下價": 0,
			"平均價": 0,
			"交易量": 0
		},
		{
			"交易日期": "114.03.03",
			"種類代碼": "V",
			"作物代號": "SQ3",
			"作物名稱": "茭白筍-去殼",
			"市場代號": 800,
			"市場名稱": "高雄市",
			"上價": 150.0,
			"中價": 140.0,
			"下價": 130.0,
			"平均價": 140.0,
			"交易量": 300.0
		},
		{
			"交易日期": "114.03.03",
			"種類代碼": "V",
			"作物代號": "",
			"作物名稱": "",
			"市場代號": 200,
			"市場名稱": "空記錄",
			"上價": 0,
			"中價": 0,
			"下價": 0,
			"平均價": 0,
			"交易量": 0
		}
	]`

	svc := &CrawlerService{}
	records, err := svc.ParseAPIResponse([]byte(mockJSON))
	if err != nil {
		t.Fatalf("ParseAPIResponse 失敗: %v", err)
	}

	// 應過濾掉 CropCode="rest" 與 CropCode="" 的記錄，剩餘 2 筆
	if len(records) != 2 {
		t.Fatalf("預期 2 筆有效記錄（過濾 rest 與空值），得到 %d 筆", len(records))
	}

	// 驗證第一筆
	if records[0].CropCode != "SQ1" {
		t.Errorf("第一筆 CropCode 預期 SQ1，得到 %s", records[0].CropCode)
	}
	if records[0].MarketCode != 400 {
		t.Errorf("第一筆 MarketCode 預期 400，得到 %d", records[0].MarketCode)
	}
	if records[0].AvgPrice != 90.0 {
		t.Errorf("第一筆 AvgPrice 預期 90.0，得到 %f", records[0].AvgPrice)
	}

	// 驗證第二筆（SQ3 高雄市）
	if records[1].CropCode != "SQ3" {
		t.Errorf("第二筆 CropCode 預期 SQ3，得到 %s", records[1].CropCode)
	}
	if records[1].AvgPrice != 140.0 {
		t.Errorf("第二筆 AvgPrice 預期 140.0，得到 %f", records[1].AvgPrice)
	}
}

// TestParseAPIResponse_EmptyArray 測試空陣列回應不應產生錯誤
func TestParseAPIResponse_EmptyArray(t *testing.T) {
	svc := &CrawlerService{}
	records, err := svc.ParseAPIResponse([]byte("[]"))
	if err != nil {
		t.Fatalf("空陣列不應回傳錯誤，得到: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("空陣列應回傳 0 筆記錄，得到 %d 筆", len(records))
	}
}

// TestAPIRecordJSONMapping 測試中文 JSON 鍵名能正確反序列化到 apiRecordJSON 結構
func TestAPIRecordJSONMapping(t *testing.T) {
	// 測試 int 格式的市場代號
	jsonStr := `{
		"交易日期": "114.03.03",
		"種類代碼": "V",
		"作物代號": "SQ1",
		"作物名稱": "茭白筍-帶殼",
		"市場代號": 400,
		"市場名稱": "台中市",
		"上價": 100.5,
		"中價": 90.3,
		"下價": 80.1,
		"平均價": 90.3,
		"交易量": 1234.5
	}`

	var rec apiRecordJSON
	err := json.Unmarshal([]byte(jsonStr), &rec)
	if err != nil {
		t.Fatalf("JSON 反序列化失敗: %v", err)
	}

	if rec.CropCode != "SQ1" {
		t.Errorf("作物代號預期 SQ1，得到 %s", rec.CropCode)
	}
	code, _ := rec.MarketCode.Int64()
	if code != 400 {
		t.Errorf("市場代號預期 400，得到 %d", code)
	}
}

// TestAPIRecordJSONMapping_StringMarketCode 測試 API 回傳 string 格式的市場代號
func TestAPIRecordJSONMapping_StringMarketCode(t *testing.T) {
	jsonStr := `{
		"交易日期": "114.03.03",
		"種類代碼": "V",
		"作物代號": "SQ1",
		"作物名稱": "茭白筍-帶殼",
		"市場代號": "400",
		"市場名稱": "台中市",
		"上價": 100.0,
		"中價": 90.0,
		"下價": 80.0,
		"平均價": 90.0,
		"交易量": 500.0
	}`

	svc := &CrawlerService{}
	records, err := svc.ParseAPIResponse([]byte("[" + jsonStr + "]"))
	if err != nil {
		t.Fatalf("ParseAPIResponse 失敗: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("預期 1 筆紀錄，得到 %d 筆", len(records))
	}
	if records[0].MarketCode != 400 {
		t.Errorf("市場代號預期 400，得到 %d", records[0].MarketCode)
	}
}

// TestParseMinguoDate 測試民國日期字串轉 time.Time
func TestParseMinguoDate(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantYear  int
		wantMonth time.Month
		wantDay   int
		wantErr   bool
	}{
		{"正常轉換 115.03.03", "115.03.03", 2026, 3, 3, false},
		{"正常轉換 114.01.15", "114.01.15", 2025, 1, 15, false},
		{"正常轉換 114.12.31", "114.12.31", 2025, 12, 31, false},
		{"錯誤格式-缺少部分", "115.03", 0, 0, 0, true},
		{"錯誤格式-非數字", "abc.03.03", 0, 0, 0, true},
		{"錯誤格式-空字串", "", 0, 0, 0, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseMinguoDate(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Errorf("ParseMinguoDate(%q) 預期錯誤，但得到 %v", tc.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseMinguoDate(%q) 意外錯誤: %v", tc.input, err)
			}
			if got.Year() != tc.wantYear || got.Month() != tc.wantMonth || got.Day() != tc.wantDay {
				t.Errorf("ParseMinguoDate(%q) = %v, 預期 %d-%d-%d", tc.input, got, tc.wantYear, tc.wantMonth, tc.wantDay)
			}
		})
	}
}

// TestSplitDateRange 測試 21天/7天=3批
func TestSplitDateRange(t *testing.T) {
	// 115.01.01 ~ 115.01.21 = 21 天，batchDays=7 → 3 批
	batches, err := SplitDateRange("115.01.01", "115.01.21", 7)
	if err != nil {
		t.Fatalf("SplitDateRange 失敗: %v", err)
	}
	if len(batches) != 3 {
		t.Fatalf("預期 3 批，得到 %d 批", len(batches))
	}
	// 第 1 批: 01.01 ~ 01.07
	if batches[0].From != "115.01.01" || batches[0].To != "115.01.07" {
		t.Errorf("第 1 批預期 115.01.01~115.01.07，得到 %s~%s", batches[0].From, batches[0].To)
	}
	// 第 2 批: 01.08 ~ 01.14
	if batches[1].From != "115.01.08" || batches[1].To != "115.01.14" {
		t.Errorf("第 2 批預期 115.01.08~115.01.14，得到 %s~%s", batches[1].From, batches[1].To)
	}
	// 第 3 批: 01.15 ~ 01.21
	if batches[2].From != "115.01.15" || batches[2].To != "115.01.21" {
		t.Errorf("第 3 批預期 115.01.15~115.01.21，得到 %s~%s", batches[2].From, batches[2].To)
	}
}

// TestSplitDateRange_SingleDay 測試單日=1批
func TestSplitDateRange_SingleDay(t *testing.T) {
	batches, err := SplitDateRange("115.03.03", "115.03.03", 7)
	if err != nil {
		t.Fatalf("SplitDateRange 失敗: %v", err)
	}
	if len(batches) != 1 {
		t.Fatalf("預期 1 批，得到 %d 批", len(batches))
	}
	if batches[0].From != "115.03.03" || batches[0].To != "115.03.03" {
		t.Errorf("預期 115.03.03~115.03.03，得到 %s~%s", batches[0].From, batches[0].To)
	}
}

// TestSplitDateRange_EndBeforeStart 測試結束日期早於起始日期（錯誤案例）
func TestSplitDateRange_EndBeforeStart(t *testing.T) {
	_, err := SplitDateRange("115.03.10", "115.03.01", 7)
	if err == nil {
		t.Fatal("預期錯誤（結束日期早於起始日期），但未收到錯誤")
	}
}

// TestSplitDateRange_NotExactMultiple 測試 10天/7天=2批（7+3）
func TestSplitDateRange_NotExactMultiple(t *testing.T) {
	// 115.02.01 ~ 115.02.10 = 10 天，batchDays=7 → 2 批 (7+3)
	batches, err := SplitDateRange("115.02.01", "115.02.10", 7)
	if err != nil {
		t.Fatalf("SplitDateRange 失敗: %v", err)
	}
	if len(batches) != 2 {
		t.Fatalf("預期 2 批，得到 %d 批", len(batches))
	}
	// 第 1 批: 02.01 ~ 02.07
	if batches[0].From != "115.02.01" || batches[0].To != "115.02.07" {
		t.Errorf("第 1 批預期 115.02.01~115.02.07，得到 %s~%s", batches[0].From, batches[0].To)
	}
	// 第 2 批: 02.08 ~ 02.10
	if batches[1].From != "115.02.08" || batches[1].To != "115.02.10" {
		t.Errorf("第 2 批預期 115.02.08~115.02.10，得到 %s~%s", batches[1].From, batches[1].To)
	}
}

// containsString 輔助函式：檢查字串是否包含子字串
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestBackoffDuration 測試指數退避計算
func TestBackoffDuration(t *testing.T) {
	svc := &CrawlerService{}

	// attempt=0, base=30s → 30s + jitter(0~30s) → 30~60s
	d0 := svc.backoffDuration(0, 30*time.Second)
	if d0 < 30*time.Second || d0 > 60*time.Second {
		t.Errorf("attempt=0 預期 30~60s，得到 %v", d0)
	}

	// attempt=1, base=30s → 60s + jitter(0~30s) → 60~90s
	d1 := svc.backoffDuration(1, 30*time.Second)
	if d1 < 60*time.Second || d1 > 90*time.Second {
		t.Errorf("attempt=1 預期 60~90s，得到 %v", d1)
	}

	// attempt=2, base=30s → 120s + jitter(0~30s) → 120~150s
	d2 := svc.backoffDuration(2, 30*time.Second)
	if d2 < 120*time.Second || d2 > 150*time.Second {
		t.Errorf("attempt=2 預期 120~150s，得到 %v", d2)
	}
}

// TestBuildRequest 測試 HTTP 請求包含正確 Header
func TestBuildRequest(t *testing.T) {
	svc := &CrawlerService{
		apiURL:   "https://data.moa.gov.tw/Service/OpenData/FromM/FarmTransData.aspx",
		cropName: "茭白筍",
	}

	apiURL := svc.BuildAPIURL("115.03.01", "115.03.03")
	req, err := svc.buildRequest(apiURL)
	if err != nil {
		t.Fatalf("buildRequest 失敗: %v", err)
	}

	ua := req.Header.Get("User-Agent")
	if ua == "" {
		t.Error("User-Agent 不應為空")
	}
	if !containsString(ua, "Mozilla") {
		t.Errorf("User-Agent 應包含 Mozilla，得到: %s", ua)
	}

	al := req.Header.Get("Accept-Language")
	if !containsString(al, "zh-TW") {
		t.Errorf("Accept-Language 應包含 zh-TW，得到: %s", al)
	}

	ref := req.Header.Get("Referer")
	if ref == "" {
		t.Error("Referer 不應為空")
	}
}

// TestClassifyHTTPError 測試 HTTP 錯誤碼分類
func TestClassifyHTTPError(t *testing.T) {
	tests := []struct {
		statusCode    int
		wantRetryable bool
		wantCategory  string
	}{
		{429, true, "rate_limited"},
		{403, false, "blocked"},
		{500, true, "server_error"},
		{502, true, "server_error"},
		{503, true, "server_error"},
		{408, true, "timeout"},
		{400, false, "client_error"},
		{404, false, "client_error"},
		{200, false, "ok"},
	}

	for _, tc := range tests {
		t.Run(fmt.Sprintf("HTTP_%d", tc.statusCode), func(t *testing.T) {
			retryable, category := classifyHTTPError(tc.statusCode)
			if retryable != tc.wantRetryable {
				t.Errorf("HTTP %d: retryable 預期 %v，得到 %v", tc.statusCode, tc.wantRetryable, retryable)
			}
			if category != tc.wantCategory {
				t.Errorf("HTTP %d: category 預期 %q，得到 %q", tc.statusCode, tc.wantCategory, category)
			}
		})
	}
}
