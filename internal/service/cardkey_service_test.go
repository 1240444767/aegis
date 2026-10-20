package service

import (
	"strings"
	"testing"
	"time"

	cardkeydomain "aegis/internal/domain/cardkey"
	apperrors "aegis/pkg/errors"
)

// 抄卡密的人会用小写、把 `-` 打成空格或下划线。不归一化的表现是
// 「明明抄对了却提示卡密不存在」，而这是最没法自查的一类错误。
func TestNormalizeCardKeyCode(t *testing.T) {
	cases := map[string]string{
		"vip-a3k9-m2p7":   "VIP-A3K9-M2P7",
		" VIP A3K9 M2P7 ": "VIP-A3K9-M2P7",
		"VIP_A3K9_M2P7":   "VIP-A3K9-M2P7",
		"VIP-A3K9-M2P7":   "VIP-A3K9-M2P7",
		// 字符集外的字符直接丢弃：卡面上不可能有它们，出现即为抄写噪声
		"VIP-A3K9!@#": "VIP-A3K9",
		"":            "",
		// 连打的分隔符与首尾分隔符收敛掉：手抄时多按一下空格不该让卡「不存在」
		"VIP--A3K9  M2P7-": "VIP-A3K9-M2P7",
		" -VIP—A3K9－M2P7":  "VIP-A3K9-M2P7",
	}
	for input, want := range cases {
		if got := NormalizeCardKeyCode(input); got != want {
			t.Errorf("NormalizeCardKeyCode(%q) = %q，期望 %q", input, got, want)
		}
	}
}

func TestMintCodeShape(t *testing.T) {
	safe := cardkeydomain.Alphabet(cardkeydomain.CharsetSafe)
	code, err := mintCode("VIP", 4, 4, safe)
	if err != nil {
		t.Fatalf("生成失败：%v", err)
	}
	parts := strings.Split(code, "-")
	if len(parts) != 5 {
		t.Fatalf("期望 前缀 + 4 段，实际 %d 段：%s", len(parts), code)
	}
	if parts[0] != "VIP" {
		t.Fatalf("前缀丢失：%s", code)
	}
	for _, segment := range parts[1:] {
		if len(segment) != 4 {
			t.Fatalf("段长应为 4：%s", code)
		}
		for _, char := range segment {
			if !strings.ContainsRune(safe, char) {
				t.Fatalf("卡面出现字符集外的字符 %q：%s", char, code)
			}
		}
	}

	noPrefix, err := mintCode("", 2, 5, safe)
	if err != nil {
		t.Fatalf("生成失败：%v", err)
	}
	if strings.HasPrefix(noPrefix, "-") || len(strings.Split(noPrefix, "-")) != 2 {
		t.Fatalf("无前缀时不该留下多余分隔符：%s", noPrefix)
	}
}

// 字符集里不能有易混字符：卡密是要被人抄写的。
func TestCodeCharsetExcludesConfusables(t *testing.T) {
	safe := cardkeydomain.Alphabet(cardkeydomain.CharsetSafe)
	for _, char := range "IO01" {
		if strings.ContainsRune(safe, char) {
			t.Errorf("字符集不该包含易混字符 %q", char)
		}
	}
	if len(safe) != 32 {
		t.Errorf("字符集长度 = %d，期望 32", len(safe))
	}
	for _, char := range "IO" {
		if strings.ContainsRune(cardkeydomain.Alphabet(cardkeydomain.CharsetLetters), char) {
			t.Errorf("纯字母字符集不该包含易混字符 %q", char)
		}
	}
}

// 10 个数字不整除 256：直接取模会让 0–5 比 6–9 多出现约 4%。
// 拒绝采样之后每个数字的频率应当落在均值附近。
func TestMintCodeDigitsIsUniform(t *testing.T) {
	digits := cardkeydomain.Alphabet(cardkeydomain.CharsetDigits)
	counts := map[rune]int{}
	total := 0
	for range 2000 {
		code, err := mintCode("", 1, 10, digits)
		if err != nil {
			t.Fatalf("生成失败：%v", err)
		}
		for _, char := range code {
			if !strings.ContainsRune(digits, char) {
				t.Fatalf("纯数字卡面出现了 %q：%s", char, code)
			}
			counts[char]++
			total++
		}
	}
	mean := float64(total) / 10
	for _, char := range digits {
		if got := float64(counts[char]); got < mean*0.9 || got > mean*1.1 {
			t.Errorf("数字 %q 出现 %v 次，偏离均值 %v 超过 10%%", char, got, mean)
		}
	}
}

func TestNormalizeGenerateInput(t *testing.T) {
	svc := &CardKeyService{}
	future := time.Now().Add(24 * time.Hour)
	past := time.Now().Add(-24 * time.Hour)

	cases := []struct {
		name    string
		input   cardkeydomain.GenerateInput
		wantErr string
		check   func(t *testing.T, got cardkeydomain.GenerateInput)
	}{
		{
			name:    "名称必填",
			input:   cardkeydomain.GenerateInput{Kind: cardkeydomain.KindRedeem, Count: 1},
			wantErr: "批次名称不能为空",
		},
		{
			name:    "类型必须合法",
			input:   cardkeydomain.GenerateInput{Name: "x", Kind: "gift", Count: 1},
			wantErr: "卡密类型",
		},
		{
			name:    "数量上界",
			input:   cardkeydomain.GenerateInput{Name: "x", Kind: cardkeydomain.KindLogin, Count: 100000},
			wantErr: "1–10000",
		},
		{
			name: "前缀不能含符号",
			input: cardkeydomain.GenerateInput{
				Name: "x", Kind: cardkeydomain.KindLogin, Count: 1, CodePrefix: "VIP!",
			},
			wantErr: "大写字母与数字",
		},
		{
			// 前缀不受随机段那条防混淆约束：VIP 里的 I 必须可用，
			// 否则最常见的那个前缀会被判非法。
			name: "前缀可以含 I / O / 0 / 1",
			input: cardkeydomain.GenerateInput{
				Name: "x", Kind: cardkeydomain.KindLogin, Count: 1, CodePrefix: "VIP2026",
			},
		},
		{
			// 1 段 3 位只有 32768 种组合，生成 10000 张必然反复撞码。
			// 不提前算的话，失败会以「生成到一半报数据库错误」的形式出现。
			name: "格式空间装不下这个数量",
			input: cardkeydomain.GenerateInput{
				Name: "x", Kind: cardkeydomain.KindRedeem, Count: 10000,
				Segments: 1, SegmentLength: 3,
				Rewards: []cardkeydomain.Reward{{Type: cardkeydomain.RewardIntegral, Amount: 10}},
			},
			wantErr: "组合太少",
		},
		{
			name: "激活即计时必须填天数",
			input: cardkeydomain.GenerateInput{
				Name: "x", Kind: cardkeydomain.KindLogin, Count: 1,
				ValidityMode: cardkeydomain.ValidityFromFirstUse,
			},
			wantErr: "有效天数",
		},
		{
			name: "统一到期必须填时间",
			input: cardkeydomain.GenerateInput{
				Name: "x", Kind: cardkeydomain.KindLogin, Count: 1,
				ValidityMode: cardkeydomain.ValidityFixedUntil,
			},
			wantErr: "到期时间",
		},
		{
			name: "统一到期不能填过去",
			input: cardkeydomain.GenerateInput{
				Name: "x", Kind: cardkeydomain.KindLogin, Count: 1,
				ValidityMode: cardkeydomain.ValidityFixedUntil, ValidUntil: &past,
			},
			wantErr: "晚于当前时间",
		},
		{
			name: "兑换卡必须带权益",
			input: cardkeydomain.GenerateInput{
				Name: "x", Kind: cardkeydomain.KindRedeem, Count: 1,
			},
			wantErr: "至少要配置一项权益",
		},
		{
			// 授权卡本身就是权益（能登录），不配权益是合法的。
			name: "授权卡可以不带权益",
			input: cardkeydomain.GenerateInput{
				Name: "授权卡", Kind: cardkeydomain.KindLogin, Count: 10,
				ValidityMode: cardkeydomain.ValidityFromFirstUse, ValidityDays: 30,
				MaxDevices: 3,
			},
			check: func(t *testing.T, got cardkeydomain.GenerateInput) {
				if got.Segments != 4 || got.SegmentLength != 4 {
					t.Errorf("卡面格式未落到默认值：%d 段 × %d 位", got.Segments, got.SegmentLength)
				}
				if got.MaxDevices != 3 {
					t.Errorf("设备数被改写成了 %d", got.MaxDevices)
				}
			},
		},
		{
			name: "兑换卡的设备数归一成 1",
			input: cardkeydomain.GenerateInput{
				Name: "兑换卡", Kind: cardkeydomain.KindRedeem, Count: 1, MaxDevices: 9,
				Rewards: []cardkeydomain.Reward{{Type: cardkeydomain.RewardVipDays, Amount: 30}},
			},
			check: func(t *testing.T, got cardkeydomain.GenerateInput) {
				if got.MaxDevices != 1 {
					t.Errorf("兑换卡不绑设备，MaxDevices 应归一成 1，实际 %d", got.MaxDevices)
				}
			},
		},
		{
			name: "统一到期时清空天数",
			input: cardkeydomain.GenerateInput{
				Name: "x", Kind: cardkeydomain.KindLogin, Count: 1,
				ValidityMode: cardkeydomain.ValidityFixedUntil, ValidUntil: &future, ValidityDays: 30,
			},
			check: func(t *testing.T, got cardkeydomain.GenerateInput) {
				if got.ValidityDays != 0 {
					t.Errorf("统一到期模式下 ValidityDays 应清零，实际 %d", got.ValidityDays)
				}
			},
		},
		{
			// 授权卡就是一个账号：多人共用、每人限兑、仅会员这几条对它都没有意义，统一归一。
			name: "授权卡的使用次数与领取规则归一",
			input: cardkeydomain.GenerateInput{
				Name: "授权卡", Kind: cardkeydomain.KindLogin, Count: 1, MaxUses: 50, PerUserLimit: 2,
				Audience: cardkeydomain.AudienceVip, SelfUnbindLimit: 3,
			},
			check: func(t *testing.T, got cardkeydomain.GenerateInput) {
				if got.MaxUses != 1 || got.PerUserLimit != 0 || got.Audience != cardkeydomain.AudienceAll {
					t.Errorf("授权卡的规则没有归一：maxUses=%d perUser=%d audience=%s",
						got.MaxUses, got.PerUserLimit, got.Audience)
				}
				if got.SelfUnbindLimit != 3 {
					t.Errorf("授权卡的自助解绑次数被改写成了 %d", got.SelfUnbindLimit)
				}
			},
		},
		{
			name: "活动码保留使用次数，兑换卡没有自助解绑",
			input: cardkeydomain.GenerateInput{
				Name: "直播口令", Kind: cardkeydomain.KindRedeem, Count: 1, MaxUses: 500, SelfUnbindLimit: 3,
				Rewards: []cardkeydomain.Reward{{Type: cardkeydomain.RewardIntegral, Amount: 10}},
			},
			check: func(t *testing.T, got cardkeydomain.GenerateInput) {
				if got.MaxUses != 500 {
					t.Errorf("活动码的使用次数被改写成了 %d", got.MaxUses)
				}
				if got.SelfUnbindLimit != 0 {
					t.Errorf("兑换卡不绑设备，自助解绑次数应为 0，实际 %d", got.SelfUnbindLimit)
				}
				if got.Origin != cardkeydomain.OriginGenerated || got.Charset != cardkeydomain.CharsetSafe {
					t.Errorf("来源 / 字符集默认值不对：%s / %s", got.Origin, got.Charset)
				}
			},
		},
		{
			// 导入时不看数量与卡面格式：卡面是现成的。
			name: "导入不受数量约束",
			input: cardkeydomain.GenerateInput{
				Name: "迁移", Kind: cardkeydomain.KindRedeem, Codes: []string{"OLD-0001"},
				Rewards: []cardkeydomain.Reward{{Type: cardkeydomain.RewardIntegral, Amount: 10}},
			},
			check: func(t *testing.T, got cardkeydomain.GenerateInput) {
				if got.Origin != cardkeydomain.OriginImported {
					t.Errorf("有卡面时应按导入处理，实际 %s", got.Origin)
				}
			},
		},
		{
			// 纯数字 1 段 4 位只有一万种组合：五千张远超四分之一的安全线。
			name: "纯数字的格式空间按 10 个字符算",
			input: cardkeydomain.GenerateInput{
				Name: "x", Kind: cardkeydomain.KindRedeem, Count: 5000, Segments: 1, SegmentLength: 4,
				Charset: cardkeydomain.CharsetDigits,
				Rewards: []cardkeydomain.Reward{{Type: cardkeydomain.RewardIntegral, Amount: 10}},
			},
			wantErr: "组合太少",
		},
		{
			name: "领取对象必须合法",
			input: cardkeydomain.GenerateInput{
				Name: "x", Kind: cardkeydomain.KindRedeem, Count: 1, Audience: "everyone",
				Rewards: []cardkeydomain.Reward{{Type: cardkeydomain.RewardIntegral, Amount: 10}},
			},
			wantErr: "领取对象",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := tc.input
			err := svc.normalizeGenerateInput(&input)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("期望报错含 %q，实际通过", tc.wantErr)
				}
				var appErr *apperrors.AppError
				if !asAppError(err, &appErr) {
					t.Fatalf("错误应当带业务码：%v", err)
				}
				if !strings.Contains(appErr.Message, tc.wantErr) {
					t.Fatalf("错误文案不含 %q：%s", tc.wantErr, appErr.Message)
				}
				return
			}
			if err != nil {
				t.Fatalf("期望通过，实际报错：%v", err)
			}
			if tc.check != nil {
				tc.check(t, input)
			}
		})
	}
}

func TestCardKeyAccountIsDeterministic(t *testing.T) {
	// 账号名由卡面派生是「首次使用自动建号」能做到确定性的前提：
	// 换成随机名，同一张卡的两次并发首登会造出两个账号，其中一个成为孤儿。
	code := "VIP-A3K9-M2P7-QXR4-T8WN"
	if CardKeyAccount(code) != CardKeyAccount(code) {
		t.Fatal("账号派生不是确定性的")
	}
	if CardKeyAccount(code) == "" {
		t.Fatal("账号不能为空")
	}
}

func TestCardKeyNicknameStaysShort(t *testing.T) {
	// 整张卡面当昵称会让用户列表里每一行都是一长串大写字母，谁也认不出谁。
	got := cardKeyNickname("VIP-A3K9-M2P7-QXR4-T8WN")
	if strings.Contains(got, "A3K9") {
		t.Fatalf("昵称不该带上整张卡面：%s", got)
	}
	if !strings.Contains(got, "T8WN") {
		t.Fatalf("昵称应当保留末段以便区分：%s", got)
	}
}

func asAppError(err error, target **apperrors.AppError) bool {
	appErr, ok := err.(*apperrors.AppError)
	if ok {
		*target = appErr
	}
	return ok
}

func TestPrepareImportedCodesRejectsDuplicatesAndShortCodes(t *testing.T) {
	svc := &CardKeyService{}
	if _, err := svc.prepareImportedCodes(t.Context(), 1, []string{"abc-1234", "ABC 1234"}); err == nil ||
		!strings.Contains(err.Error(), "重复") {
		t.Fatalf("归一化之后相同的两张卡应当报重复，实际 %v", err)
	}
	if _, err := svc.prepareImportedCodes(t.Context(), 1, []string{"A-B"}); err == nil ||
		!strings.Contains(err.Error(), "太短") {
		t.Fatalf("太短的卡面应当被拒绝，实际 %v", err)
	}
}

func TestFillCardKeyTrendCoversEveryDay(t *testing.T) {
	since := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	got := fillCardKeyTrend([]cardkeydomain.TrendPoint{{Day: "2026-10-03", Redemptions: 5, Users: 4}}, since, 5)
	if len(got) != 5 {
		t.Fatalf("应补齐 5 天，实际 %d", len(got))
	}
	if got[0].Day != "2026-10-01" || got[2].Redemptions != 5 || got[4].Day != "2026-10-05" {
		t.Fatalf("补齐结果不对：%+v", got)
	}
}
