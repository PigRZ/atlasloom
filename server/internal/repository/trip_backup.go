// 旅行备份持久化负责生成自包含文件、严格校验并创建全新的 Trip 聚合。
package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"my-travel/server/internal/model"
)

func (r *Repository) ExportTripBackup(
	ctx context.Context,
	tripID string,
) (model.TripBackup, error) {
	if mongo.SessionFromContext(ctx) != nil {
		return r.exportTripBackup(ctx, tripID)
	}
	var result model.TripBackup
	err := r.withSnapshotReadTransaction(ctx, func(tx context.Context) error {
		backup, err := r.exportTripBackup(tx, tripID)
		if err != nil {
			return err
		}
		result = backup
		return nil
	})
	return result, err
}

func (r *Repository) exportTripBackup(
	ctx context.Context,
	tripID string,
) (model.TripBackup, error) {
	payload, _, err := r.buildSnapshotPayload(ctx, tripID, "manual-backup")
	if err != nil {
		return model.TripBackup{}, err
	}
	memberIDs, tagIDs := tripBackupReferenceIDs(payload)
	dependencies := model.TripBackupDependencies{
		Members: make([]model.TripBackupMemberDependency, 0, len(memberIDs)),
		Tags:    make([]model.TripBackupTagDependency, 0, len(tagIDs)),
	}
	for _, id := range sortedSet(memberIDs) {
		row, err := r.GetLibraryRecord(ctx, model.LibraryMembers, id)
		if err != nil {
			return model.TripBackup{}, fmt.Errorf("旅行引用的成员 %s 不存在: %w", id, err)
		}
		dependencies.Members = append(dependencies.Members, model.TripBackupMemberDependency{
			SourceID:    id,
			DisplayName: strings.TrimSpace(fmt.Sprint(row.Data["displayName"])),
		})
	}
	for _, id := range sortedSet(tagIDs) {
		row, err := r.GetLibraryRecord(ctx, model.LibraryTags, id)
		if err != nil {
			return model.TripBackup{}, fmt.Errorf("旅行引用的标签 %s 不存在: %w", id, err)
		}
		dependencies.Tags = append(dependencies.Tags, model.TripBackupTagDependency{
			SourceID: id,
			Name:     strings.TrimSpace(fmt.Sprint(row.Data["name"])),
			Category: strings.TrimSpace(fmt.Sprint(row.Data["category"])),
			Color:    strings.TrimSpace(fmt.Sprint(row.Data["color"])),
		})
	}
	backup := model.TripBackup{
		Kind:         model.TripBackupKind,
		ExportedAt:   time.Now().UTC(),
		Snapshot:     payload,
		Dependencies: dependencies,
	}
	backup.ContentChecksum, err = backup.ComputedContentChecksum()
	if err != nil {
		return model.TripBackup{}, err
	}
	return backup, nil
}

func ValidateTripBackup(raw []byte) (model.TripBackup, error) {
	var backup model.TripBackup
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&backup); err != nil {
		return backup, fmt.Errorf("旅行备份 JSON 无效: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return backup, fmt.Errorf("旅行备份只能包含一个 JSON 对象")
	}
	if backup.Kind != model.TripBackupKind {
		return backup, fmt.Errorf("不是本项目生成的旅行备份")
	}
	if backup.ExportedAt.IsZero() {
		return backup, fmt.Errorf("旅行备份缺少导出时间")
	}
	if backup.Snapshot.Trip.DeletedAt != nil {
		return backup, fmt.Errorf("不能导入已经删除的旅行")
	}
	snapshotRaw, err := json.Marshal(backup.Snapshot)
	if err != nil {
		return backup, err
	}
	if _, err := ValidateSnapshotPayload(snapshotRaw, backup.Snapshot.Trip.ID); err != nil {
		return backup, fmt.Errorf("旅行备份内容无效: %w", err)
	}
	if err := validateTripBackupDependencies(backup); err != nil {
		return backup, err
	}
	checksum, err := backup.ComputedContentChecksum()
	if err != nil {
		return backup, err
	}
	if backup.ContentChecksum == "" ||
		!strings.EqualFold(backup.ContentChecksum, checksum) {
		return backup, fmt.Errorf("旅行备份校验和不匹配")
	}
	return backup, nil
}

func validateTripBackupDependencies(backup model.TripBackup) error {
	requiredMembers, requiredTags := tripBackupReferenceIDs(backup.Snapshot)
	members := make(map[string]struct{}, len(backup.Dependencies.Members))
	for _, dependency := range backup.Dependencies.Members {
		if err := model.ValidateRecordID(dependency.SourceID); err != nil {
			return fmt.Errorf("成员依赖 ID 无效: %w", err)
		}
		if strings.TrimSpace(dependency.DisplayName) == "" {
			return fmt.Errorf("成员依赖 %s 缺少名称", dependency.SourceID)
		}
		if _, duplicate := members[dependency.SourceID]; duplicate {
			return fmt.Errorf("成员依赖 %s 重复", dependency.SourceID)
		}
		members[dependency.SourceID] = struct{}{}
	}
	tags := make(map[string]struct{}, len(backup.Dependencies.Tags))
	for _, dependency := range backup.Dependencies.Tags {
		if err := model.ValidateRecordID(dependency.SourceID); err != nil {
			return fmt.Errorf("标签依赖 ID 无效: %w", err)
		}
		if strings.TrimSpace(dependency.Name) == "" {
			return fmt.Errorf("标签依赖 %s 缺少名称", dependency.SourceID)
		}
		switch dependency.Category {
		case "general", "place-type", "expense-type", "platform-type":
		default:
			return fmt.Errorf("标签依赖 %s 的分类无效", dependency.SourceID)
		}
		if _, duplicate := tags[dependency.SourceID]; duplicate {
			return fmt.Errorf("标签依赖 %s 重复", dependency.SourceID)
		}
		tags[dependency.SourceID] = struct{}{}
	}
	if err := exactDependencySet("成员", requiredMembers, members); err != nil {
		return err
	}
	return exactDependencySet("标签", requiredTags, tags)
}

func exactDependencySet(
	name string,
	required, supplied map[string]struct{},
) error {
	for id := range required {
		if _, exists := supplied[id]; !exists {
			return fmt.Errorf("旅行备份缺少%s依赖 %s", name, id)
		}
	}
	for id := range supplied {
		if _, exists := required[id]; !exists {
			return fmt.Errorf("旅行备份包含未使用的%s依赖 %s", name, id)
		}
	}
	return nil
}

func tripBackupReferenceIDs(
	payload model.SnapshotPayload,
) (map[string]struct{}, map[string]struct{}) {
	members := map[string]struct{}{}
	tags := map[string]struct{}{}
	add := func(target map[string]struct{}, value any) {
		id := strings.TrimSpace(fmt.Sprint(value))
		if id != "" && id != "<nil>" {
			target[id] = struct{}{}
		}
	}
	for resource, rows := range payload.Datasets {
		for _, row := range rows {
			switch model.Resource(resource) {
			case model.Members:
				add(members, row.Data["memberId"])
			case model.Places:
				add(tags, row.Data["type"])
			case model.Bookings:
				add(tags, row.Data["platformId"])
				add(tags, row.Data["type"])
				add(members, row.Data["bookerMemberId"])
			case model.Expenses:
				add(tags, row.Data["type"])
				add(members, row.Data["payerMemberId"])
				for _, id := range stringValues(row.Data["splitMemberIds"]) {
					add(members, id)
				}
			case model.PackingItems:
				add(members, row.Data["ownerMemberId"])
			case model.ResearchEvidence:
				add(tags, row.Data["platformId"])
				add(members, row.Data["recordedByMemberId"])
			}
		}
	}
	return members, tags
}

func sortedSet(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func (r *Repository) AvailableTripID(
	ctx context.Context,
	sourceID string,
) (string, error) {
	candidates := []string{sourceID}
	base := strings.TrimRight(sourceID, "-")
	if len(base) > 55 {
		base = strings.TrimRight(base[:55], "-")
	}
	candidates = append(candidates, base+"-copy")
	for index := 2; index <= 9999; index++ {
		candidates = append(candidates, fmt.Sprintf("%s-copy-%d", base, index))
	}
	for _, candidate := range candidates {
		if len(candidate) > 64 {
			continue
		}
		count, err := r.collection("trips").CountDocuments(
			ctx,
			bson.M{"_id": candidate},
		)
		if err != nil {
			return "", err
		}
		if count == 0 {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("无法为导入旅行生成唯一标识")
}

func (r *Repository) CreateTripFromBackup(
	ctx context.Context,
	backup model.TripBackup,
	targetTripID string,
	memberMappings, tagMappings map[string]string,
	actorMemberID string,
) (model.Trip, model.Snapshot, error) {
	trip := backup.Snapshot.Trip
	trip.ID = targetTripID
	trip.Version = 0
	trip.CreatedAt = time.Time{}
	trip.UpdatedAt = time.Time{}
	trip.DeletedAt = nil
	trip.DeletedByMemberID = ""
	created, err := r.CreateTrip(ctx, trip)
	if err != nil {
		return model.Trip{}, model.Snapshot{}, err
	}
	rows, memberIDs, err := prepareTripBackupRecords(
		backup.Snapshot,
		created,
		memberMappings,
		tagMappings,
		actorMemberID,
	)
	if err != nil {
		return model.Trip{}, model.Snapshot{}, err
	}
	if len(rows) > 0 {
		documents := make([]any, 0, len(rows))
		for _, row := range rows {
			documents = append(documents, row)
		}
		if _, err := r.collection("records").InsertMany(ctx, documents); err != nil {
			return model.Trip{}, model.Snapshot{}, err
		}
		for _, row := range rows {
			if err := r.ValidateRecordReferences(
				ctx,
				created.ID,
				row.Resource,
				row.Data,
			); err != nil {
				return model.Trip{}, model.Snapshot{}, err
			}
		}
	}
	for _, memberID := range sortedSet(memberIDs) {
		if memberID == actorMemberID {
			continue
		}
		if err := r.SetMembership(ctx, created.ID, memberID, "editor"); err != nil {
			return model.Trip{}, model.Snapshot{}, err
		}
	}
	if err := r.SetMembership(ctx, created.ID, actorMemberID, "owner"); err != nil {
		return model.Trip{}, model.Snapshot{}, err
	}
	counts := make(map[string]any, len(model.Resources()))
	for _, resource := range model.Resources() {
		counts[string(resource)] = len(backup.Snapshot.Datasets[string(resource)])
	}
	if err := r.InsertAuditLog(ctx, model.AuditLog{
		TripID: created.ID, MemberID: actorMemberID,
		Module: "旅行", Action: "从备份导入",
		Resource: "trip-imports", RecordID: created.ID,
		RecordLabel: created.Title,
		After: map[string]any{
			"sourceTripId": backup.Snapshot.Trip.ID,
			"checksum":     backup.ContentChecksum,
			"resources":    counts,
		},
	}); err != nil {
		return model.Trip{}, model.Snapshot{}, err
	}
	item, err := r.BuildSnapshot(ctx, created.ID, "trip-imported")
	if err != nil {
		return model.Trip{}, model.Snapshot{}, err
	}
	return created, item, nil
}

func prepareTripBackupRecords(
	source model.SnapshotPayload,
	target model.Trip,
	memberMappings, tagMappings map[string]string,
	actorMemberID string,
) ([]model.Record, map[string]struct{}, error) {
	idMappings := make(map[string]string)
	for _, resource := range model.Resources() {
		for _, row := range source.Datasets[string(resource)] {
			idMappings[row.ID] = newID()
		}
	}
	now := time.Now().UTC()
	rows := make([]model.Record, 0)
	memberIDs := map[string]struct{}{}
	for _, resource := range model.Resources() {
		for _, sourceRow := range source.Datasets[string(resource)] {
			data, err := cloneRecordData(sourceRow.Data)
			if err != nil {
				return nil, nil, err
			}
			if err := rewriteTripLocalReferences(resource, data, idMappings); err != nil {
				return nil, nil, err
			}
			if err := rewriteTripGlobalReferences(
				resource,
				data,
				memberMappings,
				tagMappings,
			); err != nil {
				return nil, nil, err
			}
			if resource == model.Members {
				memberID := strings.TrimSpace(fmt.Sprint(data["memberId"]))
				memberIDs[memberID] = struct{}{}
				if memberID == actorMemberID {
					data["role"] = "owner"
				} else {
					data["role"] = "editor"
				}
			}
			if err := model.ValidateRecordData(resource, data); err != nil {
				return nil, nil, fmt.Errorf(
					"%s 记录 %s 导入后无效: %w",
					resource,
					sourceRow.RecordKey,
					err,
				)
			}
			row := model.Record{
				ID:                idMappings[sourceRow.ID],
				TripID:            target.ID,
				Resource:          resource,
				RecordKey:         sourceRow.RecordKey,
				GroupKey:          groupKey(resource, data),
				Position:          sourceRow.Position,
				Version:           1,
				Data:              data,
				CreatedByMemberID: actorMemberID,
				UpdatedByMemberID: actorMemberID,
				CreatedAt:         now,
				UpdatedAt:         now,
			}
			rows = append(rows, row)
		}
	}
	return rows, memberIDs, nil
}

func cloneRecordData(source map[string]any) (map[string]any, error) {
	payload, err := json.Marshal(source)
	if err != nil {
		return nil, err
	}
	var target map[string]any
	if err := json.Unmarshal(payload, &target); err != nil {
		return nil, err
	}
	return target, nil
}

func rewriteTripLocalReferences(
	resource model.Resource,
	data map[string]any,
	mappings map[string]string,
) error {
	rewriteOne := func(field string) error {
		source := strings.TrimSpace(fmt.Sprint(data[field]))
		if source == "" || source == "<nil>" {
			return nil
		}
		target, exists := mappings[source]
		if !exists {
			return fmt.Errorf("%s 引用了不存在的旅行内记录 %s", field, source)
		}
		data[field] = target
		return nil
	}
	rewriteMany := func(field string) error {
		values := stringValues(data[field])
		if len(values) == 0 {
			return nil
		}
		result := make([]string, 0, len(values))
		for _, source := range values {
			target, exists := mappings[source]
			if !exists {
				return fmt.Errorf("%s 引用了不存在的旅行内记录 %s", field, source)
			}
			result = append(result, target)
		}
		data[field] = result
		return nil
	}
	switch resource {
	case model.Days:
		entries := dayPlaceEntries(data["placeEntries"])
		for index := range entries {
			source := strings.TrimSpace(fmt.Sprint(entries[index]["placeId"]))
			target, exists := mappings[source]
			if !exists {
				return fmt.Errorf("日程引用了不存在的地点 %s", source)
			}
			entries[index]["placeId"] = target
		}
		data["placeEntries"] = entries
	case model.Segments:
		for _, field := range []string{"dayId", "fromPlaceId", "toPlaceId"} {
			if err := rewriteOne(field); err != nil {
				return err
			}
		}
	case model.Bookings:
		return rewriteOne("placeId")
	case model.Expenses:
		return rewriteOne("bookingRecordId")
	case model.PackingItems:
		return rewriteMany("dayIds")
	case model.ResearchEvidence:
		if err := rewriteMany("dayIds"); err != nil {
			return err
		}
		return rewriteOne("placeId")
	}
	return nil
}

func rewriteTripGlobalReferences(
	resource model.Resource,
	data map[string]any,
	memberMappings, tagMappings map[string]string,
) error {
	rewriteOne := func(field string, mappings map[string]string) error {
		source := strings.TrimSpace(fmt.Sprint(data[field]))
		if source == "" || source == "<nil>" {
			return nil
		}
		target, exists := mappings[source]
		if !exists {
			return fmt.Errorf("%s 尚未完成基础资料映射", field)
		}
		data[field] = target
		return nil
	}
	rewriteMany := func(field string, mappings map[string]string) error {
		values := stringValues(data[field])
		if len(values) == 0 {
			return nil
		}
		result := make([]string, 0, len(values))
		for _, source := range values {
			target, exists := mappings[source]
			if !exists {
				return fmt.Errorf("%s 尚未完成基础资料映射", field)
			}
			result = append(result, target)
		}
		data[field] = result
		return nil
	}
	switch resource {
	case model.Members:
		return rewriteOne("memberId", memberMappings)
	case model.Places:
		return rewriteOne("type", tagMappings)
	case model.Bookings:
		if err := rewriteOne("type", tagMappings); err != nil {
			return err
		}
		if err := rewriteOne("platformId", tagMappings); err != nil {
			return err
		}
		return rewriteOne("bookerMemberId", memberMappings)
	case model.Expenses:
		if err := rewriteOne("type", tagMappings); err != nil {
			return err
		}
		if err := rewriteOne("payerMemberId", memberMappings); err != nil {
			return err
		}
		return rewriteMany("splitMemberIds", memberMappings)
	case model.PackingItems:
		return rewriteOne("ownerMemberId", memberMappings)
	case model.ResearchEvidence:
		if err := rewriteOne("platformId", tagMappings); err != nil {
			return err
		}
		return rewriteOne("recordedByMemberId", memberMappings)
	}
	return nil
}
