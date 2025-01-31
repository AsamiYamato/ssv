package params

import (
	"math"
	"time"

	"github.com/pkg/errors"
)

// scoreByWeight provides the relevant score by the provided weight and threshold.
func scoreByWeight(maxScore float64, weight, threshold float64) float64 {
	return maxScore / (weight * threshold * threshold)
}

// scoreDecay determines the decay rate from the provided time period till
// the decayToZero value. Ex: ( 1 -> 0.01)
func scoreDecay(totalDecayDuration time.Duration, decayIntervalDuration time.Duration) float64 {
	ticks := float64(totalDecayDuration / decayIntervalDuration)
	return math.Pow(decayToZero, 1/ticks)
}

// the cap for `inMesh` time scoring.
func inMeshCap(inMeshTime time.Duration) float64 {
	return float64((3600 * time.Second) / inMeshTime)
}

// decayThreshold is used to determine the threshold from the decay limit with
// a provided growth rate. This applies the decay rate to a
// computed limit.
func decayThreshold(decayRate, rate float64) (float64, error) {
	d, err := decayConvergence(decayRate, rate)
	if err != nil {
		return 0, err
	}
	return d * decayRate, nil
}

// decayConvergence computes the limit to which a decay process will convert if
// it has the given issuance rate per decay interval and the given decay factor.
func decayConvergence(decayRate, rate float64) (float64, error) {
	if 1 <= decayRate {
		return 0, errors.Errorf("invalid rate: %f", decayRate)
	}
	return rate / (1 - decayRate), nil
}
