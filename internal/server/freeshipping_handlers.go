package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// freeshippingOrder 对指定的小刀订单手动执行一次免拼发货。
// 免拼是账号级能力，不依赖自动化规则；这里用于人工补发或诊断。
func (s *Server) freeshippingOrder(w http.ResponseWriter, r *http.Request) {
	order, ok := s.requireOrderOwner(w, r, chi.URLParam(r, "order_id"))
	if !ok {
		return
	}
	if s.automation == nil {
		writeErr(w, http.StatusServiceUnavailable, "自动化中心未启用")
		return
	}
	if order.ItemID == "" || order.BuyerID == "" {
		writeErr(w, http.StatusBadRequest, "订单缺少商品或买家ID，无法免拼")
		return
	}
	if err := s.automation.FreeshippingOrder(r.Context(), order.CookieID, order.OrderID, order.ItemID, order.BuyerID); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"success": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}
