// 本文件只持久化成员维度的登录凭据、会话和旅行权限。
package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"my-travel/server/internal/model"
)

func normalizedMobile(mobile string) (string, error) {
	return model.NormalizeMobile(mobile, true)
}

func (r *Repository) CredentialCount(ctx context.Context) (int64, error) {
	return r.collection("member_credentials").CountDocuments(ctx, bson.M{})
}

func (r *Repository) CreateAuthenticatedMember(
	ctx context.Context,
	displayName, mobile, passwordHash string,
	admin bool,
) (model.MemberIdentity, error) {
	mobile, err := normalizedMobile(mobile)
	if err != nil {
		return model.MemberIdentity{}, err
	}
	row, err := r.CreateLibraryRecord(ctx, model.LibraryRecord{
		Resource:  model.LibraryMembers,
		RecordKey: "member-" + mobile,
		Data: map[string]any{
			"displayName": strings.TrimSpace(displayName),
			"mobile":      mobile,
			"admin":       admin,
			"status":      "active",
		},
	})
	if err != nil {
		return model.MemberIdentity{}, err
	}
	if err := r.SetMemberCredential(ctx, row.ID, passwordHash); err != nil {
		return model.MemberIdentity{}, err
	}
	return memberIdentity(row), nil
}

func (r *Repository) SetMemberCredential(ctx context.Context, memberID, passwordHash string) error {
	if err := model.ValidateRecordID(memberID); err != nil {
		return err
	}
	if strings.TrimSpace(passwordHash) == "" {
		return fmt.Errorf("密码不能为空")
	}
	_, err := r.collection("member_credentials").UpdateOne(
		ctx,
		bson.M{"_id": memberID},
		bson.M{"$set": bson.M{"password_hash": passwordHash}},
		options.UpdateOne().SetUpsert(true),
	)
	return err
}

func (r *Repository) GetMemberIdentity(ctx context.Context, memberID string) (model.MemberIdentity, error) {
	row, err := r.GetLibraryRecord(ctx, model.LibraryMembers, memberID)
	if err != nil {
		return model.MemberIdentity{}, err
	}
	if memberAuthenticationDisabled(row) {
		return model.MemberIdentity{}, ErrNotFound
	}
	return memberIdentity(row), nil
}

func (r *Repository) GetMemberIdentityByMobile(ctx context.Context, mobile string) (model.MemberIdentity, model.MemberCredential, error) {
	mobile, err := normalizedMobile(mobile)
	if err != nil {
		return model.MemberIdentity{}, model.MemberCredential{}, err
	}
	var row model.LibraryRecord
	err = r.collection("library_records").FindOne(ctx, bson.M{
		"resource": model.LibraryMembers, "data.mobile": mobile,
	}).Decode(&row)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return model.MemberIdentity{}, model.MemberCredential{}, ErrNotFound
	}
	if err != nil {
		return model.MemberIdentity{}, model.MemberCredential{}, err
	}
	if memberAuthenticationDisabled(row) {
		return model.MemberIdentity{}, model.MemberCredential{}, ErrNotFound
	}
	var credential model.MemberCredential
	err = r.collection("member_credentials").FindOne(ctx, bson.M{"_id": row.ID}).Decode(&credential)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return model.MemberIdentity{}, model.MemberCredential{}, ErrNotFound
	}
	if err != nil {
		return model.MemberIdentity{}, model.MemberCredential{}, err
	}
	return memberIdentity(row), credential, nil
}

func memberAuthenticationDisabled(row model.LibraryRecord) bool {
	status, ok := row.Data["status"].(string)
	return ok && strings.TrimSpace(status) == "inactive"
}

func memberIdentity(row model.LibraryRecord) model.MemberIdentity {
	mobile, _ := row.Data["mobile"].(string)
	return model.MemberIdentity{
		ID:          row.ID,
		DisplayName: strings.TrimSpace(fmt.Sprint(row.Data["displayName"])),
		Mobile:      strings.TrimSpace(mobile),
		Admin:       libraryBool(row.Data, "admin"),
	}
}

func (r *Repository) GetMemberCredential(
	ctx context.Context,
	memberID string,
) (model.MemberCredential, error) {
	var credential model.MemberCredential
	err := r.collection("member_credentials").FindOne(
		ctx,
		bson.M{"_id": memberID},
	).Decode(&credential)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return model.MemberCredential{}, ErrNotFound
	}
	return credential, err
}

func libraryBool(data map[string]any, key string) bool {
	value, _ := data[key].(bool)
	return value
}

func (r *Repository) HasMemberCredential(ctx context.Context, memberID string) (bool, error) {
	err := r.collection("member_credentials").FindOne(ctx, bson.M{"_id": memberID}).Err()
	if errors.Is(err, mongo.ErrNoDocuments) {
		return false, nil
	}
	return err == nil, err
}

func (r *Repository) DeleteMemberAuthentication(ctx context.Context, memberID string) error {
	if err := model.ValidateRecordID(memberID); err != nil {
		return err
	}
	if _, err := r.collection("sessions").DeleteMany(
		ctx,
		bson.M{"member_id": memberID},
	); err != nil {
		return err
	}
	_, err := r.collection("member_credentials").DeleteOne(
		ctx,
		bson.M{"_id": memberID},
	)
	return err
}

func (r *Repository) HasOtherAuthenticatedAdmin(
	ctx context.Context,
	memberID string,
) (bool, error) {
	cursor, err := r.collection("library_records").Find(ctx, bson.M{
		"resource":   model.LibraryMembers,
		"_id":        bson.M{"$ne": memberID},
		"data.admin": true,
	})
	if err != nil {
		return false, err
	}
	defer cursor.Close(ctx)
	var rows []model.LibraryRecord
	if err := cursor.All(ctx, &rows); err != nil {
		return false, err
	}
	for _, row := range rows {
		if memberAuthenticationDisabled(row) {
			continue
		}
		hasCredential, err := r.HasMemberCredential(ctx, row.ID)
		if err != nil {
			return false, err
		}
		if hasCredential {
			return true, nil
		}
	}
	return false, nil
}

func (r *Repository) CreateSession(ctx context.Context, session model.Session) error {
	_, err := r.collection("sessions").InsertOne(ctx, session)
	return err
}

func (r *Repository) GetSession(ctx context.Context, id string) (model.Session, error) {
	var session model.Session
	err := r.collection("sessions").FindOne(ctx, bson.M{"_id": id, "expires_at": bson.M{"$gt": time.Now().UTC()}}).Decode(&session)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return model.Session{}, ErrNotFound
	}
	return session, err
}

func (r *Repository) DeleteSession(ctx context.Context, id string) error {
	_, err := r.collection("sessions").DeleteOne(ctx, bson.M{"_id": id})
	return err
}

func (r *Repository) DeleteSessionsForMember(ctx context.Context, memberID string) error {
	_, err := r.collection("sessions").DeleteMany(ctx, bson.M{"member_id": memberID})
	return err
}

func (r *Repository) ActiveOwnedTripCount(
	ctx context.Context,
	memberID string,
) (int64, error) {
	cursor, err := r.collection("memberships").Find(ctx, bson.M{
		"member_id": memberID,
		"role":      "owner",
	})
	if err != nil {
		return 0, err
	}
	defer cursor.Close(ctx)
	var memberships []model.Membership
	if err := cursor.All(ctx, &memberships); err != nil {
		return 0, err
	}
	if len(memberships) == 0 {
		return 0, nil
	}
	tripIDs := make([]string, 0, len(memberships))
	for _, membership := range memberships {
		tripIDs = append(tripIDs, membership.TripID)
	}
	return r.collection("trips").CountDocuments(ctx, bson.M{
		"_id":        bson.M{"$in": tripIDs},
		"deleted_at": bson.M{"$exists": false},
	})
}

func (r *Repository) OwnedTripCount(
	ctx context.Context,
	memberID string,
) (int64, error) {
	return r.collection("memberships").CountDocuments(ctx, bson.M{
		"member_id": memberID,
		"role":      "owner",
	})
}

func (r *Repository) SetMembership(ctx context.Context, tripID, memberID, role string) error {
	if role != "owner" && role != "editor" && role != "viewer" {
		return fmt.Errorf("invalid role")
	}
	if _, err := r.GetLibraryRecord(ctx, model.LibraryMembers, memberID); err != nil {
		return fmt.Errorf("成员不存在: %w", err)
	}
	_, err := r.collection("memberships").UpdateOne(
		ctx,
		bson.M{"trip_id": tripID, "member_id": memberID},
		bson.M{"$set": bson.M{"role": role}, "$setOnInsert": bson.M{"_id": newID(), "created_at": time.Now().UTC()}},
		options.UpdateOne().SetUpsert(true),
	)
	return err
}

func (r *Repository) DeleteMembership(ctx context.Context, tripID, memberID string) error {
	_, err := r.collection("memberships").DeleteOne(ctx, bson.M{
		"trip_id": tripID, "member_id": memberID, "role": bson.M{"$ne": "owner"},
	})
	return err
}

func (r *Repository) RoleFor(ctx context.Context, tripID, memberID string) (string, error) {
	var membership model.Membership
	err := r.collection("memberships").FindOne(ctx, bson.M{"trip_id": tripID, "member_id": memberID}).Decode(&membership)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return "", ErrNotFound
	}
	return membership.Role, err
}
