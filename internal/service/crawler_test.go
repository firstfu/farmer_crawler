// internal/service/crawler_test.go
// 農產品價差雷達系統 — 爬蟲服務單元測試
// 測試日期轉換（西元→民國）、API URL 組合、JSON 回應解析等功能
// 所有測試皆為單元測試，不發送真實 HTTP 請求

package service

import (
	"encoding/json"
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

	if rec.TradeDate != "114.03.03" {
		t.Errorf("交易日期預期 114.03.03，得到 %s", rec.TradeDate)
	}
	if rec.TypeCode != "V" {
		t.Errorf("種類代碼預期 V，得到 %s", rec.TypeCode)
	}
	if rec.CropCode != "SQ1" {
		t.Errorf("作物代號預期 SQ1，得到 %s", rec.CropCode)
	}
	if rec.CropName != "茭白筍-帶殼" {
		t.Errorf("作物名稱預期 茭白筍-帶殼，得到 %s", rec.CropName)
	}
	if rec.MarketCode != 400 {
		t.Errorf("市場代號預期 400，得到 %d", rec.MarketCode)
	}
	if rec.MarketName != "台中市" {
		t.Errorf("市場名稱預期 台中市，得到 %s", rec.MarketName)
	}
	if rec.UpperPrice != 100.5 {
		t.Errorf("上價預期 100.5，得到 %f", rec.UpperPrice)
	}
	if rec.MiddlePrice != 90.3 {
		t.Errorf("中價預期 90.3，得到 %f", rec.MiddlePrice)
	}
	if rec.LowerPrice != 80.1 {
		t.Errorf("下價預期 80.1，得到 %f", rec.LowerPrice)
	}
	if rec.AvgPrice != 90.3 {
		t.Errorf("平均價預期 90.3，得到 %f", rec.AvgPrice)
	}
	if rec.Volume != 1234.5 {
		t.Errorf("交易量預期 1234.5，得到 %f", rec.Volume)
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
