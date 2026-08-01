// 基础库持久化负责全局记录、乐观锁、完整快照与跨旅行引用保护。
package repository

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"my-travel/server/internal/model"
)

const libraryStateID = "default"

func (r *Repository) ensureLibraryState(ctx context.Context) error {
	now := time.Now().UTC()
	_, err := r.collection("library_state").UpdateOne(
		ctx,
		bson.M{"_id": libraryStateID},
		bson.M{"$setOnInsert": bson.M{
			"version": int64(1), "reference_epoch": int64(0), "updated_at": now,
		}},
		options.UpdateOne().SetUpsert(true),
	)
	return err
}

func (r *Repository) GetLibraryState(ctx context.Context) (model.LibraryState, error) {
	var state model.LibraryState
	err := r.collection("library_state").FindOne(ctx, bson.M{"_id": libraryStateID}).Decode(&state)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return model.LibraryState{}, ErrNotFound
	}
	return state, err
}

func (r *Repository) BumpLibraryRevision(ctx context.Context) (model.LibraryState, error) {
	var state model.LibraryState
	err := r.collection("library_state").FindOneAndUpdate(
		ctx,
		bson.M{"_id": libraryStateID},
		bson.M{
			"$set": bson.M{"updated_at": time.Now().UTC()},
			"$inc": bson.M{
				"version": int64(1), "reference_epoch": int64(1),
			},
		},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&state)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return model.LibraryState{}, ErrNotFound
	}
	return state, err
}

// AdvanceLibraryReferenceEpoch makes Trip snapshot commits conflict with
// concurrent base-library mutations. The epoch is internal coordination state
// and does not change the user-visible library revision or snapshot checksum.
func (r *Repository) AdvanceLibraryReferenceEpoch(ctx context.Context) error {
	result, err := r.collection("library_state").UpdateOne(
		ctx,
		bson.M{"_id": libraryStateID},
		bson.M{"$inc": bson.M{"reference_epoch": int64(1)}},
	)
	if err != nil {
		return err
	}
	if result.MatchedCount != 1 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) ListLibraryRecords(
	ctx context.Context,
	resource model.LibraryResource,
) ([]model.LibraryRecord, error) {
	cursor, err := r.collection("library_records").Find(
		ctx,
		bson.M{"resource": resource},
		options.Find().SetSort(bson.D{{Key: "position", Value: 1}}),
	)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	rows := make([]model.LibraryRecord, 0)
	return rows, cursor.All(ctx, &rows)
}

func (r *Repository) GetLibraryRecord(
	ctx context.Context,
	resource model.LibraryResource,
	id string,
) (model.LibraryRecord, error) {
	var row model.LibraryRecord
	err := r.collection("library_records").FindOne(ctx, bson.M{
		"_id": id, "resource": resource,
	}).Decode(&row)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return model.LibraryRecord{}, ErrNotFound
	}
	return row, err
}

func (r *Repository) FindTagByCategoryAndName(
	ctx context.Context,
	category, name string,
) (model.LibraryRecord, error) {
	var row model.LibraryRecord
	err := r.collection("library_records").FindOne(ctx, bson.M{
		"resource":      model.LibraryTags,
		"data.category": category,
		"data.name":     strings.TrimSpace(name),
	}, options.FindOne().SetCollation(
		&options.Collation{Locale: "zh", Strength: 2},
	)).Decode(&row)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return model.LibraryRecord{}, ErrNotFound
	}
	return row, err
}

func (r *Repository) EnsureDefaultTagPresets(
	ctx context.Context,
) ([]model.LibraryRecord, error) {
	return r.ensureTagPresets(ctx, model.DefaultTagPresets())
}

func (r *Repository) EnsureTravelTypeTagPresets(
	ctx context.Context,
) ([]model.LibraryRecord, error) {
	return r.ensureTagPresets(ctx, model.TravelTypeTagPresets())
}

func (r *Repository) ensureTagPresets(
	ctx context.Context,
	presets []model.TagPreset,
) ([]model.LibraryRecord, error) {
	rows := make([]model.LibraryRecord, 0, len(presets))
	for _, preset := range presets {
		row, err := r.FindTagByCategoryAndName(
			ctx,
			preset.Category,
			preset.Name,
		)
		if err == nil {
			continue
		}
		if !errors.Is(err, ErrNotFound) {
			return nil, err
		}
		row, err = r.CreateLibraryRecord(ctx, model.LibraryRecord{
			Resource:  model.LibraryTags,
			RecordKey: preset.RecordKey,
			Data: map[string]any{
				"name":     preset.Name,
				"category": preset.Category,
				"color":    preset.Color,
				"status":   "active",
			},
		})
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func (r *Repository) CreateLibraryRecord(
	ctx context.Context,
	row model.LibraryRecord,
) (model.LibraryRecord, error) {
	if err := normalizeLibraryRecordData(row.Resource, row.Data); err != nil {
		return model.LibraryRecord{}, err
	}
	if err := model.ValidateLibraryRecordData(row.Resource, row.Data); err != nil {
		return model.LibraryRecord{}, err
	}
	row.RecordKey = strings.TrimSpace(row.RecordKey)
	if err := model.ValidateRecordKey(row.RecordKey); err != nil {
		return model.LibraryRecord{}, err
	}
	var latest struct {
		Position int64 `bson:"position"`
	}
	err := r.collection("library_records").FindOne(
		ctx,
		bson.M{"resource": row.Resource},
		options.FindOne().SetSort(bson.D{{Key: "position", Value: -1}}),
	).Decode(&latest)
	if err != nil && !errors.Is(err, mongo.ErrNoDocuments) {
		return model.LibraryRecord{}, err
	}
	if err == nil {
		row.Position = latest.Position + 1
	}
	now := time.Now().UTC()
	row.ID, row.Version, row.CreatedAt, row.UpdatedAt = newID(), 1, now, now
	_, err = r.collection("library_records").InsertOne(ctx, row)
	if mongo.IsDuplicateKeyError(err) {
		if row.Resource == model.LibraryTags {
			return model.LibraryRecord{}, fmt.Errorf("同一分类下的标签名称必须唯一")
		}
		return model.LibraryRecord{}, fmt.Errorf("该手机号已经绑定其他成员")
	}
	return row, err
}

func (r *Repository) UpdateLibraryRecord(
	ctx context.Context,
	resource model.LibraryResource,
	id string,
	version int64,
	data map[string]any,
) (model.LibraryRecord, error) {
	if err := normalizeLibraryRecordData(resource, data); err != nil {
		return model.LibraryRecord{}, err
	}
	if err := model.ValidateLibraryRecordData(resource, data); err != nil {
		return model.LibraryRecord{}, err
	}
	var row model.LibraryRecord
	err := r.collection("library_records").FindOneAndUpdate(
		ctx,
		bson.M{"_id": id, "resource": resource, "version": version},
		bson.M{
			"$set": bson.M{"data": data, "updated_at": time.Now().UTC()},
			"$inc": bson.M{"version": int64(1)},
		},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&row)
	if errors.Is(err, mongo.ErrNoDocuments) {
		if _, lookupErr := r.GetLibraryRecord(ctx, resource, id); errors.Is(lookupErr, ErrNotFound) {
			return model.LibraryRecord{}, ErrNotFound
		}
		return model.LibraryRecord{}, ErrConflict
	}
	if mongo.IsDuplicateKeyError(err) {
		if resource == model.LibraryTags {
			return model.LibraryRecord{}, fmt.Errorf("同一分类下的标签名称必须唯一")
		}
		return model.LibraryRecord{}, fmt.Errorf("该手机号已经绑定其他成员")
	}
	return row, err
}

func normalizeLibraryRecordData(
	resource model.LibraryResource,
	data map[string]any,
) error {
	switch resource {
	case model.LibraryMembers:
		for _, field := range []string{
			"displayName", "alternateContact", "emergencyContact", "note",
		} {
			if value, ok := data[field].(string); ok {
				data[field] = strings.TrimSpace(value)
			}
		}
		if value, exists := data["mobile"]; exists {
			text, ok := value.(string)
			if !ok {
				return fmt.Errorf("mobile must be a string")
			}
			mobile, err := model.NormalizeMobile(text, false)
			if err != nil {
				return err
			}
			if mobile == "" {
				delete(data, "mobile")
			} else {
				data["mobile"] = mobile
			}
		}
	case model.LibraryTags:
		for _, field := range []string{"name", "category", "color", "status"} {
			if value, ok := data[field].(string); ok {
				data[field] = strings.TrimSpace(value)
			}
		}
	}
	return nil
}

func (r *Repository) DeleteLibraryRecord(
	ctx context.Context,
	resource model.LibraryResource,
	id string,
	version int64,
) error {
	return r.DeleteLibraryRecordWithOptions(
		ctx,
		resource,
		id,
		version,
		false,
	)
}

func (r *Repository) DeleteLibraryRecordWithOptions(
	ctx context.Context,
	resource model.LibraryResource,
	id string,
	version int64,
	deleteLoginCredential bool,
) error {
	row, err := r.GetLibraryRecord(ctx, resource, id)
	if err != nil {
		return err
	}
	if row.Version != version {
		return ErrConflict
	}
	if err := r.validateLibraryRecordDeletion(
		ctx,
		row,
		deleteLoginCredential,
	); err != nil {
		return err
	}
	if resource == model.LibraryMembers && deleteLoginCredential {
		if err := r.DeleteMemberAuthentication(ctx, id); err != nil {
			return err
		}
	}
	result, err := r.collection("library_records").DeleteOne(ctx, bson.M{
		"_id": id, "resource": resource, "version": version,
	})
	if err != nil {
		return err
	}
	if result.DeletedCount == 1 {
		return nil
	}
	return ErrConflict
}

type LibraryReference struct {
	TripID      string         `json:"tripId"`
	TripTitle   string         `json:"tripTitle"`
	TripDeleted bool           `json:"tripDeleted"`
	Resource    model.Resource `json:"resource"`
	RecordID    string         `json:"recordId,omitempty"`
	Label       string         `json:"label"`
	Relations   []string       `json:"relations"`
}

type LibraryDeletionImpact struct {
	References              []LibraryReference `json:"references"`
	HasLoginCredential      bool               `json:"hasLoginCredential"`
	HasTripOwnership        bool               `json:"hasTripOwnership"`
	CanDeleteWithCredential bool               `json:"canDeleteWithCredential"`
	CanReplaceWithFallback  bool               `json:"canReplaceWithFallback"`
}

func (r *Repository) GetLibraryDeletionImpact(
	ctx context.Context,
	resource model.LibraryResource,
	id string,
) (LibraryDeletionImpact, error) {
	row, err := r.GetLibraryRecord(ctx, resource, id)
	if err != nil {
		return LibraryDeletionImpact{}, err
	}
	impact := LibraryDeletionImpact{
		References: []LibraryReference{},
		CanReplaceWithFallback: row.Resource == model.LibraryTags &&
			strings.TrimSpace(fmt.Sprint(row.Data["name"])) != "其他",
	}
	if row.Resource == model.LibraryMembers {
		impact.HasLoginCredential, err = r.HasMemberCredential(ctx, row.ID)
		if err != nil {
			return LibraryDeletionImpact{}, err
		}
		ownedTrips, err := r.OwnedTripCount(ctx, row.ID)
		if err != nil {
			return LibraryDeletionImpact{}, err
		}
		impact.HasTripOwnership = ownedTrips > 0
	}
	filter := libraryReferenceFilter(row)
	if len(filter) == 0 {
		return impact, nil
	}
	cursor, err := r.collection("records").Find(
		ctx,
		filter,
		options.Find().SetSort(bson.D{
			{Key: "trip_id", Value: 1},
			{Key: "resource", Value: 1},
			{Key: "position", Value: 1},
		}),
	)
	if err != nil {
		return LibraryDeletionImpact{}, err
	}
	defer cursor.Close(ctx)
	var records []model.Record
	if err := cursor.All(ctx, &records); err != nil {
		return LibraryDeletionImpact{}, err
	}
	trips := map[string]model.Trip{}
	references := make([]LibraryReference, 0, len(records))
	privatePackingReferenceIndexes := map[string]int{}
	for _, record := range records {
		trip, exists := trips[record.TripID]
		if !exists {
			var lookupErr error
			trip, lookupErr = r.getTripIncludingDeleted(ctx, record.TripID)
			if lookupErr != nil {
				return LibraryDeletionImpact{}, lookupErr
			}
			trips[record.TripID] = trip
		}
		relations := libraryReferenceRelations(row, record)
		if row.Resource == model.LibraryMembers && record.Resource == model.PackingItems {
			if index, exists := privatePackingReferenceIndexes[record.TripID]; exists {
				references[index].Relations = mergeUniqueStrings(
					references[index].Relations,
					relations,
				)
				continue
			}
			privatePackingReferenceIndexes[record.TripID] = len(references)
			references = append(references, LibraryReference{
				TripID: record.TripID, TripTitle: trip.Title,
				TripDeleted: trip.DeletedAt != nil, Resource: record.Resource,
				RecordID: "", Label: "个人物品（内容已隐藏）", Relations: relations,
			})
			continue
		}
		references = append(references, LibraryReference{
			TripID: record.TripID, TripTitle: trip.Title, TripDeleted: trip.DeletedAt != nil,
			Resource: record.Resource, RecordID: record.ID,
			Label: libraryReferenceLabel(record), Relations: relations,
		})
	}
	impact.References = references
	impact.CanDeleteWithCredential =
		row.Resource == model.LibraryMembers &&
			impact.HasLoginCredential &&
			memberAuthenticationDisabled(row) &&
			!impact.HasTripOwnership &&
			len(impact.References) == 0
	return impact, nil
}

func (r *Repository) validateLibraryRecordDeletion(
	ctx context.Context,
	row model.LibraryRecord,
	deleteLoginCredential bool,
) error {
	impact, err := r.GetLibraryDeletionImpact(ctx, row.Resource, row.ID)
	if err != nil {
		return err
	}
	if len(impact.References) > 0 {
		name := map[model.LibraryResource]string{
			model.LibraryMembers: "成员",
			model.LibraryTags:    "标签",
		}[row.Resource]
		return fmt.Errorf(
			"%s仍被以下内容使用，请先处理后再删除：%s: %w",
			name,
			LibraryReferenceSummary(impact.References),
			ErrReferenced,
		)
	}
	if impact.HasTripOwnership {
		return fmt.Errorf(
			"该成员仍是旅行负责人，不能删除；请保留负责人身份: %w",
			ErrReferenced,
		)
	}
	if impact.HasLoginCredential {
		if deleteLoginCredential && memberAuthenticationDisabled(row) {
			return nil
		}
		if !memberAuthenticationDisabled(row) {
			return fmt.Errorf(
				"该成员仍绑定启用中的登录账号；请先停用成员，再确认注销登录凭据并删除: %w",
				ErrReferenced,
			)
		}
		return fmt.Errorf(
			"该成员仍绑定登录账号；请明确确认注销登录凭据后再删除: %w",
			ErrReferenced,
		)
	}
	return nil
}

func libraryReferenceFilter(row model.LibraryRecord) bson.M {
	switch row.Resource {
	case model.LibraryMembers:
		return bson.M{"$or": bson.A{
			bson.M{"resource": model.Members, "data.memberId": row.ID},
			bson.M{"resource": model.Bookings, "data.bookerMemberId": row.ID},
			bson.M{"resource": model.Expenses, "data.payerMemberId": row.ID},
			bson.M{"resource": model.Expenses, "data.splitMemberIds": row.ID},
			bson.M{"resource": model.PackingItems, "data.ownerMemberId": row.ID},
			bson.M{"resource": model.ResearchEvidence, "data.recordedByMemberId": row.ID},
			bson.M{"created_by_member_id": row.ID},
			bson.M{"updated_by_member_id": row.ID},
		}}
	case model.LibraryTags:
		return bson.M{"$or": bson.A{
			bson.M{"resource": model.Places, "data.type": row.ID},
			bson.M{"resource": model.Bookings, "data.type": row.ID},
			bson.M{"resource": model.Expenses, "data.type": row.ID},
			bson.M{"resource": model.Bookings, "data.platformId": row.ID},
			bson.M{"resource": model.ResearchEvidence, "data.platformId": row.ID},
		}}
	default:
		return nil
	}
}

func libraryReferenceLabel(row model.Record) string {
	for _, field := range []string{"title", "name", "item", "primaryOption", "purpose"} {
		raw, exists := row.Data[field]
		if !exists || raw == nil {
			continue
		}
		if value := strings.TrimSpace(fmt.Sprint(raw)); value != "" {
			return value
		}
	}
	return row.RecordKey
}

func libraryReferenceRelations(
	libraryRecord model.LibraryRecord,
	row model.Record,
) []string {
	relations := make([]string, 0, 2)
	switch libraryRecord.Resource {
	case model.LibraryMembers:
		if row.Resource == model.Members &&
			fmt.Sprint(row.Data["memberId"]) == libraryRecord.ID {
			relations = append(relations, "trip-member")
		}
		if row.Resource == model.Expenses &&
			fmt.Sprint(row.Data["payerMemberId"]) == libraryRecord.ID {
			relations = append(relations, "expense-payer")
		}
		if row.Resource == model.Bookings &&
			fmt.Sprint(row.Data["bookerMemberId"]) == libraryRecord.ID {
			relations = append(relations, "booking-booker")
		}
		if row.Resource == model.Expenses {
			for _, memberID := range stringValues(row.Data["splitMemberIds"]) {
				if memberID == libraryRecord.ID {
					relations = append(relations, "expense-split")
					break
				}
			}
		}
		if row.Resource == model.PackingItems &&
			fmt.Sprint(row.Data["ownerMemberId"]) == libraryRecord.ID {
			relations = append(relations, "packing-owner")
		}
		if row.Resource == model.ResearchEvidence &&
			fmt.Sprint(row.Data["recordedByMemberId"]) == libraryRecord.ID {
			relations = append(relations, "research-recorder")
		}
		if row.CreatedByMemberID == libraryRecord.ID {
			relations = append(relations, "record-creator")
		}
		if row.UpdatedByMemberID == libraryRecord.ID {
			relations = append(relations, "record-updater")
		}
	case model.LibraryTags:
		if row.Resource == model.Places &&
			fmt.Sprint(row.Data["type"]) == libraryRecord.ID {
			relations = append(relations, "place-type")
		}
		if row.Resource == model.Expenses &&
			fmt.Sprint(row.Data["type"]) == libraryRecord.ID {
			relations = append(relations, "expense-type")
		}
		if row.Resource == model.Bookings &&
			fmt.Sprint(row.Data["type"]) == libraryRecord.ID {
			relations = append(relations, "booking-type")
		}
		if row.Resource == model.Bookings &&
			fmt.Sprint(row.Data["platformId"]) == libraryRecord.ID {
			relations = append(relations, "booking-platform")
		}
		if row.Resource == model.ResearchEvidence &&
			fmt.Sprint(row.Data["platformId"]) == libraryRecord.ID {
			relations = append(relations, "research-platform")
		}
	}
	return relations
}

func LibraryReferenceSummary(references []LibraryReference) string {
	const visibleLimit = 5
	parts := make([]string, 0, min(len(references), visibleLimit)+1)
	for _, reference := range references[:min(len(references), visibleLimit)] {
		tripTitle := reference.TripTitle
		if reference.TripDeleted {
			tripTitle += "（已删除旅行）"
		}
		relations := make([]string, 0, len(reference.Relations))
		for _, relation := range reference.Relations {
			relations = append(relations, libraryRelationLabel(relation))
		}
		parts = append(parts, fmt.Sprintf(
			"%s / %s / %s（%s）",
			tripTitle,
			resourceDisplayName(reference.Resource),
			reference.Label,
			strings.Join(relations, "、"),
		))
	}
	if len(references) > visibleLimit {
		parts = append(parts, fmt.Sprintf("另有 %d 条", len(references)-visibleLimit))
	}
	return strings.Join(parts, "；")
}

func libraryRelationLabel(relation string) string {
	return map[string]string{
		"trip-member":       "旅行成员",
		"booking-booker":    "预订人",
		"research-recorder": "资料记录人",
		"expense-payer":     "费用付款人",
		"expense-split":     "费用分摊成员",
		"packing-owner":     "个人物品所有人",
		"record-creator":    "创建人",
		"record-updater":    "最后修改人",
		"place-type":        "地点类型",
		"expense-type":      "费用类型",
		"booking-type":      "预订类型",
		"booking-platform":  "预订平台",
		"research-platform": "资料平台",
	}[relation]
}

func resourceDisplayName(resource model.Resource) string {
	return map[model.Resource]string{
		model.Days:             "执行日",
		model.Places:           "地点",
		model.Segments:         "交通段",
		model.Bookings:         "预订",
		model.Expenses:         "费用",
		model.PackingItems:     "物品",
		model.ResearchEvidence: "资料",
		model.Members:          "旅行成员",
	}[resource]
}

func mergeUniqueStrings(current, additions []string) []string {
	seen := make(map[string]struct{}, len(current)+len(additions))
	merged := make([]string, 0, len(current)+len(additions))
	for _, value := range current {
		seen[value] = struct{}{}
		merged = append(merged, value)
	}
	for _, value := range additions {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		merged = append(merged, value)
	}
	return merged
}

func (r *Repository) ReplaceTagWithFallbackAndDelete(
	ctx context.Context,
	id string,
	version int64,
	actorMemberID string,
) (model.LibraryRecord, []string, error) {
	if err := validateRecordActor(actorMemberID, "updatedByMemberId"); err != nil {
		return model.LibraryRecord{}, nil, err
	}
	row, err := r.GetLibraryRecord(ctx, model.LibraryTags, id)
	if err != nil {
		return model.LibraryRecord{}, nil, err
	}
	impact, err := r.GetLibraryDeletionImpact(ctx, model.LibraryTags, id)
	if err != nil {
		return model.LibraryRecord{}, nil, err
	}
	if len(impact.References) == 0 {
		return model.LibraryRecord{}, nil, r.DeleteLibraryRecord(
			ctx,
			model.LibraryTags,
			id,
			version,
		)
	}
	if strings.TrimSpace(fmt.Sprint(row.Data["name"])) == "其他" {
		return model.LibraryRecord{}, nil, fmt.Errorf("“其他”是标签删除时的兜底项，仍被引用时不能删除")
	}
	category := libraryString(row.Data, "category")
	fallback, err := r.getOrCreateFallbackTag(ctx, category)
	if err != nil {
		return model.LibraryRecord{}, nil, err
	}
	tripSet := map[string]struct{}{}
	for _, reference := range impact.References {
		tripSet[reference.TripID] = struct{}{}
	}
	if _, err = r.collection("records").UpdateMany(
		ctx,
		bson.M{"resource": model.Places, "data.type": id},
		bson.M{
			"$set": bson.M{
				"data.type":            fallback.ID,
				"updated_at":           time.Now().UTC(),
				"updated_by_member_id": actorMemberID,
			},
			"$inc": bson.M{"version": 1},
		},
	); err != nil {
		return model.LibraryRecord{}, nil, err
	}
	if _, err = r.collection("records").UpdateMany(
		ctx,
		bson.M{
			"resource":        bson.M{"$in": bson.A{model.Bookings, model.ResearchEvidence}},
			"data.platformId": id,
		},
		bson.M{
			"$set": bson.M{
				"data.platformId":      fallback.ID,
				"updated_at":           time.Now().UTC(),
				"updated_by_member_id": actorMemberID,
			},
			"$inc": bson.M{"version": 1},
		},
	); err != nil {
		return model.LibraryRecord{}, nil, err
	}
	if _, err = r.collection("records").UpdateMany(
		ctx,
		bson.M{
			"resource":  bson.M{"$in": bson.A{model.Bookings, model.Expenses}},
			"data.type": id,
		},
		bson.M{
			"$set": bson.M{
				"data.type":            fallback.ID,
				"updated_at":           time.Now().UTC(),
				"updated_by_member_id": actorMemberID,
			},
			"$inc": bson.M{"version": 1},
		},
	); err != nil {
		return model.LibraryRecord{}, nil, err
	}
	result, err := r.collection("library_records").DeleteOne(ctx, bson.M{
		"_id": id, "resource": model.LibraryTags, "version": version,
	})
	if err != nil {
		return model.LibraryRecord{}, nil, err
	}
	if result.DeletedCount != 1 {
		return model.LibraryRecord{}, nil, ErrConflict
	}
	tripIDs := make([]string, 0, len(tripSet))
	for tripID := range tripSet {
		tripIDs = append(tripIDs, tripID)
	}
	sort.Strings(tripIDs)
	return fallback, tripIDs, nil
}

func (r *Repository) getOrCreateFallbackTag(
	ctx context.Context,
	category string,
) (model.LibraryRecord, error) {
	if category != model.TagCategoryPlaceType &&
		category != model.TagCategoryExpenseType &&
		category != model.TagCategoryPlatformType {
		category = "general"
	}
	var row model.LibraryRecord
	err := r.collection("library_records").FindOne(ctx, bson.M{
		"resource":      model.LibraryTags,
		"data.name":     "其他",
		"data.category": category,
	}).Decode(&row)
	if err == nil {
		return row, nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return model.LibraryRecord{}, err
	}
	return r.CreateLibraryRecord(ctx, model.LibraryRecord{
		Resource:  model.LibraryTags,
		RecordKey: "tag-other-" + category,
		Data: map[string]any{
			"name": "其他", "color": "#718079", "category": category, "status": "active",
		},
	})
}

func libraryString(data map[string]any, field string) string {
	value, _ := data[field].(string)
	return strings.TrimSpace(value)
}

func (r *Repository) TripIDsForGlobalMember(
	ctx context.Context,
	memberID string,
) ([]string, error) {
	var tripIDs []string
	err := r.collection("records").Distinct(ctx, "trip_id", bson.M{
		"resource":      model.Members,
		"data.memberId": memberID,
	}).Decode(&tripIDs)
	if err != nil {
		return nil, err
	}
	return tripIDs, nil
}

func (r *Repository) BuildLibrarySnapshot(
	ctx context.Context,
	reason string,
) (model.LibrarySnapshot, error) {
	if mongo.SessionFromContext(ctx) != nil {
		return r.buildLibrarySnapshotInTransaction(ctx, reason)
	}
	var result model.LibrarySnapshot
	err := r.WithTransaction(ctx, func(tx context.Context) error {
		item, err := r.buildLibrarySnapshotInTransaction(tx, reason)
		if err != nil {
			return err
		}
		result = item
		return nil
	})
	if err != nil {
		return model.LibrarySnapshot{}, err
	}
	return result, nil
}

func (r *Repository) buildLibrarySnapshotInTransaction(
	ctx context.Context,
	reason string,
) (model.LibrarySnapshot, error) {
	payload, content, err := r.exportLibrarySnapshot(ctx, reason)
	if err != nil {
		return model.LibrarySnapshot{}, err
	}
	sum := sha256.Sum256(payload)
	item := model.LibrarySnapshot{
		ID:       newID(),
		Revision: content.Revision, Reason: reason, Payload: payload,
		SHA256: hex.EncodeToString(sum[:]), ContentChecksum: content.ContentChecksum,
		MirrorState: "pending", CreatedAt: time.Now().UTC(),
	}
	if _, err := r.collection("library_snapshots").InsertOne(ctx, item); err != nil {
		return model.LibrarySnapshot{}, err
	}
	return item, nil
}

func (r *Repository) ExportLibrarySnapshot(
	ctx context.Context,
	reason string,
) ([]byte, error) {
	if mongo.SessionFromContext(ctx) != nil {
		payload, _, err := r.exportLibrarySnapshot(ctx, reason)
		return payload, err
	}
	var result []byte
	err := r.withSnapshotReadTransaction(ctx, func(tx context.Context) error {
		payload, _, err := r.exportLibrarySnapshot(tx, reason)
		if err != nil {
			return err
		}
		result = append(result[:0], payload...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (r *Repository) exportLibrarySnapshot(
	ctx context.Context,
	reason string,
) ([]byte, model.LibrarySnapshotPayload, error) {
	state, err := r.GetLibraryState(ctx)
	if err != nil {
		return nil, model.LibrarySnapshotPayload{}, err
	}
	datasets := make(map[string][]model.LibraryRecord, len(model.LibraryResources()))
	for _, resource := range model.LibraryResources() {
		rows, err := r.ListLibraryRecords(ctx, resource)
		if err != nil {
			return nil, model.LibrarySnapshotPayload{}, err
		}
		datasets[string(resource)] = rows
	}
	content := model.LibrarySnapshotPayload{
		ExportedAt: time.Now().UTC(),
		Reason:     reason,
		Revision:   state.Version,
		Datasets:   datasets,
	}
	content.ContentChecksum, err = content.ComputedContentChecksum()
	if err != nil {
		return nil, model.LibrarySnapshotPayload{}, err
	}
	payload, err := json.Marshal(content)
	if err != nil {
		return nil, model.LibrarySnapshotPayload{}, err
	}
	if _, err := ValidateLibrarySnapshotPayload(payload); err != nil {
		return nil, model.LibrarySnapshotPayload{}, fmt.Errorf("基础库不能生成有效快照: %w", err)
	}
	return payload, content, nil
}

func (r *Repository) GetLatestLibrarySnapshot(
	ctx context.Context,
) (model.LibrarySnapshot, error) {
	var item model.LibrarySnapshot
	err := r.collection("library_snapshots").FindOne(
		ctx,
		bson.M{},
		options.FindOne().SetSort(bson.D{{Key: "created_at", Value: -1}}),
	).Decode(&item)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return model.LibrarySnapshot{}, ErrNotFound
	}
	return item, err
}

func (r *Repository) MarkLibrarySnapshotMirrored(
	ctx context.Context,
	id, state string,
) error {
	_, err := r.collection("library_snapshots").UpdateOne(
		ctx,
		bson.M{"_id": id},
		bson.M{"$set": bson.M{"mirror_state": state}},
	)
	return err
}

func (r *Repository) ListPendingLibrarySnapshots(
	ctx context.Context,
	limit int64,
) ([]model.LibrarySnapshot, error) {
	if limit < 1 || limit > 1000 {
		return nil, fmt.Errorf("pending snapshot limit must be between 1 and 1000")
	}
	cursor, err := r.collection("library_snapshots").Find(
		ctx,
		bson.M{"mirror_state": "pending"},
		options.Find().SetSort(bson.D{{Key: "created_at", Value: 1}}).SetLimit(limit),
	)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	result := make([]model.LibrarySnapshot, 0)
	return result, cursor.All(ctx, &result)
}

func VerifyStoredLibrarySnapshot(item model.LibrarySnapshot) error {
	sum := sha256.Sum256(item.Payload)
	if !strings.EqualFold(item.SHA256, hex.EncodeToString(sum[:])) {
		return fmt.Errorf("stored library snapshot SHA-256 does not match payload")
	}
	payload, err := ValidateLibrarySnapshotPayload(item.Payload)
	if err != nil {
		return fmt.Errorf("stored library snapshot payload is invalid: %w", err)
	}
	if item.Revision != payload.Revision ||
		item.ContentChecksum != payload.ContentChecksum {
		return fmt.Errorf("stored library snapshot metadata does not match payload")
	}
	return nil
}

func ValidateLibrarySnapshotPayload(raw []byte) (model.LibrarySnapshotPayload, error) {
	var payload model.LibrarySnapshotPayload
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return payload, fmt.Errorf("基础库快照 JSON 无效: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return payload, fmt.Errorf("基础库快照只能包含一个对象")
	}
	if payload.Revision < 1 {
		return payload, fmt.Errorf("基础库修订号无效")
	}
	for _, resource := range model.LibraryResources() {
		rows, exists := payload.Datasets[string(resource)]
		if !exists {
			return payload, fmt.Errorf("基础库快照缺少 %s", resource)
		}
		for _, row := range rows {
			if row.Resource != resource {
				return payload, fmt.Errorf("基础库资源类型不匹配")
			}
			if err := model.ValidateLibraryRecordData(resource, row.Data); err != nil {
				return payload, err
			}
		}
	}
	checksum, err := payload.ComputedContentChecksum()
	if err != nil {
		return payload, err
	}
	if checksum != payload.ContentChecksum {
		return payload, fmt.Errorf("基础库快照校验和不匹配")
	}
	return payload, nil
}
