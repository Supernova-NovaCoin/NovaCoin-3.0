package math

import (
	"math"
	"testing"
)

func TestSafeAdd(t *testing.T) {
	tests := []struct {
		a, b     uint64
		expected uint64
		wantErr  bool
	}{
		{1, 2, 3, false},
		{0, 0, 0, false},
		{math.MaxUint64, 0, math.MaxUint64, false},
		{math.MaxUint64, 1, 0, true}, // overflow
		{math.MaxUint64 - 1, 2, 0, true},
		{1000000, 2000000, 3000000, false},
	}

	for _, tt := range tests {
		result, err := SafeAdd(tt.a, tt.b)
		if tt.wantErr {
			if err == nil {
				t.Errorf("SafeAdd(%d, %d) expected error, got nil", tt.a, tt.b)
			}
		} else {
			if err != nil {
				t.Errorf("SafeAdd(%d, %d) unexpected error: %v", tt.a, tt.b, err)
			}
			if result != tt.expected {
				t.Errorf("SafeAdd(%d, %d) = %d, want %d", tt.a, tt.b, result, tt.expected)
			}
		}
	}
}

func TestSafeSub(t *testing.T) {
	tests := []struct {
		a, b     uint64
		expected uint64
		wantErr  bool
	}{
		{5, 3, 2, false},
		{0, 0, 0, false},
		{100, 100, 0, false},
		{3, 5, 0, true}, // underflow
		{0, 1, 0, true},
		{math.MaxUint64, math.MaxUint64, 0, false},
	}

	for _, tt := range tests {
		result, err := SafeSub(tt.a, tt.b)
		if tt.wantErr {
			if err == nil {
				t.Errorf("SafeSub(%d, %d) expected error, got nil", tt.a, tt.b)
			}
		} else {
			if err != nil {
				t.Errorf("SafeSub(%d, %d) unexpected error: %v", tt.a, tt.b, err)
			}
			if result != tt.expected {
				t.Errorf("SafeSub(%d, %d) = %d, want %d", tt.a, tt.b, result, tt.expected)
			}
		}
	}
}

func TestSafeMul(t *testing.T) {
	tests := []struct {
		a, b     uint64
		expected uint64
		wantErr  bool
	}{
		{2, 3, 6, false},
		{0, 100, 0, false},
		{100, 0, 0, false},
		{1, math.MaxUint64, math.MaxUint64, false},
		{2, math.MaxUint64, 0, true}, // overflow
		{math.MaxUint64/2 + 1, 2, 0, true},
		{1000000, 1000000, 1000000000000, false},
	}

	for _, tt := range tests {
		result, err := SafeMul(tt.a, tt.b)
		if tt.wantErr {
			if err == nil {
				t.Errorf("SafeMul(%d, %d) expected error, got nil", tt.a, tt.b)
			}
		} else {
			if err != nil {
				t.Errorf("SafeMul(%d, %d) unexpected error: %v", tt.a, tt.b, err)
			}
			if result != tt.expected {
				t.Errorf("SafeMul(%d, %d) = %d, want %d", tt.a, tt.b, result, tt.expected)
			}
		}
	}
}

func TestSafeDiv(t *testing.T) {
	tests := []struct {
		a, b     uint64
		expected uint64
		wantErr  bool
	}{
		{6, 2, 3, false},
		{0, 1, 0, false},
		{100, 3, 33, false},
		{10, 0, 0, true}, // division by zero
		{math.MaxUint64, 1, math.MaxUint64, false},
	}

	for _, tt := range tests {
		result, err := SafeDiv(tt.a, tt.b)
		if tt.wantErr {
			if err == nil {
				t.Errorf("SafeDiv(%d, %d) expected error, got nil", tt.a, tt.b)
			}
		} else {
			if err != nil {
				t.Errorf("SafeDiv(%d, %d) unexpected error: %v", tt.a, tt.b, err)
			}
			if result != tt.expected {
				t.Errorf("SafeDiv(%d, %d) = %d, want %d", tt.a, tt.b, result, tt.expected)
			}
		}
	}
}

func TestCheckedFunctions(t *testing.T) {
	// Test CheckedAdd
	result, ok := CheckedAdd(math.MaxUint64, 1)
	if ok {
		t.Error("CheckedAdd overflow should return false")
	}

	result, ok = CheckedAdd(1, 2)
	if !ok || result != 3 {
		t.Errorf("CheckedAdd(1, 2) = %d, %v; want 3, true", result, ok)
	}

	// Test CheckedSub
	result, ok = CheckedSub(1, 2)
	if ok {
		t.Error("CheckedSub underflow should return false")
	}

	result, ok = CheckedSub(5, 2)
	if !ok || result != 3 {
		t.Errorf("CheckedSub(5, 2) = %d, %v; want 3, true", result, ok)
	}

	// Test CheckedMul
	result, ok = CheckedMul(math.MaxUint64, 2)
	if ok {
		t.Error("CheckedMul overflow should return false")
	}

	result, ok = CheckedMul(3, 4)
	if !ok || result != 12 {
		t.Errorf("CheckedMul(3, 4) = %d, %v; want 12, true", result, ok)
	}
}
