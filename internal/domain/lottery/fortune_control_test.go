package lottery

import (
	"math"
	"testing"
	"time"
)

func ptr64(v int64) *int64 { return &v }

// 干预的合成：重抽换 z、个人定档优先于全站定档、偏移只在没有定档时累加、截在 ±2、过期与撤销不算。
func TestResolveFortune(t *testing.T) {
	fortune := &Fortune{Spread: 0.3}
	day := time.Date(2026, 10, 28, 0, 0, 0, 0, time.FixedZone("CST", 8*3600))
	now := day.Add(10 * time.Hour)
	window := func(c FortuneControl) FortuneControl {
		c.StartsAt, c.EndsAt = day, day.AddDate(0, 0, 1)
		return c
	}
	base := ResolveFortune(fortune, 3, 42, day, now, nil)
	if base.Controlled() || base.Z != base.BaseZ || base.Factor != FortuneOf(fortune, 3, 42, day) {
		t.Fatalf("没有干预时应与原来的系数一致，得到 %+v", base)
	}
	if ReadFortuneControlled(fortune, 3, 42, day, now, nil).Factor != ReadFortune(fortune, 3, 42, day).Factor {
		t.Fatal("没有干预时的解读应与原来一致")
	}

	// 重抽：z 变了，原值不变；同一段盐结果稳定
	reroll := window(FortuneControl{ID: 1, UserID: ptr64(42), Kind: ControlReroll, Salt: "abc"})
	one := ResolveFortune(fortune, 3, 42, day, now, []FortuneControl{reroll})
	two := ResolveFortune(fortune, 3, 42, day, now, []FortuneControl{reroll})
	if one.Z == base.Z || one.BaseZ != base.BaseZ || one.Z != two.Z || len(one.Applied) != 1 {
		t.Fatalf("重抽应换一个稳定的新 z，得到 %+v / %+v", one, two)
	}
	// 别人的干预不影响他
	other := window(FortuneControl{ID: 2, UserID: ptr64(7), Kind: ControlPin, Z: 1.75})
	if got := ResolveFortune(fortune, 3, 42, day, now, []FortuneControl{other}); got.Controlled() {
		t.Fatalf("别人的干预不应作用在他身上，得到 %+v", got)
	}

	// 定档：个人优先于全站；定档时偏移不累加
	globalPin := window(FortuneControl{ID: 3, Kind: ControlPin, Z: PinZ(FortuneQuiet)})
	userPin := window(FortuneControl{ID: 4, UserID: ptr64(42), Kind: ControlPin, Z: PinZ(FortuneBurst)})
	shift := window(FortuneControl{ID: 5, Kind: ControlShift, Z: 1})
	got := ResolveFortune(fortune, 3, 42, day, now, []FortuneControl{globalPin, userPin, shift})
	if got.Z != PinZ(FortuneBurst) || LevelOfZ(got.Z) != FortuneBurst {
		t.Fatalf("个人定档应优先，得到 z=%v", got.Z)
	}
	if got := ResolveFortune(fortune, 3, 42, day, now, []FortuneControl{globalPin, shift}); got.Z != PinZ(FortuneQuiet) {
		t.Fatalf("全站定档时偏移不应累加，得到 z=%v", got.Z)
	}

	// 偏移：累加并截在 ±2
	big := window(FortuneControl{ID: 6, Kind: ControlShift, Z: 4})
	if got := ResolveFortune(fortune, 3, 42, day, now, []FortuneControl{big}); got.Z != 2 {
		t.Fatalf("偏移后应截在 2，得到 %v", got.Z)
	}
	if got := ResolveFortune(fortune, 3, 42, day, now, []FortuneControl{shift}); math.Abs(got.Z-math.Min(2, base.Z+1)) > 1e-9 {
		t.Fatalf("偏移 +1 应得 %v，得到 %v", base.Z+1, got.Z)
	}
	_, high := FortuneRange(fortune)
	if got := ResolveFortune(fortune, 3, 42, day, now, []FortuneControl{big}); got.Factor != high {
		t.Fatalf("截到 +2 时系数应是范围上限 %v，得到 %v", high, got.Factor)
	}

	// 过期、未开始、已撤销的都不算
	expired := FortuneControl{ID: 7, Kind: ControlShift, Z: 1, StartsAt: day.Add(-48 * time.Hour), EndsAt: day}
	later := FortuneControl{ID: 8, Kind: ControlShift, Z: 1, StartsAt: now.Add(time.Hour), EndsAt: now.Add(2 * time.Hour)}
	revokedAt := now
	revoked := window(FortuneControl{ID: 9, Kind: ControlShift, Z: 1, Revoked: &revokedAt})
	if got := ResolveFortune(fortune, 3, 42, day, now, []FortuneControl{expired, later, revoked}); got.Controlled() {
		t.Fatalf("不在生效期的干预不应生效，得到 %+v", got)
	}
	if later.StatusAt(now) != ControlScheduled || expired.StatusAt(now) != ControlExpired || revoked.StatusAt(now) != ControlRevoked {
		t.Fatal("干预状态判断不对")
	}
	if ResolveFortune(&Fortune{}, 3, 42, day, now, []FortuneControl{userPin}) != nil {
		t.Fatal("没开每日运气时不应有运气")
	}
}

// 校验与默认值：默认只管今天、定档按档位或系数、偏移有上下限、没开运气不能干预。
func TestNormalizeControl(t *testing.T) {
	day := time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)
	now := day.Add(9 * time.Hour)
	ok := ControlInput{UserIDs: []int64{5, 5, 6}, Kind: ControlPin, Level: FortuneBurst}
	input, z, err := NormalizeControl(ok, 0.3, now, day)
	if err != nil || z != 1.75 || len(input.UserIDs) != 2 || !input.StartsAt.Equal(now) || !input.EndsAt.Equal(day.AddDate(0, 0, 1)) {
		t.Fatalf("默认今天、按档位定 z、用户去重，得到 %+v z=%v err=%v", input, z, err)
	}
	_, z, err = NormalizeControl(ControlInput{AllUsers: true, Kind: ControlPin, Factor: 1.2}, 0.3, now, day)
	if err != nil || math.Abs(math.Exp(0.3*z-0.045)-1.2) > 1e-9 {
		t.Fatalf("按系数反算 z，得到 z=%v err=%v", z, err)
	}
	bad := []ControlInput{
		{Kind: ControlReroll},
		{AllUsers: true, UserIDs: []int64{1}, Kind: ControlReroll},
		{AllUsers: true, Kind: "boost"},
		{AllUsers: true, Kind: ControlPin, Level: "mythic"},
		{AllUsers: true, Kind: ControlPin, Factor: 9},
		{AllUsers: true, Kind: ControlShift, Shift: 5},
		{AllUsers: true, Kind: ControlShift},
		{AllUsers: true, Kind: ControlReroll, EndsAt: now.Add(-time.Hour)},
		{AllUsers: true, Kind: ControlReroll, StartsAt: now, EndsAt: now.AddDate(0, 0, 100)},
	}
	for _, item := range bad {
		if _, _, err := NormalizeControl(item, 0.3, now, day); err == nil {
			t.Fatalf("应拒绝 %+v", item)
		}
	}
	if _, _, err := NormalizeControl(ControlInput{AllUsers: true, Kind: ControlReroll}, 0, now, day); err == nil {
		t.Fatal("没开每日运气时应拒绝干预")
	}
}
