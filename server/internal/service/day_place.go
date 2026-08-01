// 当天新增地点在一个事务内完成 Place 创建、Day 关联、审计与完整快照。
package service

import (
	"context"
	"encoding/json"
	"errors"

	"my-travel/server/internal/model"
	"my-travel/server/internal/repository"
)

func (s Service) CreatePlaceForDay(
	ctx context.Context,
	tripID, dayID string,
	dayVersion int64,
	place model.Record,
	actorMemberID, idempotencyKey, requestHash string,
) (model.Record, model.Record, model.Snapshot, bool, error) {
	type result struct {
		Place    model.Record   `json:"place"`
		Day      model.Record   `json:"day"`
		Snapshot model.Snapshot `json:"snapshot"`
	}
	scope := "day-place-create:" + tripID + ":" + dayID
	var output result
	var replayed bool
	err := s.Repo.WithTransaction(ctx, func(tx context.Context) error {
		if err := s.requireTripEditor(tx, tripID, actorMemberID); err != nil {
			return err
		}
		if stored, err := s.Repo.GetIdempotencyRequest(
			tx,
			scope,
			idempotencyKey,
			requestHash,
		); err == nil {
			replayed = true
			return json.Unmarshal(stored.Response, &output)
		} else if !errors.Is(err, repository.ErrNotFound) {
			return err
		}

		previousDay, err := s.Repo.GetRecord(tx, tripID, model.Days, dayID)
		if err != nil {
			return err
		}
		var dayData model.DayData
		if err := decodeRecordData(previousDay.Data, &dayData); err != nil {
			return err
		}

		place.TripID = tripID
		place.Resource = model.Places
		place.CreatedByMemberID, place.UpdatedByMemberID = actorMemberID, actorMemberID
		createdPlace, err := s.Repo.CreateRecord(tx, place)
		if err != nil {
			return err
		}

		dayData.PlaceEntries = append(
			dayData.PlaceEntries,
			model.DayPlaceEntry{PlaceID: createdPlace.ID},
		)
		nextDayData, err := encodeRecordData(dayData)
		if err != nil {
			return err
		}
		updatedDay, err := s.Repo.UpdateRecord(
			tx,
			tripID,
			model.Days,
			dayID,
			dayVersion,
			nextDayData,
			actorMemberID,
		)
		if err != nil {
			return err
		}

		if err := s.auditRecord(
			tx,
			createdPlace,
			actorMemberID,
			"创建并关联日程",
			nil,
			createdPlace.Data,
		); err != nil {
			return err
		}
		if err := s.auditRecord(
			tx,
			updatedDay,
			actorMemberID,
			"关联地点",
			previousDay.Data,
			updatedDay.Data,
		); err != nil {
			return err
		}
		if _, err := s.Repo.BumpTripRevision(tx, tripID); err != nil {
			return err
		}
		snapshot, err := s.Repo.BuildSnapshot(tx, tripID, "day-place-created")
		if err != nil {
			return err
		}
		output = result{Place: createdPlace, Day: updatedDay, Snapshot: snapshot}
		encoded, err := json.Marshal(output)
		if err != nil {
			return err
		}
		return s.Repo.StoreIdempotencyRequest(
			tx,
			scope,
			idempotencyKey,
			requestHash,
			encoded,
		)
	})
	if err != nil {
		return model.Record{}, model.Record{}, model.Snapshot{}, false, err
	}
	if replayed {
		if err := refreshSnapshots(ctx, s.Repo, &output.Snapshot); err != nil {
			return model.Record{}, model.Record{}, model.Snapshot{}, false, err
		}
	} else {
		s.mirror(ctx, tripID, &output.Snapshot)
	}
	return output.Place, output.Day, output.Snapshot, replayed, nil
}

func decodeRecordData(data map[string]any, target any) error {
	encoded, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, target)
}

func encodeRecordData(data any) (map[string]any, error) {
	encoded, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	result := map[string]any{}
	if err := json.Unmarshal(encoded, &result); err != nil {
		return nil, err
	}
	return result, nil
}
