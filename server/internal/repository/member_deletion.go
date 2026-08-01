// 旅行成员删除在一个事务内分类阻塞引用并清理明确允许级联的资料。
package repository

import (
	"context"
	"fmt"
	"strings"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"my-travel/server/internal/model"
)

type TripMemberReference struct {
	Resource  model.Resource `json:"resource"`
	RecordID  string         `json:"recordId"`
	Label     string         `json:"label"`
	Relations []string       `json:"relations"`
}

type TripMemberDeletionImpact struct {
	References             []TripMemberReference `json:"references"`
	CascadeExpenses        []TripMemberReference `json:"cascadeExpenses"`
	HasPrivatePackingItems bool                  `json:"hasPrivatePackingItems"`
}

func (r *Repository) GetTripMemberDeletionImpact(
	ctx context.Context,
	tripID, recordID string,
) (TripMemberDeletionImpact, error) {
	member, err := r.GetRecord(ctx, tripID, model.Members, recordID)
	if err != nil {
		return TripMemberDeletionImpact{}, err
	}
	memberID := strings.TrimSpace(fmt.Sprint(member.Data["memberId"]))
	impact := TripMemberDeletionImpact{
		References:      []TripMemberReference{},
		CascadeExpenses: []TripMemberReference{},
	}
	cursor, err := r.collection("records").Find(
		ctx,
		bson.M{
			"trip_id": tripID,
			"$or": bson.A{
				bson.M{
					"resource":            model.Bookings,
					"data.bookerMemberId": memberID,
				},
				bson.M{
					"resource": model.Expenses,
					"$or": bson.A{
						bson.M{"data.payerMemberId": memberID},
						bson.M{"data.splitMemberIds": memberID},
					},
				},
				bson.M{
					"resource":                model.ResearchEvidence,
					"data.recordedByMemberId": memberID,
				},
			},
		},
		options.Find().SetSort(bson.D{
			{Key: "resource", Value: 1},
			{Key: "position", Value: 1},
		}),
	)
	if err != nil {
		return TripMemberDeletionImpact{}, err
	}
	defer cursor.Close(ctx)
	var rows []model.Record
	if err := cursor.All(ctx, &rows); err != nil {
		return TripMemberDeletionImpact{}, err
	}
	for _, row := range rows {
		relations := memberRelations(row, memberID)
		reference := TripMemberReference{
			Resource: row.Resource, RecordID: row.ID,
			Label: libraryReferenceLabel(row), Relations: relations,
		}
		if row.Resource == model.Expenses && !expenseIsBookingLinked(row.Data) {
			impact.CascadeExpenses = append(impact.CascadeExpenses, reference)
			continue
		}
		impact.References = append(impact.References, reference)
	}
	privatePackingCount, err := r.collection("records").CountDocuments(
		ctx,
		bson.M{
			"trip_id": tripID, "resource": model.PackingItems,
			"data.ownerMemberId": memberID,
		},
		options.Count().SetLimit(1),
	)
	if err != nil {
		return TripMemberDeletionImpact{}, err
	}
	impact.HasPrivatePackingItems = privatePackingCount > 0
	return impact, nil
}

func memberRelations(row model.Record, memberID string) []string {
	relations := make([]string, 0, 2)
	if row.Resource == model.Bookings &&
		fmt.Sprint(row.Data["bookerMemberId"]) == memberID {
		relations = append(relations, "booking-booker")
	}
	if row.Resource == model.Expenses {
		if fmt.Sprint(row.Data["payerMemberId"]) == memberID {
			relations = append(relations, "expense-payer")
		}
		for _, splitMemberID := range stringValues(row.Data["splitMemberIds"]) {
			if splitMemberID == memberID {
				relations = append(relations, "expense-split")
				break
			}
		}
	}
	if row.Resource == model.ResearchEvidence &&
		fmt.Sprint(row.Data["recordedByMemberId"]) == memberID {
		relations = append(relations, "research-recorder")
	}
	return relations
}

func (r *Repository) DeleteTripMemberCascade(
	ctx context.Context,
	tripID, id string,
	version int64,
) ([]model.Record, error) {
	member, err := r.GetRecord(ctx, tripID, model.Members, id)
	if err != nil {
		return nil, err
	}
	if member.Version != version {
		return nil, ErrConflict
	}
	impact, err := r.GetTripMemberDeletionImpact(ctx, tripID, id)
	if err != nil {
		return nil, err
	}
	if len(impact.References) > 0 {
		return nil, fmt.Errorf(
			"%s: %w",
			tripMemberDeletionMessage(impact),
			ErrReferenced,
		)
	}
	memberID := strings.TrimSpace(fmt.Sprint(member.Data["memberId"]))
	expenseIDs := make([]string, 0, len(impact.CascadeExpenses))
	for _, expense := range impact.CascadeExpenses {
		expenseIDs = append(expenseIDs, expense.RecordID)
	}
	cascadeFilters := bson.A{
		bson.M{
			"resource":           model.PackingItems,
			"data.ownerMemberId": memberID,
		},
	}
	if len(expenseIDs) > 0 {
		cascadeFilters = append(cascadeFilters, bson.M{
			"resource": model.Expenses,
			"_id":      bson.M{"$in": expenseIDs},
		})
	}
	cursor, err := r.collection("records").Find(
		ctx,
		bson.M{
			"trip_id": tripID,
			"$or":     cascadeFilters,
		},
		options.Find().SetSort(bson.D{
			{Key: "resource", Value: 1},
			{Key: "position", Value: 1},
		}),
	)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	cascadedRecords := make([]model.Record, 0)
	if err := cursor.All(ctx, &cascadedRecords); err != nil {
		return nil, err
	}
	if len(cascadedRecords) > 0 {
		recordIDs := make([]string, 0, len(cascadedRecords))
		for _, row := range cascadedRecords {
			recordIDs = append(recordIDs, row.ID)
		}
		result, deleteErr := r.collection("records").DeleteMany(
			ctx,
			bson.M{
				"trip_id": tripID,
				"_id":     bson.M{"$in": recordIDs},
			},
		)
		if deleteErr != nil {
			return nil, deleteErr
		}
		if result.DeletedCount != int64(len(cascadedRecords)) {
			return nil, ErrConflict
		}
	}
	result, err := r.collection("records").DeleteOne(ctx, bson.M{
		"_id": id, "trip_id": tripID, "resource": model.Members,
		"version": version,
	})
	if err != nil {
		return nil, err
	}
	if result.DeletedCount != 1 {
		return nil, ErrConflict
	}
	return cascadedRecords, nil
}

func tripMemberDeletionMessage(impact TripMemberDeletionImpact) string {
	const visibleLimit = 5
	describe := func(references []TripMemberReference) []string {
		parts := make([]string, 0, min(len(references), visibleLimit)+1)
		for _, reference := range references[:min(len(references), visibleLimit)] {
			parts = append(parts, tripMemberReferenceDescription(reference))
		}
		if len(references) > visibleLimit {
			parts = append(parts, fmt.Sprintf(
				"另有 %d 条",
				len(references)-visibleLimit,
			))
		}
		return parts
	}
	sections := make([]string, 0, 3)
	if len(impact.References) > 0 {
		sections = append(
			sections,
			"不能自动清理："+strings.Join(describe(impact.References), "；"),
		)
	}
	cascadeParts := describe(impact.CascadeExpenses)
	if impact.HasPrivatePackingItems {
		cascadeParts = append(cascadeParts, "该成员的个人物品（内容已隐藏）")
	}
	if len(cascadeParts) > 0 {
		sections = append(
			sections,
			"明确确认后可连带删除："+strings.Join(cascadeParts, "；"),
		)
	}
	if len(impact.References) > 0 {
		return "该同行成员仍有关联资料，请先修改不能自动清理的内容。" +
			strings.Join(sections, "。")
	}
	return "该同行成员仍有关联内容，必须明确确认后才能删除。" +
		strings.Join(sections, "。")
}

func tripMemberReferenceDescription(reference TripMemberReference) string {
	relations := make([]string, 0, len(reference.Relations))
	for _, relation := range reference.Relations {
		relations = append(relations, map[string]string{
			"booking-booker":    "预订人",
			"expense-payer":     "付款人",
			"expense-split":     "分摊成员",
			"research-recorder": "资料记录人",
		}[relation])
	}
	return fmt.Sprintf(
		"%s“%s”（%s）",
		resourceDisplayName(reference.Resource),
		reference.Label,
		strings.Join(relations, "、"),
	)
}

func expenseIsBookingLinked(data map[string]any) bool {
	value, exists := data["bookingRecordId"]
	if !exists || value == nil {
		return false
	}
	return strings.TrimSpace(fmt.Sprint(value)) != ""
}
