package server

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"xianyu-go/internal/auth"
	"xianyu-go/internal/automation"
	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
)

const (
	maxPublishBatchRows    = 50
	publishBatchLease      = 5 * time.Minute
	publishBatchJobTimeout = 2 * time.Hour
)

type postPublishError struct{ err error }

func (e *postPublishError) Error() string { return e.err.Error() }
func (e *postPublishError) Unwrap() error { return e.err }

type uncertainRemotePublishError struct{ err error }

func (e *uncertainRemotePublishError) Error() string { return e.err.Error() }
func (e *uncertainRemotePublishError) Unwrap() error { return e.err }

type publishBatchPreviewRow struct {
	RowNo      int                     `json:"row_no"`
	Valid      bool                    `json:"valid"`
	Errors     []string                `json:"errors,omitempty"`
	CookieID   string                  `json:"cookie_id"`
	Title      string                  `json:"title"`
	Price      string                  `json:"price"`
	Quantity   int                     `json:"quantity"`
	Images     []string                `json:"images"`
	Category   mtop.PublishCategory    `json:"category"`
	Automation publishAutomationConfig `json:"automation"`
}

type publishBatchParsedRow struct {
	RowNo         int
	CookieID      string
	Title         string
	Description   string
	Price         string
	OriginalPrice string
	Quantity      int
	PostageMode   string
	Postage       string
	Images        []string
	Category      mtop.PublishCategory
	Automation    publishAutomationConfig
	Errors        []string
	Raw           map[string]any
}

type publishAutomationConfig struct {
	SchemaVersion int                     `json:"schema_version"`
	PaidDelivery  publishRuleReference    `json:"paid_delivery"`
	ReviewGift    publishRuleReference    `json:"review_gift"`
	ReviewRequest publishReviewRequestCfg `json:"review_request"`
}

type publishRuleReference struct {
	RuleID             int64              `json:"rule_id"`
	SourceRuleSnapshot *db.AutomationRule `json:"source_rule_snapshot,omitempty"`
	ParseError         string             `json:"-"`
}

type publishReviewRequestCfg struct {
	Enabled           bool   `json:"enabled"`
	AfterShippedHours int    `json:"after_shipped_hours"`
	Message           string `json:"message"`
	MaxAttempts       int    `json:"max_attempts"`
	DelaySeconds      int    `json:"delay_seconds"`
}

type publishCategoryRecommender interface {
	RecommendPublishCategory(ctx context.Context, cookiesStr, keyword string) (mtop.PublishCategory, string, error)
}

func (s *Server) recommendItemPublishCategory(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CookieID string `json:"cookie_id"`
		Keyword  string `json:"keyword"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	req.CookieID = strings.TrimSpace(req.CookieID)
	req.Keyword = strings.TrimSpace(req.Keyword)
	if req.CookieID == "" {
		writeErr(w, http.StatusBadRequest, "请先选择发布账号")
		return
	}
	if req.Keyword == "" {
		writeErr(w, http.StatusBadRequest, "请输入类目关键词")
		return
	}
	_, userID, ok := s.cookieForCurrentUser(w, r, req.CookieID)
	if !ok {
		return
	}
	recommender, ok := s.mtopClient().(publishCategoryRecommender)
	if !ok {
		writeErr(w, http.StatusNotImplemented, "当前 MTOP 客户端不支持类目推荐")
		return
	}

	credentialUnlock := s.Store.LockAccountCredentials(req.CookieID)
	runtimeCookie := ""
	defer func() {
		credentialUnlock()
		if runtimeCookie != "" {
			s.updateRunningCookie(context.Background(), req.CookieID, runtimeCookie)
		}
	}()
	latest, err := s.Store.Cookies.GetDetails(r.Context(), req.CookieID)
	if err != nil || latest == nil || latest.UserID != userID || !hasStoredCookieCredential(latest) {
		writeErr(w, http.StatusConflict, "账号凭证已变化，请重试")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	mtopCtx, cookieSession := withMTopCookieSnapshot(ctx, latest)
	category, updatedCookies, callErr := recommender.RecommendPublishCategory(mtopCtx, latest.Value, req.Keyword)
	value, valueChanged, handled, persistErr := s.persistMTopCookieSessionLocked(r.Context(), latest, cookieSession)
	if persistErr != nil {
		writeErr(w, http.StatusInternalServerError, "保存账号登录状态失败")
		return
	}
	if !handled && updatedCookies != "" && updatedCookies != latest.Value {
		if err := s.Store.Cookies.UpdateValueOwned(r.Context(), req.CookieID, updatedCookies, userID); err != nil {
			writeErr(w, http.StatusInternalServerError, "保存账号登录状态失败")
			return
		}
		runtimeCookie = updatedCookies
	} else if handled && valueChanged {
		runtimeCookie = value
	}
	if callErr != nil {
		if errors.Is(callErr, mtop.ErrPublishCategoryUnrecognized) {
			writeErr(w, http.StatusNotFound, "没有匹配到可发布类目，请换一个关键词")
			return
		}
		writeErr(w, http.StatusBadGateway, callErr.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "category": category})
}

func (s *Server) previewItemPublishBatch(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	s.cleanupExpiredPublishUploads(r.Context())
	// 表格最大 20 MiB，图片压缩包最大 200 MiB，额外预留 multipart 元数据空间。
	r.Body = http.MaxBytesReader(w, r.Body, maxItemPublishBatchBytes)
	// #nosec G120 -- 请求体已由 MaxBytesReader 限制。
	if err := r.ParseMultipartForm(maxItemPublishBatchParseBytes); err != nil {
		writeErr(w, http.StatusBadRequest, "解析上传文件失败")
		return
	}
	defaultCookieID := strings.TrimSpace(r.FormValue("default_cookie_id"))
	if defaultCookieID == "" {
		writeErr(w, http.StatusBadRequest, "请选择默认发布账号")
		return
	}
	if !s.cookieOwnedByUser(r.Context(), sess.UserID, defaultCookieID) {
		writeErr(w, http.StatusForbidden, "默认账号不属于当前用户")
		return
	}
	fallbackCategory := mtop.PublishCategory{
		CatID:        strings.TrimSpace(r.FormValue("fallback_category_id")),
		CatName:      strings.TrimSpace(r.FormValue("fallback_category_name")),
		ChannelCatID: strings.TrimSpace(r.FormValue("fallback_channel_category_id")),
		TBCatID:      strings.TrimSpace(r.FormValue("fallback_tb_category_id")),
	}
	var batchLocation mtop.PublishLocation
	locationJSON := strings.TrimSpace(r.FormValue("location"))
	if locationJSON != "" {
		if json.Unmarshal([]byte(locationJSON), &batchLocation) != nil {
			writeErr(w, http.StatusBadRequest, "发货地格式错误，请重新定位")
			return
		}
	}
	locationBytes, _ := json.Marshal(batchLocation)
	hasDefaultCategory := fallbackCategory.CatID != "" || fallbackCategory.CatName != "" || fallbackCategory.ChannelCatID != "" || fallbackCategory.TBCatID != ""
	if hasDefaultCategory && (fallbackCategory.CatID == "" || fallbackCategory.CatName == "" || fallbackCategory.ChannelCatID == "") {
		writeErr(w, http.StatusBadRequest, "默认类目信息不完整，请重新通过关键词获取")
		return
	}
	source, sourceHeader, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "缺少商品表格文件")
		return
	}
	defer source.Close()
	sourceBytes, tooLarge, err := readLimitedBytes(source, 20<<20)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "读取商品表格失败")
		return
	}
	if tooLarge {
		writeErr(w, http.StatusBadRequest, "商品表格不能超过 20 MiB")
		return
	}
	batchID := "batch_" + randomHex(12)
	uploadDir := filepath.Join(s.publishUploadRoot(), "publish_batches", batchID)
	if err := os.MkdirAll(uploadDir, 0o750); err != nil {
		writeErr(w, http.StatusInternalServerError, "创建上传目录失败")
		return
	}
	keepUpload := false
	defer func() {
		if !keepUpload {
			_ = os.RemoveAll(uploadDir)
		}
	}()
	sourceName := safeBaseName(sourceHeader.Filename)
	if sourceName == "" {
		sourceName = "products.csv"
	}
	if err := writeFileWithinRoot(uploadDir, sourceName, sourceBytes); err != nil {
		writeErr(w, http.StatusInternalServerError, "保存商品表格失败")
		return
	}

	if zipFile, zipHeader, err := r.FormFile("images_zip"); err == nil {
		defer zipFile.Close()
		zipBytes, tooLarge, err := readLimitedBytes(zipFile, 200<<20)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "读取图片 zip 失败")
			return
		}
		if tooLarge {
			writeErr(w, http.StatusBadRequest, "图片 zip 不能超过 200 MiB")
			return
		}
		zipName := safeBaseName(zipHeader.Filename)
		if zipName == "" {
			zipName = "images.zip"
		}
		if err := writeFileWithinRoot(uploadDir, zipName, zipBytes); err != nil {
			writeErr(w, http.StatusInternalServerError, "保存图片 zip 失败")
			return
		}
		if err := extractPublishImagesZip(zipBytes, uploadDir); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	maps, err := parsePublishSheetBytesWithLimit(sourceBytes, sourceName, maxPublishBatchRows)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(maps) > maxPublishBatchRows {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("单个批次最多支持 %d 条商品", maxPublishBatchRows))
		return
	}
	parsed := s.parsePublishRows(r.Context(), sess.UserID, defaultCookieID, uploadDir, fallbackCategory, maps)
	rows := make([]db.ItemPublishBatchRow, 0, len(parsed))
	previewRows := make([]publishBatchPreviewRow, 0, len(parsed))
	valid, invalid := 0, 0
	for _, p := range parsed {
		isValid := len(p.Errors) == 0
		if isValid {
			valid++
		} else {
			invalid++
		}
		imagesJSON, _ := json.Marshal(p.Images)
		categoryJSON, _ := json.Marshal(p.Category)
		automationJSON, _ := json.Marshal(p.Automation)
		rawJSON, _ := json.Marshal(p.Raw)
		status := "pending"
		errMsg := ""
		if !isValid {
			status = "failed"
			errMsg = strings.Join(p.Errors, "；")
		}
		rows = append(rows, db.ItemPublishBatchRow{
			RowNo:          p.RowNo,
			CookieID:       p.CookieID,
			Title:          p.Title,
			Description:    p.Description,
			Price:          p.Price,
			OriginalPrice:  p.OriginalPrice,
			Quantity:       p.Quantity,
			PostageMode:    p.PostageMode,
			Postage:        p.Postage,
			ImagesJSON:     string(imagesJSON),
			CategoryJSON:   string(categoryJSON),
			AutomationJSON: string(automationJSON),
			Status:         status,
			ErrorMessage:   errMsg,
			FailureKind:    map[bool]string{true: "", false: "validation"}[isValid],
			RawJSON:        string(rawJSON),
		})
		previewRows = append(previewRows, publishBatchPreviewRow{
			RowNo:      p.RowNo,
			Valid:      isValid,
			Errors:     p.Errors,
			CookieID:   p.CookieID,
			Title:      p.Title,
			Price:      p.Price,
			Quantity:   p.Quantity,
			Images:     p.Images,
			Category:   p.Category,
			Automation: p.Automation,
		})
	}
	if len(rows) == 0 {
		writeErr(w, http.StatusBadRequest, "表格中没有有效数据行")
		return
	}
	if err := s.Store.PublishBatches.Create(r.Context(), &db.ItemPublishBatch{
		ID:              batchID,
		UserID:          sess.UserID,
		DefaultCookieID: defaultCookieID,
		Filename:        sourceName,
		UploadDir:       uploadDir,
		LocationJSON:    string(locationBytes),
		Status:          "preview",
	}, rows); err != nil {
		writeErr(w, http.StatusInternalServerError, "保存预检结果失败")
		return
	}
	keepUpload = true
	_ = s.Store.PublishBatches.Recount(r.Context(), batchID)
	writeJSON(w, http.StatusOK, map[string]any{
		"success":    true,
		"preview_id": batchID,
		"total":      len(rows),
		"valid":      valid,
		"invalid":    invalid,
		"rows":       previewRows,
	})
}

func (s *Server) startItemPublishBatch(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	var req struct {
		PreviewID string `json:"preview_id"`
		BatchID   string `json:"batch_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	batchID := strings.TrimSpace(req.PreviewID)
	if batchID == "" {
		batchID = strings.TrimSpace(req.BatchID)
	}
	if batchID == "" {
		writeErr(w, http.StatusBadRequest, "缺少 preview_id")
		return
	}
	batch, err := s.Store.PublishBatches.Get(r.Context(), sess.UserID, batchID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "批量任务不存在")
		return
	}
	activeRunning := batch.Status == "running" && batch.LeaseExpiresAt > time.Now().UTC().Unix()
	if activeRunning {
		writeErr(w, http.StatusConflict, "任务正在由其他 worker 运行")
		return
	}
	if batch.Status != "preview" && batch.Status != "pending" && batch.Status != "completed" && batch.Status != "running" {
		writeErr(w, http.StatusBadRequest, "当前任务状态不能开始发布")
		return
	}
	workerToken := randomHex(16)
	claimed, err := s.Store.PublishBatches.ClaimBatch(r.Context(), batch.ID, workerToken, time.Now().UTC().Add(publishBatchLease).Unix())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "启动任务失败")
		return
	}
	if !claimed {
		writeErr(w, http.StatusConflict, "任务已被其他 worker 启动")
		return
	}
	pending, err := s.Store.PublishBatches.PendingRows(r.Context(), batch.ID, false)
	if err != nil {
		s.failClaimedPublishBatch(batch.ID, workerToken)
		writeErr(w, http.StatusInternalServerError, "读取任务失败")
		return
	}
	if len(pending) == 0 {
		_, _, _ = s.Store.PublishBatches.FinalizeBatch(r.Context(), batch.ID, workerToken)
		writeErr(w, http.StatusBadRequest, "没有可发布的商品行")
		return
	}
	s.startPublishBatchWorker(s.lifecycleContext(), sess.UserID, batch.ID, workerToken)
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "batch_id": batch.ID})
}

func (s *Server) listItemPublishBatches(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	limit := atoiDefault(r.URL.Query().Get("limit"), 20)
	batches, err := s.Store.PublishBatches.ListForUser(r.Context(), sess.UserID, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取批量任务失败")
		return
	}
	result := make([]map[string]any, 0, len(batches))
	for i := range batches {
		result = append(result, publishBatchToMap(&batches[i], nil))
	}
	writeJSON(w, http.StatusOK, map[string]any{"batches": result})
}

func (s *Server) getItemPublishBatch(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	batchID := chi.URLParam(r, "batch_id")
	batch, err := s.Store.PublishBatches.Get(r.Context(), sess.UserID, batchID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "批量任务不存在")
		return
	}
	rows, err := s.Store.PublishBatches.Rows(r.Context(), batch.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取任务明细失败")
		return
	}
	writeJSON(w, http.StatusOK, publishBatchToMap(batch, rows))
}

func (s *Server) cancelItemPublishBatch(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	batchID := chi.URLParam(r, "batch_id")
	_, err := s.Store.PublishBatches.Get(r.Context(), sess.UserID, batchID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "批量任务不存在")
		return
	}
	workerToken, running, err := s.Store.PublishBatches.RequestCancel(r.Context(), batchID)
	if err != nil {
		if errors.Is(err, db.ErrPublishBatchChanged) {
			writeErr(w, http.StatusConflict, "任务状态刚刚发生变化，请重试")
			return
		}
		writeErr(w, http.StatusInternalServerError, "取消任务失败")
		return
	}
	if running {
		s.cancelPublishBatch(batchID, workerToken)
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "status": map[bool]string{true: "canceling", false: "canceled"}[running]})
}

func (s *Server) deleteItemPublishBatch(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	batchID := chi.URLParam(r, "batch_id")
	batch, err := s.Store.PublishBatches.Get(r.Context(), sess.UserID, batchID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "批量任务不存在")
		return
	}
	if batch.Status == "running" || batch.Status == "canceling" {
		writeErr(w, http.StatusConflict, "运行中的任务不能删除，请先取消")
		return
	}
	if err := s.Store.PublishBatches.Delete(r.Context(), sess.UserID, batchID); err != nil {
		writeErr(w, http.StatusInternalServerError, "删除批量任务失败")
		return
	}
	if strings.TrimSpace(batch.UploadDir) != "" {
		_ = os.RemoveAll(batch.UploadDir)
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) retryFailedItemPublishBatch(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	batchID := chi.URLParam(r, "batch_id")
	batch, err := s.Store.PublishBatches.Get(r.Context(), sess.UserID, batchID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "批量任务不存在")
		return
	}
	if batch.Status == "running" && batch.LeaseExpiresAt > time.Now().UTC().Unix() {
		writeErr(w, http.StatusConflict, "任务正在运行，不能重复重试")
		return
	}
	workerToken := randomHex(16)
	claimed, err := s.Store.PublishBatches.ClaimBatch(r.Context(), batchID, workerToken, time.Now().UTC().Add(publishBatchLease).Unix())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "启动重试失败")
		return
	}
	if !claimed {
		writeErr(w, http.StatusConflict, "任务已被其他 worker 启动")
		return
	}
	if err := s.Store.PublishBatches.ResetFailed(r.Context(), batchID); err != nil {
		s.failClaimedPublishBatch(batchID, workerToken)
		writeErr(w, http.StatusInternalServerError, "重置失败项失败")
		return
	}
	_ = s.Store.PublishBatches.Recount(r.Context(), batchID)
	pending, err := s.Store.PublishBatches.PendingRows(r.Context(), batchID, false)
	if err != nil {
		s.failClaimedPublishBatch(batchID, workerToken)
		writeErr(w, http.StatusInternalServerError, "读取可重试项失败")
		return
	}
	if len(pending) == 0 {
		_, _, _ = s.Store.PublishBatches.FinalizeBatch(r.Context(), batchID, workerToken)
		writeErr(w, http.StatusBadRequest, "没有可重试的失败项")
		return
	}
	s.startPublishBatchWorker(s.lifecycleContext(), sess.UserID, batchID, workerToken)
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "batch_id": batchID})
}

func (s *Server) startPublishBatchWorker(parent context.Context, userID int64, batchID, workerToken string) {
	workerDone := s.beginWorker()
	// #nosec G118 -- worker 由超时和 Server 根 context 共同约束。
	go func() {
		defer workerDone()
		jobCtx, cancel := context.WithTimeout(parent, publishBatchJobTimeout)
		s.registerPublishBatchCancel(batchID, workerToken, cancel)
		defer cancel()
		defer s.unregisterPublishBatchCancel(batchID, workerToken)
		s.runItemPublishBatch(jobCtx, userID, batchID, workerToken, false)
	}()
}

// RunPublishBatchRecovery 定期接管租约过期或明确因进程中断失败的批次。
func (s *Server) RunPublishBatchRecovery(ctx context.Context) {
	s.recoverPublishBatchesOnce(ctx)
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.recoverPublishBatchesOnce(ctx)
		}
	}
}

// StartPublishBatchRecovery 先登记生命周期，再启动恢复循环，避免关闭流程在
// goroutine 尚未调度时误判扫描器已经退出。
func (s *Server) StartPublishBatchRecovery(ctx context.Context) {
	s.recoveryWG.Add(1)
	go func() {
		defer s.recoveryWG.Done()
		s.RunPublishBatchRecovery(ctx)
	}()
}

func (s *Server) recoverPublishBatchesOnce(ctx context.Context) {
	batches, err := s.Store.PublishBatches.Recoverable(ctx, time.Now().UTC().Unix(), 20)
	if err != nil {
		s.Logger.Warn("扫描可恢复批量发布任务失败", "err", err)
		return
	}
	for _, batch := range batches {
		if batch.Status == "canceling" {
			_, _ = s.Store.PublishBatches.FinalizeExpiredCancellation(ctx, batch.ID, time.Now().UTC().Unix())
			continue
		}
		workerToken := randomHex(16)
		claimed, claimErr := s.Store.PublishBatches.ClaimBatch(ctx, batch.ID, workerToken, time.Now().UTC().Add(publishBatchLease).Unix())
		if claimErr != nil || !claimed {
			continue
		}
		if err := s.Store.PublishBatches.ResetInterrupted(ctx, batch.ID); err != nil {
			s.failClaimedPublishBatch(batch.ID, workerToken)
			continue
		}
		_ = s.Store.PublishBatches.Recount(ctx, batch.ID)
		pending, pendingErr := s.Store.PublishBatches.PendingRows(ctx, batch.ID, false)
		if pendingErr != nil || len(pending) == 0 {
			if pendingErr == nil {
				_, _, _ = s.Store.PublishBatches.FinalizeBatch(ctx, batch.ID, workerToken)
			} else {
				s.failClaimedPublishBatch(batch.ID, workerToken)
			}
			continue
		}
		s.startPublishBatchWorker(ctx, batch.UserID, batch.ID, workerToken)
	}
}

func (s *Server) failClaimedPublishBatch(batchID, workerToken string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if released, err := s.Store.PublishBatches.FailClaimedBatch(ctx, batchID, workerToken); err != nil {
		s.Logger.Warn("释放异常批量发布任务失败", "batch", batchID, "err", err)
	} else if !released {
		s.Logger.Debug("批量发布任务租约已转移，无需释放", "batch", batchID)
	}
}

func (s *Server) downloadItemPublishBatchResult(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	batchID := chi.URLParam(r, "batch_id")
	batch, err := s.Store.PublishBatches.Get(r.Context(), sess.UserID, batchID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "批量任务不存在")
		return
	}
	rows, err := s.Store.PublishBatches.Rows(r.Context(), batch.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取任务明细失败")
		return
	}
	var buf bytes.Buffer
	buf.WriteString("\xEF\xBB\xBF")
	cw := csv.NewWriter(&buf)
	_ = cw.Write([]string{"行号", "状态", "账号ID", "标题", "价格", "库存", "默认类目ID", "默认类目名称", "商品ID", "商品URL", "错误原因"})
	for _, row := range rows {
		var category mtop.PublishCategory
		_ = json.Unmarshal([]byte(row.CategoryJSON), &category)
		_ = cw.Write([]string{
			strconv.Itoa(row.RowNo), safeCSVCell(row.Status), safeCSVCell(row.CookieID), safeCSVCell(row.Title), safeCSVCell(row.Price),
			strconv.Itoa(row.Quantity), safeCSVCell(category.CatID), safeCSVCell(category.CatName),
			safeCSVCell(row.ItemID), safeCSVCell(row.ItemURL), safeCSVCell(row.ErrorMessage),
		})
	}
	cw.Flush()
	filename := fmt.Sprintf("publish_result_%s.csv", batch.ID)
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename="+strconv.Quote(filename))
	_, _ = w.Write(buf.Bytes())
}

// safeCSVCell 防止用户可控内容被电子表格应用解释为公式。开头的单引号
// 在 Excel/LibreOffice 中作为文本标记，不改变导出的可见内容。
func safeCSVCell(value string) string {
	trimmed := strings.TrimLeft(value, " \t\r\n")
	if trimmed == "" {
		return value
	}
	switch trimmed[0] {
	case '=', '+', '-', '@':
		return "'" + value
	default:
		return value
	}
}

func (s *Server) runItemPublishBatch(ctx context.Context, userID int64, batchID, workerToken string, failedOnly bool) {
	rows, err := s.Store.PublishBatches.PendingRows(ctx, batchID, failedOnly)
	if err != nil {
		if s.Logger != nil {
			s.Logger.Warn("读取批量发布行失败", "batch", batchID, "err", err)
		}
		if ctx.Err() != nil {
			s.finishInterruptedPublishBatch(ctx, userID, batchID, workerToken)
		}
		return
	}
	client := s.mtopClient()
	for idx, row := range rows {
		if ctx.Err() != nil {
			s.finishInterruptedPublishBatch(ctx, userID, batchID, workerToken)
			return
		}
		wctx, cancel := publishStatusContext(ctx)
		renewed, renewErr := s.Store.PublishBatches.RenewBatchLease(wctx, batchID, workerToken, time.Now().UTC().Add(publishBatchLease).Unix())
		cancel()
		if renewErr != nil || !renewed {
			s.finishInterruptedPublishBatch(ctx, userID, batchID, workerToken)
			return
		}
		wctx, cancel = publishStatusContext(ctx)
		batch, err := s.Store.PublishBatches.Get(wctx, userID, batchID)
		cancel()
		if err != nil || batch.Status != "running" || batch.WorkerToken != workerToken {
			s.finishInterruptedPublishBatch(ctx, userID, batchID, workerToken)
			return
		}
		wctx, cancel = publishStatusContext(ctx)
		claimed, claimErr := s.Store.PublishBatches.ClaimRow(wctx, row.ID, workerToken)
		cancel()
		if claimErr != nil {
			s.finishInterruptedPublishBatch(ctx, userID, batchID, workerToken)
			return
		}
		if !claimed {
			continue
		}
		if rowErr := s.publishBatchRow(ctx, userID, client, row, workerToken); rowErr != nil {
			sessionExpired := mtop.IsSessionExpiredErr(rowErr)
			statusCtx, statusCancel := context.WithTimeout(context.Background(), 5*time.Second)
			status, _ := s.Store.PublishBatches.BatchStatus(statusCtx, batchID)
			statusCancel()
			message, failureKind := publishBatchFailure(rowErr, status)
			wctx, cancel := publishStatusContext(ctx)
			marked, markErr := s.Store.PublishBatches.MarkClaimedRowFailed(wctx, row.ID, workerToken, message, failureKind)
			cancel()
			if markErr != nil || !marked {
				if s.Logger != nil {
					s.Logger.Warn("保存批量发布失败状态失败，等待租约恢复", "batch", batchID, "row", row.ID, "err", markErr)
				}
				return
			}
			if sessionExpired {
				s.finishInterruptedPublishBatch(ctx, userID, batchID, workerToken)
				return
			}
		}
		wctx, cancel = publishStatusContext(ctx)
		if err := s.Store.PublishBatches.Recount(wctx, batchID); err != nil && s.Logger != nil {
			s.Logger.Warn("重算批量发布进度失败", "batch", batchID, "err", err)
		}
		cancel()
		if idx < len(rows)-1 {
			delay := time.Duration(10+idx%21) * time.Second
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				s.finishInterruptedPublishBatch(ctx, userID, batchID, workerToken)
				return
			case <-timer.C:
			}
		}
	}
	s.finishPublishBatch(ctx, userID, batchID, workerToken)
}

func publishBatchFailure(err error, batchStatus string) (string, string) {
	message := err.Error()
	failureKind := "publish"
	var postErr *postPublishError
	var uncertainErr *uncertainRemotePublishError
	if errors.As(err, &uncertainErr) {
		failureKind = "uncertain_remote"
		message += "；远端结果未能可靠落库，禁止自动重试，请人工核对闲鱼商品列表"
	} else if errors.As(err, &postErr) {
		failureKind = "post_publish"
	}
	if batchStatus == "canceled" || batchStatus == "canceling" {
		if failureKind == "uncertain_remote" {
			message = "任务已取消；" + message
		} else {
			message = "任务已取消"
		}
	}
	return message, failureKind
}

func (s *Server) registerPublishBatchCancel(batchID, workerToken string, cancel context.CancelFunc) {
	s.publishMu.Lock()
	defer s.publishMu.Unlock()
	if s.publishCancels == nil {
		s.publishCancels = make(map[string]publishBatchWorker)
	}
	if old := s.publishCancels[batchID]; old.cancel != nil {
		old.cancel()
	}
	s.publishCancels[batchID] = publishBatchWorker{token: workerToken, cancel: cancel}
}

func (s *Server) unregisterPublishBatchCancel(batchID, workerToken string) {
	s.publishMu.Lock()
	defer s.publishMu.Unlock()
	if current := s.publishCancels[batchID]; current.token == workerToken {
		delete(s.publishCancels, batchID)
	}
}

func (s *Server) cancelPublishBatch(batchID, workerToken string) bool {
	s.publishMu.Lock()
	worker := s.publishCancels[batchID]
	s.publishMu.Unlock()
	if worker.token != workerToken || worker.cancel == nil {
		return false
	}
	worker.cancel()
	return true
}

func publishStatusContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent != nil && parent.Err() == nil {
		return context.WithTimeout(parent, 5*time.Second)
	}
	return context.WithTimeout(context.Background(), 5*time.Second)
}

func (s *Server) finishInterruptedPublishBatch(ctx context.Context, userID int64, batchID, workerToken string) {
	wctx, cancel := publishStatusContext(ctx)
	defer cancel()
	batch, err := s.Store.PublishBatches.Get(wctx, userID, batchID)
	if err != nil {
		return
	}
	if batch.Status == "canceled" || batch.Status == "canceling" {
		if batch.Status == "canceling" {
			if batch.WorkerToken != workerToken {
				return
			}
			_, _ = s.Store.PublishBatches.FinalizeCanceled(wctx, batchID, workerToken)
		}
		// 取消产生的 interrupted 失败行允许用户稍后重试，图片由统一的过期清理保留 7 天。
		return
	}
	_, _, finalizeErr := s.Store.PublishBatches.FinalizeInterrupted(wctx, batchID, workerToken, "任务超时或已中断")
	if finalizeErr != nil && s.Logger != nil {
		s.Logger.Warn("结束中断的批量发布任务失败，等待租约恢复", "batch", batchID, "err", finalizeErr)
	}
}

func (s *Server) finishPublishBatch(ctx context.Context, userID int64, batchID, workerToken string) {
	wctx, cancel := publishStatusContext(ctx)
	defer cancel()
	batch, err := s.Store.PublishBatches.Get(wctx, userID, batchID)
	if err != nil || batch.Status == "canceled" || batch.WorkerToken != workerToken {
		return
	}
	if batch.Status == "canceling" {
		_, _ = s.Store.PublishBatches.FinalizeCanceled(wctx, batchID, workerToken)
		// 取消任务仍可“重试失败项”，不能在此删除重试所需的本地图片。
		return
	}
	finalStatus, finished, finishErr := s.Store.PublishBatches.FinalizeBatch(wctx, batchID, workerToken)
	if finishErr != nil {
		if s.Logger != nil {
			s.Logger.Warn("结束批量发布任务失败，等待租约恢复", "batch", batchID, "err", finishErr)
		}
		return
	}
	if finished && finalStatus == "completed" && strings.TrimSpace(batch.UploadDir) != "" {
		s.removePublishUploadDir(wctx, batch)
	}
}

func finalPublishBatchStatus(batch *db.ItemPublishBatch) string {
	if batch == nil {
		return "failed"
	}
	if batch.FailedCount > 0 {
		if batch.SuccessCount > 0 {
			return "partially_failed"
		}
		return "failed"
	}
	return "completed"
}

func (s *Server) publishBatchRow(ctx context.Context, userID int64, client mtop.Client, row db.ItemPublishBatchRow, workerToken string) error {
	batch, err := s.Store.PublishBatches.Get(ctx, userID, row.BatchID)
	if err != nil {
		return errors.New("批量任务不存在")
	}
	var location mtop.PublishLocation
	locationJSON := strings.TrimSpace(batch.LocationJSON)
	if locationJSON == "" {
		locationJSON = "{}"
	}
	if err := json.Unmarshal([]byte(locationJSON), &location); err != nil {
		return errors.New("批量任务发货地配置损坏，请重新创建任务")
	}
	if strings.TrimSpace(location.DivisionID) == "" {
		// Legacy v1.0.2 batches persisted PascalCase location keys.
		var legacy struct {
			Area       string  `json:"Area"`
			City       string  `json:"City"`
			DivisionID string  `json:"DivisionID"`
			Longitude  float64 `json:"Longitude"`
			Latitude   float64 `json:"Latitude"`
			POIID      string  `json:"POIID"`
			POIName    string  `json:"POIName"`
			Province   string  `json:"Province"`
		}
		if err := json.Unmarshal([]byte(locationJSON), &legacy); err != nil {
			return errors.New("批量任务发货地配置损坏，请重新创建任务")
		}
		if strings.TrimSpace(legacy.DivisionID) != "" {
			location = mtop.PublishLocation{Area: legacy.Area, City: legacy.City, DivisionID: legacy.DivisionID, Longitude: legacy.Longitude, Latitude: legacy.Latitude, POIID: legacy.POIID, POIName: legacy.POIName, Province: legacy.Province}
		}
	}
	var selectedLocation *mtop.PublishLocation
	if strings.TrimSpace(location.DivisionID) != "" {
		selectedLocation = &location
	}
	cookieValue, err := s.cookieValueForUser(ctx, userID, row.CookieID)
	if err != nil {
		return err
	}
	priceCents, err := parseMoneyCents(row.Price)
	if err != nil || priceCents <= 0 {
		return errors.New("商品价格必须大于 0")
	}
	origCents, _ := parseMoneyCents(row.OriginalPrice)
	postageCents, _ := parseMoneyCents(row.Postage)
	res := &mtop.PublishItemResult{ItemID: row.ItemID, ItemURL: row.ItemURL, Title: row.Title, PriceText: row.Price, Quantity: row.Quantity}
	var responseCookieErr error
	if row.ItemID == "" {
		var preferredCategory *mtop.PublishCategory
		rawCategory := strings.TrimSpace(row.CategoryJSON)
		if rawCategory != "" && rawCategory != "{}" {
			var configured mtop.PublishCategory
			if err := json.Unmarshal([]byte(rawCategory), &configured); err != nil {
				return errors.New("默认类目配置损坏，请重新创建批量任务")
			}
			if strings.TrimSpace(configured.CatID) != "" || strings.TrimSpace(configured.CatName) != "" || strings.TrimSpace(configured.ChannelCatID) != "" || strings.TrimSpace(configured.TBCatID) != "" {
				if strings.TrimSpace(configured.CatID) == "" || strings.TrimSpace(configured.CatName) == "" || strings.TrimSpace(configured.ChannelCatID) == "" {
					return errors.New("默认类目信息不完整，请重新创建批量任务")
				}
				preferredCategory = &configured
			}
		}
		images, err := loadBatchPublishImages(ctx, batch.UploadDir, row)
		if err != nil {
			return err
		}
		markCtx, markCancel := publishStatusContext(ctx)
		remoteStarted, markErr := s.Store.PublishBatches.MarkClaimedRemoteStarted(markCtx, row.ID, workerToken)
		markCancel()
		if markErr != nil || !remoteStarted {
			return fmt.Errorf("保存远端发布前检查点失败: %w", firstError(markErr, errors.New("批次租约已失效")))
		}
		runtimeCookie := ""
		runtimeCookieChanged := false
		res, err = func() (*mtop.PublishItemResult, error) {
			credentialUnlock := s.Store.LockAccountCredentials(row.CookieID)
			defer credentialUnlock()
			latest, latestErr := s.Store.Cookies.GetDetails(ctx, row.CookieID)
			if latestErr != nil {
				return nil, latestErr
			}
			if latest == nil || latest.UserID != userID {
				return nil, db.ErrForbidden
			}
			if !hasStoredCookieCredential(latest) {
				return nil, errors.New("账号 Cookie 为空")
			}
			cookieValue = latest.Value
			pctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			defer cancel()
			mtopCtx, cookieSession := withMTopCookieSnapshot(pctx, latest)
			published, publishErr := client.PublishItem(mtopCtx, cookieValue, mtop.PublishItemRequest{
				Title:              row.Title,
				Description:        firstNonEmpty(row.Description, row.Title),
				PriceCents:         priceCents,
				OriginalPriceCents: origCents,
				Quantity:           row.Quantity,
				PostageMode:        row.PostageMode,
				PostageCents:       postageCents,
				Virtual:            true,
				Location:           selectedLocation,
				PreferredCategory:  preferredCategory,
				Images:             images,
			})
			value, valueChanged, handled, persistErr := s.persistMTopCookieSessionLocked(ctx, latest, cookieSession)
			if persistErr != nil {
				cookieErr := fmt.Errorf("发布商品后保存响应 Cookie Jar: %w", persistErr)
				if publishErr != nil {
					return published, errors.Join(publishErr, cookieErr)
				}
				responseCookieErr = cookieErr
			} else if handled {
				if valueChanged {
					runtimeCookie = value
					runtimeCookieChanged = true
				}
			} else if publishErr == nil && published != nil && published.UpdatedCookies != "" && published.UpdatedCookies != cookieValue {
				if saveErr := s.Store.Cookies.UpdateValueOwned(ctx, row.CookieID, published.UpdatedCookies, userID); saveErr != nil {
					responseCookieErr = fmt.Errorf("发布商品后保存响应 Cookie: %w", saveErr)
				} else {
					runtimeCookie = published.UpdatedCookies
					runtimeCookieChanged = true
				}
			}
			if publishErr != nil {
				return published, publishErr
			}
			if published == nil {
				return nil, errors.New("发布商品接口未返回结果")
			}
			return published, nil
		}()
		if runtimeCookieChanged {
			s.updateRunningCookie(ctx, row.CookieID, runtimeCookie)
		}
		if err != nil {
			s.recoverExpiredMTOPSession(ctx, row.CookieID, err)
			if ctx.Err() != nil {
				return &uncertainRemotePublishError{err: fmt.Errorf("取消时远端发布结果未知: %w", err)}
			}
			var perr *mtop.PublishError
			if errors.As(err, &perr) {
				if perr.Code == mtop.PublishErrorStockPermissionMissing {
					return errors.New("该账号没有库存发布权限，无法按库存数量发布商品")
				}
				return err
			}
			if errors.Is(err, mtop.ErrPublishCategoryUnrecognized) {
				return err
			}
			return &uncertainRemotePublishError{err: fmt.Errorf("远端发布调用失败且结果未知: %w", err)}
		}
		rawJSON, _ := json.Marshal(res.RawData)
		saveCtx, saveCancel := publishStatusContext(ctx)
		saved, saveErr := s.Store.PublishBatches.SaveClaimedRemoteResult(saveCtx, row.ID, workerToken, res.ItemID, res.ItemURL, string(rawJSON))
		saveCancel()
		if saveErr != nil || !saved {
			return &uncertainRemotePublishError{err: fmt.Errorf("保存远端发布结果失败: %w", firstError(saveErr, errors.New("批次租约已失效")))}
		}
		if responseCookieErr != nil {
			return &postPublishError{err: responseCookieErr}
		}
	} else if strings.TrimSpace(row.RawJSON) != "" {
		_ = json.Unmarshal([]byte(row.RawJSON), &res.RawData)
	}
	if ctx.Err() != nil {
		return &postPublishError{err: ctx.Err()}
	}
	currentBatch, err := s.Store.PublishBatches.Get(ctx, userID, row.BatchID)
	if err != nil || currentBatch.Status == "canceled" || currentBatch.WorkerToken != workerToken {
		return &postPublishError{err: context.Canceled}
	}
	if res.ItemID != "" {
		detail := map[string]any{
			"item_image":    res.ImageURL,
			"web_url":       res.ItemURL,
			"category_name": res.CategoryName,
			"quantity":      res.Quantity,
			"publish_raw":   res.RawData,
		}
		detailJSON, _ := json.Marshal(detail)
		if err := s.Store.Items.Upsert(ctx, &db.ItemInfoRow{
			CookieID:              row.CookieID,
			ItemID:                res.ItemID,
			ItemTitle:             firstNonEmpty(res.Title, row.Title),
			ItemDescription:       row.Description,
			ItemCategory:          res.CategoryID,
			ItemPrice:             res.PriceText,
			ItemDetail:            string(detailJSON),
			MultiQuantityDelivery: row.Quantity > 1,
		}); err != nil {
			return &postPublishError{err: fmt.Errorf("保存发布商品信息: %w", err)}
		}
		if err := s.createPublishAutomationRules(ctx, userID, row, res); err != nil {
			return &postPublishError{err: fmt.Errorf("创建发布商品自动化规则: %w", err)}
		}
	}
	rawJSON, _ := json.Marshal(res.RawData)
	marked, err := s.Store.PublishBatches.MarkClaimedRowSuccess(ctx, row.ID, workerToken, res.ItemID, res.ItemURL, string(rawJSON))
	if err != nil {
		return err
	}
	if !marked {
		return errors.New("批量任务租约已失效")
	}
	return nil
}

func firstError(err, fallback error) error {
	if err != nil {
		return err
	}
	return fallback
}

func (s *Server) clonePublishAutomationRule(ctx context.Context, userID int64, cookieID, itemID, name string, source db.AutomationRule) error {
	if source.ItemID == "" || source.CookieID != cookieID || itemID == "" {
		return errors.New("自动化规则来源或目标商品无效")
	}
	input := db.AutomationRuleInput{UserID: userID, CookieID: cookieID, ItemID: itemID, Name: name, TriggerType: source.TriggerType, Enabled: source.Enabled, Priority: source.Priority, ConfigJSON: source.ConfigJSON}
	for _, action := range source.Actions {
		bindings := append([]db.DeliveryTemplateBinding(nil), action.TemplateBindings...)
		input.Actions = append(input.Actions, db.AutomationActionInput{ActionType: action.ActionType, CardID: action.CardID, DeliveryCount: action.DeliveryCount, MessageTemplate: action.MessageTemplate, DelaySeconds: action.DelaySeconds, ConfigJSON: action.ConfigJSON, Enabled: action.Enabled, SortOrder: action.SortOrder, DeliveryTemplateID: action.DeliveryTemplateID, TemplateBindings: bindings})
	}
	return s.ensurePublishAutomationRule(ctx, input)
}

func (s *Server) createPublishAutomationRules(ctx context.Context, userID int64, row db.ItemPublishBatchRow, res *mtop.PublishItemResult) error {
	var cfg publishAutomationConfig
	if err := json.Unmarshal([]byte(row.AutomationJSON), &cfg); err != nil {
		return err
	}
	title := firstNonEmpty(res.Title, row.Title)
	if cfg.PaidDelivery.RuleID > 0 && cfg.PaidDelivery.SourceRuleSnapshot != nil {
		if err := s.clonePublishAutomationRule(ctx, userID, row.CookieID, res.ItemID, "付款后自动发货 - "+title, *cfg.PaidDelivery.SourceRuleSnapshot); err != nil {
			return err
		}
	}
	if cfg.ReviewGift.RuleID > 0 && cfg.ReviewGift.SourceRuleSnapshot != nil {
		if err := s.clonePublishAutomationRule(ctx, userID, row.CookieID, res.ItemID, "评价后发送赠品 - "+title, *cfg.ReviewGift.SourceRuleSnapshot); err != nil {
			return err
		}
	}
	if cfg.ReviewRequest.Enabled {
		cfgJSON, _ := json.Marshal(map[string]any{"after_shipped_hours": cfg.ReviewRequest.AfterShippedHours, "max_attempts": cfg.ReviewRequest.MaxAttempts})
		if err := s.ensurePublishAutomationRule(ctx, db.AutomationRuleInput{
			UserID: userID, CookieID: row.CookieID, ItemID: res.ItemID,
			Name: "超时未评价求评价 - " + title, TriggerType: automation.TriggerReviewMissingTimeout,
			Enabled: true, Priority: 100, ConfigJSON: string(cfgJSON),
			Actions: []db.AutomationActionInput{
				{ActionType: automation.ActionSendText, MessageTemplate: cfg.ReviewRequest.Message, DelaySeconds: cfg.ReviewRequest.DelaySeconds, Enabled: true, SortOrder: 1},
			},
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) ensurePublishAutomationRule(ctx context.Context, input db.AutomationRuleInput) error {
	var exists bool
	err := s.Store.DB.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM automation_rules
		 WHERE user_id=? AND cookie_id=? AND item_id=? AND trigger_type=? AND name=? AND deleted_at IS NULL
	)`, input.UserID, input.CookieID, input.ItemID, input.TriggerType, input.Name).Scan(&exists)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	_, err = s.Store.Automation.Create(ctx, input)
	return err
}

func (s *Server) parsePublishRows(ctx context.Context, userID int64, defaultCookieID, uploadDir string, fallbackCategory mtop.PublishCategory, input []map[string]any) []publishBatchParsedRow {
	out := make([]publishBatchParsedRow, 0, len(input))
	for i, m := range input {
		row := publishBatchParsedRow{
			RowNo:         i + 2,
			CookieID:      firstImportString(m, "cookie_id", "账号ID", "账号id", "账号"),
			Title:         firstImportString(m, "title", "标题", "商品标题", "商品名称"),
			Description:   firstImportString(m, "description", "描述", "商品描述", "商品详情"),
			Price:         firstImportString(m, "price", "价格", "商品价格"),
			OriginalPrice: firstImportString(m, "original_price", "原价"),
			PostageMode:   firstImportString(m, "postage_mode", "邮费模式"),
			Postage:       firstImportString(m, "postage", "邮费"),
			Raw:           m,
		}
		if row.CookieID == "" {
			row.CookieID = defaultCookieID
		}
		if row.Description == "" {
			row.Description = row.Title
		}
		if row.PostageMode == "" {
			row.PostageMode = "free"
		}
		row.PostageMode = strings.ToLower(row.PostageMode)
		if row.PostageMode == "包邮" || row.PostageMode == "free_shipping" {
			row.PostageMode = "free"
		}
		if row.PostageMode == "固定邮费" || row.PostageMode == "一口价邮费" {
			row.PostageMode = "fixed"
		}
		row.Quantity = atoiPublishDefault(firstImportString(m, "quantity", "库存", "数量"), 1)
		rowCategory := mtop.PublishCategory{
			CatID:        firstImportString(m, "category_id", "类目ID", "商品类目ID"),
			CatName:      firstImportString(m, "category_name", "类目名称", "商品类目名称", "类目"),
			ChannelCatID: firstImportString(m, "channel_category_id", "频道类目ID"),
			TBCatID:      firstImportString(m, "tb_category_id", "淘宝类目ID"),
		}
		hasRowCategory := rowCategory.CatID != "" || rowCategory.CatName != "" || rowCategory.ChannelCatID != "" || rowCategory.TBCatID != ""
		if hasRowCategory {
			row.Category = rowCategory
			if rowCategory.CatID == "" || rowCategory.CatName == "" || rowCategory.ChannelCatID == "" {
				row.Errors = append(row.Errors, "指定行类目时必须同时填写类目ID、类目名称和频道类目ID")
			}
		} else {
			row.Category = fallbackCategory
		}
		row.Automation = parsePublishAutomation(m)
		row.Images = splitImageRefs(firstImportString(m, "images", "image", "图片", "商品图片"))
		if row.CookieID == "" {
			row.Errors = append(row.Errors, "缺少账号ID")
		} else if !s.cookieOwnedByUser(ctx, userID, row.CookieID) {
			row.Errors = append(row.Errors, "账号不存在或不属于当前用户")
		}
		if strings.TrimSpace(row.Title) == "" {
			row.Errors = append(row.Errors, "缺少标题")
		}
		if cents, err := parseMoneyCents(row.Price); err != nil || cents <= 0 {
			row.Errors = append(row.Errors, "价格必须大于 0")
		}
		if strings.TrimSpace(row.OriginalPrice) != "" {
			if cents, err := parseMoneyCents(row.OriginalPrice); err != nil || cents <= 0 {
				row.Errors = append(row.Errors, "原价格式错误")
			}
		}
		if row.Quantity <= 0 {
			row.Errors = append(row.Errors, "库存必须大于 0")
		}
		if row.PostageMode != "free" && row.PostageMode != "fixed" {
			row.Errors = append(row.Errors, "邮费模式必须是 free 或 fixed")
		}
		if row.PostageMode == "fixed" {
			if cents, err := parseMoneyCents(row.Postage); err != nil || cents < 0 {
				row.Errors = append(row.Errors, "固定邮费格式错误")
			}
		}
		if len(row.Images) == 0 {
			row.Errors = append(row.Errors, "缺少图片")
		}
		if len(row.Images) > 9 {
			row.Errors = append(row.Errors, "商品图片最多 9 张")
		}
		for _, ref := range row.Images {
			if err := validateBatchImageRef(uploadDir, ref); err != nil {
				row.Errors = append(row.Errors, err.Error())
			}
		}
		row.Errors = append(row.Errors, s.validatePublishAutomation(ctx, userID, row.CookieID, &row.Automation)...)
		out = append(out, row)
	}
	return out
}

func parsePublishRuleReference(raw string) publishRuleReference {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return publishRuleReference{}
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return publishRuleReference{ParseError: "规则 ID 必须是正整数"}
	}
	return publishRuleReference{RuleID: id}
}

func parsePublishAutomation(m map[string]any) publishAutomationConfig {
	cfg := publishAutomationConfig{SchemaVersion: 2}
	cfg.PaidDelivery = parsePublishRuleReference(firstImportString(m, "paid_delivery_rule_id", "付款发货规则ID", "付款后自动发货规则ID"))
	cfg.ReviewGift = parsePublishRuleReference(firstImportString(m, "review_gift_rule_id", "评价赠品规则ID", "评价后发送赠品规则ID"))
	cfg.ReviewRequest = publishReviewRequestCfg{
		Enabled:           parseLooseBool(firstImportString(m, "review_request_enabled", "求评价启用")),
		AfterShippedHours: atoiPublishDefault(firstImportString(m, "review_request_after_hours", "求评价等待小时"), 72),
		Message:           firstImportString(m, "review_request_message", "求评价文案"),
		MaxAttempts:       atoiPublishDefault(firstImportString(m, "review_request_max_attempts", "求评价最多次数"), 1),
		DelaySeconds:      atoiPublishDefault(firstImportString(m, "review_request_delay_seconds", "求评价延迟秒"), 0),
	}
	return cfg
}

func (s *Server) validatePublishAutomation(ctx context.Context, userID int64, cookieID string, cfg *publishAutomationConfig) []string {
	var errs []string
	validateRule := func(ref *publishRuleReference, trigger, label string) {
		if ref.RuleID == 0 && ref.ParseError == "" {
			return
		}
		if ref.ParseError != "" {
			errs = append(errs, label+ref.ParseError)
			return
		}
		rules, err := s.Store.Automation.ListForUser(ctx, userID)
		if err != nil {
			errs = append(errs, label+"规则查询失败")
			return
		}
		var matched *db.AutomationRule
		for index := range rules {
			rule := &rules[index]
			if rule.ID == ref.RuleID {
				matched = rule
				break
			}
		}
		if matched == nil {
			errs = append(errs, fmt.Sprintf("%s规则 %d 不存在或无权限", label, ref.RuleID))
			return
		}
		if matched.CookieID != cookieID {
			errs = append(errs, label+"规则不属于当前行账号")
			return
		}
		if matched.TriggerType != trigger {
			errs = append(errs, label+"规则触发类型不匹配")
			return
		}
		if matched.ItemID == "" {
			errs = append(errs, label+"规则必须绑定具体商品")
			return
		}
		if matched.ItemTitle == "" {
			matched.ItemTitle = matched.ItemID
		}
		if !matched.Enabled {
			errs = append(errs, label+"规则已停用")
			return
		}
		snapshot := *matched
		ref.SourceRuleSnapshot = &snapshot
	}
	validateRule(&cfg.PaidDelivery, automation.TriggerOrderPaid, "付款发货")
	validateRule(&cfg.ReviewGift, automation.TriggerBuyerReviewed, "评价赠品")
	if cfg.ReviewRequest.Enabled {
		if cfg.ReviewRequest.AfterShippedHours <= 0 {
			errs = append(errs, "求评价等待小时必须大于 0")
		}
		if strings.TrimSpace(cfg.ReviewRequest.Message) == "" {
			errs = append(errs, "求评价文案不能为空")
		}
		if cfg.ReviewRequest.MaxAttempts <= 0 {
			errs = append(errs, "求评价最多次数必须大于 0")
		}
	}
	return errs
}

func publishBatchToMap(batch *db.ItemPublishBatch, rows []db.ItemPublishBatchRow) map[string]any {
	locationJSON := strings.TrimSpace(batch.LocationJSON)
	if locationJSON == "" {
		locationJSON = "{}"
	}
	outRows := make([]map[string]any, 0, len(rows))
	pending := 0
	running := 0
	for _, row := range rows {
		if row.Status == "pending" {
			pending++
		}
		if row.Status == "running" {
			running++
		}
		var refs []string
		_ = json.Unmarshal([]byte(row.ImagesJSON), &refs)
		var category mtop.PublishCategory
		_ = json.Unmarshal([]byte(row.CategoryJSON), &category)
		var automationCfg publishAutomationConfig
		_ = json.Unmarshal([]byte(row.AutomationJSON), &automationCfg)
		outRows = append(outRows, map[string]any{
			"id": row.ID, "row_no": row.RowNo, "cookie_id": row.CookieID, "title": row.Title,
			"price": row.Price, "quantity": row.Quantity, "images": refs,
			"category":   category,
			"automation": automationCfg,
			"status":     row.Status, "item_id": row.ItemID, "item_url": row.ItemURL,
			"error_message": row.ErrorMessage, "failure_kind": row.FailureKind,
		})
	}
	retryable := 0
	for _, row := range rows {
		if row.Status == "failed" && row.FailureKind != "validation" && row.FailureKind != "uncertain_remote" {
			retryable++
		}
	}
	return map[string]any{
		"id": batch.ID, "status": batch.Status, "filename": batch.Filename,
		"total": batch.TotalCount, "success": batch.SuccessCount, "failed": batch.FailedCount,
		"pending": pending, "running": running, "retryable": retryable, "rows": outRows,
		"location":   json.RawMessage(locationJSON),
		"created_at": batch.CreatedAt, "updated_at": batch.UpdatedAt,
	}
}

func (s *Server) removePublishUploadDir(ctx context.Context, batch *db.ItemPublishBatch) {
	if batch == nil || strings.TrimSpace(batch.UploadDir) == "" {
		return
	}
	_ = os.RemoveAll(batch.UploadDir)
	_ = s.Store.PublishBatches.ClearUploadDir(ctx, batch.ID)
}

func (s *Server) cleanupExpiredPublishUploads(ctx context.Context) {
	cutoff := time.Now().UTC().Add(-7 * 24 * time.Hour).Format("2006-01-02 15:04:05")
	batches, err := s.Store.PublishBatches.ExpiredUploads(ctx, cutoff, 100)
	if err != nil {
		return
	}
	for i := range batches {
		s.removePublishUploadDir(ctx, &batches[i])
	}
}

func (s *Server) cookieOwnedByUser(ctx context.Context, userID int64, cookieID string) bool {
	all, err := s.Store.Cookies.AllForUser(ctx, userID)
	if err != nil {
		return false
	}
	_, ok := all[cookieID]
	return ok
}

func (s *Server) cookieValueForUser(ctx context.Context, userID int64, cookieID string) (string, error) {
	all, err := s.Store.Cookies.AllForUser(ctx, userID)
	if err != nil {
		return "", err
	}
	value, ok := all[cookieID]
	if !ok || strings.TrimSpace(value) == "" {
		return "", errors.New("账号不存在或 Cookie 为空")
	}
	return value, nil
}

func (s *Server) cardOwnedByUser(ctx context.Context, userID int64, cardID int64) bool {
	var exists bool
	err := s.Store.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM cards WHERE id=? AND user_id=?)`, cardID, userID).Scan(&exists)
	return err == nil && exists
}

func (s *Server) publishUploadRoot() string {
	return defaultPublishUploadRoot()
}

func defaultPublishUploadRoot() string {
	if v := strings.TrimSpace(os.Getenv("XIANYU_UPLOAD_DIR")); v != "" {
		return v
	}
	return filepath.Join("data", "uploads")
}

func parseLooseBool(raw string) bool {
	raw = strings.ToLower(strings.TrimSpace(raw))
	switch raw {
	case "1", "true", "yes", "y", "on", "是", "开启", "启用":
		return true
	default:
		return false
	}
}

func atoiPublishDefault(raw string, def int) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return def
	}
	if f, err := strconv.ParseFloat(raw, 64); err == nil {
		return int(f)
	}
	return def
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
