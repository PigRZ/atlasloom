// 旅行选择成员时同步成员访问权，旅行负责人权限不被自动降级。
package service

import (
	"context"
	"errors"
	"strings"

	"my-travel/server/internal/repository"
)

func (s Service) syncTripMemberAccess(
	ctx context.Context,
	tripID, previousMemberID, nextMemberID string,
) error {
	previousMemberID = strings.TrimSpace(previousMemberID)
	nextMemberID = strings.TrimSpace(nextMemberID)
	if previousMemberID == nextMemberID {
		return nil
	}
	if previousMemberID != "" {
		if err := s.Repo.DeleteMembership(ctx, tripID, previousMemberID); err != nil {
			return err
		}
	}
	if nextMemberID == "" {
		return nil
	}
	role, err := s.Repo.RoleFor(ctx, tripID, nextMemberID)
	if err == nil && role == "owner" {
		return nil
	}
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return err
	}
	return s.Repo.SetMembership(ctx, tripID, nextMemberID, "editor")
}

func (s Service) GetTripMemberDeletionImpact(
	ctx context.Context,
	tripID, recordID, actorMemberID string,
) (repository.TripMemberDeletionImpact, error) {
	if err := s.requireTripEditor(ctx, tripID, actorMemberID); err != nil {
		return repository.TripMemberDeletionImpact{}, err
	}
	return s.Repo.GetTripMemberDeletionImpact(ctx, tripID, recordID)
}
