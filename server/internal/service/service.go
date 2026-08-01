// Service 编排授权后的业务写入，保证变更、聚合修订号与快照位于同一事务。
package service

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"my-travel/server/internal/app"
	"my-travel/server/internal/auth"
	"my-travel/server/internal/model"
	"my-travel/server/internal/repository"
)

type Service struct {
	Repo   *repository.Repository
	Config app.Config
}

type bootstrapStore interface {
	WithTransaction(context.Context, func(context.Context) error) error
	CredentialCount(context.Context) (int64, error)
	CreateAuthenticatedMember(context.Context, string, string, string, bool) (model.MemberIdentity, error)
	EnsureDefaultTagPresets(context.Context) ([]model.LibraryRecord, error)
	ListTrips(context.Context) ([]model.Trip, error)
	SetMembership(context.Context, string, string, string) error
}

func (s Service) Bootstrap(
	ctx context.Context,
	token, displayName, mobile, password string,
) (model.MemberIdentity, error) {
	if s.Config.BootstrapToken == "" || token != s.Config.BootstrapToken {
		return model.MemberIdentity{}, fmt.Errorf("bootstrap is not authorized")
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return model.MemberIdentity{}, err
	}
	return bootstrap(ctx, s.Repo, displayName, mobile, hash)
}

func (s Service) ProvisionUser(
	ctx context.Context,
	token, displayName, mobile, password string,
) (model.MemberIdentity, error) {
	if s.Config.ProvisionToken == "" ||
		subtle.ConstantTimeCompare([]byte(token), []byte(s.Config.ProvisionToken)) != 1 {
		return model.MemberIdentity{}, fmt.Errorf("user provisioning is not authorized")
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return model.MemberIdentity{}, err
	}
	return provisionUser(ctx, s.Repo, displayName, mobile, hash)
}

func bootstrap(
	ctx context.Context,
	store bootstrapStore,
	displayName, mobile, passwordHash string,
) (model.MemberIdentity, error) {
	var member model.MemberIdentity
	err := store.WithTransaction(ctx, func(tx context.Context) error {
		count, err := store.CredentialCount(tx)
		if err != nil {
			return err
		}
		if count != 0 {
			return fmt.Errorf("bootstrap has already completed")
		}
		member, err = store.CreateAuthenticatedMember(
			tx, displayName, mobile, passwordHash, true,
		)
		if err != nil {
			return err
		}
		if _, err := store.EnsureDefaultTagPresets(tx); err != nil {
			return err
		}
		trips, err := store.ListTrips(tx)
		if err != nil {
			return err
		}
		for _, trip := range trips {
			if err := store.SetMembership(tx, trip.ID, member.ID, "owner"); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return model.MemberIdentity{}, err
	}
	return member, nil
}

func provisionUser(
	ctx context.Context,
	store bootstrapStore,
	displayName, mobile, passwordHash string,
) (model.MemberIdentity, error) {
	var member model.MemberIdentity
	err := store.WithTransaction(ctx, func(tx context.Context) error {
		var err error
		member, err = store.CreateAuthenticatedMember(
			tx, displayName, mobile, passwordHash, false,
		)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return model.MemberIdentity{}, err
	}
	return member, nil
}

func (s Service) Login(
	ctx context.Context,
	mobile, password string,
) (model.MemberIdentity, model.Session, error) {
	member, credential, err := s.Repo.GetMemberIdentityByMobile(ctx, mobile)
	if err != nil {
		return model.MemberIdentity{}, model.Session{}, fmt.Errorf("invalid mobile or password")
	}
	if !auth.VerifyPassword(credential.PasswordHash, password) {
		return model.MemberIdentity{}, model.Session{}, fmt.Errorf("invalid mobile or password")
	}
	id, err := auth.RandomToken(32)
	if err != nil {
		return model.MemberIdentity{}, model.Session{}, err
	}
	csrf, err := auth.RandomToken(24)
	if err != nil {
		return model.MemberIdentity{}, model.Session{}, err
	}
	session := model.Session{
		ID: id, MemberID: member.ID, CSRFToken: csrf,
		ExpiresAt: time.Now().UTC().Add(s.Config.SessionTTL),
	}
	if err := s.Repo.CreateSession(ctx, session); err != nil {
		return model.MemberIdentity{}, model.Session{}, err
	}
	confirmedMember, err := s.Repo.GetMemberIdentity(ctx, member.ID)
	if err != nil {
		_ = s.Repo.DeleteSession(ctx, session.ID)
		if errors.Is(err, repository.ErrNotFound) {
			return model.MemberIdentity{}, model.Session{}, fmt.Errorf("invalid mobile or password")
		}
		return model.MemberIdentity{}, model.Session{}, err
	}
	return confirmedMember, session, nil
}

func (s Service) CreateTrip(
	ctx context.Context,
	userID string,
	trip model.Trip,
	idempotencyKey, requestHash string,
) (model.Trip, model.Snapshot, bool, error) {
	type result struct {
		Trip     model.Trip     `json:"trip"`
		Snapshot model.Snapshot `json:"snapshot"`
	}
	scope := "trip-create:" + userID
	var output result
	var replayed bool
	err := s.Repo.WithTransaction(ctx, func(tx context.Context) error {
		if err := s.requireActiveActor(tx, userID); err != nil {
			return err
		}
		if stored, err := s.Repo.GetIdempotencyRequest(tx, scope, idempotencyKey, requestHash); err == nil {
			replayed = true
			return json.Unmarshal(stored.Response, &output)
		} else if !errors.Is(err, repository.ErrNotFound) {
			return err
		}
		created, err := s.Repo.CreateTrip(tx, trip)
		if err != nil {
			return err
		}
		if err := s.Repo.SetMembership(tx, created.ID, userID, "owner"); err != nil {
			return err
		}
		days, err := initialTripDays(created)
		if err != nil {
			return err
		}
		for _, day := range days {
			day.CreatedByMemberID, day.UpdatedByMemberID = userID, userID
			createdDay, err := s.Repo.CreateRecord(tx, day)
			if err != nil {
				return err
			}
			if err := s.auditRecord(
				tx,
				createdDay,
				userID,
				"自动创建",
				nil,
				createdDay.Data,
			); err != nil {
				return err
			}
		}
		if err := s.Repo.InsertAuditLog(tx, model.AuditLog{
			TripID: created.ID, MemberID: userID, Module: "旅行",
			Action: "创建", Resource: "trips", RecordID: created.ID,
			RecordLabel: created.Title, After: tripAuditData(created),
		}); err != nil {
			return err
		}
		snap, err := s.Repo.BuildSnapshot(tx, created.ID, "trip-created")
		if err != nil {
			return err
		}
		output = result{Trip: created, Snapshot: snap}
		encoded, err := json.Marshal(output)
		if err != nil {
			return err
		}
		return s.Repo.StoreIdempotencyRequest(tx, scope, idempotencyKey, requestHash, encoded)
	})
	if err != nil {
		return model.Trip{}, model.Snapshot{}, false, err
	}
	if replayed {
		if err := refreshSnapshots(ctx, s.Repo, &output.Snapshot); err != nil {
			return model.Trip{}, model.Snapshot{}, false, err
		}
	} else {
		s.mirror(ctx, output.Trip.ID, &output.Snapshot)
	}
	return output.Trip, output.Snapshot, replayed, nil
}

func (s Service) UpdateTrip(
	ctx context.Context,
	tripID string,
	version int64,
	patch map[string]any,
	actorMemberID string,
) (model.Trip, model.Snapshot, error) {
	var updated model.Trip
	var snap model.Snapshot
	err := s.Repo.WithTransaction(ctx, func(tx context.Context) error {
		if err := s.requireTripEditor(tx, tripID, actorMemberID); err != nil {
			return err
		}
		previous, err := s.Repo.GetTrip(tx, tripID)
		if err != nil {
			return err
		}
		updated, err = s.Repo.UpdateTrip(tx, tripID, version, patch)
		if err != nil {
			return err
		}
		if err := s.Repo.InsertAuditLog(tx, model.AuditLog{
			TripID: tripID, MemberID: actorMemberID, Module: "旅行",
			Action: "修改", Resource: "trips", RecordID: tripID,
			RecordLabel: updated.Title, Before: tripAuditData(previous),
			After: tripAuditData(updated),
		}); err != nil {
			return err
		}
		snap, err = s.Repo.BuildSnapshot(tx, tripID, "trip-updated")
		return err
	})
	if err != nil {
		return model.Trip{}, model.Snapshot{}, err
	}
	s.mirror(ctx, tripID, &snap)
	return updated, snap, nil
}

func (s Service) DeleteTrip(
	ctx context.Context,
	tripID string,
	version int64,
	actorMemberID string,
) (model.Snapshot, error) {
	var snap model.Snapshot
	err := s.Repo.WithTransaction(ctx, func(tx context.Context) error {
		if err := s.requireTripDeletePermission(tx, tripID, actorMemberID); err != nil {
			return err
		}
		previous, err := s.Repo.GetTrip(tx, tripID)
		if err != nil {
			return err
		}
		deleted, err := s.Repo.DeleteTrip(tx, tripID, version, actorMemberID)
		if err != nil {
			return err
		}
		if err := s.Repo.InsertAuditLog(tx, model.AuditLog{
			TripID: tripID, MemberID: actorMemberID, Module: "旅行",
			Action: "删除", Resource: "trips", RecordID: tripID,
			RecordLabel: deleted.Title, Before: tripAuditData(previous),
			After: tripAuditData(deleted),
		}); err != nil {
			return err
		}
		snap, err = s.Repo.BuildSnapshot(tx, tripID, "trip-deleted")
		return err
	})
	if err != nil {
		return model.Snapshot{}, err
	}
	s.mirror(ctx, tripID, &snap)
	return snap, nil
}

func (s Service) CreateRecord(
	ctx context.Context,
	row model.Record,
	actorMemberID string,
	idempotencyKey, requestHash string,
) (model.Record, model.Snapshot, bool, error) {
	type result struct {
		Record   model.Record   `json:"record"`
		Snapshot model.Snapshot `json:"snapshot"`
	}
	scope := "record-create:" + row.TripID + ":" + string(row.Resource) + ":" + actorMemberID
	var output result
	var replayed bool
	err := s.Repo.WithTransaction(ctx, func(tx context.Context) error {
		if err := s.requireTripEditor(tx, row.TripID, actorMemberID); err != nil {
			return err
		}
		if stored, err := s.Repo.GetIdempotencyRequest(tx, scope, idempotencyKey, requestHash); err == nil {
			replayed = true
			return json.Unmarshal(stored.Response, &output)
		} else if !errors.Is(err, repository.ErrNotFound) {
			return err
		}
		row.CreatedByMemberID, row.UpdatedByMemberID = actorMemberID, actorMemberID
		if err := s.prepareRecordData(tx, &row, actorMemberID, true); err != nil {
			return err
		}
		created, err := s.Repo.CreateRecord(tx, row)
		if err != nil {
			return err
		}
		if row.Resource == model.Members {
			if err := s.syncTripMemberAccess(
				tx,
				row.TripID,
				"",
				fmt.Sprint(created.Data["memberId"]),
			); err != nil {
				return err
			}
		}
		if row.Resource == model.Bookings {
			if _, err := s.syncBookingExpense(tx, created, actorMemberID); err != nil {
				return err
			}
		}
		if err := s.auditRecord(tx, created, actorMemberID, "创建", nil, created.Data); err != nil {
			return err
		}
		if _, err := s.Repo.BumpTripRevision(tx, row.TripID); err != nil {
			return err
		}
		snap, err := s.Repo.BuildSnapshot(tx, row.TripID, string(row.Resource)+"-created")
		if err != nil {
			return err
		}
		output = result{Record: created, Snapshot: snap}
		encoded, err := json.Marshal(output)
		if err != nil {
			return err
		}
		return s.Repo.StoreIdempotencyRequest(tx, scope, idempotencyKey, requestHash, encoded)
	})
	if err != nil {
		return model.Record{}, model.Snapshot{}, false, err
	}
	if replayed {
		if err := refreshSnapshots(ctx, s.Repo, &output.Snapshot); err != nil {
			return model.Record{}, model.Snapshot{}, false, err
		}
	} else {
		s.mirror(ctx, row.TripID, &output.Snapshot)
	}
	return output.Record, output.Snapshot, replayed, nil
}

func (s Service) UpdateRecord(
	ctx context.Context,
	tripID string,
	resource model.Resource,
	id string,
	version int64,
	data map[string]any,
	actorMemberID string,
) (model.Record, model.Snapshot, error) {
	var updated model.Record
	var snap model.Snapshot
	err := s.Repo.WithTransaction(ctx, func(tx context.Context) error {
		if err := s.requireTripEditor(tx, tripID, actorMemberID); err != nil {
			return err
		}
		previous, err := s.Repo.GetRecord(tx, tripID, resource, id)
		if err != nil {
			return err
		}
		if resource == model.PackingItems &&
			fmt.Sprint(previous.Data["ownerMemberId"]) != actorMemberID {
			return repository.ErrNotFound
		}
		if resource == model.Expenses {
			bookingID := expenseBookingID(previous.Data)
			if bookingID == "" && !blankService(data["bookingRecordId"]) {
				return fmt.Errorf("费用不能手动关联预订")
			}
			if bookingID != "" {
				booking, err := s.Repo.GetRecord(tx, tripID, model.Bookings, bookingID)
				if err != nil {
					return err
				}
				data = cloneMap(data)
				data["bookingRecordId"] = bookingID
				data["item"] = fmt.Sprint(booking.Data["item"])
				data["type"] = fmt.Sprint(booking.Data["type"])
				data["payerMemberId"] = fmt.Sprint(booking.Data["bookerMemberId"])
			}
		}
		candidate := model.Record{TripID: tripID, Resource: resource, Data: data}
		if err := s.prepareRecordData(tx, &candidate, actorMemberID, false); err != nil {
			return err
		}
		updated, err = s.Repo.UpdateRecord(
			tx, tripID, resource, id, version, candidate.Data, actorMemberID,
		)
		if err != nil {
			return err
		}
		if resource == model.Members {
			if err := s.syncTripMemberAccess(
				tx,
				tripID,
				fmt.Sprint(previous.Data["memberId"]),
				fmt.Sprint(updated.Data["memberId"]),
			); err != nil {
				return err
			}
		}
		if resource == model.Bookings {
			if _, err := s.syncBookingExpense(tx, updated, actorMemberID); err != nil {
				return err
			}
		}
		if err := s.auditRecord(
			tx, updated, actorMemberID, "修改", previous.Data, updated.Data,
		); err != nil {
			return err
		}
		if _, err := s.Repo.BumpTripRevision(tx, tripID); err != nil {
			return err
		}
		snap, err = s.Repo.BuildSnapshot(tx, tripID, string(resource)+"-updated")
		return err
	})
	if err != nil {
		return model.Record{}, model.Snapshot{}, err
	}
	s.mirror(ctx, tripID, &snap)
	return updated, snap, nil
}

func (s Service) DeleteRecord(
	ctx context.Context,
	tripID string,
	resource model.Resource,
	id string,
	version int64,
	actorMemberID string,
) (model.Snapshot, error) {
	return s.DeleteRecordWithOptions(
		ctx, tripID, resource, id, version, false, actorMemberID,
	)
}

func (s Service) DeleteRecordWithOptions(
	ctx context.Context,
	tripID string,
	resource model.Resource,
	id string,
	version int64,
	cascade bool,
	actorMemberID string,
) (model.Snapshot, error) {
	var snap model.Snapshot
	err := s.Repo.WithTransaction(ctx, func(tx context.Context) error {
		if err := s.requireTripEditor(tx, tripID, actorMemberID); err != nil {
			return err
		}
		previous, err := s.Repo.GetRecord(tx, tripID, resource, id)
		if err != nil {
			return err
		}
		if resource == model.PackingItems &&
			fmt.Sprint(previous.Data["ownerMemberId"]) != actorMemberID {
			return repository.ErrNotFound
		}
		if resource == model.Expenses && !blankService(previous.Data["bookingRecordId"]) {
			return fmt.Errorf("预订自动生成的费用不能手动删除，请删除对应预订")
		}
		if resource == model.Bookings {
			if expense, lookupErr := s.Repo.GetExpenseByBooking(tx, tripID, id); lookupErr == nil {
				if err := s.Repo.DeleteRecord(
					tx, tripID, model.Expenses, expense.ID, expense.Version,
				); err != nil {
					return err
				}
				if err := s.auditRecord(
					tx, expense, actorMemberID, "自动删除", expense.Data, nil,
				); err != nil {
					return err
				}
			} else if !errors.Is(lookupErr, repository.ErrNotFound) {
				return lookupErr
			}
		}
		var cascadedRecords []model.Record
		if resource == model.Places && cascade {
			err = s.Repo.DeletePlaceCascade(
				tx, tripID, id, version, actorMemberID,
			)
		} else if resource == model.Members && cascade {
			cascadedRecords, err = s.Repo.DeleteTripMemberCascade(
				tx,
				tripID,
				id,
				version,
			)
		} else {
			err = s.Repo.DeleteRecord(tx, tripID, resource, id, version)
		}
		if err != nil {
			return err
		}
		for _, cascadedRecord := range cascadedRecords {
			if err := s.auditRecord(
				tx,
				cascadedRecord,
				actorMemberID,
				"连带删除",
				cascadedRecord.Data,
				nil,
			); err != nil {
				return err
			}
		}
		if resource == model.Members {
			if err := s.syncTripMemberAccess(
				tx,
				tripID,
				fmt.Sprint(previous.Data["memberId"]),
				"",
			); err != nil {
				return err
			}
		}
		if err := s.auditRecord(
			tx, previous, actorMemberID, "删除", previous.Data, nil,
		); err != nil {
			return err
		}
		if _, err := s.Repo.BumpTripRevision(tx, tripID); err != nil {
			return err
		}
		snap, err = s.Repo.BuildSnapshot(tx, tripID, string(resource)+"-deleted")
		return err
	})
	if err != nil {
		return model.Snapshot{}, err
	}
	s.mirror(ctx, tripID, &snap)
	return snap, nil
}

func (s Service) Reorder(
	ctx context.Context,
	tripID string,
	resource model.Resource,
	group string,
	ids []string,
	baseRevision int64,
	actorMemberID string,
	idempotencyKey, requestHash string,
) (model.Snapshot, bool, error) {
	scope := "record-reorder:" + tripID + ":" + string(resource) + ":" + group
	var snap model.Snapshot
	var replayed bool
	err := s.Repo.WithTransaction(ctx, func(tx context.Context) error {
		if err := s.requireTripEditor(tx, tripID, actorMemberID); err != nil {
			return err
		}
		if stored, err := s.Repo.GetIdempotencyRequest(tx, scope, idempotencyKey, requestHash); err == nil {
			replayed = true
			return json.Unmarshal(stored.Response, &snap)
		} else if !errors.Is(err, repository.ErrNotFound) {
			return err
		}
		trip, err := s.Repo.GetTrip(tx, tripID)
		if err != nil {
			return err
		}
		if trip.Version != baseRevision {
			return repository.ErrConflict
		}
		currentRows, err := s.Repo.ListRecords(tx, tripID, resource)
		if err != nil {
			return err
		}
		beforeIDs := recordIDsForGroup(currentRows, group)
		if err := s.Repo.Reorder(
			tx, tripID, resource, group, ids, actorMemberID,
		); err != nil {
			return err
		}
		if err := s.Repo.InsertAuditLog(tx, model.AuditLog{
			TripID: tripID, MemberID: actorMemberID,
			Module: resourceModule(resource), Action: "调整顺序",
			Resource: string(resource), RecordLabel: "顺序",
			Before: map[string]any{"ids": beforeIDs},
			After:  map[string]any{"ids": ids},
		}); err != nil {
			return err
		}
		if _, err := s.Repo.BumpTripRevision(tx, tripID); err != nil {
			return err
		}
		snap, err = s.Repo.BuildSnapshot(tx, tripID, string(resource)+"-reordered")
		if err != nil {
			return err
		}
		encoded, err := json.Marshal(snap)
		if err != nil {
			return err
		}
		return s.Repo.StoreIdempotencyRequest(tx, scope, idempotencyKey, requestHash, encoded)
	})
	if err != nil {
		return model.Snapshot{}, false, err
	}
	if replayed {
		if err := refreshSnapshots(ctx, s.Repo, &snap); err != nil {
			return model.Snapshot{}, false, err
		}
	} else {
		s.mirror(ctx, tripID, &snap)
	}
	return snap, replayed, nil
}

func (s Service) prepareRecordData(
	ctx context.Context,
	row *model.Record,
	actorMemberID string,
	creating bool,
) error {
	if row.Data == nil {
		row.Data = map[string]any{}
	}
	if row.Resource == model.PackingItems {
		row.Data["ownerMemberId"] = actorMemberID
	}
	if row.Resource == model.Bookings {
		if blankService(row.Data["type"]) {
			return fmt.Errorf("预订类型不能为空")
		}
		if blankService(row.Data["bookerMemberId"]) {
			return fmt.Errorf("预订人不能为空")
		}
	}
	if row.Resource != model.Expenses {
		return nil
	}
	if creating && !blankService(row.Data["bookingRecordId"]) {
		return fmt.Errorf("新增费用不能手动关联预订")
	}
	if blankService(row.Data["occurredAt"]) {
		row.Data["occurredAt"] = time.Now().In(time.FixedZone("CST", 8*3600)).
			Format("2006-01-02T15:04")
	}
	mode := strings.TrimSpace(fmt.Sprint(row.Data["splitMode"]))
	if mode == "" {
		mode = "all"
		row.Data["splitMode"] = mode
	}
	if mode == "all" {
		memberIDs, err := s.Repo.SelectedMemberIDs(ctx, row.TripID)
		if err != nil {
			return err
		}
		if len(memberIDs) == 0 {
			return fmt.Errorf("当前旅行还没有成员，无法保存分摊快照")
		}
		row.Data["splitMemberIds"] = memberIDs
	}
	return nil
}

func (s Service) syncBookingExpense(
	ctx context.Context,
	booking model.Record,
	actorMemberID string,
) (model.Record, error) {
	occurredAt := bookingOccurredAt(booking.Data)
	current, err := s.Repo.GetExpenseByBooking(ctx, booking.TripID, booking.ID)
	if err == nil {
		data := cloneMap(current.Data)
		data["item"] = fmt.Sprint(booking.Data["item"])
		data["type"] = fmt.Sprint(booking.Data["type"])
		data["payerMemberId"] = fmt.Sprint(booking.Data["bookerMemberId"])
		if blankService(data["occurredAt"]) {
			data["occurredAt"] = occurredAt
		}
		updated, err := s.Repo.UpdateRecord(
			ctx, booking.TripID, model.Expenses, current.ID,
			current.Version, data, actorMemberID,
		)
		if err != nil {
			return model.Record{}, err
		}
		if err := s.auditRecord(
			ctx, updated, actorMemberID, "自动同步", current.Data, updated.Data,
		); err != nil {
			return model.Record{}, err
		}
		return updated, nil
	}
	if !errors.Is(err, repository.ErrNotFound) {
		return model.Record{}, err
	}
	memberIDs, err := s.Repo.SelectedMemberIDs(ctx, booking.TripID)
	if err != nil {
		return model.Record{}, err
	}
	if len(memberIDs) == 0 {
		return model.Record{}, fmt.Errorf("当前旅行还没有成员，无法创建预订费用")
	}
	expense := model.Record{
		TripID: booking.TripID, Resource: model.Expenses,
		RecordKey:         "booking-expense-" + booking.ID,
		CreatedByMemberID: actorMemberID, UpdatedByMemberID: actorMemberID,
		Data: map[string]any{
			"item":            fmt.Sprint(booking.Data["item"]),
			"occurredAt":      occurredAt,
			"type":            fmt.Sprint(booking.Data["type"]),
			"payerMemberId":   fmt.Sprint(booking.Data["bookerMemberId"]),
			"splitMode":       "all",
			"splitMemberIds":  memberIDs,
			"bookingRecordId": booking.ID,
		},
	}
	created, err := s.Repo.CreateRecord(ctx, expense)
	if err != nil {
		return model.Record{}, err
	}
	if err := s.auditRecord(
		ctx, created, actorMemberID, "自动创建", nil, created.Data,
	); err != nil {
		return model.Record{}, err
	}
	return created, nil
}

func bookingOccurredAt(data map[string]any) string {
	value := strings.TrimSpace(fmt.Sprint(data["dateTime"]))
	if len(value) >= 16 {
		normalized := strings.Replace(value[:16], " ", "T", 1)
		if _, err := time.Parse("2006-01-02T15:04", normalized); err == nil {
			return normalized
		}
	}
	if len(value) >= 10 {
		if _, err := time.Parse("2006-01-02", value[:10]); err == nil {
			return value[:10] + "T00:00"
		}
	}
	return time.Now().In(time.FixedZone("CST", 8*3600)).Format("2006-01-02T15:04")
}

func (s Service) auditRecord(
	ctx context.Context,
	row model.Record,
	memberID, action string,
	before, after map[string]any,
) error {
	return s.Repo.InsertAuditLog(ctx, model.AuditLog{
		TripID: row.TripID, MemberID: memberID,
		Module: resourceModule(row.Resource), Action: action,
		Resource: string(row.Resource), RecordID: row.ID,
		RecordLabel: recordAuditLabel(row), Before: cloneMap(before),
		After: cloneMap(after),
	})
}

func resourceModule(resource model.Resource) string {
	switch resource {
	case model.Days:
		return "日程"
	case model.Places:
		return "地点"
	case model.Segments:
		return "交通"
	case model.Bookings:
		return "预订"
	case model.Expenses:
		return "费用"
	case model.PackingItems:
		return "物品"
	case model.ResearchEvidence:
		return "资料"
	case model.Members:
		return "成员"
	default:
		return "旅行资料"
	}
}

func recordAuditLabel(row model.Record) string {
	for _, field := range []string{"title", "name", "item"} {
		if value := strings.TrimSpace(fmt.Sprint(row.Data[field])); value != "" && value != "<nil>" {
			return value
		}
	}
	return row.RecordKey
}

func tripAuditData(trip model.Trip) map[string]any {
	data := map[string]any{
		"title": trip.Title, "startDate": trip.StartDate, "endDate": trip.EndDate,
		"summary": trip.Summary,
	}
	if trip.DeletedAt != nil {
		data["deletedAt"] = trip.DeletedAt.UTC().Format(time.RFC3339)
		data["deletedByMemberId"] = trip.DeletedByMemberID
	}
	return data
}

func cloneMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	result := make(map[string]any, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func blankService(value any) bool {
	return value == nil || strings.TrimSpace(fmt.Sprint(value)) == ""
}

func expenseBookingID(data map[string]any) string {
	value := data["bookingRecordId"]
	if blankService(value) {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func recordIDsForGroup(rows []model.Record, group string) []string {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.GroupKey == group {
			ids = append(ids, row.ID)
		}
	}
	return ids
}
