package server

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"xianyu-go/internal/auth"
	"xianyu-go/internal/db"
)

// 单次批量导入最多处理的数据行数（一行一条卡密）。
const maxCardBatchRows = 5000

type cardBatchGroupRow struct {
	Name     string `json:"name"`
	ItemType string `json:"type"`
	CardID   int64  `json:"card_id"`
	Created  bool   `json:"created"`
	Added    int    `json:"added"`
	Failed   int    `json:"failed"`
}

type cardBatchRowError struct {
	RowNo int    `json:"row_no"`
	Name  string `json:"name"`
	Error string `json:"error"`
}

// batchGroup 是一次导入里的一个卡种分组。同名行归入同一分组。
type batchGroup struct {
	name     string
	cardID   int64
	itemType string
	created  bool
	resolved bool
	added    int
	failed   int
}

// batchCreateCards 按卡种批量导入卡密。每行一条卡密，同一个「卡种」名称的行并入同一卡种：
// 卡种不存在时用该组首行的描述/启用/延时创建，已存在时直接追加卡密。
// 类型与内容同时留空的行只用于确保卡种存在（不写卡密）。
func (s *Server) batchCreateCards(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	// 表格最大 5 MiB（几千行卡密文本足够）。
	r.Body = http.MaxBytesReader(w, r.Body, maxCardBatchUploadBytes)
	if err := r.ParseMultipartForm(maxCardBatchUploadBytes); err != nil {
		writeErr(w, http.StatusBadRequest, "解析上传文件失败")
		return
	}
	source, sourceHeader, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "缺少卡密表格文件")
		return
	}
	defer source.Close()
	sourceBytes, tooLarge, err := readLimitedBytes(source, 5<<20)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "读取卡密表格失败")
		return
	}
	if tooLarge {
		writeErr(w, http.StatusBadRequest, "卡密表格不能超过 5 MiB")
		return
	}
	sourceName := safeBaseName(sourceHeader.Filename)
	if sourceName == "" {
		sourceName = "cards.csv"
	}
	maps, err := parsePublishSheetBytesWithLimit(sourceBytes, sourceName, maxCardBatchRows)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	groupsByKey := make(map[string]*batchGroup)
	groupOrder := make([]*batchGroup, 0, 8)
	rowErrors := make([]cardBatchRowError, 0)
	itemsAdded, failedRows := 0, 0

	failRow := func(rowNo int, name, message string) {
		rowErrors = append(rowErrors, cardBatchRowError{RowNo: rowNo, Name: name, Error: message})
		failedRows++
	}

	for i, m := range maps {
		rowNo := i + 2
		name := firstImportString(m, "name", "名称", "卡种", "卡种名称", "卡密组名称", "卡密名称")
		itemType := strings.ToLower(firstImportString(m, "type", "类型", "卡密类型"))
		content := firstImportString(m, "content", "内容", "卡密内容", "卡密")

		if name == "" {
			failRow(rowNo, "", "缺少卡种名称")
			continue
		}
		if itemType != "" && !db.ValidCardKind(itemType) {
			failRow(rowNo, name, "卡密类型必须为 card/text/image")
			continue
		}
		if itemType == "" && strings.TrimSpace(content) != "" {
			failRow(rowNo, name, "填写内容时必须指定卡密类型")
			continue
		}
		delaySeconds := atoiPublishDefault(firstImportString(m, "delay_seconds", "延迟秒"), 0)
		if delaySeconds < 0 || delaySeconds > 3600 {
			failRow(rowNo, name, "延时发货必须在 0 到 3600 秒之间")
			continue
		}

		key := strings.ToLower(name)
		group := groupsByKey[key]
		if group == nil {
			group = &batchGroup{name: name}
			groupsByKey[key] = group
			groupOrder = append(groupOrder, group)
		}
		// 分组首次出现时定位已有卡种。
		if !group.resolved {
			group.resolved = true
			existing, findErr := s.Store.Cards.FindByName(r.Context(), sess.UserID, name)
			switch {
			case findErr == nil:
				group.cardID = existing.ID
				group.itemType = existing.Kind
			case errors.Is(findErr, db.ErrNotFound):
			default:
				group.failed++
				failRow(rowNo, name, "读取卡种失败: "+findErr.Error())
				continue
			}
		}

		if itemType != "" {
			if group.itemType == "" {
				group.itemType = itemType
			} else if group.itemType != itemType {
				group.failed++
				failRow(rowNo, name, fmt.Sprintf("该卡种已按 %s 类型使用，不能混入 %s", group.itemType, itemType))
				continue
			}
		}
		// 类型与内容都留空的行只用于确保卡种存在；其余情况需先确定类型。
		onlyEnsureCard := itemType == "" && strings.TrimSpace(content) == ""
		if !onlyEnsureCard && group.itemType == "" {
			group.failed++
			failRow(rowNo, name, "缺少卡密类型")
			continue
		}

		// 卡种尚未创建时按本行元数据创建。
		if group.cardID == 0 {
			cf := &db.CardFull{
				Name:         name,
				Description:  firstImportString(m, "description", "描述"),
				Enabled:      true,
				DelaySeconds: delaySeconds,
				IsMultiSpec:  parseLooseBool(firstImportString(m, "is_multi_spec", "多规格")),
				SpecName:     firstImportString(m, "spec_name", "规格名"),
				SpecValue:    firstImportString(m, "spec_value", "规格值"),
				UserID:       sess.UserID,
			}
			if v := firstImportString(m, "enabled", "启用"); v != "" {
				cf.Enabled = parseLooseBool(v)
			}
			id, createErr := s.Store.Cards.Create(r.Context(), cf)
			cf.Kind = group.itemType
			if createErr != nil {
				group.failed++
				failRow(rowNo, name, "创建卡种失败: "+createErr.Error())
				continue
			}
			group.cardID = id
			group.created = true
		}

		if onlyEnsureCard || strings.TrimSpace(content) == "" {
			// 只建/确保卡种，不写卡密。
			continue
		}
		added, addErr := s.Store.Cards.AddItems(r.Context(), group.cardID, content)
		if addErr != nil {
			group.failed++
			failRow(rowNo, name, addErr.Error())
			continue
		}
		group.added += added
		itemsAdded += added
	}

	groups := make([]cardBatchGroupRow, 0, len(groupOrder))
	createdGroups, appendedGroups := 0, 0
	for _, group := range groupOrder {
		if group.cardID != 0 {
			if group.created {
				createdGroups++
			} else {
				appendedGroups++
			}
		}
		groups = append(groups, cardBatchGroupRow{
			Name: group.name, ItemType: group.itemType, CardID: group.cardID,
			Created: group.created, Added: group.added, Failed: group.failed,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":  true,
		"rows":     len(maps),
		"created":  createdGroups,
		"appended": appendedGroups,
		"items":    itemsAdded,
		"failed":   failedRows,
		"groups":   groups,
		"errors":   rowErrors,
	})
}
