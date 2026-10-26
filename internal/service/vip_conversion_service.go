package service

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	authdomain "aegis/internal/domain/auth"
	vipdomain "aegis/internal/domain/vip"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
)

// 会员权益的统一折算（规则见 internal/domain/vip/conversion.go，文档 docs/vip-conversion.md）。
//
// 折算本身发生在发放事务里（仓储层），这里只有：策略的读写、折算记录的查询、
// 发放前的预览（管理员发放表单、用户端永久套餐卡片）。

// AdminConversionPolicy 应用的折算策略（没配过时为默认策略）。
func (s *VipService) AdminConversionPolicy(ctx context.Context, appID int64) (vipdomain.ConversionPolicy, error) {
	return s.pg.GetVipConversionPolicy(ctx, appID)
}

// AdminSaveConversionPolicy 整体覆盖保存折算策略。
func (s *VipService) AdminSaveConversionPolicy(ctx context.Context, policy vipdomain.ConversionPolicy) (vipdomain.ConversionPolicy, error) {
	policy.Owned = strings.TrimSpace(policy.Owned)
	policy.Upgrade = strings.TrimSpace(policy.Upgrade)
	if err := policy.Validate(); err != nil {
		return policy, apperrors.New(40000, http.StatusBadRequest, err.Error())
	}
	return s.pg.SaveVipConversionPolicy(ctx, policy)
}

// AdminListConversions 折算记录（userID 为 0 查全应用）。
func (s *VipService) AdminListConversions(ctx context.Context, appID, userID int64, page, limit int) ([]vipdomain.Conversion, int64, error) {
	page, limit = normalizePageLimit(page, limit, 100)
	return s.pg.ListVipConversions(ctx, appID, userID, page, limit)
}

// MyConversions 当前用户的折算记录。
func (s *VipService) MyConversions(ctx context.Context, session *authdomain.Session, page, limit int) ([]vipdomain.Conversion, int64, error) {
	if session == nil {
		return nil, 0, apperrors.New(40170, http.StatusUnauthorized, "未认证")
	}
	page, limit = normalizePageLimit(page, limit, 100)
	return s.pg.ListVipConversions(ctx, session.AppID, session.UserID, page, limit)
}

// previewUpgrade 假如此刻开通一笔带这些功能的永久会员，存量会怎样。mode 为空按应用策略。
func (s *VipService) previewUpgrade(ctx context.Context, appID, userID int64, features []string, mode string) (*vipdomain.UpgradePreview, error) {
	plan, policy, err := s.pg.PreviewLifetimeUpgrade(ctx, appID, userID, features)
	if err != nil {
		return nil, err
	}
	if mode == "" {
		mode = policy.Upgrade
	}
	return plan.Preview(mode), nil
}

// GrantPreview 管理员发放前的预览：这一笔会被折算，还是会折掉存量。
type GrantPreview struct {
	Holdings vipdomain.Holdings         `json:"holdings"`
	Policy   vipdomain.ConversionPolicy `json:"policy"`
	// Owned 这一笔对他已是空的、将按策略折成积分（照常发放时为空）
	Owned *GrantOwnedPreview `json:"owned,omitempty"`
	// Upgrade 发的是永久会员时，存量会怎样（没有存量时为空）
	Upgrade *vipdomain.UpgradePreview `json:"upgrade,omitempty"`
}

// GrantOwnedPreview 已拥有发放的预览。
type GrantOwnedPreview struct {
	// Covered 这一笔对他是空的（永久会员已包含）
	Covered bool `json:"covered"`
	// Converted 将按策略折成积分；false 表示照常发放（管理员选了照常发放，或应用策略是照常发放）
	Converted bool                     `json:"converted"`
	Item      vipdomain.ConversionItem `json:"item"`
	Message   string                   `json:"message"`
}

// AdminPreviewGrant 管理员发放前的预览（不落账）。
func (s *VipService) AdminPreviewGrant(ctx context.Context, in AdminVipGrantInput) (*GrantPreview, error) {
	grant, err := s.buildAdminGrant(ctx, in)
	if err != nil {
		return nil, err
	}
	holdings, err := s.pg.GetVipHoldings(ctx, in.AppID, in.UserID)
	if err != nil {
		if errors.Is(err, pgrepo.ErrUserNotFound) {
			return nil, apperrors.New(40401, http.StatusNotFound, "用户不存在")
		}
		return nil, err
	}
	policy, err := s.pg.GetVipConversionPolicy(ctx, in.AppID)
	if err != nil {
		return nil, err
	}
	preview := &GrantPreview{Holdings: holdings, Policy: policy}

	if holdings.CoversMembership(grant.Features) {
		label := grant.PlanName
		item := policy.OwnedVipItem(label, grant.DurationDays, grant.Lifetime, grant.Features)
		converted := grant.ConvertOwned && policy.Owned == vipdomain.OwnedConvert
		owned := &GrantOwnedPreview{Covered: true, Converted: converted, Item: item}
		if converted {
			owned.Message = "该用户已是永久会员并已包含这些权益，本次发放将折算为 " + strconv.FormatInt(item.Points, 10) + " 积分"
		} else {
			owned.Message = "该用户已是永久会员并已包含这些权益，照常发放不会带来新的权益"
		}
		preview.Owned = owned
		if converted {
			return preview, nil
		}
	}
	if grant.Lifetime {
		upgrade, err := s.previewUpgrade(ctx, in.AppID, in.UserID, grant.Features, grant.UpgradeMode)
		if err != nil {
			return nil, err
		}
		preview.Upgrade = upgrade
	}
	return preview, nil
}
