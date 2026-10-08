package store

import (
	"context"
	"errors"

	"usaf-pricing-service/domain"
)

var (
	ErrNotFound        = errors.New("vendor program not found")
	ErrInvalidID       = errors.New("vendor program id must be a 24-character hexadecimal MongoDB ObjectID")
	ErrConflict        = errors.New("vendor program changed during the update; reload and retry")
	ErrAmbiguousVendor = errors.New("multiple vendor programs found for vendor")
)

type VendorPrograms interface {
	Create(context.Context, *domain.VendorProgram) error
	Get(context.Context, string) (*domain.VendorProgram, error)
	GetByVendor(context.Context, string) (*domain.VendorProgram, error)
	List(ctx context.Context, limit, offset int64) ([]domain.VendorProgram, error)
	// Update commits the mutation only when it succeeds and the stored version
	// has not changed. The callback must not retain the supplied program.
	Update(context.Context, string, func(*domain.VendorProgram) error) (*domain.VendorProgram, error)
	Delete(context.Context, string) error
}
