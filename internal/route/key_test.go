package route

import (
	"strconv"
	"strings"
)

func testMemberKey(value string) MemberKey {
	channel, rest, _ := strings.Cut(value, ":")
	upstream, member, _ := strings.Cut(rest, "#")
	id, _ := strconv.Atoi(channel)
	memberID, _ := strconv.Atoi(member)
	return MemberKey{ChannelID: id, UpstreamModel: upstream, MemberID: memberID}
}
