package service

import "context"

type transactionRunner interface {
	WithTransaction(context.Context, func(context.Context) error) error
}

// withTransactionResult keeps every callback attempt isolated. MongoDB may
// invoke the callback more than once after a transient transaction failure, so
// only the result from the attempt that ultimately commits may escape.
func withTransactionResult[T any](
	ctx context.Context,
	runner transactionRunner,
	attempt func(context.Context) (T, error),
) (T, error) {
	var committed T
	err := runner.WithTransaction(ctx, func(tx context.Context) error {
		current, err := attempt(tx)
		if err != nil {
			return err
		}
		committed = current
		return nil
	})
	if err != nil {
		var zero T
		return zero, err
	}
	return committed, nil
}
