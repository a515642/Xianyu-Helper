package server

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"xianyu-go/internal/auth"
	"xianyu-go/internal/db"
)

const (
	// maxKeywordExpressions 限制单条规则可保存的表达式数量。
	maxKeywordExpressions = 50
	// maxKeywordExpressionLength 限制单个表达式的字符数，避免异常大的正则拖慢匹配。
	maxKeywordExpressionLength = 500
)

// normalizeKeywordInput 校验并规整关键词规则输入，返回可直接持久化的字段。
// keyword 为历史单表达式字段，最终会与 expressions 的首项保持一致。
func normalizeKeywordInput(keyword string, expressions []string, matchType string) (string, []string, string, error) {
	rawMatchType := strings.ToLower(strings.TrimSpace(matchType))
	switch rawMatchType {
	case "", "contains", "regexp", "exact", "fuzzy":
	default:
		return "", nil, "", errors.New("匹配模式必须是 contains 或 regexp")
	}
	normalizedType := db.NormalizeKeywordMatchType(rawMatchType)
	normalized := db.NormalizeKeywordExpressions(expressions, strings.TrimSpace(keyword))
	if len(normalized) == 0 {
		return "", nil, "", errors.New("关键词或匹配表达式不能为空")
	}
	if len(normalized) > maxKeywordExpressions {
		return "", nil, "", fmt.Errorf("匹配表达式不能超过 %d 条", maxKeywordExpressions)
	}
	for index, expression := range normalized {
		if utf8.RuneCountInString(expression) > maxKeywordExpressionLength {
			return "", nil, "", fmt.Errorf("第 %d 条匹配表达式不能超过 %d 个字符", index+1, maxKeywordExpressionLength)
		}
		if normalizedType == "regexp" {
			if _, err := regexp.Compile("(?i:" + expression + ")"); err != nil {
				return "", nil, "", fmt.Errorf("第 %d 条正则表达式无效", index+1)
			}
		}
	}
	return normalized[0], normalized, normalizedType, nil
}

// mountKeywordsReal 关键字端点。
func (s *Server) mountKeywordsReal(r chi.Router) {
	r.Get("/keywords/{cid}", s.listKeywords)
	r.Post("/keywords/{cid}", s.addKeyword)
	r.Get("/keywords-with-item-id/{cid}", s.listKeywordsWithItemID)
	r.Post("/keywords-with-item-id/{cid}", s.addKeywordWithItemID)
	r.Get("/keywords-with-type/{cid}", s.listKeywordsWithType)
	r.Put("/keywords-with-type/{cid}/{id}", s.updateKeywordByID)
	r.Delete("/keywords-with-type/{cid}/{id}", s.deleteKeywordByID)
	r.Delete("/keywords/{cid}/{index}", s.deleteKeyword)
}

func (s *Server) listKeywords(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cid")
	if _, ok := s.requireCookieOwner(w, r, cid); !ok {
		return
	}
	rows, err := s.Store.Keywords.AllRows(r.Context(), cid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, k := range rows {
		out = append(out, map[string]any{"keyword": k.Keyword, "reply": k.Reply})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) listKeywordsWithItemID(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cid")
	if _, ok := s.requireCookieOwner(w, r, cid); !ok {
		return
	}
	rows, err := s.Store.Keywords.AllRows(r.Context(), cid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, k := range rows {
		out = append(out, map[string]any{"keyword": k.Keyword, "reply": k.Reply, "item_id": k.ItemID})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) listKeywordsWithType(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cid")
	if _, ok := s.requireCookieOwner(w, r, cid); !ok {
		return
	}
	rows, err := s.Store.Keywords.AllRows(r.Context(), cid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, k := range rows {
		out = append(out, map[string]any{
			"id": k.ID, "keyword": k.Keyword, "reply": k.Reply, "item_id": k.ItemID,
			"type": k.Type, "image_url": k.ImageURL,
			"expressions": k.Expressions, "match_type": k.MatchType,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) addKeyword(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cid")
	if _, ok := s.requireCookieOwner(w, r, cid); !ok {
		return
	}
	var req struct {
		Keyword string `json:"keyword"`
		Reply   string `json:"reply"`
	}
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.Keyword) == "" {
		writeErr(w, http.StatusBadRequest, "keyword 必填")
		return
	}
	if strings.TrimSpace(req.Reply) == "" {
		writeErr(w, http.StatusBadRequest, "文字回复内容不能为空")
		return
	}
	if _, err := s.Store.Keywords.Add(r.Context(), cid, req.Keyword, req.Reply, "", "text", ""); err != nil {
		writeErr(w, http.StatusInternalServerError, "添加失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) addKeywordWithItemID(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cid")
	if _, ok := s.requireCookieOwner(w, r, cid); !ok {
		return
	}
	var req struct {
		Keyword     string   `json:"keyword"`
		Reply       string   `json:"reply"`
		ItemID      string   `json:"item_id"`
		Type        string   `json:"type"`
		ImageURL    string   `json:"image_url"`
		Expressions []string `json:"expressions"`
		MatchType   string   `json:"match_type"`
		Keywords    *[]struct {
			Keyword     string   `json:"keyword"`
			Reply       string   `json:"reply"`
			ItemID      string   `json:"item_id"`
			Type        string   `json:"type"`
			ImageURL    string   `json:"image_url"`
			Expressions []string `json:"expressions"`
			MatchType   string   `json:"match_type"`
		} `json:"keywords"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if req.Keywords != nil {
		rows := make([]db.KeywordRow, 0, len(*req.Keywords))
		for _, item := range *req.Keywords {
			keyword, expressions, matchType, err := normalizeKeywordInput(item.Keyword, item.Expressions, item.MatchType)
			if err != nil {
				writeErr(w, http.StatusBadRequest, err.Error())
				return
			}
			item.Type = strings.ToLower(strings.TrimSpace(item.Type))
			if item.Type == "" {
				item.Type = "text"
			}
			switch item.Type {
			case "text":
				if strings.TrimSpace(item.Reply) == "" {
					writeErr(w, http.StatusBadRequest, "文字回复内容不能为空")
					return
				}
				item.ImageURL = ""
			case "image":
				if strings.TrimSpace(item.ImageURL) == "" {
					writeErr(w, http.StatusBadRequest, "图片回复 URL 不能为空")
					return
				}
				item.Reply = ""
			default:
				writeErr(w, http.StatusBadRequest, "回复类型必须是 text 或 image")
				return
			}
			rows = append(rows, db.KeywordRow{
				CookieID:    cid,
				Keyword:     keyword,
				Reply:       item.Reply,
				ItemID:      item.ItemID,
				Type:        item.Type,
				ImageURL:    item.ImageURL,
				Expressions: expressions,
				MatchType:   matchType,
			})
		}
		if err := s.Store.Keywords.ReplaceForCookie(r.Context(), cid, rows); err != nil {
			writeErr(w, http.StatusInternalServerError, "保存失败")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true})
		return
	}
	keyword, expressions, matchType, err := normalizeKeywordInput(req.Keyword, req.Expressions, req.MatchType)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	req.Type = strings.ToLower(strings.TrimSpace(req.Type))
	if req.Type == "" {
		req.Type = "text"
	}
	if req.Type == "text" && strings.TrimSpace(req.Reply) == "" {
		writeErr(w, http.StatusBadRequest, "文字回复内容不能为空")
		return
	}
	if req.Type == "image" && strings.TrimSpace(req.ImageURL) == "" {
		writeErr(w, http.StatusBadRequest, "图片回复 URL 不能为空")
		return
	}
	if req.Type != "text" && req.Type != "image" {
		writeErr(w, http.StatusBadRequest, "回复类型必须是 text 或 image")
		return
	}
	if req.Type == "image" {
		req.Reply = ""
	} else {
		req.ImageURL = ""
	}
	id, err := s.Store.Keywords.AddRow(r.Context(), db.KeywordRow{
		CookieID: cid, Keyword: keyword, Reply: req.Reply, ItemID: req.ItemID,
		Type: req.Type, ImageURL: req.ImageURL, Expressions: expressions, MatchType: matchType,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "添加失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "id": id})
}

func (s *Server) updateKeywordByID(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cid")
	if _, ok := s.requireCookieOwner(w, r, cid); !ok {
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "无效关键词ID")
		return
	}
	var req struct {
		Keyword     string   `json:"keyword"`
		Reply       string   `json:"reply"`
		ItemID      string   `json:"item_id"`
		Type        string   `json:"type"`
		ImageURL    string   `json:"image_url"`
		Expressions []string `json:"expressions"`
		MatchType   string   `json:"match_type"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	keyword, expressions, matchType, err := normalizeKeywordInput(req.Keyword, req.Expressions, req.MatchType)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	req.Type = strings.ToLower(strings.TrimSpace(req.Type))
	if req.Type == "" {
		req.Type = "text"
	}
	switch req.Type {
	case "text":
		if strings.TrimSpace(req.Reply) == "" {
			writeErr(w, http.StatusBadRequest, "文字回复内容不能为空")
			return
		}
		req.ImageURL = ""
	case "image":
		if strings.TrimSpace(req.ImageURL) == "" {
			writeErr(w, http.StatusBadRequest, "图片回复 URL 不能为空")
			return
		}
		req.Reply = ""
	default:
		writeErr(w, http.StatusBadRequest, "回复类型必须是 text 或 image")
		return
	}
	err = s.Store.Keywords.UpdateByID(r.Context(), db.KeywordRow{
		ID: id, CookieID: cid, Keyword: keyword, Reply: req.Reply,
		ItemID: req.ItemID, Type: req.Type, ImageURL: req.ImageURL,
		Expressions: expressions, MatchType: matchType,
	})
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "关键字不存在")
			return
		}
		writeErr(w, http.StatusInternalServerError, "保存失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) deleteKeywordByID(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cid")
	if _, ok := s.requireCookieOwner(w, r, cid); !ok {
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "无效关键词ID")
		return
	}
	if err := s.Store.Keywords.DeleteByID(r.Context(), cid, id); err != nil {
		writeErr(w, http.StatusNotFound, "关键字不存在")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) deleteKeyword(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cid")
	if _, ok := s.requireCookieOwner(w, r, cid); !ok {
		return
	}
	index := atoiDefault(chi.URLParam(r, "index"), -1)
	if err := s.Store.Keywords.DeleteByIndex(r.Context(), cid, index); err != nil {
		writeErr(w, http.StatusNotFound, "关键字不存在")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// ---- 指定商品回复 ----
func (s *Server) mountItemRepliesReal(r chi.Router) {
	r.Get("/itemReplays", s.listItemReplies)
	r.Get("/item-reply/{cookie_id}/{item_id}", s.getItemReply)
	r.Put("/item-reply/{cookie_id}/{item_id}", s.setItemReply)
	r.Delete("/item-reply/{cookie_id}/{item_id}", s.deleteItemReply)
}

func (s *Server) listItemReplies(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	all, _ := s.Store.Cookies.AllForUser(r.Context(), sess.UserID)
	var result []map[string]any
	for cid := range all {
		rows, _ := s.Store.ItemReps.AllForUser(r.Context(), cid)
		for _, ir := range rows {
			result = append(result, map[string]any{
				"item_id": ir.ItemID, "cookie_id": ir.CookieID, "reply_content": ir.ReplyContent,
			})
		}
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) getItemReply(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cookie_id")
	if _, ok := s.requireCookieOwner(w, r, cid); !ok {
		return
	}
	itemID := chi.URLParam(r, "item_id")
	ir, err := s.Store.ItemReps.Get(r.Context(), cid, itemID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"reply_content": ""})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"item_id": ir.ItemID, "cookie_id": ir.CookieID, "reply_content": ir.ReplyContent,
	})
}

func (s *Server) setItemReply(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cookie_id")
	if _, ok := s.requireCookieOwner(w, r, cid); !ok {
		return
	}
	itemID := chi.URLParam(r, "item_id")
	var req struct {
		ReplyContent string `json:"reply_content"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if err := s.Store.ItemReps.Set(r.Context(), cid, itemID, req.ReplyContent); err != nil {
		writeErr(w, http.StatusInternalServerError, "保存失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) deleteItemReply(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cookie_id")
	if _, ok := s.requireCookieOwner(w, r, cid); !ok {
		return
	}
	itemID := chi.URLParam(r, "item_id")
	if err := s.Store.ItemReps.Delete(r.Context(), cid, itemID); err != nil {
		writeErr(w, http.StatusInternalServerError, "删除失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}
