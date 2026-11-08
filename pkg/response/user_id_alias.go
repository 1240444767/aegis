package response

import (
	"encoding/json"
	"regexp"
	"strconv"

	"github.com/gin-gonic/gin"
)

// 对外用户 ID（docs/user-public-id.md）。
//
// 用户端接口返回的流水、订单、通知、设置……几十种结构里都挂着一个 userId，值是调用者自己的数据库主键。
// 这些结构与管理端共用，逐个改字段既改不全，也会把管理端一起改坏。这里换个落点：用户认证中间件
// 把「主键 → 对外 ID」登记在请求上，写响应时把数据里**名字是用户 ID、值恰好是调用者主键**的字段换成对外 ID。
//
// 只认名字与值同时对上的字段：名字叫 id 的不碰（订单号、流水号与主键撞值是常事），别人的 ID 不碰
// （排行榜之类由各自的接口显式换算）。以后新增的用户端接口不用做任何事，自己的 ID 自动不出门。

const userIDAliasKey = "response.userIDAlias"

type userIDAlias struct {
	internal string
	public   string
}

// SetUserIDAlias 登记这次请求的调用者：响应里等于 internalID 的用户 ID 字段写成 publicID。
// 任一为 0 时不登记（查不到对外 ID 时宁可原样返回，也不能写成 0）。
func SetUserIDAlias(c *gin.Context, internalID, publicID int64) {
	if c == nil || internalID <= 0 || publicID <= 0 {
		return
	}
	c.Set(userIDAliasKey, userIDAlias{
		internal: strconv.FormatInt(internalID, 10),
		public:   strconv.FormatInt(publicID, 10),
	})
}

// userIDField 匹配 encoding/json 输出里的 "键":整数。字符串里的引号一律被转义成 \"，
// 所以这个模式不会命中字符串值内部的文本。
var userIDField = regexp.MustCompile(`"(uid|userId|user_id|[A-Za-z]+UserId|[a-z]+_user_id|uploadedById)":(-?\d+)`)

func applyUserIDAlias(c *gin.Context, data any) any {
	if c == nil || data == nil {
		return data
	}
	raw, ok := c.Get(userIDAliasKey)
	if !ok {
		return data
	}
	alias, ok := raw.(userIDAlias)
	if !ok {
		return data
	}
	body, err := json.Marshal(data)
	if err != nil {
		// 编不出来就交还原值，让 c.JSON 报出它本来就会报的错
		return data
	}
	return json.RawMessage(RewriteUserID(body, alias.internal, alias.public))
}

// RewriteUserID 把 JSON 里值等于 internal 的用户 ID 字段改写为 public。导出给测试与 WebSocket 帧复用。
func RewriteUserID(body []byte, internal, public string) []byte {
	return userIDField.ReplaceAllFunc(body, func(match []byte) []byte {
		sub := userIDField.FindSubmatchIndex(match)
		if string(match[sub[4]:sub[5]]) != internal {
			return match
		}
		out := make([]byte, 0, len(match)+len(public))
		out = append(out, match[:sub[4]]...)
		return append(out, public...)
	})
}
