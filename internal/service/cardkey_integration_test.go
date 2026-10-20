package service

import (
	"context"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"go.uber.org/zap"

	cardkeydomain "aegis/internal/domain/cardkey"
	vipdomain "aegis/internal/domain/vip"
	pgrepo "aegis/internal/repository/postgres"
)

// 卡密的端到端集成测试：真实 Postgres，跑全部迁移，再走一遍
// 「一卡一用 → 活动码（每人一次、用满、并发抢）→ 每人限兑 → 生效时间与预览 → 领取对象 →
// 导入与归一化 → 补发 → 提卡 → 删除的约束 → 功能解锁 → 授权卡（激活、设备上限、自助解绑限次）→
// 续费卡 → 管理员代兑 → 调整单卡 → 统计」，最后验证老数据的已用次数回填。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55432/postgres go test ./internal/service -run TestCardKeyIntegration
func TestCardKeyIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	vip := NewVipService(zap.NewNop(), pg)
	cards := NewCardKeyService(zap.NewNop(), pg)
	const appID = int64(41000)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'cardkey-test') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	if _, err := vip.AdminSaveFeature(ctx, vipdomain.FeatureMutation{AppID: appID, Tag: "online_music", Name: ptrTo("在线音乐")}); err != nil {
		t.Fatal(err)
	}

	alice := insertTestUser(t, ctx, pool, appID, "ck-alice", "0")
	bob := insertTestUser(t, ctx, pool, appID, "ck-bob", "0")
	carol := insertTestUser(t, ctx, pool, appID, "ck-carol", "0")

	generate := func(input cardkeydomain.GenerateInput) *cardkeydomain.Batch {
		t.Helper()
		input.AppID = appID
		if input.Operator == "" {
			input.Operator = "admin"
		}
		batch, err := cards.GenerateBatch(ctx, input)
		if err != nil {
			t.Fatalf("生成「%s」失败：%v", input.Name, err)
		}
		return batch
	}
	codesOf := func(batchID int64) []cardkeydomain.Card {
		t.Helper()
		page, err := cards.ListCards(ctx, cardkeydomain.CardQuery{AppID: appID, BatchID: batchID, Limit: 200})
		if err != nil {
			t.Fatal(err)
		}
		out := page.Items
		// 列表按 id 倒序，测试里按生成顺序用
		for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
			out[i], out[j] = out[j], out[i]
		}
		return out
	}
	client := func(userID int64) CardKeyClientContext {
		return CardKeyClientContext{AppID: appID, UserID: userID, DeviceID: "dev-test", ClientIP: "127.0.0.1"}
	}
	redeem := func(userID int64, code string) (*cardkeydomain.RedeemResult, error) {
		return cards.Redeem(ctx, client(userID), code)
	}
	mustRedeem := func(userID int64, code string) *cardkeydomain.RedeemResult {
		t.Helper()
		result, err := redeem(userID, code)
		if err != nil {
			t.Fatalf("兑换 %s 失败：%v", code, err)
		}
		return result
	}
	integral := func(userID int64) int64 {
		var value int64
		if err := pool.QueryRow(ctx, `SELECT integral FROM users WHERE id = $1`, userID).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	integralReward := func(amount int64) []cardkeydomain.Reward {
		return []cardkeydomain.Reward{{Type: cardkeydomain.RewardIntegral, Amount: amount}}
	}

	// ── 一卡一用 ──
	single := generate(cardkeydomain.GenerateInput{Name: "礼品卡", Kind: cardkeydomain.KindRedeem, Count: 5,
		CodePrefix: "GIFT", Rewards: []cardkeydomain.Reward{
			{Type: cardkeydomain.RewardIntegral, Amount: 100}, {Type: cardkeydomain.RewardVipDays, Amount: 7}},
		FaceValue: decimal.RequireFromString("9.9"), Channel: "淘宝"})
	if single.Stats == nil || single.Stats.Total != 5 || single.Stats.Available != 5 {
		t.Fatalf("新批次的进度不对：%+v", single.Stats)
	}
	singleCodes := codesOf(single.ID)
	if !strings.HasPrefix(singleCodes[0].Code, "GIFT-") {
		t.Fatalf("卡面没有带前缀：%s", singleCodes[0].Code)
	}
	result := mustRedeem(alice, strings.ToLower(strings.ReplaceAll(singleCodes[0].Code, "-", " ")))
	if len(result.Results) != 2 || result.Results[0].Type != cardkeydomain.RewardVipDays || result.BatchName != "礼品卡" {
		t.Fatalf("核销结果不对：%+v", result)
	}
	if integral(alice) != 100 {
		t.Fatalf("积分应到账 100，实际 %d", integral(alice))
	}
	if _, err := redeem(bob, singleCodes[0].Code); errCode(err) != errCodeCardKeyUsed {
		t.Fatalf("一卡一用的卡第二个人用应报 40341，得到 %v", err)
	}
	if _, err := redeem(bob, "GIFT-NOPE-NOPE-NOPE-NOPE"); errCode(err) != errCodeCardKeyNotFound {
		t.Fatalf("不存在的卡应报 40441，得到 %v", err)
	}

	// ── 活动码：每人一次、用满即止 ──
	event := generate(cardkeydomain.GenerateInput{Name: "直播口令", Kind: cardkeydomain.KindRedeem, Count: 1, MaxUses: 2,
		Rewards: integralReward(10)})
	eventCode := codesOf(event.ID)[0].Code
	mustRedeem(alice, eventCode)
	if _, err := redeem(alice, eventCode); errCode(err) != errCodeCardKeyAlreadyRedeemed {
		t.Fatalf("同一个人第二次用活动码应报 40976，得到 %v", err)
	}
	mustRedeem(bob, eventCode)
	if _, err := redeem(carol, eventCode); errCode(err) != errCodeCardKeyUsed {
		t.Fatalf("活动码用满后应报 40341，得到 %v", err)
	}
	if card := codesOf(event.ID)[0]; card.UseCount != 2 || card.Status != cardkeydomain.StatusUsed {
		t.Fatalf("活动码用满后应为 used 2/2，实际 %s %d/%d", card.Status, card.UseCount, card.MaxUses)
	}

	// 并发抢一个 3 人份的活动码：恰好 3 个人成功
	race := generate(cardkeydomain.GenerateInput{Name: "并发口令", Kind: cardkeydomain.KindRedeem, Count: 1, MaxUses: 3,
		Rewards: integralReward(1)})
	raceCode := codesOf(race.ID)[0].Code
	racers := make([]int64, 0, 8)
	for index := range 8 {
		racers = append(racers, insertTestUser(t, ctx, pool, appID, "ck-racer-"+string(rune('a'+index)), "0"))
	}
	var wins atomic.Int32
	var wg sync.WaitGroup
	for _, userID := range racers {
		wg.Add(1)
		go func(userID int64) {
			defer wg.Done()
			if _, err := redeem(userID, raceCode); err == nil {
				wins.Add(1)
			} else if code := errCode(err); code != errCodeCardKeyUsed {
				t.Errorf("并发兑换的失败应当只有「已用完」，得到 %v", err)
			}
		}(userID)
	}
	wg.Wait()
	if wins.Load() != 3 {
		t.Fatalf("3 人份的活动码应恰好 3 人成功，实际 %d", wins.Load())
	}

	// ── 每人限兑 ──
	limited := generate(cardkeydomain.GenerateInput{Name: "新人券", Kind: cardkeydomain.KindRedeem, Count: 3, PerUserLimit: 1,
		Rewards: integralReward(5)})
	limitedCodes := codesOf(limited.ID)
	mustRedeem(carol, limitedCodes[0].Code)
	if _, err := redeem(carol, limitedCodes[1].Code); errCode(err) != errCodeCardKeyUserLimit {
		t.Fatalf("每人限兑 1 张时第二张应报 40977，得到 %v", err)
	}
	preview, err := cards.Preview(ctx, client(carol), limitedCodes[1].Code)
	if err != nil || preview.Usable || preview.ReasonCode != errCodeCardKeyUserLimit {
		t.Fatalf("预览应说明每人限兑已满：%+v %v", preview, err)
	}

	// ── 生效时间与预览 ──
	later := time.Now().Add(48 * time.Hour)
	scheduled := generate(cardkeydomain.GenerateInput{Name: "双十一", Kind: cardkeydomain.KindRedeem, Count: 1, StartsAt: &later,
		Rewards: []cardkeydomain.Reward{{Type: cardkeydomain.RewardVipDays, Amount: 3}}})
	scheduledCode := codesOf(scheduled.ID)[0].Code
	if _, err := redeem(bob, scheduledCode); errCode(err) != errCodeCardKeyNotStarted {
		t.Fatalf("未到生效时间应报 40975，得到 %v", err)
	}
	preview, err = cards.Preview(ctx, client(bob), scheduledCode)
	if err != nil || preview.Usable || preview.ReasonCode != errCodeCardKeyNotStarted || len(preview.Rewards) != 1 ||
		preview.Rewards[0].Detail != "3 天" {
		t.Fatalf("预览应给出权益与「未生效」：%+v %v", preview, err)
	}
	// 把生效时间改到过去，之后就能用了
	earlier := time.Now().Add(-time.Minute)
	if _, err := cards.UpdateBatch(ctx, cardkeydomain.BatchUpdateInput{AppID: appID, BatchID: scheduled.ID, Name: "双十一",
		StartsAt: &earlier, Rewards: scheduled.Rewards, Audience: cardkeydomain.AudienceAll}); err != nil {
		t.Fatal(err)
	}
	preview, err = cards.Preview(ctx, client(bob), scheduledCode)
	if err != nil || !preview.Usable {
		t.Fatalf("改了生效时间之后预览应可用：%+v %v", preview, err)
	}

	// ── 领取对象：alice 已经是会员（礼品卡送了 7 天） ──
	nonVip := generate(cardkeydomain.GenerateInput{Name: "拉新", Kind: cardkeydomain.KindRedeem, Count: 2,
		Audience: cardkeydomain.AudienceNonVip, Rewards: integralReward(1)})
	nonVipCodes := codesOf(nonVip.ID)
	if _, err := redeem(alice, nonVipCodes[0].Code); errCode(err) != errCodeCardKeyAudience {
		t.Fatalf("会员兑换仅限非会员的卡应报 40978，得到 %v", err)
	}
	mustRedeem(carol, nonVipCodes[0].Code)

	// ── 导入与撞码 ──
	if _, err := cards.GenerateBatch(ctx, cardkeydomain.GenerateInput{AppID: appID, Name: "撞码", Kind: cardkeydomain.KindRedeem,
		Codes: []string{"OLD-0001", singleCodes[1].Code}, Rewards: integralReward(1)}); errCode(err) != errCodeCardKeyCodeTaken {
		t.Fatalf("导入已存在的卡面应报 40985，得到 %v", err)
	}
	imported := generate(cardkeydomain.GenerateInput{Name: "迁移", Kind: cardkeydomain.KindRedeem,
		Codes: []string{"old-0001", "OLD 0002", " old_0003 "}, Rewards: integralReward(3)})
	if imported.Origin != cardkeydomain.OriginImported || imported.Total != 3 {
		t.Fatalf("导入批次不对：%+v", imported)
	}
	mustRedeem(bob, "Old-0002")
	if _, err := cards.AppendBatch(ctx, cardkeydomain.AppendInput{AppID: appID, BatchID: imported.ID, Count: 1}); errCode(err) != 40000 {
		t.Fatalf("导入的批次不能补发，得到 %v", err)
	}

	// ── 补发与提卡 ──
	appended, err := cards.AppendBatch(ctx, cardkeydomain.AppendInput{AppID: appID, BatchID: single.ID, Count: 3, Operator: "admin"})
	if err != nil || appended.Total != 8 || appended.Stats.Total != 8 {
		t.Fatalf("补发后总数应为 8：%+v %v", appended, err)
	}
	_, extracted, err := cards.ExtractCards(ctx, cardkeydomain.ExtractInput{AppID: appID, BatchID: single.ID, Count: 4, Note: "代理 A"})
	if err != nil || len(extracted) != 4 {
		t.Fatalf("提卡失败：%d %v", len(extracted), err)
	}
	for _, card := range extracted {
		if card.DistributedAt == nil || card.DistributionNote != "代理 A" || card.Code == singleCodes[0].Code {
			t.Fatalf("提出来的卡不对：%+v", card)
		}
	}
	after, _ := cards.GetBatch(ctx, appID, single.ID)
	if after.Stats.Available != 3 || after.Stats.Distributed != 4 {
		t.Fatalf("提卡后可提取应为 3、已提取 4：%+v", after.Stats)
	}
	if _, _, err := cards.ExtractCards(ctx, cardkeydomain.ExtractInput{AppID: appID, BatchID: single.ID, Count: 4}); errCode(err) != 40000 {
		t.Fatalf("可提取的不够时应拒绝，得到 %v", err)
	}
	undistributed, err := cards.ListCards(ctx, cardkeydomain.CardQuery{AppID: appID, BatchID: single.ID, Distributed: "no",
		Status: cardkeydomain.StatusUnused})
	if err != nil || undistributed.Total != 3 {
		t.Fatalf("未提取的未使用卡应为 3：%v %v", undistributed, err)
	}
	_, exported, err := cards.Export(ctx, cardkeydomain.ExportQuery{AppID: appID, BatchID: single.ID,
		Status: cardkeydomain.StatusUnused, OnlyUndistributed: true})
	if err != nil || len(exported) != 3 {
		t.Fatalf("导出未提取的可用卡应为 3 张：%d %v", len(exported), err)
	}

	// ── 删除的约束 ──
	if err := cards.DeleteBatch(ctx, appID, single.ID); errCode(err) != errCodeCardKeyBatchInUse {
		t.Fatalf("有核销记录的批次不能删，得到 %v", err)
	}
	deleted, err := cards.DeleteCards(ctx, appID, []int64{singleCodes[0].ID, singleCodes[1].ID})
	if err != nil || deleted != 1 {
		t.Fatalf("只能删掉没用过的那一张：%d %v", deleted, err)
	}
	spare := generate(cardkeydomain.GenerateInput{Name: "废批次", Kind: cardkeydomain.KindRedeem, Count: 2, Rewards: integralReward(1)})
	if err := cards.DeleteBatch(ctx, appID, spare.ID); err != nil {
		t.Fatalf("没用过的批次应能删除：%v", err)
	}

	// ── 功能解锁 ──
	music := generate(cardkeydomain.GenerateInput{Name: "听歌卡", Kind: cardkeydomain.KindRedeem, Count: 2,
		Rewards: []cardkeydomain.Reward{{Type: cardkeydomain.RewardFeature, Feature: "online_music", Amount: 0}}})
	if _, err := cards.GenerateBatch(ctx, cardkeydomain.GenerateInput{AppID: appID, Name: "坏卡", Kind: cardkeydomain.KindRedeem, Count: 1,
		Rewards: []cardkeydomain.Reward{{Type: cardkeydomain.RewardFeature, Feature: "no_such", Amount: 1}}}); errCode(err) != 40000 {
		t.Fatalf("引用不存在的功能应被拒，得到 %v", err)
	}
	musicCodes := codesOf(music.ID)
	unlocked := mustRedeem(bob, musicCodes[0].Code)
	if unlocked.Results[0].Detail != "在线音乐 永久" {
		t.Fatalf("功能解锁的结果文案不对：%+v", unlocked.Results)
	}
	if _, err := redeem(bob, musicCodes[1].Code); errCode(err) != errCodeCardKeyRewardOwned {
		t.Fatalf("已永久拥有时应报 40983，得到 %v", err)
	}
	if card := codesOf(music.ID)[1]; card.UseCount != 0 || card.Status != cardkeydomain.StatusUnused {
		t.Fatalf("发放失败时卡不应被核销：%+v", card)
	}

	// ── 授权卡 ──
	license := generate(cardkeydomain.GenerateInput{Name: "月卡", Kind: cardkeydomain.KindLogin, Count: 2, MaxDevices: 1,
		ValidityMode: cardkeydomain.ValidityFromFirstUse, ValidityDays: 30, SelfUnbindLimit: 1,
		Rewards: integralReward(50)})
	licenseCodes := codesOf(license.ID)
	dave := insertTestUser(t, ctx, pool, appID, licenseCodes[0].Code, "0")
	card, err := cards.PrepareLogin(ctx, appID, strings.ToLower(licenseCodes[0].Code), "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	auth, err := cards.Activate(ctx, cardkeydomain.ActivateLoginInput{AppID: appID, CardID: card.ID, UserID: dave, DeviceID: "phone-1", DeviceName: "Pixel"})
	if err != nil || !auth.FirstActivation || auth.DeviceCount != 1 || auth.ExpiresAt == nil {
		t.Fatalf("首次激活不对：%+v %v", auth, err)
	}
	if integral(dave) != 50 {
		t.Fatalf("随卡权益应到账 50，实际 %d", integral(dave))
	}
	if _, err := cards.Activate(ctx, cardkeydomain.ActivateLoginInput{AppID: appID, CardID: card.ID, UserID: dave, DeviceID: "phone-2"}); errCode(err) != errCodeCardKeyDeviceLimit {
		t.Fatalf("超出设备数应报 40343，得到 %v", err)
	}
	if _, err := cards.Redeem(ctx, client(bob), licenseCodes[0].Code); errCode(err) != errCodeCardKeyUsed {
		t.Fatalf("别人兑换已激活的授权卡应报 40341，得到 %v", err)
	}
	licenses, err := cards.MyLicenses(ctx, appID, dave, "phone-1")
	if err != nil || len(licenses) != 1 || len(licenses[0].Devices) != 1 || !licenses[0].Devices[0].Current ||
		licenses[0].SelfUnbindRemaining != 1 {
		t.Fatalf("我的授权不对：%+v %v", licenses, err)
	}
	if err := cards.SelfUnbind(ctx, appID, bob, card.ID, "phone-1"); errCode(err) != errCodeCardKeyNotFound {
		t.Fatalf("别人的卡不能自助解绑，得到 %v", err)
	}
	if err := cards.SelfUnbind(ctx, appID, dave, card.ID, "phone-1"); err != nil {
		t.Fatalf("自助解绑失败：%v", err)
	}
	if _, err := cards.Activate(ctx, cardkeydomain.ActivateLoginInput{AppID: appID, CardID: card.ID, UserID: dave, DeviceID: "phone-2"}); err != nil {
		t.Fatalf("解绑之后新设备应能登录：%v", err)
	}
	if err := cards.SelfUnbind(ctx, appID, dave, card.ID, "phone-2"); errCode(err) != errCodeCardKeyUnbindLimit {
		t.Fatalf("自助解绑次数用完应报 40979，得到 %v", err)
	}
	if err := cards.UnbindDevice(ctx, appID, card.ID, "phone-9", "admin"); errCode(err) != errCodeCardKeyDeviceMissing {
		t.Fatalf("解绑不存在的设备应报 40458，得到 %v", err)
	}
	if removed, err := cards.ResetDevices(ctx, appID, card.ID, "admin"); err != nil || removed != 1 {
		t.Fatalf("清空设备失败：%d %v", removed, err)
	}
	detail, err := cards.GetCardDetail(ctx, appID, card.ID)
	if err != nil || len(detail.Unbinds) != 2 || len(detail.Redemptions) != 1 || detail.Card.BoundAccount != licenseCodes[0].Code {
		t.Fatalf("单卡全貌不对：%+v %v", detail, err)
	}

	// ── 续费卡 ──
	renew := generate(cardkeydomain.GenerateInput{Name: "续费 10 天", Kind: cardkeydomain.KindRedeem, Count: 2,
		Rewards: []cardkeydomain.Reward{{Type: cardkeydomain.RewardLicenseDays, Amount: 10}}})
	renewCodes := codesOf(renew.ID)
	before := *auth.ExpiresAt
	renewed := mustRedeem(dave, renewCodes[0].Code)
	if renewed.Results[0].ExpireAt == nil || renewed.Results[0].ExpireAt.Sub(before) < 10*24*time.Hour-time.Minute {
		t.Fatalf("续费后应顺延 10 天：%v → %+v", before, renewed.Results[0])
	}
	if _, err := redeem(carol, renewCodes[1].Code); errCode(err) != errCodeCardKeyNoLoginCard {
		t.Fatalf("名下没有授权卡时续费卡应报 40346，得到 %v", err)
	}

	// ── 管理员调整单卡与代兑 ──
	permanent, err := cards.UpdateCard(ctx, cardkeydomain.CardUpdateInput{AppID: appID, CardID: card.ID,
		ExpiryAction: cardkeydomain.ExpiryPermanent, MaxDevices: ptrTo(3), Remark: ptrTo(" VIP 客户 ")})
	if err != nil || permanent.ExpiresAt != nil || permanent.MaxDevices != 3 || permanent.Remark != "VIP 客户" {
		t.Fatalf("调整单卡不对：%+v %v", permanent, err)
	}
	if _, err := redeem(dave, renewCodes[1].Code); errCode(err) != errCodeCardKeyLicensePermanent {
		t.Fatalf("永久授权再续费应报 40981，得到 %v", err)
	}
	adminResult, err := cards.AdminRedeem(ctx, appID, renewCodes[1].Code, 0, "ck-nobody", "admin")
	if errCode(err) != 40401 || adminResult != nil {
		t.Fatalf("代兑给不存在的账号应报 40401，得到 %v", err)
	}
	if _, err := cards.AdminRedeem(ctx, appID, limitedCodes[2].Code, 0, "ck-bob", "admin"); err != nil {
		t.Fatalf("按账号代兑失败：%v", err)
	}
	page, err := cards.ListRedemptions(ctx, cardkeydomain.RedemptionQuery{AppID: appID, Source: cardkeydomain.SourceAdmin})
	if err != nil || page.Total != 1 || page.Items[0].Operator != "admin" || page.Items[0].Account != "ck-bob" {
		t.Fatalf("代兑的核销记录不对：%+v %v", page, err)
	}
	history, err := cards.MyRedemptions(ctx, appID, bob, 1, 20)
	if err != nil || history.Total < 4 || history.Items[0].BatchName != "新人券" {
		t.Fatalf("我的兑换记录不对：%+v %v", history, err)
	}

	// ── 作废与恢复 ──
	if affected, err := cards.DisableCards(ctx, appID, []int64{card.ID}, "测试"); err != nil || affected != 1 {
		t.Fatalf("作废失败：%d %v", affected, err)
	}
	if _, err := cards.PrepareLogin(ctx, appID, licenseCodes[0].Code, ""); errCode(err) != errCodeCardKeyDisabled {
		t.Fatalf("作废的授权卡不能登录，得到 %v", err)
	}
	if affected, err := cards.RestoreCards(ctx, appID, []int64{card.ID}); err != nil || affected != 1 {
		t.Fatalf("恢复失败：%d %v", affected, err)
	}
	restored, _ := cards.GetCardDetail(ctx, appID, card.ID)
	if restored.Card.Status != cardkeydomain.StatusActive {
		t.Fatalf("恢复后授权卡应回到使用中，实际 %s", restored.Card.Status)
	}

	// ── 统计（提出的 4 张里有一张随后被删了，已提取剩 3） ──
	stats, err := cards.Stats(ctx, appID, 7, "Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	var redemptions int64
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM card_key_redemptions WHERE appid = $1`, appID).Scan(&redemptions); err != nil {
		t.Fatal(err)
	}
	if stats.Redemptions != redemptions || stats.RedemptionsDay != redemptions || len(stats.Trend) != 7 ||
		stats.Trend[6].Redemptions != redemptions || stats.ActiveLicenses != 1 || stats.Distributed != 3 {
		t.Fatalf("统计不对：%+v（核销 %d）", stats, redemptions)
	}
	if !stats.FaceValueUsed.Equal(decimal.RequireFromString("9.9")) {
		t.Fatalf("已用面值应为 9.9，实际 %s", stats.FaceValueUsed)
	}

	// ── 老数据回填：只看状态与绑定的卡也要有已用次数，且重跑迁移不会把它改回去 ──
	if _, err := pool.Exec(ctx, `UPDATE card_keys SET use_count = 0 WHERE id = $1`, singleCodes[0].ID); err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../migrations/postgres/000111_card_key_rework.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := pool.Exec(context.Background(), string(migration)); err != nil {
			t.Fatalf("重跑 000111 失败：%v", err)
		}
	}
	var useCount int
	if err := pool.QueryRow(ctx, `SELECT use_count FROM card_keys WHERE id = $1`, singleCodes[0].ID).Scan(&useCount); err != nil {
		t.Fatal(err)
	}
	if useCount != 1 {
		t.Fatalf("回填后已用次数应为 1，实际 %d", useCount)
	}
	if card := codesOf(event.ID)[0]; card.UseCount != 2 {
		t.Fatalf("重跑迁移不应改动已有的计数，实际 %d", card.UseCount)
	}
}

// fakeCardKeyGuard 内存版的防爆破计数，行为与 Redis 版一致（窗口与锁定时长只记不算）。
type fakeCardKeyGuard struct {
	mu       sync.Mutex
	failures map[string]int64
	locks    map[string]time.Duration
}

func (g *fakeCardKeyGuard) key(kind string, subject string) string { return kind + ":" + subject }

func (g *fakeCardKeyGuard) IncrFailure(_ context.Context, kind string, _ int64, subject string, _ time.Duration) (int64, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.failures[g.key(kind, subject)]++
	return g.failures[g.key(kind, subject)], nil
}

func (g *fakeCardKeyGuard) ClearFailure(_ context.Context, kind string, _ int64, subject string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.failures, g.key(kind, subject))
	return nil
}

func (g *fakeCardKeyGuard) SetLock(_ context.Context, kind string, _ int64, subject string, ttl time.Duration) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.locks[g.key(kind, subject)] = ttl
	return nil
}

func (g *fakeCardKeyGuard) LockRemaining(_ context.Context, kind string, _ int64, subject string) (time.Duration, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.locks[g.key(kind, subject)], nil
}

// 只有「卡密不存在」计入输错次数；达到阈值后整个账号被锁，换一张真卡也不行。
func TestCardKeyGuardLocksAfterRepeatedMisses(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	guard := &fakeCardKeyGuard{failures: map[string]int64{}, locks: map[string]time.Duration{}}
	cards := NewCardKeyService(zap.NewNop(), pg)
	cards.SetGuard(guard)
	const appID = int64(41001)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'cardkey-guard') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	user := insertTestUser(t, ctx, pool, appID, "ck-guard", "0")
	batch, err := cards.GenerateBatch(ctx, cardkeydomain.GenerateInput{AppID: appID, Name: "真卡", Kind: cardkeydomain.KindRedeem,
		Count: 1, Rewards: []cardkeydomain.Reward{{Type: cardkeydomain.RewardIntegral, Amount: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	page, _ := cards.ListCards(ctx, cardkeydomain.CardQuery{AppID: appID, BatchID: batch.ID})
	client := CardKeyClientContext{AppID: appID, UserID: user, ClientIP: "10.0.0.1"}

	for index := range cardKeyGuardThreshold {
		if _, err := cards.Redeem(ctx, client, "FAKE-CODE-"+string(rune('A'+index))); errCode(err) != errCodeCardKeyNotFound {
			t.Fatalf("第 %d 次应报不存在，得到 %v", index+1, err)
		}
	}
	if _, err := cards.Redeem(ctx, client, page.Items[0].Code); errCode(err) != errCodeCardKeyTooManyAttempts {
		t.Fatalf("锁定后真卡也应报 42916，得到 %v", err)
	}
	if _, err := cards.Preview(ctx, client, page.Items[0].Code); errCode(err) != errCodeCardKeyTooManyAttempts {
		t.Fatalf("锁定后预览也应报 42916，得到 %v", err)
	}
}
