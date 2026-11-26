package model

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/constant"
	kitdto "github.com/zzyyyds88/PowerBarRations/relaykit/dto"
)

func setupChannelCacheForTest(t *testing.T) {
	t.Helper()
	oldEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	channelSyncLock.Lock()
	channelsIDM = make(map[int]*Channel)
	channel2advancedCustomConfig = make(map[int]*kitdto.AdvancedCustomConfig)
	channelSyncLock.Unlock()
	t.Cleanup(func() {
		common.MemoryCacheEnabled = oldEnabled
		channelSyncLock.Lock()
		channelsIDM = nil
		channel2advancedCustomConfig = nil
		channelSyncLock.Unlock()
	})
}

func TestCacheGetChannelReturnsDeepSnapshot(t *testing.T) {
	setupChannelCacheForTest(t)
	baseURL := "https://example.invalid"
	original := &Channel{
		Id:      1,
		Name:    "cached",
		BaseURL: &baseURL,
		Keys:    []string{"key-a", "key-b"},
		ChannelInfo: ChannelInfo{
			IsMultiKey:             true,
			MultiKeyStatusList:     map[int]int{0: 1, 1: 1},
			MultiKeyDisabledReason: map[int]string{},
			MultiKeyPollingIndex:   1,
		},
	}
	CacheUpdateChannel(original)

	original.Name = "mutated"
	*original.BaseURL = "mutated"
	original.Keys[0] = "mutated"
	original.ChannelInfo.MultiKeyStatusList[0] = 0
	original.ChannelInfo.MultiKeyPollingIndex = 99

	first, err := CacheGetChannel(1)
	require.NoError(t, err)
	assert.Equal(t, "cached", first.Name)
	assert.Equal(t, "https://example.invalid", *first.BaseURL)
	assert.Equal(t, []string{"key-a", "key-b"}, first.Keys)
	assert.Equal(t, 1, first.ChannelInfo.MultiKeyStatusList[0])
	assert.Equal(t, 1, first.ChannelInfo.MultiKeyPollingIndex)

	first.Name = "snapshot-mutated"
	first.Keys[0] = "snapshot-mutated"
	first.ChannelInfo.MultiKeyStatusList[0] = 0
	first.ChannelInfo.MultiKeyPollingIndex = 42

	second, err := CacheGetChannel(1)
	require.NoError(t, err)
	assert.Equal(t, "cached", second.Name)
	assert.Equal(t, "key-a", second.Keys[0])
	assert.Equal(t, 1, second.ChannelInfo.MultiKeyStatusList[0])
	assert.Equal(t, 1, second.ChannelInfo.MultiKeyPollingIndex)
}

func TestCacheGetChannelInfoReturnsDeepSnapshot(t *testing.T) {
	setupChannelCacheForTest(t)
	CacheUpdateChannel(&Channel{
		Id:   2,
		Name: "cached-info",
		ChannelInfo: ChannelInfo{
			MultiKeyStatusList:     map[int]int{0: 1},
			MultiKeyDisabledReason: map[int]string{0: "reason"},
			MultiKeyDisabledTime:   map[int]int64{0: 123},
			MultiKeyPollingIndex:   1,
		},
	})

	info, err := CacheGetChannelInfo(2)
	require.NoError(t, err)
	info.MultiKeyStatusList[0] = 0
	info.MultiKeyDisabledReason[0] = "mutated"
	info.MultiKeyDisabledTime[0] = 999
	info.MultiKeyPollingIndex = 99

	again, err := CacheGetChannelInfo(2)
	require.NoError(t, err)
	assert.Equal(t, 1, again.MultiKeyStatusList[0])
	assert.Equal(t, "reason", again.MultiKeyDisabledReason[0])
	assert.Equal(t, int64(123), again.MultiKeyDisabledTime[0])
	assert.Equal(t, 1, again.MultiKeyPollingIndex)
}

func TestCachedChannelPollingIndexUsesAtomicSnapshot(t *testing.T) {
	setupChannelCacheForTest(t)
	CacheUpdateChannel(&Channel{Id: 3, Name: "polling", ChannelInfo: ChannelInfo{MultiKeyPollingIndex: 2}})

	assert.Equal(t, 2, CachedChannelPollingIndex(3))
	SetCachedChannelPollingIndex(3, 5)
	assert.Equal(t, 5, CachedChannelPollingIndex(3))

	cached, err := CacheGetChannel(3)
	require.NoError(t, err)
	assert.Equal(t, 5, cached.ChannelInfo.MultiKeyPollingIndex)
}

func TestGetNextEnabledKeyReloadsAuthoritativeStatus(t *testing.T) {
	setupChannelCacheForTest(t)
	original := &Channel{
		Id:   5,
		Name: "polling-authoritative",
		Key:  "key-a\nkey-b",
		Keys: []string{"key-a", "key-b"},
		ChannelInfo: ChannelInfo{
			IsMultiKey:           true,
			MultiKeyMode:         constant.MultiKeyModePolling,
			MultiKeyStatusList:   map[int]int{0: common.ChannelStatusManuallyDisabled, 1: common.ChannelStatusEnabled},
			MultiKeyPollingIndex: 0,
		},
	}
	CacheUpdateChannel(original)
	stale := &Channel{
		Id:   5,
		Name: "polling-authoritative",
		Key:  "key-a\nkey-b",
		Keys: []string{"key-a", "key-b"},
		ChannelInfo: ChannelInfo{
			IsMultiKey:           true,
			MultiKeyMode:         constant.MultiKeyModePolling,
			MultiKeyStatusList:   map[int]int{0: common.ChannelStatusEnabled, 1: common.ChannelStatusEnabled},
			MultiKeyPollingIndex: 0,
		},
	}

	key, index, apiErr := stale.GetNextEnabledKey()
	require.Nil(t, apiErr)
	assert.Equal(t, "key-b", key)
	assert.Equal(t, 1, index)

	cached, err := CacheGetChannel(5)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusManuallyDisabled, cached.ChannelInfo.MultiKeyStatusList[0], "旧快照不得复活已禁用 key")
}

func TestChannelCacheConcurrentSnapshotsAndUpdates(t *testing.T) {
	setupChannelCacheForTest(t)
	CacheUpdateChannel(&Channel{
		Id:   4,
		Name: "race",
		ChannelInfo: ChannelInfo{
			IsMultiKey:         true,
			MultiKeyStatusList: map[int]int{0: 1, 1: 1},
		},
	})

	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				switch worker % 3 {
				case 0:
					snapshot, err := CacheGetChannel(4)
					if err == nil {
						snapshot.ChannelInfo.MultiKeyStatusList[0] = i
						snapshot.Keys = append(snapshot.Keys, fmt.Sprintf("%d", i))
					}
				case 1:
					info, err := CacheGetChannelInfo(4)
					if err == nil {
						info.MultiKeyStatusList[1] = i
					}
				case 2:
					CacheUpdateChannel(&Channel{
						Id:   4,
						Name: fmt.Sprintf("race-%d", i),
						ChannelInfo: ChannelInfo{
							IsMultiKey:         true,
							MultiKeyStatusList: map[int]int{0: 1, 1: 1},
						},
					})
					SetCachedChannelPollingIndex(4, i%2)
				}
			}
		}(worker)
	}
	wg.Wait()

	cached, err := CacheGetChannel(4)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusEnabled, cached.ChannelInfo.MultiKeyStatusList[0])
}
