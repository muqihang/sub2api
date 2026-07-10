package ent

import "context"

// WithoutTx returns a context that does not carry the parent transaction.
func WithoutTx(ctx context.Context) context.Context {
	if TxFromContext(ctx) == nil {
		return ctx
	}
	return context.WithValue(ctx, txCtxKey{}, (*Tx)(nil))
}
