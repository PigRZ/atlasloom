// 旅行备份服务负责依赖预览、显式映射与事务化创建新旅行。
package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"my-travel/server/internal/model"
	"my-travel/server/internal/repository"
)

type TripBackupDependencyPreview struct {
	SourceID    string `json:"sourceId"`
	DisplayName string `json:"displayName,omitempty"`
	Name        string `json:"name,omitempty"`
	Category    string `json:"category,omitempty"`
	Status      string `json:"status"`
	TargetID    string `json:"targetId,omitempty"`
}

type TripBackupTripPreview struct {
	Title     string `json:"title"`
	StartDate string `json:"startDate"`
	EndDate   string `json:"endDate"`
}

type TripBackupImportPreview struct {
	Valid            bool                          `json:"valid"`
	Ready            bool                          `json:"ready"`
	ValidationErrors []string                      `json:"validationErrors"`
	Checksum         string                        `json:"checksum,omitempty"`
	SuggestedTripID  string                        `json:"suggestedTripId,omitempty"`
	Trip             TripBackupTripPreview         `json:"trip"`
	Counts           map[string]int                `json:"counts"`
	Members          []TripBackupDependencyPreview `json:"members"`
	Tags             []TripBackupDependencyPreview `json:"tags"`
}

type TripBackupDependencyMapping struct {
	SourceID string `json:"sourceId"`
	TargetID string `json:"targetId,omitempty"`
	Create   bool   `json:"create"`
}

type TripBackupImportResult struct {
	Trip            model.Trip             `json:"trip"`
	Snapshot        model.Snapshot         `json:"snapshot"`
	LibrarySnapshot *model.LibrarySnapshot `json:"librarySnapshot,omitempty"`
	Replayed        bool                   `json:"replayed"`
}

func (s Service) PreviewTripBackupImport(
	ctx context.Context,
	raw []byte,
) (TripBackupImportPreview, error) {
	preview := TripBackupImportPreview{
		ValidationErrors: []string{},
		Counts:           map[string]int{},
		Members:          []TripBackupDependencyPreview{},
		Tags:             []TripBackupDependencyPreview{},
	}
	backup, err := repository.ValidateTripBackup(raw)
	if err != nil {
		preview.ValidationErrors = append(preview.ValidationErrors, err.Error())
		return preview, nil
	}
	memberRows, err := s.Repo.ListLibraryRecords(ctx, model.LibraryMembers)
	if err != nil {
		return preview, err
	}
	tagRows, err := s.Repo.ListLibraryRecords(ctx, model.LibraryTags)
	if err != nil {
		return preview, err
	}
	preview.Valid = true
	preview.Ready = true
	preview.Checksum = backup.ContentChecksum
	preview.Trip = TripBackupTripPreview{
		Title:     backup.Snapshot.Trip.Title,
		StartDate: backup.Snapshot.Trip.StartDate,
		EndDate:   backup.Snapshot.Trip.EndDate,
	}
	for _, resource := range model.Resources() {
		preview.Counts[string(resource)] = len(
			backup.Snapshot.Datasets[string(resource)],
		)
	}
	for _, dependency := range backup.Dependencies.Members {
		resolution := TripBackupDependencyPreview{
			SourceID:    dependency.SourceID,
			DisplayName: dependency.DisplayName,
			Status:      "missing",
		}
		for _, row := range memberRows {
			if row.ID == dependency.SourceID {
				resolution.Status = "matched-id"
				resolution.TargetID = row.ID
				break
			}
		}
		if resolution.TargetID == "" {
			preview.Ready = false
		}
		preview.Members = append(preview.Members, resolution)
	}
	for _, dependency := range backup.Dependencies.Tags {
		resolution := resolveTagDependency(dependency, tagRows)
		if resolution.TargetID == "" {
			preview.Ready = false
		}
		preview.Tags = append(preview.Tags, resolution)
	}
	preview.SuggestedTripID, err = s.Repo.AvailableTripID(
		ctx,
		backup.Snapshot.Trip.ID,
	)
	if err != nil {
		return TripBackupImportPreview{}, err
	}
	return preview, nil
}

func resolveTagDependency(
	dependency model.TripBackupTagDependency,
	rows []model.LibraryRecord,
) TripBackupDependencyPreview {
	result := TripBackupDependencyPreview{
		SourceID: dependency.SourceID,
		Name:     dependency.Name,
		Category: dependency.Category,
		Status:   "missing",
	}
	for _, row := range rows {
		if row.ID != dependency.SourceID {
			continue
		}
		if sameBackupTag(dependency, row) {
			result.Status = "matched-id"
			result.TargetID = row.ID
		} else {
			result.Status = "conflict"
		}
		return result
	}
	var matches []model.LibraryRecord
	for _, row := range rows {
		if sameBackupTag(dependency, row) {
			matches = append(matches, row)
		}
	}
	if len(matches) == 1 {
		result.Status = "matched-semantic"
		result.TargetID = matches[0].ID
	} else if len(matches) > 1 {
		result.Status = "conflict"
	}
	return result
}

func sameBackupTag(
	dependency model.TripBackupTagDependency,
	row model.LibraryRecord,
) bool {
	return strings.TrimSpace(fmt.Sprint(row.Data["category"])) == dependency.Category &&
		strings.EqualFold(
			strings.TrimSpace(fmt.Sprint(row.Data["name"])),
			strings.TrimSpace(dependency.Name),
		)
}

func (s Service) ApplyTripBackupImport(
	ctx context.Context,
	raw []byte,
	confirmChecksum string,
	memberInputs, tagInputs []TripBackupDependencyMapping,
	actor model.MemberIdentity,
	idempotencyKey, requestHash string,
) (TripBackupImportResult, error) {
	backup, err := repository.ValidateTripBackup(raw)
	if err != nil {
		return TripBackupImportResult{}, err
	}
	if confirmChecksum == "" ||
		!strings.EqualFold(confirmChecksum, backup.ContentChecksum) {
		return TripBackupImportResult{}, fmt.Errorf("确认校验和与旅行备份不一致")
	}
	scope := "trip-backup-import:" + actor.ID
	output, err := withTransactionResult(
		ctx,
		s.Repo,
		func(tx context.Context) (TripBackupImportResult, error) {
			var attempt TripBackupImportResult
			currentActor, err := s.activeActor(tx, actor.ID)
			if err != nil {
				return TripBackupImportResult{}, err
			}
			if stored, err := s.Repo.GetIdempotencyRequest(
				tx,
				scope,
				idempotencyKey,
				requestHash,
			); err == nil {
				if err := json.Unmarshal(stored.Response, &attempt); err != nil {
					return TripBackupImportResult{}, err
				}
				attempt.Replayed = true
				return attempt, nil
			} else if !errors.Is(err, repository.ErrNotFound) {
				return TripBackupImportResult{}, err
			}
			targetTripID, err := s.Repo.AvailableTripID(
				tx,
				backup.Snapshot.Trip.ID,
			)
			if err != nil {
				return TripBackupImportResult{}, err
			}
			memberMappings, createdMembers, err := s.resolveMemberMappings(
				tx,
				backup,
				memberInputs,
				targetTripID,
				currentActor,
			)
			if err != nil {
				return TripBackupImportResult{}, err
			}
			tagMappings, createdTags, err := s.resolveTagMappings(
				tx,
				backup,
				tagInputs,
				targetTripID,
				currentActor,
			)
			if err != nil {
				return TripBackupImportResult{}, err
			}
			if len(createdMembers)+len(createdTags) > 0 {
				if _, err := s.Repo.BumpLibraryRevision(tx); err != nil {
					return TripBackupImportResult{}, err
				}
				item, err := s.Repo.BuildLibrarySnapshot(
					tx,
					"trip-backup-dependencies-created",
				)
				if err != nil {
					return TripBackupImportResult{}, err
				}
				attempt.LibrarySnapshot = &item
			}
			attempt.Trip, attempt.Snapshot, err = s.Repo.CreateTripFromBackup(
				tx,
				backup,
				targetTripID,
				memberMappings,
				tagMappings,
				currentActor.ID,
			)
			if err != nil {
				return TripBackupImportResult{}, err
			}
			encoded, err := json.Marshal(attempt)
			if err != nil {
				return TripBackupImportResult{}, err
			}
			if err := s.Repo.StoreIdempotencyRequest(
				tx,
				scope,
				idempotencyKey,
				requestHash,
				encoded,
			); err != nil {
				return TripBackupImportResult{}, err
			}
			return attempt, nil
		},
	)
	if err != nil {
		return TripBackupImportResult{}, err
	}
	if output.Replayed {
		if err := refreshSnapshots(ctx, s.Repo, &output.Snapshot); err != nil {
			return TripBackupImportResult{}, err
		}
	} else {
		s.mirror(ctx, output.Trip.ID, &output.Snapshot)
		if output.LibrarySnapshot != nil {
			s.mirrorLibrary(ctx, output.LibrarySnapshot)
		}
	}
	return output, nil
}

func (s Service) resolveMemberMappings(
	ctx context.Context,
	backup model.TripBackup,
	inputs []TripBackupDependencyMapping,
	targetTripID string,
	actor model.MemberIdentity,
) (map[string]string, []model.LibraryRecord, error) {
	inputBySource, err := indexMappings(inputs)
	if err != nil {
		return nil, nil, err
	}
	result := make(map[string]string, len(backup.Dependencies.Members))
	usedTargets := map[string]struct{}{}
	created := []model.LibraryRecord{}
	for _, dependency := range backup.Dependencies.Members {
		input, exists := inputBySource[dependency.SourceID]
		if !exists {
			return nil, nil, fmt.Errorf("成员“%s”尚未完成映射", dependency.DisplayName)
		}
		delete(inputBySource, dependency.SourceID)
		var target model.LibraryRecord
		if input.Create {
			if !actor.Admin {
				return nil, nil, fmt.Errorf("只有管理员可以创建缺失的基础成员")
			}
			if strings.TrimSpace(input.TargetID) != "" {
				return nil, nil, fmt.Errorf("创建成员时不能同时指定已有成员")
			}
			target, err = s.Repo.CreateLibraryRecord(ctx, model.LibraryRecord{
				Resource: model.LibraryMembers,
				RecordKey: importDependencyRecordKey(
					"member",
					backup.ContentChecksum,
					dependency.SourceID,
					targetTripID,
				),
				Data: map[string]any{
					"displayName": dependency.DisplayName,
					"admin":       false,
					"status":      "active",
				},
			})
			if err != nil {
				return nil, nil, err
			}
			created = append(created, target)
			if err := s.auditImportedLibraryRecord(
				ctx,
				target,
				actor.ID,
				backup.ContentChecksum,
			); err != nil {
				return nil, nil, err
			}
		} else {
			if strings.TrimSpace(input.TargetID) == "" {
				return nil, nil, fmt.Errorf("成员“%s”尚未完成映射", dependency.DisplayName)
			}
			target, err = s.Repo.GetLibraryRecord(
				ctx,
				model.LibraryMembers,
				input.TargetID,
			)
			if err != nil {
				return nil, nil, fmt.Errorf("选择的基础成员不存在: %w", err)
			}
		}
		if _, duplicate := usedTargets[target.ID]; duplicate {
			return nil, nil, fmt.Errorf("不同备份成员不能映射到同一个基础成员")
		}
		usedTargets[target.ID] = struct{}{}
		result[dependency.SourceID] = target.ID
	}
	if len(inputBySource) > 0 {
		return nil, nil, fmt.Errorf("成员映射包含旅行未引用的项目")
	}
	return result, created, nil
}

func (s Service) resolveTagMappings(
	ctx context.Context,
	backup model.TripBackup,
	inputs []TripBackupDependencyMapping,
	targetTripID string,
	actor model.MemberIdentity,
) (map[string]string, []model.LibraryRecord, error) {
	inputBySource, err := indexMappings(inputs)
	if err != nil {
		return nil, nil, err
	}
	result := make(map[string]string, len(backup.Dependencies.Tags))
	usedTargets := map[string]struct{}{}
	created := []model.LibraryRecord{}
	for _, dependency := range backup.Dependencies.Tags {
		input, exists := inputBySource[dependency.SourceID]
		if !exists {
			return nil, nil, fmt.Errorf("标签“%s”尚未完成映射", dependency.Name)
		}
		delete(inputBySource, dependency.SourceID)
		var target model.LibraryRecord
		if input.Create {
			if !actor.Admin {
				return nil, nil, fmt.Errorf("只有管理员可以创建缺失的标签")
			}
			if strings.TrimSpace(input.TargetID) != "" {
				return nil, nil, fmt.Errorf("创建标签时不能同时指定已有标签")
			}
			if existing, lookupErr := s.Repo.FindTagByCategoryAndName(
				ctx,
				dependency.Category,
				dependency.Name,
			); lookupErr == nil {
				return nil, nil, fmt.Errorf(
					"标签“%s”已存在，请选择现有标签 %s",
					dependency.Name,
					existing.ID,
				)
			} else if !errors.Is(lookupErr, repository.ErrNotFound) {
				return nil, nil, lookupErr
			}
			target, err = s.Repo.CreateLibraryRecord(ctx, model.LibraryRecord{
				Resource: model.LibraryTags,
				RecordKey: importDependencyRecordKey(
					"tag",
					backup.ContentChecksum,
					dependency.SourceID,
					targetTripID,
				),
				Data: map[string]any{
					"name":     dependency.Name,
					"category": dependency.Category,
					"color":    dependency.Color,
					"status":   "active",
				},
			})
			if err != nil {
				return nil, nil, err
			}
			created = append(created, target)
			if err := s.auditImportedLibraryRecord(
				ctx,
				target,
				actor.ID,
				backup.ContentChecksum,
			); err != nil {
				return nil, nil, err
			}
		} else {
			if strings.TrimSpace(input.TargetID) == "" {
				return nil, nil, fmt.Errorf("标签“%s”尚未完成映射", dependency.Name)
			}
			target, err = s.Repo.GetLibraryRecord(
				ctx,
				model.LibraryTags,
				input.TargetID,
			)
			if err != nil {
				return nil, nil, fmt.Errorf("选择的标签不存在: %w", err)
			}
			if strings.TrimSpace(fmt.Sprint(target.Data["category"])) !=
				dependency.Category {
				return nil, nil, fmt.Errorf("标签“%s”不能跨分类映射", dependency.Name)
			}
		}
		if _, duplicate := usedTargets[target.ID]; duplicate {
			return nil, nil, fmt.Errorf("不同备份标签不能映射到同一个基础标签")
		}
		usedTargets[target.ID] = struct{}{}
		result[dependency.SourceID] = target.ID
	}
	if len(inputBySource) > 0 {
		return nil, nil, fmt.Errorf("标签映射包含旅行未引用的项目")
	}
	return result, created, nil
}

func indexMappings(
	inputs []TripBackupDependencyMapping,
) (map[string]TripBackupDependencyMapping, error) {
	result := make(map[string]TripBackupDependencyMapping, len(inputs))
	for _, input := range inputs {
		input.SourceID = strings.TrimSpace(input.SourceID)
		if input.SourceID == "" {
			return nil, fmt.Errorf("基础资料映射缺少来源 ID")
		}
		if _, duplicate := result[input.SourceID]; duplicate {
			return nil, fmt.Errorf("基础资料映射重复")
		}
		result[input.SourceID] = input
	}
	return result, nil
}

func importDependencyRecordKey(
	resource, checksum, sourceID, targetTripID string,
) string {
	sum := sha256.Sum256([]byte(
		resource + "\x00" + checksum + "\x00" + sourceID + "\x00" + targetTripID,
	))
	return resource + "-import-" + hex.EncodeToString(sum[:12])
}

func (s Service) auditImportedLibraryRecord(
	ctx context.Context,
	row model.LibraryRecord,
	actorMemberID, checksum string,
) error {
	return s.Repo.InsertAuditLog(ctx, model.AuditLog{
		MemberID:    actorMemberID,
		Module:      "基础资料",
		Action:      "随旅行备份创建",
		Resource:    string(row.Resource),
		RecordID:    row.ID,
		RecordLabel: libraryAuditLabel(row),
		After: map[string]any{
			"data":     cloneMap(row.Data),
			"checksum": checksum,
		},
	})
}
