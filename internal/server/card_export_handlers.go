package server

import (
	"encoding/csv"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"xianyu-go/internal/auth"
	"xianyu-go/internal/db"
)

// 单次导出的最大条数，避免一次拉爆内存。
const maxCardItemExport = 20000

// exportCardItems 导出卡密列表。POST /cards/{card_id}/items/export
// body: {ids?: number[], format: "txt"|"csv"|"xlsx", ...filters}
// 传了 ids 就按所选导出，否则按当前筛选条件导出。
func (s *Server) exportCardItems(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	id, err := strconv.ParseInt(chi.URLParam(r, "card_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效卡种ID")
		return
	}
	card, ok := s.requireCardOwner(w, r, id)
	if !ok {
		return
	}
	var req struct {
		IDs       []int64 `json:"ids"`
		Format    string  `json:"format"`
		Status    string  `json:"status"`
		Keyword   string  `json:"keyword"`
		BuyerID   string  `json:"buyer_id"`
		BuyerName string  `json:"buyer_name"`
		OrderID   string  `json:"order_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	format := strings.ToLower(strings.TrimSpace(req.Format))
	switch format {
	case "txt", "csv", "xlsx":
	case "":
		format = "csv"
	default:
		writeErr(w, http.StatusBadRequest, "导出格式只支持 txt、csv 或 xlsx")
		return
	}
	status := strings.TrimSpace(req.Status)
	if status != "" && !db.ValidCardItemStatus(status) {
		writeErr(w, http.StatusBadRequest, "非法卡密状态")
		return
	}

	var items []db.CardItem
	if len(req.IDs) > 0 {
		items, err = s.Store.Cards.ListItemsByIDs(r.Context(), sess.UserID, req.IDs)
	} else {
		var total int
		items, total, err = s.Store.Cards.ListItems(r.Context(), db.CardItemFilter{
			UserID: sess.UserID, CardID: card.ID, Status: status,
			Keyword: req.Keyword, BuyerID: req.BuyerID, BuyerName: req.BuyerName, OrderID: req.OrderID,
			Limit: maxCardItemExport, Offset: 0,
		})
		if err == nil && total > maxCardItemExport {
			writeErr(w, http.StatusBadRequest, "匹配条数超过单次导出上限，请先筛选或分批导出")
			return
		}
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "导出失败")
		return
	}
	if len(items) == 0 {
		writeErr(w, http.StatusBadRequest, "没有可导出的卡密")
		return
	}

	base := sanitizeDownloadName(card.Name)
	switch format {
	case "txt":
		lines := make([]string, 0, len(items))
		for _, item := range items {
			lines = append(lines, item.Content)
		}
		body := strings.Join(lines, "\r\n") + "\r\n"
		writeAttachment(w, base+".txt", "text/plain; charset=utf-8", []byte(body))
	case "xlsx":
		data, xerr := buildXLSX(card.Name, cardItemExportRows(items))
		if xerr != nil {
			writeErr(w, http.StatusInternalServerError, "生成 xlsx 失败")
			return
		}
		writeAttachment(w, base+".xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", data)
	default:
		buf := &strings.Builder{}
		cw := csv.NewWriter(buf)
		for _, row := range cardItemExportRows(items) {
			_ = cw.Write(row)
		}
		cw.Flush()
		writeAttachment(w, base+".csv", "text/csv; charset=utf-8", []byte("\ufeff"+buf.String()))
	}
}

// cardItemExportRows 输出统一的表头 + 数据行（csv/xlsx 共用）。
func cardItemExportRows(items []db.CardItem) [][]string {
	statusLabels := map[string]string{db.CardItemAvailable: "可用", db.CardItemUsed: "已用", db.CardItemVoid: "作废"}
	rows := make([][]string, 0, len(items)+1)
	rows = append(rows, []string{"卡密内容", "状态", "买家ID", "买家昵称", "订单号", "售出时间", "创建时间"})
	for _, item := range items {
		rows = append(rows, []string{
			item.Content, statusLabels[item.Status], item.BuyerID, item.BuyerName,
			item.OrderID, item.SoldAt, item.CreatedAt,
		})
	}
	return rows
}

// sanitizeDownloadName 生成安全的下载文件名（去掉路径分隔符与引号）。
func sanitizeDownloadName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "card-items"
	}
	replacer := strings.NewReplacer("\\", "_", "/", "_", "\"", "_", "'", "_", "\r", "", "\n", "")
	name = replacer.Replace(name)
	if len([]rune(name)) > 40 {
		name = string([]rune(name)[:40])
	}
	return name
}

func writeAttachment(w http.ResponseWriter, filename, contentType string, data []byte) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(filename))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
