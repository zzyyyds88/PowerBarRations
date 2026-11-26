package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLaneRelayConfigERejectsInvalidValues(t *testing.T) {
	_, err := ParseLaneRelayConfigE(`{"member_max_attempts":-1}`)
	require.NotNil(t, err)
	assert.Equal(t, "invalid_lane_config", err.Code)
	assert.Equal(t, "member_max_attempts", err.Path)

	_, err = ParseLaneRelayConfigE(`{`)
	require.NotNil(t, err)
	assert.Equal(t, "invalid_lane_config", err.Code)
}

func TestParseLaneRelayOverridesERejectsInvalidValues(t *testing.T) {
	_, err := ParseLaneRelayOverridesE(`{"member_cooldown_seconds":-2}`)
	require.NotNil(t, err)
	assert.Equal(t, "invalid_lane_config", err.Code)
	assert.Equal(t, "member_cooldown_seconds", err.Path)
}

func TestEffectiveConfigEPropagatesMemberOverrideError(t *testing.T) {
	route := &ResolvedRoute{Config: DefaultLaneRelayConfig()}
	member := &RouteMember{Overrides: `{"member_retry_interval_seconds":-1}`}
	_, err := route.EffectiveConfigE(member)
	require.NotNil(t, err)
	assert.Equal(t, "member_retry_interval_seconds", err.Path)
}
