package model

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resetClientKeyTouchStateForTest(t *testing.T) {
	t.Helper()
	clientKeyTouchMu.Lock()
	clear(clientKeyLastTouch)
	clear(clientKeyTouchPending)
	clientKeyTouchMu.Unlock()
	select {
	case <-clientKeyTouchSignal:
	default:
	}
	t.Cleanup(func() {
		clientKeyTouchMu.Lock()
		clear(clientKeyLastTouch)
		clear(clientKeyTouchPending)
		clientKeyTouchMu.Unlock()
	})
}

func TestTouchClientKeyCoalescesAndFlushesOnce(t *testing.T) {
	truncateTables(t)
	resetClientKeyTouchStateForTest(t)

	key := &ClientKey{Name: "touch-coalesce", KeyHash: "hash", Enabled: true}
	require.NoError(t, DB.Create(key).Error)

	TouchClientKey(key.Id)
	TouchClientKey(key.Id)
	TouchClientKey(key.Id)

	clientKeyTouchMu.Lock()
	assert.Len(t, clientKeyTouchPending, 1)
	clientKeyTouchMu.Unlock()

	flushClientKeyTouches(time.Now().Unix())

	var stored ClientKey
	require.NoError(t, DB.First(&stored, key.Id).Error)
	assert.Positive(t, stored.LastUsedAt)

	clientKeyTouchMu.Lock()
	assert.Empty(t, clientKeyTouchPending)
	clientKeyTouchMu.Unlock()
}

func TestTouchClientKeyConcurrentCallsShareOnePendingEntry(t *testing.T) {
	truncateTables(t)
	resetClientKeyTouchStateForTest(t)

	key := &ClientKey{Name: "touch-concurrent", KeyHash: "hash", Enabled: true}
	require.NoError(t, DB.Create(key).Error)

	var wg sync.WaitGroup
	for range 64 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			TouchClientKey(key.Id)
		}()
	}
	wg.Wait()

	clientKeyTouchMu.Lock()
	assert.Len(t, clientKeyTouchPending, 1)
	clientKeyTouchMu.Unlock()
}

func TestTouchClientKeyStateTTLPrunesDeletedIdentities(t *testing.T) {
	resetClientKeyTouchStateForTest(t)
	now := time.Now().Unix()
	clientKeyTouchMu.Lock()
	clientKeyLastTouch[1] = now - int64(clientKeyTouchStateTTL.Seconds()) - 1
	clientKeyTouchPending[1] = struct{}{}
	clientKeyLastTouch[2] = now
	clientKeyTouchPending[2] = struct{}{}
	clientKeyTouchMu.Unlock()

	pruneClientKeyTouchStateLocked(now)

	clientKeyTouchMu.Lock()
	assert.NotContains(t, clientKeyLastTouch, 1)
	assert.NotContains(t, clientKeyTouchPending, 1)
	assert.Contains(t, clientKeyLastTouch, 2)
	clientKeyTouchMu.Unlock()
}

func TestTouchClientKeyMapIsBounded(t *testing.T) {
	resetClientKeyTouchStateForTest(t)
	clientKeyTouchMu.Lock()
	for i := 1; i <= clientKeyTouchMaxKeys; i++ {
		clientKeyLastTouch[i] = time.Now().Unix()
	}
	clientKeyTouchMu.Unlock()

	TouchClientKey(clientKeyTouchMaxKeys + 1)

	clientKeyTouchMu.Lock()
	assert.NotContains(t, clientKeyLastTouch, clientKeyTouchMaxKeys+1)
	clientKeyTouchMu.Unlock()
}

func TestDeleteClientKeyByNameDropsTouchState(t *testing.T) {
	truncateTables(t)
	resetClientKeyTouchStateForTest(t)

	key := &ClientKey{Name: "touch-delete", KeyHash: "hash", Enabled: true}
	require.NoError(t, DB.Create(key).Error)
	TouchClientKey(key.Id)

	require.NoError(t, DeleteClientKeyByName(key.Name))

	clientKeyTouchMu.Lock()
	assert.NotContains(t, clientKeyLastTouch, key.Id)
	assert.NotContains(t, clientKeyTouchPending, key.Id)
	clientKeyTouchMu.Unlock()

	var count int64
	require.NoError(t, DB.Model(&ClientKey{}).Where("id = ?", key.Id).Count(&count).Error)
	assert.Zero(t, count)
}
