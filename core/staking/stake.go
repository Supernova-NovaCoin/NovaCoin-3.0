package staking

import (
	"crypto/ed25519"
	"fmt"
	"novacoin/core/execution"
	"novacoin/core/pulse"
)

const MinStakeRequired = 1000 * 1_000_000 // 1000 NVN

// ValidateBlock checks if the block author has sufficient stake AND a valid signature.
func ValidateBlock(v *pulse.Vertex, state *execution.StateManager) error {
	// 1. Verify Signature
	if v.Signature == nil {
		return fmt.Errorf("missing signature")
	}
	if !ed25519.Verify(v.Author[:], v.Hash[:], v.Signature) {
		return fmt.Errorf("invalid signature")
	}

	// 2. Verify Stake
	// Voting Power = Liquid Stake + Grant License Stake
	liquid := state.GetStake(v.Author)
	grant := state.GetGrantStake(v.Author)
	if liquid+grant < MinStakeRequired {
		return fmt.Errorf("insufficient stake: %d < %d", liquid+grant, MinStakeRequired)
	}
	return nil
}
