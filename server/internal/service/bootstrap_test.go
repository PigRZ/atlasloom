package service

import (
	"context"
	"errors"
	"testing"

	"my-travel/server/internal/model"
)

type bootstrapTransactionKey struct{}

type bootstrapStoreStub struct {
	credentials         int64
	trips               []model.Trip
	failCredentialCount error
	failCreateMember    error
	failEnsurePresets   error
	failListTrips       error
	failMembershipAt    string
	pendingMember       model.MemberIdentity
	pendingOwners       []string
	pendingRoles        []string
	pendingPresets      []model.LibraryRecord
	committedMember     model.MemberIdentity
	committedOwners     []string
	committedRoles      []string
	committedPresets    []model.LibraryRecord
}

func (stub *bootstrapStoreStub) WithTransaction(ctx context.Context, fn func(context.Context) error) error {
	tx := context.WithValue(ctx, bootstrapTransactionKey{}, true)
	if err := fn(tx); err != nil {
		stub.pendingMember = model.MemberIdentity{}
		stub.pendingOwners = nil
		stub.pendingRoles = nil
		stub.pendingPresets = nil
		return err
	}
	stub.committedMember = stub.pendingMember
	stub.committedOwners = append([]string(nil), stub.pendingOwners...)
	stub.committedRoles = append([]string(nil), stub.pendingRoles...)
	stub.committedPresets = append([]model.LibraryRecord(nil), stub.pendingPresets...)
	return nil
}

func (stub *bootstrapStoreStub) CredentialCount(ctx context.Context) (int64, error) {
	stub.requireTransaction(ctx)
	return stub.credentials, stub.failCredentialCount
}

func (stub *bootstrapStoreStub) CreateAuthenticatedMember(
	ctx context.Context,
	displayName, mobile, passwordHash string,
	admin bool,
) (model.MemberIdentity, error) {
	stub.requireTransaction(ctx)
	if stub.failCreateMember != nil {
		return model.MemberIdentity{}, stub.failCreateMember
	}
	stub.pendingMember = model.MemberIdentity{
		ID: "member-1", DisplayName: displayName, Mobile: mobile, Admin: admin,
	}
	return stub.pendingMember, nil
}

func (stub *bootstrapStoreStub) EnsureDefaultTagPresets(
	ctx context.Context,
) ([]model.LibraryRecord, error) {
	stub.requireTransaction(ctx)
	if stub.failEnsurePresets != nil {
		return nil, stub.failEnsurePresets
	}
	for _, preset := range model.DefaultTagPresets() {
		stub.pendingPresets = append(stub.pendingPresets, model.LibraryRecord{
			RecordKey: preset.RecordKey,
			Data: map[string]any{
				"name": preset.Name, "category": preset.Category,
			},
		})
	}
	return stub.pendingPresets, nil
}

func (stub *bootstrapStoreStub) ListTrips(ctx context.Context) ([]model.Trip, error) {
	stub.requireTransaction(ctx)
	return stub.trips, stub.failListTrips
}

func (stub *bootstrapStoreStub) SetMembership(ctx context.Context, tripID, memberID, role string) error {
	stub.requireTransaction(ctx)
	if memberID != "member-1" || (role != "owner" && role != "editor") {
		return errors.New("unexpected membership")
	}
	if tripID == stub.failMembershipAt {
		return errors.New("membership failed")
	}
	stub.pendingOwners = append(stub.pendingOwners, tripID)
	stub.pendingRoles = append(stub.pendingRoles, role)
	return nil
}

func (stub *bootstrapStoreStub) requireTransaction(ctx context.Context) {
	if ctx.Value(bootstrapTransactionKey{}) != true {
		panic("bootstrap operation ran outside its transaction")
	}
}

func TestBootstrapCreatesUserAndExistingTripOwnershipsAtomically(t *testing.T) {
	store := &bootstrapStoreStub{
		trips: []model.Trip{{ID: "trip-1"}, {ID: "trip-2"}},
	}
	user, err := bootstrap(
		context.Background(),
		store,
		"旅行者",
		"13800138000",
		"hash",
	)
	if err != nil {
		t.Fatal(err)
	}
	if user.ID != "member-1" || store.committedMember.ID != user.ID {
		t.Fatalf("bootstrap member was not committed: %#v", store.committedMember)
	}
	if !user.Admin {
		t.Fatal("bootstrap user must be the library administrator")
	}
	if len(store.committedPresets) != len(model.DefaultTagPresets()) {
		t.Fatalf("default tag presets were not committed: %#v", store.committedPresets)
	}
	if len(store.committedOwners) != 2 ||
		store.committedOwners[0] != "trip-1" ||
		store.committedOwners[1] != "trip-2" {
		t.Fatalf("unexpected owner memberships: %#v", store.committedOwners)
	}
}

func TestBootstrapRollsBackUserWhenPlatformPresetsFail(t *testing.T) {
	store := &bootstrapStoreStub{
		failEnsurePresets: errors.New("platform presets failed"),
	}
	if _, err := bootstrap(
		context.Background(),
		store,
		"旅行者",
		"13800138000",
		"hash",
	); err == nil {
		t.Fatal("platform preset failure was ignored")
	}
	if store.committedMember.ID != "" || len(store.committedPresets) != 0 {
		t.Fatalf(
			"partial bootstrap was committed: member=%#v presets=%#v",
			store.committedMember,
			store.committedPresets,
		)
	}
}

func TestBootstrapRollsBackUserWhenExistingTripOwnershipFails(t *testing.T) {
	store := &bootstrapStoreStub{
		trips:            []model.Trip{{ID: "trip-1"}, {ID: "trip-2"}},
		failMembershipAt: "trip-2",
	}
	if _, err := bootstrap(
		context.Background(),
		store,
		"旅行者",
		"13800138000",
		"hash",
	); err == nil {
		t.Fatal("membership failure was ignored")
	}
	if store.committedMember.ID != "" || len(store.committedOwners) != 0 {
		t.Fatalf("partial bootstrap was committed: member=%#v owners=%#v", store.committedMember, store.committedOwners)
	}
}

func TestBootstrapPropagatesTripListingFailure(t *testing.T) {
	store := &bootstrapStoreStub{failListTrips: errors.New("list trips failed")}
	if _, err := bootstrap(
		context.Background(),
		store,
		"旅行者",
		"13800138000",
		"hash",
	); err == nil {
		t.Fatal("trip listing failure was ignored")
	}
	if store.committedMember.ID != "" {
		t.Fatalf("member remained committed after listing failed: %#v", store.committedMember)
	}
}

func TestProvisionUserDoesNotJoinEveryTrip(t *testing.T) {
	store := &bootstrapStoreStub{trips: []model.Trip{{ID: "trip-1"}, {ID: "trip-2"}}}
	user, err := provisionUser(
		context.Background(),
		store,
		"同行成员",
		"13900139000",
		"hash",
	)
	if err != nil {
		t.Fatal(err)
	}
	if user.Mobile != "13900139000" || len(store.committedOwners) != 0 {
		t.Fatalf("unexpected provision result: user=%#v memberships=%#v", user, store.committedOwners)
	}
	if user.Admin {
		t.Fatal("provisioned users must not become administrators")
	}
}
