package service

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"

	"aegis/internal/domain/featuretime"
	vipdomain "aegis/internal/domain/vip"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
)

// 功能时长的业务错误码（见 docs/feature-time.md）。
const (
	errCodeFeatureTimeExhausted     = 40328 // 403 余额为 0，租不出来
	errCodeFeatureTimeLeaseNotFound = 40406 // 404 租约不存在或不属于当前用户
)

// FeatureTimeService 功能时长：余额查询、租约与结算（概念见 internal/domain/featuretime）。
//
// 发放不在这里：看激励广告领「功能时长」与其它权益同事务发放（grantAdRewardsTx）。
// 这里只管用：先判免扣（会员或已拥有该功能），不免扣时才去余额里预扣。
type FeatureTimeService struct {
	log *zap.Logger
	pg  *pgrepo.Repository
}

func NewFeatureTimeService(log *zap.Logger, pg *pgrepo.Repository) *FeatureTimeService {
	if log == nil {
		log = zap.NewNop()
	}
	return &FeatureTimeService{log: log, pg: pg}
}

// Status 当前用户在一个功能上的时长：余额、累计获得与实际用掉的秒数、是否免扣。
//
// 功能不要求登记在功能目录里：没有发放过的功能余额就是 0，name 为空串。
func (s *FeatureTimeService) Status(ctx context.Context, appID, userID int64, feature string) (*featuretime.Status, error) {
	feature, err := normalizeFeatureTimeTag(feature)
	if err != nil {
		return nil, err
	}
	exempt, err := s.exempt(ctx, appID, userID, feature)
	if err != nil {
		return nil, err
	}
	account, err := s.pg.GetFeatureTimeAccount(ctx, appID, userID, feature)
	if err != nil {
		return nil, err
	}
	return &featuretime.Status{
		Feature:        feature,
		Name:           s.featureName(ctx, appID, feature),
		BalanceSeconds: account.BalanceSeconds,
		Exempt:         exempt,
		EarnedSeconds:  account.EarnedSeconds,
		UsedSeconds:    account.UsedSeconds,
	}, nil
}

// Lease 租一段时长（开始使用前调用，用完再租下一段）。
//
// 免扣时不扣余额、不建租约，返回 exempt=true；余额为 0 报 40328；
// 否则扣 min(要的秒数, 余额)，返回租约号与到期时间。
func (s *FeatureTimeService) Lease(ctx context.Context, appID, userID int64, feature string, seconds int) (*featuretime.Lease, error) {
	feature, err := normalizeFeatureTimeTag(feature)
	if err != nil {
		return nil, err
	}
	seconds, err = featuretime.ClampLeaseSeconds(seconds)
	if err != nil {
		return nil, apperrors.New(40000, http.StatusBadRequest, err.Error())
	}
	exempt, err := s.exempt(ctx, appID, userID, feature)
	if err != nil {
		return nil, err
	}
	if exempt {
		account, err := s.pg.GetFeatureTimeAccount(ctx, appID, userID, feature)
		if err != nil {
			return nil, err
		}
		return &featuretime.Lease{Exempt: true, BalanceSeconds: account.BalanceSeconds}, nil
	}
	lease, err := s.pg.LeaseFeatureTime(ctx, featuretime.LeaseInput{
		AppID: appID, UserID: userID, Feature: feature, Seconds: seconds, Now: time.Now(),
	})
	if err != nil {
		return nil, s.translate(err)
	}
	return lease, nil
}

// Settle 结算一个租约（停止使用时调用），没用完的退回余额。
//
// usedSeconds 夹到 [0, 租约秒数]；重复结算原样返回第一次的结论；过期的租约不退款。
func (s *FeatureTimeService) Settle(ctx context.Context, appID, userID int64, leaseNo string, usedSeconds int) (*featuretime.Settlement, error) {
	leaseNo = strings.TrimSpace(leaseNo)
	if leaseNo == "" || len(leaseNo) > 40 {
		return nil, s.translate(pgrepo.ErrFeatureTimeLeaseNotFound)
	}
	settlement, err := s.pg.SettleFeatureTimeLease(ctx, featuretime.SettleInput{
		AppID: appID, UserID: userID, LeaseNo: leaseNo, UsedSeconds: usedSeconds, Now: time.Now(),
	})
	if err != nil {
		return nil, s.translate(err)
	}
	return settlement, nil
}

// exempt 免扣判定：会员，或权益里带这个功能（含功能解锁）。判据见 featuretime.Exempt。
func (s *FeatureTimeService) exempt(ctx context.Context, appID, userID int64, feature string) (bool, error) {
	facts, err := s.pg.GetVipEntitlementFacts(ctx, appID, userID)
	if err != nil {
		if errors.Is(err, pgrepo.ErrUserNotFound) {
			return false, apperrors.New(40401, http.StatusNotFound, "用户不存在")
		}
		return false, err
	}
	return featuretime.Exempt(vipdomain.Evaluate(*facts, time.Now()), feature), nil
}

// featureName 功能目录里的展示名；目录里没有（或查不出来）时为空串。
func (s *FeatureTimeService) featureName(ctx context.Context, appID int64, feature string) string {
	item, err := s.pg.GetVipFeature(ctx, appID, feature)
	if err != nil || item == nil {
		return ""
	}
	return item.Name
}

func normalizeFeatureTimeTag(feature string) (string, error) {
	tag, err := featuretime.NormalizeFeature(feature)
	if err != nil {
		return "", apperrors.New(40000, http.StatusBadRequest, err.Error())
	}
	return tag, nil
}

func (s *FeatureTimeService) translate(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, pgrepo.ErrFeatureTimeExhausted):
		return apperrors.New(errCodeFeatureTimeExhausted, http.StatusForbidden, "使用时长已用完")
	case errors.Is(err, pgrepo.ErrFeatureTimeLeaseNotFound):
		return apperrors.New(errCodeFeatureTimeLeaseNotFound, http.StatusNotFound, "租约不存在")
	case pgrepo.IsUniqueViolation(err):
		// 租约号撞车（同一秒内的六位随机数相同）：整笔已回滚，客户端重试即可
		s.log.Warn("feature time lease number collided", zap.Error(err))
		return apperrors.New(50000, http.StatusInternalServerError, "租用失败，请重试")
	}
	return err
}
