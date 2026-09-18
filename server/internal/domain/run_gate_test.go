package domain_test

import (
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/assert"
)

func TestIsRunGateRejection_RecognizesEveryPrefix(t *testing.T) {
	for _, prefix := range domain.RunGateRejectionPrefixes {
		assert.True(t, domain.IsRunGateRejection(prefix+" something specific happened"), prefix)
	}
}

func TestIsRunGateRejection_RejectsUnrelatedFailures(t *testing.T) {
	assert.False(t, domain.IsRunGateRejection("session limit reached"))
	assert.False(t, domain.IsRunGateRejection("exit 143"))
	assert.False(t, domain.IsRunGateRejection(""))
}
