// 快照仓库集中处理完整聚合导出、校验、比较与原子替换。
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

type snapshotValidationError struct {
	message string
}

func (err snapshotValidationError) Error() string {
	return err.message
}

func (err snapshotValidationError) Unwrap() error {
	return ErrSnapshotValidation
}

func snapshotValidationErrorf(format string, values ...any) error {
	return snapshotValidationError{message: fmt.Sprintf(format, values...)}
}

func validateSnapshotTripDeletionTransition(current, incoming model.Trip) error {
	sameDeletedAt := current.DeletedAt == nil && incoming.DeletedAt == nil
	if current.DeletedAt != nil && incoming.DeletedAt != nil {
		sameDeletedAt = current.DeletedAt.Equal(*incoming.DeletedAt)
	}
	if !sameDeletedAt ||
		current.DeletedByMemberID != incoming.DeletedByMemberID {
		return snapshotValidationErrorf("快照不能改变当前旅行的逻辑删除状态")
	}
	return nil
}

func (r *Repository) buildSnapshotPayload(
	ctx context.Context,
	tripID, reason string,
) (model.SnapshotPayload, []byte, error) {
	trip, err := r.getTripIncludingDeleted(ctx, tripID)
	if err != nil {
		return model.SnapshotPayload{}, nil, err
	}
	datasets := make(map[string][]model.Record, len(model.Resources()))
	for _, resource := range model.Resources() {
		rows, err := r.listRecords(ctx, tripID, resource)
		if err != nil {
			return model.SnapshotPayload{}, nil, err
		}
		datasets[string(resource)] = rows
	}
	content := model.SnapshotPayload{
		ExportedAt: time.Now().UTC(),
		Reason:     reason,
		Revision:   trip.Version,
		Trip:       trip,
		Datasets:   datasets,
	}
	content.ContentChecksum, err = content.ComputedContentChecksum()
	if err != nil {
		return model.SnapshotPayload{}, nil, err
	}
	payload, err := json.Marshal(content)
	if err != nil {
		return model.SnapshotPayload{}, nil, err
	}
	if _, err := ValidateSnapshotPayload(payload, tripID); err != nil {
		return model.SnapshotPayload{}, nil, fmt.Errorf(
			"runtime aggregate cannot produce a valid snapshot: %w",
			err,
		)
	}
	if err := r.ValidateSnapshotExternalReferences(ctx, content); err != nil {
		if errors.Is(err, ErrSnapshotValidation) {
			return model.SnapshotPayload{}, nil, fmt.Errorf(
				"当前旅行不能生成有效快照: %w",
				err,
			)
		}
		return model.SnapshotPayload{}, nil, err
	}
	return content, payload, nil
}

func (r *Repository) BuildSnapshot(ctx context.Context, tripID, reason string) (model.Snapshot, error) {
	if mongo.SessionFromContext(ctx) != nil {
		return r.buildSnapshotInTransaction(ctx, tripID, reason)
	}
	var result model.Snapshot
	err := r.WithTransaction(ctx, func(tx context.Context) error {
		item, err := r.buildSnapshotInTransaction(tx, tripID, reason)
		if err != nil {
			return err
		}
		result = item
		return nil
	})
	return result, err
}

func (r *Repository) buildSnapshotInTransaction(
	ctx context.Context,
	tripID, reason string,
) (model.Snapshot, error) {
	content, payload, err := r.buildSnapshotPayload(ctx, tripID, reason)
	if err != nil {
		return model.Snapshot{}, err
	}
	if err := r.AdvanceLibraryReferenceEpoch(ctx); err != nil {
		return model.Snapshot{}, err
	}
	sum := sha256.Sum256(payload)
	snapshot := model.Snapshot{
		ID:              newID(),
		TripID:          tripID,
		Revision:        content.Revision,
		Reason:          reason,
		Payload:         payload,
		SHA256:          hex.EncodeToString(sum[:]),
		ContentChecksum: content.ContentChecksum,
		MirrorState:     "pending",
		CreatedAt:       time.Now().UTC(),
	}
	if _, err := r.collection("snapshots").InsertOne(ctx, snapshot); err != nil {
		return model.Snapshot{}, err
	}
	return snapshot, nil
}

func (r *Repository) ListSnapshots(ctx context.Context, tripID string) ([]model.Snapshot, error) {
	cursor, err := r.collection("snapshots").Find(
		ctx,
		bson.M{"trip_id": tripID},
		options.Find().
			SetProjection(bson.M{"payload": 0}).
			SetSort(bson.D{{Key: "created_at", Value: -1}}),
	)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	result := make([]model.Snapshot, 0)
	return result, cursor.All(ctx, &result)
}

func (r *Repository) ListPendingSnapshots(ctx context.Context, limit int64) ([]model.Snapshot, error) {
	if limit < 1 || limit > 1000 {
		return nil, fmt.Errorf("pending snapshot limit must be between 1 and 1000")
	}
	cursor, err := r.collection("snapshots").Find(
		ctx,
		bson.M{"mirror_state": "pending"},
		options.Find().SetSort(bson.D{{Key: "created_at", Value: 1}}).SetLimit(limit),
	)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	result := make([]model.Snapshot, 0)
	return result, cursor.All(ctx, &result)
}

func (r *Repository) GetSnapshot(ctx context.Context, tripID, snapshotID string) (model.Snapshot, error) {
	var snapshot model.Snapshot
	err := r.collection("snapshots").FindOne(ctx, bson.M{"_id": snapshotID, "trip_id": tripID}).Decode(&snapshot)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return model.Snapshot{}, ErrNotFound
	}
	return snapshot, err
}

func (r *Repository) MarkSnapshotMirrored(ctx context.Context, snapshotID, state string) error {
	_, err := r.collection("snapshots").UpdateOne(
		ctx,
		bson.M{"_id": snapshotID},
		bson.M{"$set": bson.M{"mirror_state": state}},
	)
	return err
}

func ValidateSnapshotPayload(raw []byte, tripID string) (model.SnapshotPayload, error) {
	var payload model.SnapshotPayload
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return model.SnapshotPayload{}, fmt.Errorf("snapshot JSON is invalid: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return model.SnapshotPayload{}, fmt.Errorf("snapshot JSON must contain exactly one object")
	}
	if payload.Trip.ID != tripID {
		return model.SnapshotPayload{}, fmt.Errorf("snapshot trip id does not match path")
	}
	if err := payload.Trip.Validate(); err != nil {
		return model.SnapshotPayload{}, fmt.Errorf("snapshot trip is invalid: %w", err)
	}
	if payload.Revision < 1 || payload.Trip.Version != payload.Revision {
		return model.SnapshotPayload{}, fmt.Errorf("snapshot revision must equal trip version")
	}
	expectedResources := make(map[string]model.Resource, len(model.Resources()))
	for _, resource := range model.Resources() {
		expectedResources[string(resource)] = resource
		if _, ok := payload.Datasets[string(resource)]; !ok {
			return model.SnapshotPayload{}, fmt.Errorf("snapshot is missing resource %s", resource)
		}
	}
	if len(payload.Datasets) != len(expectedResources) {
		return model.SnapshotPayload{}, fmt.Errorf("snapshot must contain exactly %d resources", len(expectedResources))
	}

	ids := make(map[model.Resource]map[string]model.Record, len(expectedResources))
	globalIDs := make(map[string]string)
	for dataset, rows := range payload.Datasets {
		resource, ok := expectedResources[dataset]
		if !ok {
			return model.SnapshotPayload{}, fmt.Errorf("snapshot contains unknown resource %s", dataset)
		}
		ids[resource] = make(map[string]model.Record, len(rows))
		recordKeys := make(map[string]struct{}, len(rows))
		positions := make(map[string]struct{}, len(rows))
		for index, row := range rows {
			if err := model.ValidateRecordID(row.ID); err != nil {
				return model.SnapshotPayload{}, fmt.Errorf("%s[%d]: %w", dataset, index, err)
			}
			if err := model.ValidateRecordKey(row.RecordKey); err != nil {
				return model.SnapshotPayload{}, fmt.Errorf("%s[%d]: %w", dataset, index, err)
			}
			if row.Version < 1 || row.Position < 0 {
				return model.SnapshotPayload{}, fmt.Errorf(
					"%s[%d] must have positive version and non-negative position",
					dataset,
					index,
				)
			}
			for field, memberID := range map[string]string{
				"createdByMemberId": row.CreatedByMemberID,
				"updatedByMemberId": row.UpdatedByMemberID,
			} {
				if err := model.ValidateRecordID(memberID); err != nil {
					return model.SnapshotPayload{}, fmt.Errorf(
						"%s[%d].%s is required and must reference a valid member id",
						dataset,
						index,
						field,
					)
				}
			}
			if row.TripID != tripID || row.Resource != resource {
				return model.SnapshotPayload{}, fmt.Errorf("%s[%d] crosses trip or resource boundary", dataset, index)
			}
			if err := model.ValidateRecordData(resource, row.Data); err != nil {
				return model.SnapshotPayload{}, fmt.Errorf("%s record %s is invalid: %w", dataset, row.ID, err)
			}
			if _, exists := ids[resource][row.ID]; exists {
				return model.SnapshotPayload{}, fmt.Errorf("%s contains duplicate id %s", dataset, row.ID)
			}
			if previousResource, exists := globalIDs[row.ID]; exists {
				return model.SnapshotPayload{}, fmt.Errorf(
					"record id %s is reused by %s and %s",
					row.ID,
					previousResource,
					dataset,
				)
			}
			if _, exists := recordKeys[row.RecordKey]; exists {
				return model.SnapshotPayload{}, fmt.Errorf("%s contains duplicate recordKey %s", dataset, row.RecordKey)
			}
			positionKey := fmt.Sprintf("%s:%d", groupKey(resource, row.Data), row.Position)
			if _, exists := positions[positionKey]; exists {
				return model.SnapshotPayload{}, fmt.Errorf("%s contains duplicate position in one group", dataset)
			}
			ids[resource][row.ID] = row
			globalIDs[row.ID] = dataset
			recordKeys[row.RecordKey] = struct{}{}
			positions[positionKey] = struct{}{}
		}
	}
	if err := validateSnapshotReferences(ids); err != nil {
		return model.SnapshotPayload{}, err
	}
	if err := validateSnapshotPackingOwners(ids); err != nil {
		return model.SnapshotPayload{}, err
	}
	if err := validateSnapshotBookingExpenses(ids); err != nil {
		return model.SnapshotPayload{}, err
	}
	if err := validateSnapshotDays(payload); err != nil {
		return model.SnapshotPayload{}, err
	}
	checksum, err := payload.ComputedContentChecksum()
	if err != nil {
		return model.SnapshotPayload{}, err
	}
	if payload.ContentChecksum == "" || !strings.EqualFold(payload.ContentChecksum, checksum) {
		return model.SnapshotPayload{}, fmt.Errorf(
			"contentChecksum does not match snapshot content: got %s, computed %s",
			payload.ContentChecksum,
			checksum,
		)
	}
	return payload, nil
}

func validateSnapshotDays(payload model.SnapshotPayload) error {
	seenDates := make(map[string]struct{}, len(payload.Datasets[string(model.Days)]))
	lastDate := ""
	for _, day := range payload.Datasets[string(model.Days)] {
		date := fmt.Sprint(day.Data["date"])
		if date < payload.Trip.StartDate || date > payload.Trip.EndDate {
			return fmt.Errorf("day %s is outside the trip date range", day.ID)
		}
		if _, duplicate := seenDates[date]; duplicate {
			return fmt.Errorf("trip contains duplicate day date %s", date)
		}
		if lastDate != "" && date < lastDate {
			return fmt.Errorf("day positions must follow calendar order")
		}
		seenDates[date] = struct{}{}
		lastDate = date
	}
	return nil
}

func validateSnapshotReferences(ids map[model.Resource]map[string]model.Record) error {
	for resource, records := range ids {
		for _, row := range records {
			for _, reference := range recordReferences(resource, row.Data) {
				for _, id := range reference.IDs {
					if _, ok := ids[reference.Target][id]; !ok {
						return fmt.Errorf(
							"%s record %s field %s references missing %s %s",
							resource,
							row.ID,
							reference.Field,
							reference.Target,
							id,
						)
					}
				}
			}
		}
	}
	for _, segment := range ids[model.Segments] {
		dayID := fmt.Sprint(segment.Data["dayId"])
		if _, ok := ids[model.Days][dayID]; !ok {
			return fmt.Errorf("segment %s references missing day %s", segment.ID, dayID)
		}
		day := ids[model.Days][dayID]
		places := map[string]struct{}{}
		for _, placeID := range dayPlaceIDs(day.Data) {
			places[placeID] = struct{}{}
		}
		for _, field := range []string{"fromPlaceId", "toPlaceId"} {
			placeID := fmt.Sprint(segment.Data[field])
			if _, ok := places[placeID]; !ok {
				return fmt.Errorf("segment %s references a place outside its day", segment.ID)
			}
		}
	}
	return nil
}

func validateSnapshotBookingExpenses(
	ids map[model.Resource]map[string]model.Record,
) error {
	expensesByBooking := make(map[string]model.Record)
	for _, expense := range ids[model.Expenses] {
		bookingID := strings.TrimSpace(fmt.Sprint(expense.Data["bookingRecordId"]))
		if bookingID == "" || bookingID == "<nil>" {
			continue
		}
		if _, duplicate := expensesByBooking[bookingID]; duplicate {
			return fmt.Errorf("预订 %s 关联了多条自动费用", bookingID)
		}
		expensesByBooking[bookingID] = expense
	}
	for bookingID, booking := range ids[model.Bookings] {
		expense, exists := expensesByBooking[bookingID]
		if !exists {
			return fmt.Errorf("预订 %s 必须且只能关联一条自动费用", bookingID)
		}
		if fmt.Sprint(expense.Data["item"]) != fmt.Sprint(booking.Data["item"]) {
			return fmt.Errorf("预订 %s 的自动费用项目必须与预订一致", bookingID)
		}
		if bookingType := strings.TrimSpace(fmt.Sprint(booking.Data["type"])); bookingType != "" && bookingType != "<nil>" &&
			fmt.Sprint(expense.Data["type"]) != bookingType {
			return fmt.Errorf("预订 %s 的自动费用类型必须与预订一致", bookingID)
		}
		if bookerID := strings.TrimSpace(fmt.Sprint(booking.Data["bookerMemberId"])); bookerID != "" && bookerID != "<nil>" &&
			fmt.Sprint(expense.Data["payerMemberId"]) != bookerID {
			return fmt.Errorf("预订 %s 的自动费用支付人必须与预订人一致", bookingID)
		}
	}
	return nil
}

func validateSnapshotPackingOwners(
	ids map[model.Resource]map[string]model.Record,
) error {
	selected := make(map[string]struct{}, len(ids[model.Members]))
	for _, member := range ids[model.Members] {
		memberID := strings.TrimSpace(fmt.Sprint(member.Data["memberId"]))
		selected[memberID] = struct{}{}
	}
	for _, item := range ids[model.PackingItems] {
		ownerMemberID := strings.TrimSpace(fmt.Sprint(item.Data["ownerMemberId"]))
		if _, ok := selected[ownerMemberID]; !ok {
			return fmt.Errorf("物品 %s 的 ownerMemberId 不是当前旅行成员", item.ID)
		}
	}
	return nil
}

func recordReferences(resource model.Resource, data map[string]any) []recordReference {
	references := make([]recordReference, 0)
	addOne := func(field string, target model.Resource) {
		value := strings.TrimSpace(fmt.Sprint(data[field]))
		if value != "" && value != "<nil>" {
			references = append(references, recordReference{Field: field, Target: target, IDs: []string{value}})
		}
	}
	addMany := func(field string, target model.Resource) {
		values := stringValues(data[field])
		if len(values) > 0 {
			references = append(references, recordReference{Field: field, Target: target, IDs: values})
		}
	}
	switch resource {
	case model.Days:
		for _, id := range dayPlaceIDs(data) {
			references = append(references, recordReference{
				Field: "placeEntries", Target: model.Places, IDs: []string{id},
			})
		}
	case model.Segments:
		addOne("dayId", model.Days)
		addOne("fromPlaceId", model.Places)
		addOne("toPlaceId", model.Places)
	case model.Bookings:
		addOne("placeId", model.Places)
	case model.Expenses:
		addOne("bookingRecordId", model.Bookings)
	case model.PackingItems:
		addMany("dayIds", model.Days)
	case model.ResearchEvidence:
		addMany("dayIds", model.Days)
		addOne("placeId", model.Places)
	}
	return references
}

func stringValues(value any) []string {
	switch values := value.(type) {
	case []string:
		return values
	case []any:
		result := make([]string, 0, len(values))
		for _, value := range values {
			if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
				result = append(result, strings.TrimSpace(text))
			}
		}
		return result
	case bson.A:
		result := make([]string, 0, len(values))
		for _, value := range values {
			if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
				result = append(result, strings.TrimSpace(text))
			}
		}
		return result
	default:
		return nil
	}
}

func dayPlaceEntries(value any) []map[string]any {
	asMap := func(raw any) (map[string]any, bool) {
		switch entry := raw.(type) {
		case map[string]any:
			return entry, true
		case bson.M:
			return map[string]any(entry), true
		case bson.D:
			result := make(map[string]any, len(entry))
			for _, element := range entry {
				result[element.Key] = element.Value
			}
			return result, true
		default:
			return nil, false
		}
	}
	switch entries := value.(type) {
	case []map[string]any:
		return entries
	case []any:
		result := make([]map[string]any, 0, len(entries))
		for _, raw := range entries {
			if entry, ok := asMap(raw); ok {
				result = append(result, entry)
			}
		}
		return result
	case bson.A:
		result := make([]map[string]any, 0, len(entries))
		for _, raw := range entries {
			if entry, ok := asMap(raw); ok {
				result = append(result, entry)
			}
		}
		return result
	default:
		return nil
	}
}

func dayPlaceIDs(data map[string]any) []string {
	entries := dayPlaceEntries(data["placeEntries"])
	result := make([]string, 0, len(entries))
	for _, entry := range entries {
		if id := strings.TrimSpace(fmt.Sprint(entry["placeId"])); id != "" {
			result = append(result, id)
		}
	}
	return result
}

// ValidateSnapshotExternalReferences checks the portion of the current
// aggregate contract that cannot be proven from a self-contained Trip
// snapshot: base-library existence, tag categories and selected-member
// ownership. It deliberately ignores members.data.role because Trip access is
// granted by selecting a member, while the existing membership keeps its
// editor/viewer role.
func (r *Repository) ValidateSnapshotExternalReferences(
	ctx context.Context,
	payload model.SnapshotPayload,
) error {
	selectedMembers, err := snapshotSelectedMemberIDs(payload)
	if err != nil {
		return snapshotValidationError{message: err.Error()}
	}

	memberIDs := make(map[string]struct{}, len(selectedMembers))
	for memberID := range selectedMembers {
		memberIDs[memberID] = struct{}{}
	}
	actorReferences := make(map[string]string)
	tagExpectations := make(map[string]map[string]string)
	addTagExpectation := func(
		raw any,
		category, message string,
		optional bool,
	) error {
		tagID := strings.TrimSpace(fmt.Sprint(raw))
		if tagID == "" || tagID == "<nil>" {
			if optional {
				return nil
			}
			return snapshotValidationError{message: message}
		}
		if err := model.ValidateRecordID(tagID); err != nil {
			return snapshotValidationError{message: message}
		}
		if tagExpectations[tagID] == nil {
			tagExpectations[tagID] = make(map[string]string)
		}
		tagExpectations[tagID][category] = message
		return nil
	}

	for _, resource := range model.Resources() {
		for _, row := range payload.Datasets[string(resource)] {
			for _, actor := range []struct {
				field string
				value string
			}{
				{field: "createdByMemberId", value: row.CreatedByMemberID},
				{field: "updatedByMemberId", value: row.UpdatedByMemberID},
			} {
				memberID := strings.TrimSpace(actor.value)
				if memberID == "" {
					return snapshotValidationErrorf(
						"记录 %s 的 %s 必填",
						row.ID,
						actor.field,
					)
				}
				if err := model.ValidateRecordID(memberID); err != nil {
					return snapshotValidationErrorf(
						"记录 %s 的 %s 无效",
						row.ID,
						actor.field,
					)
				}
				memberIDs[memberID] = struct{}{}
				if _, exists := actorReferences[memberID]; !exists {
					actorReferences[memberID] = fmt.Sprintf(
						"记录 %s 的 %s",
						row.ID,
						actor.field,
					)
				}
			}

			switch resource {
			case model.Places:
				if err := addTagExpectation(
					row.Data["type"],
					model.TagCategoryPlaceType,
					"地点 type 必须引用一个有效的地点类型标签",
					false,
				); err != nil {
					return err
				}
			case model.Expenses:
				if err := addTagExpectation(
					row.Data["type"],
					model.TagCategoryExpenseType,
					"费用 type 必须引用一个有效的费用类型标签",
					false,
				); err != nil {
					return err
				}
			case model.Bookings:
				if err := addTagExpectation(
					row.Data["type"],
					model.TagCategoryExpenseType,
					"预订 type 必须引用一个有效的费用类型标签",
					true,
				); err != nil {
					return err
				}
				if err := addTagExpectation(
					row.Data["platformId"],
					model.TagCategoryPlatformType,
					"platformId 必须引用一个有效的平台类型标签",
					true,
				); err != nil {
					return err
				}
			case model.ResearchEvidence:
				if err := addTagExpectation(
					row.Data["platformId"],
					model.TagCategoryPlatformType,
					"platformId 必须引用一个有效的平台类型标签",
					true,
				); err != nil {
					return err
				}
			}
			for _, field := range []string{
				"ownerMemberId", "payerMemberId", "bookerMemberId",
				"recordedByMemberId",
			} {
				memberID := strings.TrimSpace(fmt.Sprint(row.Data[field]))
				if memberID == "" || memberID == "<nil>" {
					continue
				}
				if _, selected := selectedMembers[memberID]; !selected {
					return snapshotValidationErrorf("%s 不是当前旅行成员", field)
				}
			}
			for _, memberID := range stringValues(row.Data["splitMemberIds"]) {
				if _, selected := selectedMembers[memberID]; !selected {
					return snapshotValidationErrorf("分摊成员 %s 不是当前旅行成员", memberID)
				}
			}
		}
	}

	members, err := r.snapshotLibraryRecordsByID(
		ctx,
		model.LibraryMembers,
		memberIDs,
	)
	if err != nil {
		return err
	}
	for _, memberID := range sortedSet(selectedMembers) {
		if _, exists := members[memberID]; !exists {
			return snapshotValidationErrorf("旅行成员 %s 不存在于基础成员库", memberID)
		}
	}
	for _, memberID := range sortedSet(memberIDs) {
		if _, exists := members[memberID]; exists {
			continue
		}
		if reference := actorReferences[memberID]; reference != "" {
			return snapshotValidationErrorf(
				"%s 引用的基础成员 %s 不存在",
				reference,
				memberID,
			)
		}
	}

	tagIDs := make(map[string]struct{}, len(tagExpectations))
	for tagID := range tagExpectations {
		tagIDs[tagID] = struct{}{}
	}
	tags, err := r.snapshotLibraryRecordsByID(ctx, model.LibraryTags, tagIDs)
	if err != nil {
		return err
	}
	for _, tagID := range sortedSet(tagIDs) {
		expectations := tagExpectations[tagID]
		categories := make(map[string]struct{}, len(expectations))
		for category := range expectations {
			categories[category] = struct{}{}
		}
		tag, exists := tags[tagID]
		actualCategory := strings.TrimSpace(fmt.Sprint(tag.Data["category"]))
		for _, category := range sortedSet(categories) {
			if !exists || actualCategory != category {
				return snapshotValidationError{
					message: expectations[category],
				}
			}
		}
	}
	return nil
}

func (r *Repository) snapshotLibraryRecordsByID(
	ctx context.Context,
	resource model.LibraryResource,
	ids map[string]struct{},
) (map[string]model.LibraryRecord, error) {
	result := make(map[string]model.LibraryRecord, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	cursor, err := r.collection("library_records").Find(ctx, bson.M{
		"resource": resource,
		"_id":      bson.M{"$in": sortedSet(ids)},
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrRepositoryUnavailable, err)
	}
	defer cursor.Close(ctx)
	rows := make([]model.LibraryRecord, 0, len(ids))
	if err := cursor.All(ctx, &rows); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrRepositoryUnavailable, err)
	}
	for _, row := range rows {
		result[row.ID] = row
	}
	return result, nil
}

func snapshotSelectedMemberIDs(
	payload model.SnapshotPayload,
) (map[string]struct{}, error) {
	selected := make(map[string]struct{})
	for _, row := range payload.Datasets[string(model.Members)] {
		memberID := strings.TrimSpace(fmt.Sprint(row.Data["memberId"]))
		if _, duplicate := selected[memberID]; duplicate {
			return nil, fmt.Errorf("成员 %s 在当前旅行中被重复选择", memberID)
		}
		selected[memberID] = struct{}{}
	}
	return selected, nil
}

func (r *Repository) validateSelectedMember(
	ctx context.Context,
	tripID, memberID string,
) error {
	count, err := r.collection("records").CountDocuments(ctx, bson.M{
		"trip_id": tripID, "resource": model.Members, "data.memberId": memberID,
	}, options.Count().SetLimit(1))
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) SelectedMemberIDs(ctx context.Context, tripID string) ([]string, error) {
	rows, err := r.listRecords(ctx, tripID, model.Members)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		if id := strings.TrimSpace(fmt.Sprint(row.Data["memberId"])); id != "" {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func (r *Repository) PreviewRestore(ctx context.Context, tripID, snapshotID string) (RestorePreview, error) {
	target, err := r.GetSnapshot(ctx, tripID, snapshotID)
	if err != nil {
		return RestorePreview{}, err
	}
	if err := VerifyStoredSnapshot(target); err != nil {
		return RestorePreview{}, err
	}
	payload, err := ValidateSnapshotPayload(target.Payload, tripID)
	if err != nil {
		return RestorePreview{}, fmt.Errorf("stored snapshot is invalid: %w", err)
	}
	if err := r.ValidateSnapshotExternalReferences(ctx, payload); err != nil {
		return RestorePreview{}, fmt.Errorf("stored snapshot references are invalid: %w", err)
	}
	diff, err := r.DiffSnapshot(ctx, tripID, payload)
	if err != nil {
		return RestorePreview{}, err
	}
	return newRestorePreview(target, payload, diff), nil
}

func newRestorePreview(
	target model.Snapshot,
	payload model.SnapshotPayload,
	diff SnapshotDiff,
) RestorePreview {
	return RestorePreview{
		SnapshotID: target.ID, SHA256: target.SHA256,
		SnapshotDiff: diff,
	}
}

func VerifyStoredSnapshot(snapshot model.Snapshot) error {
	sum := sha256.Sum256(snapshot.Payload)
	if !strings.EqualFold(snapshot.SHA256, hex.EncodeToString(sum[:])) {
		return fmt.Errorf("stored snapshot SHA-256 does not match payload")
	}
	payload, err := ValidateSnapshotPayload(snapshot.Payload, snapshot.TripID)
	if err != nil {
		return fmt.Errorf("stored snapshot payload is invalid: %w", err)
	}
	if snapshot.Revision != payload.Revision ||
		snapshot.ContentChecksum != payload.ContentChecksum {
		return fmt.Errorf("stored snapshot metadata does not match payload")
	}
	return nil
}

func (r *Repository) RestoreSnapshot(
	ctx context.Context,
	tripID, snapshotID string,
	allowDelete bool,
	actorMemberID string,
) (model.Snapshot, model.Snapshot, bool, error) {
	if err := validateRecordActor(actorMemberID, "updatedByMemberId"); err != nil {
		return model.Snapshot{}, model.Snapshot{}, false, err
	}
	target, err := r.GetSnapshot(ctx, tripID, snapshotID)
	if err != nil {
		return model.Snapshot{}, model.Snapshot{}, false, err
	}
	if err := VerifyStoredSnapshot(target); err != nil {
		return model.Snapshot{}, model.Snapshot{}, false, err
	}
	payload, err := ValidateSnapshotPayload(target.Payload, tripID)
	if err != nil {
		return model.Snapshot{}, model.Snapshot{}, false, fmt.Errorf("stored snapshot is invalid: %w", err)
	}
	if err := r.ValidateSnapshotExternalReferences(ctx, payload); err != nil {
		return model.Snapshot{}, model.Snapshot{}, false, fmt.Errorf(
			"stored snapshot references are invalid: %w",
			err,
		)
	}
	diff, err := r.DiffSnapshot(ctx, tripID, payload)
	if err != nil {
		return model.Snapshot{}, model.Snapshot{}, false, err
	}
	if !diff.HasChanges {
		return model.Snapshot{}, model.Snapshot{}, false, nil
	}
	if diff.DeleteCount > 0 && !allowDelete {
		return model.Snapshot{}, model.Snapshot{}, false, fmt.Errorf(
			"快照恢复将删除 %d 项资料或成员访问权；请先检查预览并明确允许删除",
			diff.DeleteCount,
		)
	}
	current, err := r.GetTrip(ctx, tripID)
	if err != nil {
		return model.Snapshot{}, model.Snapshot{}, false, err
	}
	before, err := r.BuildSnapshot(ctx, tripID, "restore-before-"+snapshotID)
	if err != nil {
		return model.Snapshot{}, model.Snapshot{}, false, err
	}
	if err := r.replaceFromSnapshot(ctx, current, payload, actorMemberID); err != nil {
		return model.Snapshot{}, model.Snapshot{}, false, err
	}
	after, err := r.BuildSnapshot(ctx, tripID, "restored-"+snapshotID)
	return before, after, err == nil, err
}

func (r *Repository) DiffSnapshot(ctx context.Context, tripID string, payload model.SnapshotPayload) (SnapshotDiff, error) {
	currentTrip, err := r.GetTrip(ctx, tripID)
	if err != nil {
		return SnapshotDiff{}, err
	}
	if err := validateSnapshotTripDeletionTransition(currentTrip, payload.Trip); err != nil {
		return SnapshotDiff{}, err
	}
	currentDatasets := make(map[string][]model.Record, len(model.Resources()))
	for _, resource := range model.Resources() {
		rows, err := r.listRecords(ctx, tripID, resource)
		if err != nil {
			return SnapshotDiff{}, err
		}
		currentDatasets[string(resource)] = rows
	}
	if err := ValidateRecordIdentityTransition(currentDatasets, payload.Datasets); err != nil {
		return SnapshotDiff{}, err
	}
	result := SnapshotDiff{
		BaseRevision: payload.Revision, CurrentRevision: currentTrip.Version,
		Checksum:    payload.ContentChecksum,
		TripChanges: DiffTripMetadata(currentTrip, payload.Trip),
		Resources:   make(map[string]ResourceDiff, len(model.Resources())),
	}
	for _, resource := range model.Resources() {
		current := currentDatasets[string(resource)]
		incoming := payload.Datasets[string(resource)]
		currentByKey := make(map[string]model.Record, len(current))
		incomingByKey := make(map[string]model.Record, len(incoming))
		for _, row := range current {
			currentByKey[row.RecordKey] = row
		}
		for _, row := range incoming {
			incomingByKey[row.RecordKey] = row
		}
		diff := ResourceDiff{Current: len(current), Incoming: len(incoming)}
		for key, row := range incomingByKey {
			existing, ok := currentByKey[key]
			if !ok {
				diff.Created++
				continue
			}
			if existing.ID != row.ID {
				return SnapshotDiff{}, fmt.Errorf(
					"%w: %s recordKey %s",
					ErrSnapshotIdentity,
					resource,
					key,
				)
			}
			if !sameRecordMeaning(existing, row) {
				diff.Updated++
			}
		}
		for key := range currentByKey {
			if _, ok := incomingByKey[key]; !ok {
				diff.Deleted++
				result.DeleteCount++
			}
		}
		result.Resources[string(resource)] = diff
	}
	membershipAccess, err := r.snapshotMembershipAccessDiff(ctx, tripID, payload)
	if err != nil {
		return SnapshotDiff{}, err
	}
	result.MembershipAccess = membershipAccess
	result.DeleteCount += len(membershipAccess.Deleted)
	result.Finalize()
	return result, nil
}

func sameRecordMeaning(left, right model.Record) bool {
	if left.Position != right.Position {
		return false
	}
	return model.SameJSONMeaning(left.Data, right.Data)
}

func (r *Repository) ApplySnapshot(
	ctx context.Context,
	tripID string,
	baseRevision int64,
	payload model.SnapshotPayload,
	actorMemberID string,
) (model.Snapshot, model.Snapshot, error) {
	if err := validateRecordActor(actorMemberID, "updatedByMemberId"); err != nil {
		return model.Snapshot{}, model.Snapshot{}, err
	}
	if err := r.ValidateSnapshotExternalReferences(ctx, payload); err != nil {
		return model.Snapshot{}, model.Snapshot{}, err
	}
	current, err := r.GetTrip(ctx, tripID)
	if err != nil {
		return model.Snapshot{}, model.Snapshot{}, err
	}
	if err := validateSnapshotTripDeletionTransition(current, payload.Trip); err != nil {
		return model.Snapshot{}, model.Snapshot{}, err
	}
	if current.Version != baseRevision {
		return model.Snapshot{}, model.Snapshot{}, ErrConflict
	}
	before, err := r.BuildSnapshot(ctx, tripID, "import-before-"+payload.ContentChecksum[:12])
	if err != nil {
		return model.Snapshot{}, model.Snapshot{}, err
	}
	if err := r.replaceFromSnapshot(ctx, current, payload, actorMemberID); err != nil {
		return model.Snapshot{}, model.Snapshot{}, err
	}
	after, err := r.BuildSnapshot(ctx, tripID, "import-applied-"+payload.ContentChecksum[:12])
	return before, after, err
}

func (r *Repository) replaceFromSnapshot(
	ctx context.Context,
	current model.Trip,
	payload model.SnapshotPayload,
	actorMemberID string,
) error {
	if err := validateRecordActor(actorMemberID, "updatedByMemberId"); err != nil {
		return err
	}
	if err := validateSnapshotTripDeletionTransition(current, payload.Trip); err != nil {
		return err
	}
	currentRows := make(map[string]model.Record)
	currentDatasets := make(map[string][]model.Record, len(model.Resources()))
	for _, resource := range model.Resources() {
		rows, err := r.listRecords(ctx, current.ID, resource)
		if err != nil {
			return err
		}
		currentDatasets[string(resource)] = rows
		for _, row := range rows {
			currentRows[string(resource)+"\x00"+row.RecordKey] = row
		}
	}
	if err := ValidateRecordIdentityTransition(currentDatasets, payload.Datasets); err != nil {
		return err
	}
	now := time.Now().UTC()
	rows := make([]any, 0)
	for _, resource := range model.Resources() {
		for _, row := range payload.Datasets[string(resource)] {
			previous, existed := currentRows[string(resource)+"\x00"+row.RecordKey]
			if existed && row.ID != previous.ID {
				return fmt.Errorf(
					"%s recordKey %s cannot change immutable id",
					resource,
					row.RecordKey,
				)
			}
			row.TripID = current.ID
			row.Resource = resource
			row.GroupKey = groupKey(resource, row.Data)
			if existed && sameRecordMeaning(previous, row) {
				rows = append(rows, previous)
				continue
			}
			if existed {
				row.ID = previous.ID
				row.Version = previous.Version + 1
				row.CreatedByMemberID = previous.CreatedByMemberID
				row.UpdatedByMemberID = actorMemberID
				row.CreatedAt = previous.CreatedAt
				row.UpdatedAt = now
			} else {
				row.Version = 1
				row.CreatedByMemberID = actorMemberID
				row.UpdatedByMemberID = actorMemberID
				row.CreatedAt = now
				row.UpdatedAt = now
			}
			rows = append(rows, row)
		}
	}
	if _, err := r.collection("records").DeleteMany(ctx, bson.M{"trip_id": current.ID}); err != nil {
		return err
	}
	if len(rows) > 0 {
		if _, err := r.collection("records").InsertMany(ctx, rows); err != nil {
			return err
		}
	}
	if err := r.reconcileSnapshotMemberships(ctx, current.ID, payload); err != nil {
		return err
	}
	replacement := payload.Trip
	replacement.ID = current.ID
	replacement.Version = current.Version + 1
	replacement.CreatedAt = current.CreatedAt
	replacement.UpdatedAt = now
	if err := replacement.Validate(); err != nil {
		return err
	}
	result, err := r.collection("trips").ReplaceOne(
		ctx,
		bson.M{"_id": current.ID, "version": current.Version},
		replacement,
	)
	if err != nil {
		return err
	}
	if result.MatchedCount != 1 {
		return ErrConflict
	}
	return nil
}

func (r *Repository) snapshotMembershipAccessDiff(
	ctx context.Context,
	tripID string,
	payload model.SnapshotPayload,
) (MembershipAccessDiff, error) {
	selected, err := snapshotSelectedMemberIDs(payload)
	if err != nil {
		return MembershipAccessDiff{}, err
	}
	memberships, ownerID, err := r.snapshotMembershipState(ctx, tripID)
	if err != nil {
		return MembershipAccessDiff{}, err
	}
	return buildMembershipAccessDiff(selected, memberships, ownerID), nil
}

func buildMembershipAccessDiff(
	selected map[string]struct{},
	memberships map[string]model.Membership,
	ownerID string,
) MembershipAccessDiff {
	diff := MembershipAccessDiff{
		Created: make([]MembershipAccessChange, 0),
		Deleted: make([]MembershipAccessChange, 0),
	}
	for _, membership := range memberships {
		if membership.MemberID == ownerID {
			continue
		}
		if _, keep := selected[membership.MemberID]; !keep {
			diff.Deleted = append(diff.Deleted, MembershipAccessChange{
				MemberID: membership.MemberID,
				Role:     membership.Role,
			})
		}
	}
	for memberID := range selected {
		if memberID == ownerID {
			continue
		}
		if _, exists := memberships[memberID]; !exists {
			diff.Created = append(diff.Created, MembershipAccessChange{
				MemberID: memberID,
				Role:     "editor",
			})
		}
	}
	sortMembershipAccessChanges(diff.Created)
	sortMembershipAccessChanges(diff.Deleted)
	return diff
}

func sortMembershipAccessChanges(changes []MembershipAccessChange) {
	sort.Slice(changes, func(left, right int) bool {
		if changes[left].MemberID == changes[right].MemberID {
			return changes[left].Role < changes[right].Role
		}
		return changes[left].MemberID < changes[right].MemberID
	})
}

func (r *Repository) reconcileSnapshotMemberships(
	ctx context.Context,
	tripID string,
	payload model.SnapshotPayload,
) error {
	access, err := r.snapshotMembershipAccessDiff(ctx, tripID, payload)
	if err != nil {
		return err
	}
	for _, change := range access.Deleted {
		if err := r.DeleteMembership(ctx, tripID, change.MemberID); err != nil {
			return err
		}
	}
	for _, change := range access.Created {
		if err := r.SetMembership(ctx, tripID, change.MemberID, change.Role); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) snapshotMembershipState(
	ctx context.Context,
	tripID string,
) (map[string]model.Membership, string, error) {
	cursor, err := r.collection("memberships").Find(ctx, bson.M{"trip_id": tripID})
	if err != nil {
		return nil, "", err
	}
	defer cursor.Close(ctx)
	rows := make([]model.Membership, 0)
	if err := cursor.All(ctx, &rows); err != nil {
		return nil, "", err
	}
	memberships := make(map[string]model.Membership, len(rows))
	ownerID := ""
	for _, membership := range rows {
		switch membership.Role {
		case "owner":
			if ownerID != "" {
				return nil, "", fmt.Errorf("旅行必须且只能保留一个 owner")
			}
			ownerID = membership.MemberID
		case "editor", "viewer":
		default:
			return nil, "", fmt.Errorf("旅行成员 %s 的访问角色无效", membership.MemberID)
		}
		memberships[membership.MemberID] = membership
	}
	if ownerID == "" {
		return nil, "", fmt.Errorf("旅行必须且只能保留一个 owner")
	}
	return memberships, ownerID, nil
}
