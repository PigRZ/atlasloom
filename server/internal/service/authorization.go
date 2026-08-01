// 事务内授权保证 MongoDB 重试不会沿用事务外已经过期的会话或角色判断。
package service

import (
	"context"
	"errors"

	"my-travel/server/internal/model"
	"my-travel/server/internal/repository"
)

func (s Service) activeActor(
	ctx context.Context,
	actorMemberID string,
) (model.MemberIdentity, error) {
	actor, err := s.Repo.GetMemberIdentity(ctx, actorMemberID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return model.MemberIdentity{}, repository.ErrForbidden
		}
		return model.MemberIdentity{}, err
	}
	return actor, nil
}

func (s Service) requireActiveActor(
	ctx context.Context,
	actorMemberID string,
) error {
	_, err := s.activeActor(ctx, actorMemberID)
	if err != nil {
		return err
	}
	return nil
}

func (s Service) requireLibraryAdmin(
	ctx context.Context,
	actorMemberID string,
) error {
	actor, err := s.activeActor(ctx, actorMemberID)
	if err != nil {
		return err
	}
	if !actor.Admin {
		return repository.ErrForbidden
	}
	return nil
}

func (s Service) requireTripEditor(
	ctx context.Context,
	tripID, actorMemberID string,
) error {
	if err := s.requireActiveActor(ctx, actorMemberID); err != nil {
		return err
	}
	role, err := s.Repo.RoleFor(ctx, tripID, actorMemberID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return repository.ErrForbidden
		}
		return err
	}
	if role != "owner" && role != "editor" {
		return repository.ErrForbidden
	}
	return nil
}

func (s Service) requireTripOwner(
	ctx context.Context,
	tripID, actorMemberID string,
) error {
	if err := s.requireActiveActor(ctx, actorMemberID); err != nil {
		return err
	}
	role, err := s.Repo.RoleFor(ctx, tripID, actorMemberID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return repository.ErrForbidden
		}
		return err
	}
	if role != "owner" {
		return repository.ErrForbidden
	}
	return nil
}

func (s Service) requireTripDeletePermission(
	ctx context.Context,
	tripID, actorMemberID string,
) error {
	actor, err := s.activeActor(ctx, actorMemberID)
	if err != nil {
		return err
	}
	if actor.Admin {
		return nil
	}
	role, err := s.Repo.RoleFor(ctx, tripID, actorMemberID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return repository.ErrForbidden
		}
		return err
	}
	if role != "owner" {
		return repository.ErrForbidden
	}
	return nil
}
