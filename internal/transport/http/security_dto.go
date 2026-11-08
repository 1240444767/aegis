package httptransport

import "encoding/json"

type SecondFactorVerifyRequest struct {
	ChallengeID  string `json:"challengeId" form:"challengeId" binding:"required"`
	Code         string `json:"code" form:"code"`
	RecoveryCode string `json:"recoveryCode" form:"recoveryCode"`
}

type TOTPEnableRequest struct {
	EnrollmentID string `json:"enrollmentId" form:"enrollmentId" binding:"required"`
	Code         string `json:"code" form:"code" binding:"required"`
}

type TOTPDisableRequest struct {
	Code         string `json:"code" form:"code"`
	RecoveryCode string `json:"recoveryCode" form:"recoveryCode"`
}

type RecoveryCodesRegenerateRequest struct {
	Code         string `json:"code" form:"code"`
	RecoveryCode string `json:"recoveryCode" form:"recoveryCode"`
}

type PasskeyRegistrationFinishRequest struct {
	ChallengeID    string          `json:"challengeId" form:"challengeId" binding:"required"`
	Credential     json.RawMessage `json:"credential"`
	Payload        json.RawMessage `json:"payload"`
	CredentialName string          `json:"credentialName" form:"credentialName"`
}

type PasskeyLoginBeginRequest struct {
	AppID    int64  `json:"appid" form:"appid" binding:"required"`
	DeviceID string `json:"deviceId" form:"deviceId"`
	Device   string `json:"device" form:"device"`
	MarkCode string `json:"markcode" form:"markcode"` // Deprecated
}

type PasskeyLoginVerifyRequest struct {
	AppID       int64  `json:"appid" form:"appid" binding:"required"`
	ChallengeID string `json:"challengeId" form:"challengeId"`
	// SessionID 与 ChallengeID 同义：options 返回的是 session.challengeId，
	// 网关文档与 SDK 一直叫它 sessionId。两个都收，免得一边改名另一边全挂。
	SessionID  string          `json:"sessionId" form:"sessionId"`
	Credential json.RawMessage `json:"credential"`
	Payload    json.RawMessage `json:"payload"`
	DeviceID   string          `json:"deviceId" form:"deviceId"`
	Device     string          `json:"device" form:"device"`
	MarkCode   string          `json:"markcode" form:"markcode"` // Deprecated
}
