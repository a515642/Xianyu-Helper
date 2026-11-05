package server

import (
	"encoding/csv"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"xianyu-go/internal/auth"
	"xianyu-go/internal/db"
)

// 卡密售出台账查询与导出。台账按用户隔离，卡种删除后历史仍可读取。

// listCardSales 查询售出台账。GET /card-sales?card_id=&status=&search=&cookie_id=&order_id=&page=&page_size=
func (s *Server) listCardSales(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	query := r.URL.Query()
	page := atoiDefault(query.Get("page"), 1)
	if page < 1 {
		page = 1
	}
	pageSize := atoiDefault(query.Get("page_size"), 20)
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}
	status := strings.TrimSpace(query.Get("status"))
	if status != "" && !db.ValidCardSaleStatus(status) {
		writeErr(w, http.StatusBadRequest, "非法售出台账状态")
		return
	}
	cardID := int64(atoiDefault(query.Get("card_id"), 0))
	if cardID < 0 {
		cardID = 0
	}
	result, err := s.Store.Cards.ListCardSales(r.Context(), db.CardSaleFilter{
		UserID:   sess.UserID,
		CardID:   cardID,
		Status:   status,
		Search:   query.Get("search"),
		CookieID: query.Get("cookie_id"),
		OrderID:  query.Get("order_id"),
		Limit:    pageSize,
		Offset:   (page - 1) * pageSize,
	})
	if err != nil {
		s.Logger.Error("查询卡密售出台账失败", "user_id", sess.UserID, "err", err)
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	totalPages := 0
	if result.Total > 0 {
		totalPages = (result.Total + pageSize - 1) / pageSize
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"records": result.Records, "total": result.Total,
		"page": page, "page_size": pageSize, "total_pages": totalPages,
	})
}

// exportCardSales 导出某卡种的全部售出台账为 CSV。GET /cards/{card_id}/sales/export?status=
func (s *Server) exportCardSales(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	cardID, err := strconv.ParseInt(chi.URLParam(r, "card_id"), 10, 64)
	if err != nil || cardID <= 0 {
		writeErr(w, http.StatusBadRequest, "无效卡种ID")
		return
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status != "" && !db.ValidCardSaleStatus(status) {
		writeErr(w, http.StatusBadRequest, "非法售出台账状态")
		return
	}
	records, err := s.Store.Cards.AllCardSales(r.Context(), sess.UserID, cardID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "导出失败")
		return
	}
	if status != "" {
		filtered := records[:0]
		for _, record := range records {
			if record.Status == status {
				filtered = append(filtered, record)
			}
		}
		records = filtered
	}
	if len(records) == 0 {
		writeErr(w, http.StatusBadRequest, "没有可导出的售出台账")
		return
	}
	if len(records) > maxCardItemExport {
		writeErr(w, http.StatusBadRequest, "匹配条数超过单次导出上限，请先筛选")
		return
	}
	name := "card-sales"
	if records[0].CardName != "" {
		name = records[0].CardName
	}
	buf := &strings.Builder{}
	cw := csv.NewWriter(buf)
	_ = cw.Write([]string{"卡密内容", "状态", "卡种", "账号", "订单号", "商品ID", "买家ID", "扣减时间", "确认时间"})
	for _, record := range records {
		_ = cw.Write([]string{
			record.Content, cardSaleStatusLabel(record.Status), record.CardName, record.CookieID,
			record.OrderID, record.ItemID, record.BuyerID,
			formatSaleUnix(record.CreatedAt), formatSaleUnix(record.SoldAt),
		})
	}
	cw.Flush()
	// UTF-8 BOM 让 Excel 正确识别中文；用字节字面量避免源码中出现不可见字符。
	body := make([]byte, 0, 3+len(buf.String()))
	body = append(body, 0xEF, 0xBB, 0xBF)
	body = append(body, buf.String()...)
	writeAttachment(w, sanitizeDownloadName(name)+"-售出.csv", "text/csv; charset=utf-8", body)
}

func cardSaleStatusLabel(status string) string {
	if status == db.CardSaleSold {
		return "已售出"
	}
	return "待核对"
}

// formatSaleUnix 把 Unix 秒按北京时间格式化；零值表示尚未确认。
func formatSaleUnix(ts int64) string {
	if ts <= 0 {
		return ""
	}
	return time.Unix(ts, 0).In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format("2006-01-02 15:04:05")
}
