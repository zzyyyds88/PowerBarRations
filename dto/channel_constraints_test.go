package dto

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolvedPinPriorityAndMerge(t *testing.T) {
	t.Run("token pin wins", func(t *testing.T) {
		constraints := &ChannelConstraints{}
		constraints.AddPin(ChannelPin{ChannelId: 1, Source: PinSourceToken, Rank: PinRankToken, RetryMode: PinRetrySingleAttempt})

		pin, found, overridden := constraints.ResolvedPin()
		require.True(t, found)
		assert.Equal(t, 1, pin.ChannelId)
		assert.Equal(t, PinSourceToken, pin.Source)
		assert.Equal(t, PinRetrySingleAttempt, pin.RetryMode)
		assert.Empty(t, overridden)
		assert.True(t, constraints.SuppressesRetry())
	})

	t.Run("empty set has no pin", func(t *testing.T) {
		pin, found, overridden := (*ChannelConstraints)(nil).ResolvedPin()
		assert.False(t, found)
		assert.Zero(t, pin.ChannelId)
		assert.Nil(t, overridden)
	})
}
