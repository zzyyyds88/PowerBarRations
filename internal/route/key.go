package route

import (
	"strconv"

	"github.com/zzyyyds88/PowerBarRations/model"
)

// MemberKey 是同一车道内的成员运行态身份。车道身份由 Registry 的 lane key 承载。
type MemberKey struct {
	ChannelID     int
	UpstreamModel string
	MemberID      int
}

// MemberKeyOf 从解析后的成员建立运行态键。
func MemberKeyOf(member *model.RouteMember) MemberKey {
	if member == nil {
		return MemberKey{}
	}
	return MemberKey{ChannelID: member.ChannelId, UpstreamModel: member.UpstreamModel, MemberID: member.MemberId}
}

// memberKeyOf 保留包内既有构造入口，避免运行态键散落为字符串。
func memberKeyOf(member *model.RouteMember) MemberKey { return MemberKeyOf(member) }

// String 保持既有日志形状 channelId:upstreamModel#memberId。
func (key MemberKey) String() string {
	return strconv.Itoa(key.ChannelID) + ":" + key.UpstreamModel + "#" + strconv.Itoa(key.MemberID)
}

// EventMember 是事件投影 channelId:upstreamModel。
func (key MemberKey) EventMember() string {
	if key == (MemberKey{}) {
		return ""
	}
	return strconv.Itoa(key.ChannelID) + ":" + key.UpstreamModel
}

// eventMember 是事件与日志的成员投影。
func eventMember(key MemberKey) string { return key.EventMember() }
