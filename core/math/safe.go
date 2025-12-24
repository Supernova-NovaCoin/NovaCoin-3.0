package math

import (
	"errors"
	"math"
)

var (
	ErrOverflow  = errors.New("arithmetic overflow")
	ErrUnderflow = errors.New("arithmetic underflow")
	ErrDivZero   = errors.New("division by zero")
)

// SafeAdd returns a + b, or an error if the result overflows uint64.
func SafeAdd(a, b uint64) (uint64, error) {
	if a > math.MaxUint64-b {
		return 0, ErrOverflow
	}
	return a + b, nil
}

// SafeSub returns a - b, or an error if b > a (underflow).
func SafeSub(a, b uint64) (uint64, error) {
	if b > a {
		return 0, ErrUnderflow
	}
	return a - b, nil
}

// SafeMul returns a * b, or an error if the result overflows uint64.
func SafeMul(a, b uint64) (uint64, error) {
	if a == 0 || b == 0 {
		return 0, nil
	}
	result := a * b
	if result/a != b {
		return 0, ErrOverflow
	}
	return result, nil
}

// SafeDiv returns a / b, or an error if b is zero.
func SafeDiv(a, b uint64) (uint64, error) {
	if b == 0 {
		return 0, ErrDivZero
	}
	return a / b, nil
}

// MustAdd panics on overflow - use only when overflow is impossible.
func MustAdd(a, b uint64) uint64 {
	result, err := SafeAdd(a, b)
	if err != nil {
		panic(err)
	}
	return result
}

// MustSub panics on underflow - use only when underflow is impossible.
func MustSub(a, b uint64) uint64 {
	result, err := SafeSub(a, b)
	if err != nil {
		panic(err)
	}
	return result
}

// CheckedAdd returns result and boolean indicating success.
func CheckedAdd(a, b uint64) (uint64, bool) {
	if a > math.MaxUint64-b {
		return 0, false
	}
	return a + b, true
}

// CheckedSub returns result and boolean indicating success.
func CheckedSub(a, b uint64) (uint64, bool) {
	if b > a {
		return 0, false
	}
	return a - b, true
}

// CheckedMul returns result and boolean indicating success.
func CheckedMul(a, b uint64) (uint64, bool) {
	if a == 0 || b == 0 {
		return 0, true
	}
	result := a * b
	if result/a != b {
		return 0, false
	}
	return result, true
}
