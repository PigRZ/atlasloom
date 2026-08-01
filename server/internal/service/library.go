// 基础库服务保证全局资料、登录凭据、修订号和完整快照原子提交。
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"my-travel/server/internal/auth"
	"my-travel/server/internal/model"
	"my-travel/server/internal/repository"
	"my-travel/server/internal/snapshot"
)

func (s Service) CreateLibraryRecord(
	ctx context.Context,
	row model.LibraryRecord,
	password string,
	actorMemberID string,
	idempotencyKey, requestHash string,
) (model.LibraryRecord, model.LibrarySnapshot, bool, error) {
	type result struct {
		Record   model.LibraryRecord   `json:"record"`
		Snapshot model.LibrarySnapshot `json:"snapshot"`
	}
	scope := "library-create:" + string(row.Resource)
	var output result
	var replayed bool
	err := s.Repo.WithTransaction(ctx, func(tx context.Context) error {
		if err := s.requireLibraryAdmin(tx, actorMemberID); err != nil {
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
		if password != "" {
			if row.Resource != model.LibraryMembers {
				return fmt.Errorf("只有成员可以设置登录密码")
			}
			if _, err := model.NormalizeMobile(
				strings.TrimSpace(fmt.Sprint(row.Data["mobile"])),
				true,
			); err != nil {
				return err
			}
		}
		created, err := s.Repo.CreateLibraryRecord(tx, row)
		if err != nil {
			return err
		}
		if password != "" {
			hash, hashErr := auth.HashPassword(password)
			if hashErr != nil {
				return hashErr
			}
			if err := s.Repo.SetMemberCredential(tx, created.ID, hash); err != nil {
				return err
			}
		}
		if err := s.Repo.InsertAuditLog(tx, model.AuditLog{
			MemberID: actorMemberID, Module: "基础资料", Action: "创建",
			Resource: string(row.Resource), RecordID: created.ID,
			RecordLabel: libraryAuditLabel(created), After: cloneMap(created.Data),
		}); err != nil {
			return err
		}
		if _, err := s.Repo.BumpLibraryRevision(tx); err != nil {
			return err
		}
		item, err := s.Repo.BuildLibrarySnapshot(tx, string(row.Resource)+"-created")
		if err != nil {
			return err
		}
		output = result{Record: created, Snapshot: item}
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
		return model.LibraryRecord{}, model.LibrarySnapshot{}, false, err
	}
	if !replayed {
		s.mirrorLibrary(ctx, &output.Snapshot)
	}
	return output.Record, output.Snapshot, replayed, nil
}

func (s Service) AddTravelTypeTagPresets(
	ctx context.Context,
	actorMemberID string,
	idempotencyKey, requestHash string,
) ([]model.LibraryRecord, *model.LibrarySnapshot, bool, error) {
	type result struct {
		Records  []model.LibraryRecord  `json:"records"`
		Snapshot *model.LibrarySnapshot `json:"snapshot,omitempty"`
	}
	const scope = "library-tag-presets"
	output := result{Records: []model.LibraryRecord{}}
	var replayed bool
	err := s.Repo.WithTransaction(ctx, func(tx context.Context) error {
		if err := s.requireLibraryAdmin(tx, actorMemberID); err != nil {
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
		created, err := s.Repo.EnsureTravelTypeTagPresets(tx)
		if err != nil {
			return err
		}
		output.Records = created
		for _, row := range created {
			if err := s.Repo.InsertAuditLog(tx, model.AuditLog{
				MemberID: actorMemberID, Module: "基础资料", Action: "补充常用类型",
				Resource: string(model.LibraryTags), RecordID: row.ID,
				RecordLabel: libraryAuditLabel(row), After: cloneMap(row.Data),
			}); err != nil {
				return err
			}
		}
		if len(created) > 0 {
			if _, err := s.Repo.BumpLibraryRevision(tx); err != nil {
				return err
			}
			item, err := s.Repo.BuildLibrarySnapshot(tx, "tag-presets-created")
			if err != nil {
				return err
			}
			output.Snapshot = &item
		}
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
		return nil, nil, false, err
	}
	if !replayed && output.Snapshot != nil {
		s.mirrorLibrary(ctx, output.Snapshot)
	}
	return output.Records, output.Snapshot, replayed, nil
}

func (s Service) UpdateLibraryRecord(
	ctx context.Context,
	resource model.LibraryResource,
	id string,
	version int64,
	data map[string]any,
	password string,
	actorMemberID string,
) (model.LibraryRecord, model.LibrarySnapshot, error) {
	var updated model.LibraryRecord
	var item model.LibrarySnapshot
	err := s.Repo.WithTransaction(ctx, func(tx context.Context) error {
		if err := s.requireLibraryAdmin(tx, actorMemberID); err != nil {
			return err
		}
		current, err := s.Repo.GetLibraryRecord(tx, resource, id)
		if err != nil {
			return err
		}
		if resource == model.LibraryMembers {
			if _, supplied := data["admin"]; !supplied {
				data["admin"] = current.Data["admin"]
			}
			currentAdmin, _ := current.Data["admin"].(bool)
			nextAdmin, _ := data["admin"].(bool)
			currentInactive := memberAuthenticationDisabled(current.Data)
			nextInactive := memberAuthenticationDisabled(data)
			if currentAdmin && !currentInactive && (!nextAdmin || nextInactive) {
				hasOtherAdmin, err := s.Repo.HasOtherAuthenticatedAdmin(tx, id)
				if err != nil {
					return err
				}
				if !hasOtherAdmin {
					return fmt.Errorf("至少保留一名已启用且可登录的管理员")
				}
			}
			if !currentInactive && nextInactive {
				ownedTrips, err := s.Repo.ActiveOwnedTripCount(tx, id)
				if err != nil {
					return err
				}
				if ownedTrips > 0 {
					return fmt.Errorf(
						"该成员仍是 %d 次未删除旅行的负责人；当前不支持负责人转移，不能停用",
						ownedTrips,
					)
				}
			}
			hasCredential, err := s.Repo.HasMemberCredential(tx, id)
			if err != nil {
				return err
			}
			mobileValue := ""
			if raw, exists := data["mobile"]; exists {
				mobileValue = strings.TrimSpace(fmt.Sprint(raw))
			}
			mobile, mobileErr := model.NormalizeMobile(
				mobileValue,
				false,
			)
			if mobileErr != nil {
				return mobileErr
			}
			if hasCredential && mobile == "" {
				return fmt.Errorf("可登录成员必须保留手机号")
			}
			if password != "" && mobile == "" {
				return fmt.Errorf("设置登录密码前必须填写手机号")
			}
		} else {
			if password != "" {
				return fmt.Errorf("只有成员可以设置登录密码")
			}
			currentCategory := strings.TrimSpace(fmt.Sprint(current.Data["category"]))
			nextCategory := strings.TrimSpace(fmt.Sprint(data["category"]))
			if currentCategory != nextCategory {
				impact, err := s.Repo.GetLibraryDeletionImpact(tx, resource, id)
				if err != nil {
					return err
				}
				if len(impact.References) > 0 {
					return fmt.Errorf(
						"标签仍被以下内容使用，不能直接修改分类；请先调整引用：%s: %w",
						repository.LibraryReferenceSummary(impact.References),
						repository.ErrReferenced,
					)
				}
			}
		}
		updated, err = s.Repo.UpdateLibraryRecord(tx, resource, id, version, data)
		if err != nil {
			return err
		}
		if resource == model.LibraryMembers && memberAuthenticationDisabled(updated.Data) {
			if err := s.Repo.DeleteSessionsForMember(tx, id); err != nil {
				return err
			}
		}
		if password != "" {
			hash, hashErr := auth.HashPassword(password)
			if hashErr != nil {
				return hashErr
			}
			if err := s.Repo.SetMemberCredential(tx, id, hash); err != nil {
				return err
			}
		}
		if err := s.Repo.InsertAuditLog(tx, model.AuditLog{
			MemberID: actorMemberID, Module: "基础资料", Action: "修改",
			Resource: string(resource), RecordID: updated.ID,
			RecordLabel: libraryAuditLabel(updated), Before: cloneMap(current.Data),
			After: cloneMap(updated.Data),
		}); err != nil {
			return err
		}
		if _, err := s.Repo.BumpLibraryRevision(tx); err != nil {
			return err
		}
		item, err = s.Repo.BuildLibrarySnapshot(tx, string(resource)+"-updated")
		return err
	})
	if err != nil {
		return model.LibraryRecord{}, model.LibrarySnapshot{}, err
	}
	s.mirrorLibrary(ctx, &item)
	return updated, item, nil
}

func memberAuthenticationDisabled(data map[string]any) bool {
	status, ok := data["status"].(string)
	return ok && strings.TrimSpace(status) == "inactive"
}

func (s Service) DeleteLibraryRecord(
	ctx context.Context,
	resource model.LibraryResource,
	id string,
	version int64,
	replaceTagWithOther bool,
	deleteLoginCredential bool,
	actorMemberID string,
) (model.LibrarySnapshot, error) {
	type transactionResult struct {
		Item          model.LibrarySnapshot
		TripSnapshots []model.Snapshot
	}
	result, err := withTransactionResult(
		ctx,
		s.Repo,
		func(tx context.Context) (transactionResult, error) {
			attempt := transactionResult{
				TripSnapshots: make([]model.Snapshot, 0),
			}
			if err := s.requireLibraryAdmin(tx, actorMemberID); err != nil {
				return transactionResult{}, err
			}
			previous, err := s.Repo.GetLibraryRecord(tx, resource, id)
			if err != nil {
				return transactionResult{}, err
			}
			if resource == model.LibraryTags && replaceTagWithOther {
				fallback, tripIDs, err := s.Repo.ReplaceTagWithFallbackAndDelete(
					tx,
					id,
					version,
					actorMemberID,
				)
				if err != nil {
					return transactionResult{}, err
				}
				for _, tripID := range tripIDs {
					if err := s.Repo.InsertAuditLog(tx, model.AuditLog{
						TripID: tripID, MemberID: actorMemberID,
						Module: "标签", Action: "替换并删除",
						Resource: string(resource), RecordID: id,
						RecordLabel: libraryAuditLabel(previous),
						Before:      map[string]any{"tagId": id, "name": previous.Data["name"]},
						After: map[string]any{
							"tagId": fallback.ID, "name": fallback.Data["name"],
						},
					}); err != nil {
						return transactionResult{}, err
					}
					if _, err := s.Repo.BumpTripRevision(tx, tripID); err != nil {
						return transactionResult{}, err
					}
					snapshot, err := s.Repo.BuildSnapshot(tx, tripID, "tag-replaced-with-other")
					if err != nil {
						return transactionResult{}, err
					}
					attempt.TripSnapshots = append(attempt.TripSnapshots, snapshot)
				}
			} else if err := s.Repo.DeleteLibraryRecordWithOptions(
				tx,
				resource,
				id,
				version,
				resource == model.LibraryMembers && deleteLoginCredential,
			); err != nil {
				return transactionResult{}, err
			}
			action := "删除"
			if resource == model.LibraryMembers && deleteLoginCredential {
				action = "注销并删除"
			}
			if err := s.Repo.InsertAuditLog(tx, model.AuditLog{
				MemberID: actorMemberID, Module: "基础资料", Action: action,
				Resource: string(resource), RecordID: id,
				RecordLabel: libraryAuditLabel(previous), Before: cloneMap(previous.Data),
			}); err != nil {
				return transactionResult{}, err
			}
			if _, err := s.Repo.BumpLibraryRevision(tx); err != nil {
				return transactionResult{}, err
			}
			attempt.Item, err = s.Repo.BuildLibrarySnapshot(tx, string(resource)+"-deleted")
			if err != nil {
				return transactionResult{}, err
			}
			return attempt, nil
		},
	)
	if err != nil {
		return model.LibrarySnapshot{}, err
	}
	for index := range result.TripSnapshots {
		s.mirror(
			ctx,
			result.TripSnapshots[index].TripID,
			&result.TripSnapshots[index],
		)
	}
	s.mirrorLibrary(ctx, &result.Item)
	return result.Item, nil
}

func (s Service) mirrorLibrary(ctx context.Context, item *model.LibrarySnapshot) {
	if item.ID == "" || len(item.Payload) == 0 {
		return
	}
	if err := snapshot.Mirror(
		s.Config.SnapshotDir,
		"library",
		item.ID,
		item.Payload,
	); err != nil {
		_ = s.Repo.MarkLibrarySnapshotMirrored(
			context.Background(),
			item.ID,
			"pending",
		)
		return
	}
	if err := s.Repo.MarkLibrarySnapshotMirrored(ctx, item.ID, "mirrored"); err == nil {
		item.MirrorState = "mirrored"
	}
}

func libraryString(data map[string]any, field string) string {
	value, _ := data[field].(string)
	return strings.TrimSpace(value)
}

func libraryAuditLabel(row model.LibraryRecord) string {
	for _, field := range []string{"displayName", "name"} {
		if value := strings.TrimSpace(fmt.Sprint(row.Data[field])); value != "" && value != "<nil>" {
			return value
		}
	}
	return row.RecordKey
}
