package model

import (
	"fmt"
	"sync"
	"time"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/constant"
	"github.com/zzyyyds88/PowerBarRations/logger"
	kitdto "github.com/zzyyyds88/PowerBarRations/relaykit/dto"
)

var channelsIDM map[int]*Channel // all channels include disabled
// channel2advancedCustomConfig caches parsed Advanced Custom (type 58) configs so
// path-aware selection avoids re-parsing JSON per request. Refreshed on full sync.
var channel2advancedCustomConfig map[int]*kitdto.AdvancedCustomConfig
var channelSyncLock sync.RWMutex

func InitChannelCache() {
	if !common.MemoryCacheEnabled {
		InvalidatePricingCache()
		return
	}
	newChannelId2channel := make(map[int]*Channel)
	newChannel2advancedCustomConfig := make(map[int]*kitdto.AdvancedCustomConfig)
	var channels []*Channel
	if err := DB.Find(&channels).Error; err != nil {
		// 读库失败不能把线上缓存换成空 map——那会让所有渠道瞬间“不存在”。
		common.SysError("failed to sync channel cache: " + err.Error())
		return
	}
	for _, channel := range channels {
		if channel == nil {
			continue
		}
		channel = cloneChannel(channel)
		if channel.ChannelInfo.IsMultiKey && channel.ChannelInfo.MultiKeyMode == constant.MultiKeyModePolling {
			channel.ChannelInfo.MultiKeyPollingIndex = CachedChannelPollingIndex(channel.Id)
		}
		newChannelId2channel[channel.Id] = channel
		if channel.Type == constant.ChannelTypeAdvancedCustom {
			if config := channel.GetOtherSettings().AdvancedCustom; config != nil {
				newChannel2advancedCustomConfig[channel.Id] = config
			}
		}
	}
	channelSyncLock.Lock()
	channelsIDM = newChannelId2channel
	channel2advancedCustomConfig = newChannel2advancedCustomConfig
	channelSyncLock.Unlock()
	// Lock ordering: InvalidatePricingCache acquires updatePricingLock, and
	// GetPricing (holding updatePricingLock) nests channelSyncLock.RLock via
	// loadPricingAdvancedCustomConfigs. channelSyncLock MUST be released before
	// invalidating the pricing cache, otherwise the reversed order deadlocks.
	InvalidatePricingCache()
	common.SysLog("channels synced from database")
}

func SyncChannelCache(frequency int) {
	for {
		time.Sleep(time.Duration(frequency) * time.Second)
		common.SysLog("syncing channels from database")
		InitChannelCache()
	}
}

// CacheGetChannel returns an immutable deep snapshot. Callers frequently mutate
// ChannelInfo while selecting keys; returning the shared cache pointer made
// those mutations race with cache refresh and other requests.
func CacheGetChannel(id int) (*Channel, error) {
	if !common.MemoryCacheEnabled {
		return GetChannelById(id, true)
	}
	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()

	c, ok := channelsIDM[id]
	if !ok {
		return nil, fmt.Errorf("渠道# %d，已不存在", id)
	}
	return cloneChannel(c), nil
}

// CacheGetChannelInfo returns a deep snapshot of ChannelInfo, including all
// mutable maps, so callers cannot mutate cache-owned state through the result.
func CacheGetChannelInfo(id int) (*ChannelInfo, error) {
	if !common.MemoryCacheEnabled {
		channel, err := GetChannelById(id, true)
		if err != nil {
			return nil, err
		}
		info := cloneChannelInfo(channel.ChannelInfo)
		return &info, nil
	}
	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()

	c, ok := channelsIDM[id]
	if !ok {
		return nil, fmt.Errorf("渠道# %d，已不存在", id)
	}
	info := cloneChannelInfo(c.ChannelInfo)
	return &info, nil
}

// CachedChannelPollingIndex reads the cache-owned polling index under the same
// per-channel lock used by SetCachedChannelPollingIndex. Lock order is always
// polling lock -> channelSyncLock.
func CachedChannelPollingIndex(id int) int {
	lock := GetChannelPollingLock(id)
	lock.Lock()
	defer lock.Unlock()
	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()
	if channel, ok := channelsIDM[id]; ok {
		return channel.ChannelInfo.MultiKeyPollingIndex
	}
	return 0
}

// SetCachedChannelPollingIndex atomically advances the cache-owned polling
// index. channel.go's polling branch should call this while holding the same
// per-channel lock around key selection.
func SetCachedChannelPollingIndex(id int, index int) {
	lock := GetChannelPollingLock(id)
	lock.Lock()
	defer lock.Unlock()
	channelSyncLock.Lock()
	defer channelSyncLock.Unlock()
	if channel, ok := channelsIDM[id]; ok {
		channel.ChannelInfo.MultiKeyPollingIndex = index
	}
}

// CacheUpdateChannelInfo 更新缓存中的完整 ChannelInfo 快照。调用方必须已持有
// GetChannelPollingLock(id)，这样多 key 状态更新与 polling index 推进共用同一锁域。
func CacheUpdateChannelInfo(id int, info ChannelInfo) {
	channelSyncLock.Lock()
	defer channelSyncLock.Unlock()
	if channel, ok := channelsIDM[id]; ok {
		channel.ChannelInfo = cloneChannelInfo(info)
	}
}

func CacheUpdateChannelStatus(id int, status int) {
	if !common.MemoryCacheEnabled {
		return
	}
	channelSyncLock.Lock()
	defer channelSyncLock.Unlock()
	if channel, ok := channelsIDM[id]; ok {
		channel.Status = status
	}
}

func CacheUpdateChannel(channel *Channel) {
	if !common.MemoryCacheEnabled || channel == nil {
		return
	}
	channel = cloneChannel(channel)
	lock := GetChannelPollingLock(channel.Id)
	lock.Lock()
	channelSyncLock.Lock()
	if channelsIDM == nil {
		channelsIDM = make(map[int]*Channel)
	}
	if oldChannel, ok := channelsIDM[channel.Id]; ok {
		if oldChannel.ChannelInfo.IsMultiKey &&
			oldChannel.ChannelInfo.MultiKeyMode == constant.MultiKeyModePolling &&
			channel.ChannelInfo.IsMultiKey &&
			channel.ChannelInfo.MultiKeyMode == constant.MultiKeyModePolling {
			channel.ChannelInfo.MultiKeyPollingIndex = oldChannel.ChannelInfo.MultiKeyPollingIndex
		}
		logger.LogDebug(nil, "CacheUpdateChannel before: id=%d, name=%s, status=%d, polling_index=%d", channel.Id, oldChannel.Name, oldChannel.Status, oldChannel.ChannelInfo.MultiKeyPollingIndex)
	}
	channelsIDM[channel.Id] = channel
	if channel2advancedCustomConfig == nil {
		channel2advancedCustomConfig = make(map[int]*kitdto.AdvancedCustomConfig)
	}
	delete(channel2advancedCustomConfig, channel.Id)
	if channel.Type == constant.ChannelTypeAdvancedCustom {
		if config := channel.GetOtherSettings().AdvancedCustom; config != nil {
			channel2advancedCustomConfig[channel.Id] = config
		}
	}
	logger.LogDebug(nil, "CacheUpdateChannel after: id=%d, name=%s, status=%d, polling_index=%d", channel.Id, channel.Name, channel.Status, channel.ChannelInfo.MultiKeyPollingIndex)
	channelSyncLock.Unlock()
	lock.Unlock()
	// Lock ordering: do NOT hold channelSyncLock while calling
	// InvalidatePricingCache. GetPricing acquires updatePricingLock first and then
	// channelSyncLock.RLock (via loadPricingAdvancedCustomConfigs); acquiring
	// updatePricingLock while holding channelSyncLock would be an AB-BA deadlock.
	InvalidatePricingCache()
}

func cloneChannel(channel *Channel) *Channel {
	if channel == nil {
		return nil
	}
	cloned := *channel
	cloned.OpenAIOrganization = clonePtr(channel.OpenAIOrganization)
	cloned.TestModel = clonePtr(channel.TestModel)
	cloned.BaseURL = clonePtr(channel.BaseURL)
	cloned.ModelMapping = clonePtr(channel.ModelMapping)
	cloned.StatusCodeMapping = clonePtr(channel.StatusCodeMapping)
	cloned.AutoBan = clonePtr(channel.AutoBan)
	cloned.Tag = clonePtr(channel.Tag)
	cloned.Setting = clonePtr(channel.Setting)
	cloned.ParamOverride = clonePtr(channel.ParamOverride)
	cloned.HeaderOverride = clonePtr(channel.HeaderOverride)
	cloned.Remark = clonePtr(channel.Remark)
	cloned.ChannelInfo = cloneChannelInfo(channel.ChannelInfo)
	cloned.Keys = append([]string(nil), channel.Keys...)
	return &cloned
}

func cloneChannelInfo(info ChannelInfo) ChannelInfo {
	cloned := info
	cloned.MultiKeyStatusList = cloneMap(info.MultiKeyStatusList)
	cloned.MultiKeyDisabledReason = cloneMap(info.MultiKeyDisabledReason)
	cloned.MultiKeyDisabledTime = cloneMap(info.MultiKeyDisabledTime)
	return cloned
}

func clonePtr[T any](value *T) *T {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneMap[K comparable, V any](source map[K]V) map[K]V {
	if source == nil {
		return nil
	}
	cloned := make(map[K]V, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}
