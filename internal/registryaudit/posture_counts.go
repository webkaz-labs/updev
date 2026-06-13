package registryaudit

import "github.com/webkaz-labs/updev/internal/securitygate"

func NPMPostureReviewCount(postures []NPMPosture) int {
	count := 0
	for _, posture := range postures {
		if securitygate.DecisionNeedsAttention(posture.Decision) {
			count++
		}
	}
	return count
}

func CargoPostureReviewCount(postures []CargoPosture) int {
	count := 0
	for _, posture := range postures {
		if securitygate.DecisionNeedsAttention(posture.Decision) {
			count++
		}
	}
	return count
}

func PyPIPostureReviewCount(postures []PyPIPosture) int {
	count := 0
	for _, posture := range postures {
		if securitygate.DecisionNeedsAttention(posture.Decision) {
			count++
		}
	}
	return count
}
