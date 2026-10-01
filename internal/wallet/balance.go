package wallet

import "math"

// Deposit returns the balance after a deposit of amount. Both values must be zero or more.
func Deposit(balance, amount int64) (int64, error) {
	if amount > math.MaxInt64-balance {
		return 0, ErrBalanceLimitExceeded
	}
	return balance + amount, nil
}

// Withdraw returns the balance after a withdrawal of amount. Both values must be zero or more.
func Withdraw(balance, amount int64) (int64, error) {
	if amount > balance {
		return 0, ErrInsufficientFunds
	}
	return balance - amount, nil
}
