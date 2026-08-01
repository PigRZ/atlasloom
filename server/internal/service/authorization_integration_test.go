package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"my-travel/server/internal/app"
	"my-travel/server/internal/model"
	"my-travel/server/internal/repository"
)

func TestMongoIntegrationServiceRejectsStaleActorAuthorization(t *testing.T) {
	t.Run("inactive editor cannot create a record", func(t *testing.T) {
		ctx, travelService, repo := authorizationIntegrationService(t)
		owner := createAuthorizationMember(t, ctx, repo, "sequence-owner", false)
		editor := createAuthorizationMember(t, ctx, repo, "sequence-editor", false)
		trip := createAuthorizationTrip(t, ctx, repo, "sequence-inactive-editor", owner.ID)
		if err := repo.SetMembership(ctx, trip.ID, editor.ID, "editor"); err != nil {
			t.Fatal(err)
		}
		editorData := copyAuthorizationData(editor.Data)
		editorData["status"] = "inactive"
		if _, err := repo.UpdateLibraryRecord(
			ctx,
			model.LibraryMembers,
			editor.ID,
			editor.Version,
			editorData,
		); err != nil {
			t.Fatal(err)
		}

		_, _, _, err := travelService.CreateRecord(
			ctx,
			authorizationDay(trip.ID, "sequence-inactive-day", editor.ID),
			editor.ID,
			"sequence-inactive-editor-create",
			strings.Repeat("a", 64),
		)
		if !errors.Is(err, repository.ErrForbidden) {
			t.Fatalf("inactive editor write error=%v, want ErrForbidden", err)
		}
		assertNoAuthorizationRecord(t, ctx, repo, trip.ID, "sequence-inactive-day")
	})

	t.Run("demoted admin cannot mutate the library or delete a trip", func(t *testing.T) {
		ctx, travelService, repo := authorizationIntegrationService(t)
		owner := createAuthorizationMember(t, ctx, repo, "sequence-delete-owner", false)
		admin := createAuthorizationMember(t, ctx, repo, "sequence-demoted-admin", true)
		trip := createAuthorizationTrip(t, ctx, repo, "sequence-demoted-admin", owner.ID)
		adminData := copyAuthorizationData(admin.Data)
		adminData["admin"] = false
		if _, err := repo.UpdateLibraryRecord(
			ctx,
			model.LibraryMembers,
			admin.ID,
			admin.Version,
			adminData,
		); err != nil {
			t.Fatal(err)
		}

		_, _, _, err := travelService.CreateLibraryRecord(
			ctx,
			authorizationTag("sequence-forbidden-tag"),
			"",
			admin.ID,
			"sequence-demoted-library-create",
			strings.Repeat("b", 64),
		)
		if !errors.Is(err, repository.ErrForbidden) {
			t.Fatalf("demoted admin library write error=%v, want ErrForbidden", err)
		}
		if _, err := travelService.DeleteTrip(
			ctx,
			trip.ID,
			trip.Version,
			admin.ID,
		); !errors.Is(err, repository.ErrForbidden) {
			t.Fatalf("demoted admin trip delete error=%v, want ErrForbidden", err)
		}
		assertNoAuthorizationTag(t, ctx, repo, "sequence-forbidden-tag")
		current, err := repo.GetTrip(ctx, trip.ID)
		if err != nil {
			t.Fatal(err)
		}
		if current.Version != trip.Version || current.DeletedAt != nil {
			t.Fatalf("rejected stale admin delete changed trip: %#v", current)
		}
	})

	t.Run("viewer cannot inspect destructive member impact", func(t *testing.T) {
		ctx, travelService, repo := authorizationIntegrationService(t)
		owner := createAuthorizationMember(t, ctx, repo, "impact-owner", false)
		viewer := createAuthorizationMember(t, ctx, repo, "impact-viewer", false)
		trip := createAuthorizationTrip(t, ctx, repo, "impact-readonly", owner.ID)
		if err := repo.SetMembership(ctx, trip.ID, viewer.ID, "viewer"); err != nil {
			t.Fatal(err)
		}
		selected, err := repo.CreateRecord(ctx, model.Record{
			TripID: trip.ID, Resource: model.Members,
			RecordKey: "impact-selected-owner",
			Data: map[string]any{
				"memberId": owner.ID,
				"role":     "owner",
			},
			CreatedByMemberID: owner.ID,
			UpdatedByMemberID: owner.ID,
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := travelService.GetTripMemberDeletionImpact(
			ctx,
			trip.ID,
			selected.ID,
			viewer.ID,
		); !errors.Is(err, repository.ErrForbidden) {
			t.Fatalf("viewer deletion impact error=%v, want ErrForbidden", err)
		}
		if _, err := travelService.GetTripMemberDeletionImpact(
			ctx,
			trip.ID,
			selected.ID,
			owner.ID,
		); err != nil {
			t.Fatalf("owner deletion impact error=%v", err)
		}
	})
}

func TestMongoIntegrationAuthorizationIsRecheckedAfterTransactionRetry(t *testing.T) {
	t.Run("admin demotion rejects the retried library write", func(t *testing.T) {
		ctx, travelService, repo := authorizationIntegrationService(t)
		admin := createAuthorizationMember(t, ctx, repo, "retry-admin", true)
		beforeState, err := repo.GetLibraryState(ctx)
		if err != nil {
			t.Fatal(err)
		}

		firstAttemptReady := make(chan struct{})
		continueFirstAttempt := make(chan struct{})
		transactionDone := make(chan error, 1)
		var attempts atomic.Int32
		go func() {
			transactionDone <- repo.WithTransaction(ctx, func(tx context.Context) error {
				attempt := attempts.Add(1)
				if err := travelService.requireLibraryAdmin(tx, admin.ID); err != nil {
					return err
				}
				if _, err := repo.CreateLibraryRecord(
					tx,
					authorizationTag("retry-forbidden-tag"),
				); err != nil {
					return err
				}
				if attempt == 1 {
					close(firstAttemptReady)
					select {
					case <-continueFirstAttempt:
					case <-tx.Done():
						return tx.Err()
					}
				}
				if _, err := repo.BumpLibraryRevision(tx); err != nil {
					return err
				}
				_, err := repo.BuildLibrarySnapshot(tx, "retry-forbidden-library-write")
				return err
			})
		}()

		waitForAuthorizationAttempt(t, firstAttemptReady)
		adminData := copyAuthorizationData(admin.Data)
		adminData["admin"] = false
		demotionErr := repo.WithTransaction(ctx, func(tx context.Context) error {
			if _, err := repo.UpdateLibraryRecord(
				tx,
				model.LibraryMembers,
				admin.ID,
				admin.Version,
				adminData,
			); err != nil {
				return err
			}
			if _, err := repo.BumpLibraryRevision(tx); err != nil {
				return err
			}
			_, err := repo.BuildLibrarySnapshot(tx, "admin-demoted")
			return err
		})
		close(continueFirstAttempt)
		if demotionErr != nil {
			t.Fatal(demotionErr)
		}

		transactionErr := waitForAuthorizationResult(t, transactionDone)
		if !errors.Is(transactionErr, repository.ErrForbidden) {
			t.Fatalf("retried demoted-admin write error=%v, want ErrForbidden", transactionErr)
		}
		if attempts.Load() < 2 {
			t.Fatalf("library transaction was not retried: attempts=%d", attempts.Load())
		}
		assertNoAuthorizationTag(t, ctx, repo, "retry-forbidden-tag")
		afterState, err := repo.GetLibraryState(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if afterState.Version != beforeState.Version+1 {
			t.Fatalf(
				"aborted admin attempt changed library revision: before=%d after=%d",
				beforeState.Version,
				afterState.Version,
			)
		}
	})

	t.Run("membership revocation rejects the retried trip write", func(t *testing.T) {
		ctx, travelService, repo := authorizationIntegrationService(t)
		owner := createAuthorizationMember(t, ctx, repo, "retry-owner", false)
		editor := createAuthorizationMember(t, ctx, repo, "retry-editor", false)
		trip := createAuthorizationTrip(t, ctx, repo, "retry-membership", owner.ID)
		if err := repo.SetMembership(ctx, trip.ID, editor.ID, "editor"); err != nil {
			t.Fatal(err)
		}

		firstAttemptReady := make(chan struct{})
		continueFirstAttempt := make(chan struct{})
		transactionDone := make(chan error, 1)
		var attempts atomic.Int32
		go func() {
			transactionDone <- repo.WithTransaction(ctx, func(tx context.Context) error {
				attempt := attempts.Add(1)
				if err := travelService.requireTripEditor(tx, trip.ID, editor.ID); err != nil {
					return err
				}
				if _, err := repo.CreateRecord(
					tx,
					authorizationDay(trip.ID, "retry-membership-day", editor.ID),
				); err != nil {
					return err
				}
				if attempt == 1 {
					close(firstAttemptReady)
					select {
					case <-continueFirstAttempt:
					case <-tx.Done():
						return tx.Err()
					}
				}
				if _, err := repo.BumpTripRevision(tx, trip.ID); err != nil {
					return err
				}
				_, err := repo.BuildSnapshot(tx, trip.ID, "retry-membership-write")
				return err
			})
		}()

		waitForAuthorizationAttempt(t, firstAttemptReady)
		revocationErr := repo.WithTransaction(ctx, func(tx context.Context) error {
			if err := repo.DeleteMembership(tx, trip.ID, editor.ID); err != nil {
				return err
			}
			if _, err := repo.BumpTripRevision(tx, trip.ID); err != nil {
				return err
			}
			_, err := repo.BuildSnapshot(tx, trip.ID, "editor-revoked")
			return err
		})
		close(continueFirstAttempt)
		if revocationErr != nil {
			t.Fatal(revocationErr)
		}

		transactionErr := waitForAuthorizationResult(t, transactionDone)
		if !errors.Is(transactionErr, repository.ErrForbidden) {
			t.Fatalf("retried revoked-editor write error=%v, want ErrForbidden", transactionErr)
		}
		if attempts.Load() < 2 {
			t.Fatalf("Trip transaction was not retried: attempts=%d", attempts.Load())
		}
		assertNoAuthorizationRecord(t, ctx, repo, trip.ID, "retry-membership-day")
		current, err := repo.GetTrip(ctx, trip.ID)
		if err != nil {
			t.Fatal(err)
		}
		if current.Version != trip.Version+1 {
			t.Fatalf(
				"aborted editor attempt changed Trip revision: before=%d after=%d",
				trip.Version,
				current.Version,
			)
		}
	})

	t.Run("actor deactivation rejects the epoch-retried trip write", func(t *testing.T) {
		ctx, travelService, repo := authorizationIntegrationService(t)
		owner := createAuthorizationMember(t, ctx, repo, "retry-active-owner", false)
		editor := createAuthorizationMember(t, ctx, repo, "retry-active-editor", false)
		trip := createAuthorizationTrip(t, ctx, repo, "retry-inactive-editor", owner.ID)
		if err := repo.SetMembership(ctx, trip.ID, editor.ID, "editor"); err != nil {
			t.Fatal(err)
		}
		beforeState, err := repo.GetLibraryState(ctx)
		if err != nil {
			t.Fatal(err)
		}

		firstAttemptReady := make(chan struct{})
		continueFirstAttempt := make(chan struct{})
		transactionDone := make(chan error, 1)
		var attempts atomic.Int32
		go func() {
			transactionDone <- repo.WithTransaction(ctx, func(tx context.Context) error {
				attempt := attempts.Add(1)
				if err := travelService.requireTripEditor(tx, trip.ID, editor.ID); err != nil {
					return err
				}
				if _, err := repo.CreateRecord(
					tx,
					authorizationDay(trip.ID, "retry-inactive-day", editor.ID),
				); err != nil {
					return err
				}
				if _, err := repo.BumpTripRevision(tx, trip.ID); err != nil {
					return err
				}
				if attempt == 1 {
					close(firstAttemptReady)
					select {
					case <-continueFirstAttempt:
					case <-tx.Done():
						return tx.Err()
					}
				}
				_, err := repo.BuildSnapshot(tx, trip.ID, "retry-inactive-write")
				return err
			})
		}()

		waitForAuthorizationAttempt(t, firstAttemptReady)
		editorData := copyAuthorizationData(editor.Data)
		editorData["status"] = "inactive"
		deactivationErr := repo.WithTransaction(ctx, func(tx context.Context) error {
			if _, err := repo.UpdateLibraryRecord(
				tx,
				model.LibraryMembers,
				editor.ID,
				editor.Version,
				editorData,
			); err != nil {
				return err
			}
			if _, err := repo.BumpLibraryRevision(tx); err != nil {
				return err
			}
			_, err := repo.BuildLibrarySnapshot(tx, "editor-deactivated")
			return err
		})
		close(continueFirstAttempt)
		if deactivationErr != nil {
			t.Fatal(deactivationErr)
		}

		transactionErr := waitForAuthorizationResult(t, transactionDone)
		if !errors.Is(transactionErr, repository.ErrForbidden) {
			t.Fatalf("retried inactive-editor write error=%v, want ErrForbidden", transactionErr)
		}
		if attempts.Load() < 2 {
			t.Fatalf("epoch-coordinated Trip transaction was not retried: attempts=%d", attempts.Load())
		}
		assertNoAuthorizationRecord(t, ctx, repo, trip.ID, "retry-inactive-day")
		current, err := repo.GetTrip(ctx, trip.ID)
		if err != nil {
			t.Fatal(err)
		}
		if current.Version != trip.Version {
			t.Fatalf(
				"aborted inactive-editor attempt changed Trip revision: before=%d after=%d",
				trip.Version,
				current.Version,
			)
		}
		snapshots, err := repo.ListSnapshots(ctx, trip.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(snapshots) != 0 {
			t.Fatalf("aborted inactive-editor attempt left snapshots behind: %#v", snapshots)
		}
		afterState, err := repo.GetLibraryState(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if afterState.Version != beforeState.Version+1 {
			t.Fatalf(
				"aborted inactive-editor attempt changed library revision: before=%d after=%d",
				beforeState.Version,
				afterState.Version,
			)
		}
	})
}

func authorizationIntegrationService(
	t *testing.T,
) (context.Context, Service, *repository.Repository) {
	t.Helper()
	uri := strings.TrimSpace(os.Getenv("MONGODB_TEST_URI"))
	if uri == "" {
		t.Skip("MONGODB_TEST_URI 未设置，跳过事务授权集成测试")
	}
	database := fmt.Sprintf("my_travel_authorization_%d", time.Now().UnixNano())
	config := app.Config{
		MongoURI:      uri,
		MongoDatabase: database,
		SnapshotDir:   t.TempDir(),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	repo, err := repository.Connect(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Initialize(ctx); err != nil {
		_ = repo.Close(context.Background())
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		client, err := mongo.Connect(options.Client().ApplyURI(uri))
		if err == nil {
			_ = client.Database(database).Drop(cleanupCtx)
			_ = client.Disconnect(cleanupCtx)
		}
		_ = repo.Close(cleanupCtx)
	})
	return ctx, Service{Repo: repo, Config: config}, repo
}

func createAuthorizationMember(
	t *testing.T,
	ctx context.Context,
	repo *repository.Repository,
	recordKey string,
	admin bool,
) model.LibraryRecord {
	t.Helper()
	row, err := repo.CreateLibraryRecord(ctx, model.LibraryRecord{
		Resource:  model.LibraryMembers,
		RecordKey: recordKey,
		Data: map[string]any{
			"displayName": recordKey,
			"admin":       admin,
			"status":      "active",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func createAuthorizationTrip(
	t *testing.T,
	ctx context.Context,
	repo *repository.Repository,
	id, ownerMemberID string,
) model.Trip {
	t.Helper()
	trip, err := repo.CreateTrip(ctx, model.Trip{
		ID:        id,
		Title:     id,
		StartDate: "2026-09-01",
		EndDate:   "2026-09-02",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SetMembership(ctx, trip.ID, ownerMemberID, "owner"); err != nil {
		t.Fatal(err)
	}
	return trip
}

func authorizationDay(tripID, recordKey, actorMemberID string) model.Record {
	return model.Record{
		TripID:            tripID,
		Resource:          model.Days,
		RecordKey:         recordKey,
		CreatedByMemberID: actorMemberID,
		UpdatedByMemberID: actorMemberID,
		Data: map[string]any{
			"title":        recordKey,
			"date":         "2026-09-01",
			"placeEntries": []any{},
		},
	}
}

func authorizationTag(recordKey string) model.LibraryRecord {
	return model.LibraryRecord{
		Resource:  model.LibraryTags,
		RecordKey: recordKey,
		Data: map[string]any{
			"name":     recordKey,
			"category": model.TagCategoryGeneral,
			"status":   "active",
		},
	}
}

func copyAuthorizationData(data map[string]any) map[string]any {
	result := make(map[string]any, len(data))
	for key, value := range data {
		result[key] = value
	}
	return result
}

func assertNoAuthorizationRecord(
	t *testing.T,
	ctx context.Context,
	repo *repository.Repository,
	tripID, recordKey string,
) {
	t.Helper()
	rows, err := repo.ListRecords(ctx, tripID, model.Days)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.RecordKey == recordKey {
			t.Fatalf("rejected authorization write left record behind: %#v", row)
		}
	}
}

func assertNoAuthorizationTag(
	t *testing.T,
	ctx context.Context,
	repo *repository.Repository,
	recordKey string,
) {
	t.Helper()
	rows, err := repo.ListLibraryRecords(ctx, model.LibraryTags)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.RecordKey == recordKey {
			t.Fatalf("rejected authorization write left tag behind: %#v", row)
		}
	}
}

func waitForAuthorizationAttempt(t *testing.T, ready <-chan struct{}) {
	t.Helper()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("transaction did not reach its first authorized attempt")
	}
}

func waitForAuthorizationResult(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("transaction did not finish after the concurrent authorization change")
		return nil
	}
}
