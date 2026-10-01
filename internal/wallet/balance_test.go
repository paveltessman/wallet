package wallet_test

import (
	"errors"
	"math"
	"testing"

	"wallet/internal/wallet"
)

func TestDeposit(t *testing.T) {
	tests := []struct {
		name    string
		balance int64
		amount  int64
		want    int64
		err     error
	}{
		{"adds the amount", 100, 50, 150, nil},
		{"a zero amount changes nothing", 100, 0, 100, nil},
		{"reaches the maximum", math.MaxInt64 - 10, 10, math.MaxInt64, nil},
		{"an overflow gives ErrBalanceLimitExceeded", math.MaxInt64 - 10, 11, 0, wallet.ErrBalanceLimitExceeded},
		{"the maximum amount on a zero balance", 0, math.MaxInt64, math.MaxInt64, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := wallet.Deposit(tt.balance, tt.amount)
			if got != tt.want || !errors.Is(err, tt.err) {
				t.Errorf("Deposit(%d, %d) = %d, %v, want %d, %v", tt.balance, tt.amount, got, err, tt.want, tt.err)
			}
		})
	}
}

func TestWithdraw(t *testing.T) {
	tests := []struct {
		name    string
		balance int64
		amount  int64
		want    int64
		err     error
	}{
		{"subtracts the amount", 100, 30, 70, nil},
		{"a zero amount changes nothing", 100, 0, 100, nil},
		{"the full balance leaves zero", 100, 100, 0, nil},
		{"an overdraw gives ErrInsufficientFunds", 100, 101, 0, wallet.ErrInsufficientFunds},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := wallet.Withdraw(tt.balance, tt.amount)
			if got != tt.want || !errors.Is(err, tt.err) {
				t.Errorf("Withdraw(%d, %d) = %d, %v, want %d, %v", tt.balance, tt.amount, got, err, tt.want, tt.err)
			}
		})
	}
}
