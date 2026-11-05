package server

import (
	"archive/zip"
	"bytes"
	"testing"
)

// TestBuildXLSXRoundTrip 导出的 xlsx 必须能被仓库自己的解析器读回。
func TestBuildXLSXRoundTrip(t *testing.T) {
	rows := [][]string{
		{"卡密内容", "状态"},
		{"CODE-1", "可用"},
		{"CODE-2", "已用"},
	}
	data, err := buildXLSX("月卡", rows)
	if err != nil {
		t.Fatalf("buildXLSX: %v", err)
	}
	if len(data) == 0 || data[0] != 0x50 {
		t.Fatalf("不是 zip 数据: % x", data[:min(4, len(data))])
	}
	if _, err := zip.NewReader(bytes.NewReader(data), int64(len(data))); err != nil {
		t.Fatalf("zip 解析失败: %v", err)
	}
	parsed, err := parseXLSXPublishSheet(data)
	if err != nil {
		t.Fatalf("parseXLSXPublishSheet: %v", err)
	}
	if len(parsed) != 2 {
		t.Fatalf("rows=%d want 2: %+v", len(parsed), parsed)
	}
	if got := firstImportString(parsed[0], "卡密内容"); got != "CODE-1" {
		t.Fatalf("row0 content=%q", got)
	}
	if got := firstImportString(parsed[1], "状态"); got != "已用" {
		t.Fatalf("row1 status=%q", got)
	}
	// 特殊字符会被转义，读回后应与原始内容一致（zip 压缩过，不能直接搜字节）。
	raw := `a<b&c"d'e`
	data2, err := buildXLSX("s", [][]string{{"col"}, {raw}})
	if err != nil {
		t.Fatal(err)
	}
	parsed2, err := parseXLSXPublishSheet(data2)
	if err != nil {
		t.Fatalf("解析转义内容失败: %v", err)
	}
	if len(parsed2) != 1 || firstImportString(parsed2[0], "col") != raw {
		t.Fatalf("转义内容读回异常: %+v", parsed2)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
