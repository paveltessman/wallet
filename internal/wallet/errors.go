// Package wallet holds the domain errors of the service.
package wallet

import "errors"

var (
	ErrNotFound = errors.New("the wallet does not exist")

	ErrInsufficientFunds = errors.New("the balance is less than the amount")

	ErrBalanceLimitExceeded = errors.New("the balance goes above the limit")

	ErrOverloaded = errors.New("no database connection is free")

	// ErrUnavailable means that the database is not available and the operation did not apply.
	ErrUnavailable = errors.New("the database is not available")

	// ErrOutcomeUnknown means that the statement reached the database, but the result did not come back.
	ErrOutcomeUnknown = errors.New("the outcome of the operation is unknown")
)
