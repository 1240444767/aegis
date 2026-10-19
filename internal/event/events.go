package event

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	SubjectAuthLoginAuditRequested = "auth.login.audit.requested"
	SubjectSessionAuditRequested   = "auth.session.audit.requested"
	SubjectUserMyAccessed          = "user.my.accessed"
	SubjectUserProfileRefresh      = "user.profile.cache.refresh.requested"
	SubjectUserSignedIn            = "user.signin.completed"
	SubjectUserAutoSignSync        = "user.autosign.sync.requested"
	SubjectFirewallBlocked         = "firewall.blocked"
	SubjectRealtimeUserPrefix      = "realtime.user"
	// SubjectRealtimeAppPrefix 应用级广播：该应用下所有在线连接都会收到（如应用公告变更）
	SubjectRealtimeAppPrefix = "realtime.app"
	// SubjectRealtimePresence 连接上线 / 下线事件，各实例的在线统计推送据此生成动态
	SubjectRealtimePresence = "realtime.presence"
	// SubjectRealtimeDisconnect 要求所有实例断开某用户的实时连接（强制下线后）
	SubjectRealtimeDisconnect = "realtime.control.disconnect"

	// 系统公告事件
	SubjectSystemAnnouncement = "system.announcement"

	// 抽奖系统事件
	SubjectLotteryDrawCompleted   = "lottery.draw.completed"
	SubjectLotteryActivityCreated = "lottery.activity.created"
	SubjectLotteryActivityUpdated = "lottery.activity.updated"
)

func SubjectRealtimeUser(appID int64, userID int64) string {
	return fmt.Sprintf("%s.%d.%d", SubjectRealtimeUserPrefix, appID, userID)
}

func MatchRealtimeUserSubject(subject string) (int64, int64, bool) {
	parts := strings.Split(strings.TrimSpace(subject), ".")
	if len(parts) != 4 {
		return 0, 0, false
	}
	if strings.Join(parts[:2], ".") != SubjectRealtimeUserPrefix {
		return 0, 0, false
	}
	appID, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return 0, 0, false
	}
	userID, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil {
		return 0, 0, false
	}
	return appID, userID, true
}

func SubjectRealtimeApp(appID int64) string {
	return fmt.Sprintf("%s.%d", SubjectRealtimeAppPrefix, appID)
}

func MatchRealtimeAppSubject(subject string) (int64, bool) {
	parts := strings.Split(strings.TrimSpace(subject), ".")
	if len(parts) != 3 {
		return 0, false
	}
	if strings.Join(parts[:2], ".") != SubjectRealtimeAppPrefix {
		return 0, false
	}
	appID, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return 0, false
	}
	return appID, true
}
