package types

import (
	spectypes "github.com/bloxapp/ssv-spec/types"
)

var (
	domain = spectypes.V3Testnet
)

// GetDefaultDomain returns the global domain used across the system
func GetDefaultDomain() spectypes.DomainType {
	return domain
}

// SetDefaultDomain updates the global domain used across the system
// allows injecting domain for testnets
func SetDefaultDomain(d spectypes.DomainType) {
	domain = d
}
