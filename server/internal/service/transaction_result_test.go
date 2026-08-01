package service

import (
	"context"
	"errors"
	"strconv"
	"testing"
)

type retryingTransactionRunner struct {
	attempts int
}

func (runner *retryingTransactionRunner) WithTransaction(
	ctx context.Context,
	callback func(context.Context) error,
) error {
	for range 2 {
		runner.attempts++
		if err := callback(ctx); err != nil {
			return err
		}
	}
	return nil
}

func TestWithTransactionResultPublishesOnlyFinalAttempt(t *testing.T) {
	runner := &retryingTransactionRunner{}
	result, err := withTransactionResult(
		context.Background(),
		runner,
		func(context.Context) ([]string, error) {
			return []string{"attempt", strconv.Itoa(runner.attempts)}, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if runner.attempts != 2 ||
		len(result) != 2 ||
		result[0] != "attempt" ||
		result[1] != "2" {
		t.Fatalf("attempts=%d result=%#v", runner.attempts, result)
	}
}

func TestWithTransactionResultDoesNotPublishAbortedAttempt(t *testing.T) {
	runner := &retryingTransactionRunner{}
	expected := errors.New("second attempt failed")
	result, err := withTransactionResult(
		context.Background(),
		runner,
		func(context.Context) ([]string, error) {
			if runner.attempts == 2 {
				return []string{"aborted-second"}, expected
			}
			return []string{"aborted-first"}, nil
		},
	)
	if !errors.Is(err, expected) {
		t.Fatalf("error=%v, want %v", err, expected)
	}
	if result != nil {
		t.Fatalf("failed transaction published result %#v", result)
	}
}
