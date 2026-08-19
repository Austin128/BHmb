package errs

// 错误码结构为 6 位数字 MMSSNN：MM 模块号，SS 资源号（00 为模块通用），NN 序号。
// 数值与文案以 docs/05-API接口规范.md 5.5 为唯一来源；新增错误码必须同时改动
// 本文件常量、statusMap（status.go）与 05 号文档表格，由 scripts/check-errcode.sh 校验。
const (
	// 通用 10xxxx
	CodeInvalidParam     = 100001
	CodeInvalidJSON      = 100002
	CodeNotFound         = 100003
	CodeConflict         = 100004
	CodeInternal         = 100005
	CodeTooManyRequests  = 100006
	CodeUnsupported      = 100007
	CodeNoSpace          = 100008
	CodeTimeout          = 100009
	CodeIdempotentReplay = 100010
	CodeResourceBusy     = 100011
	CodeBadPagination    = 100012
	CodeMaintenance      = 100013
	CodeNeedConfirm      = 100014
	CodeBodyTooLarge     = 100015
	CodeExecutorFailed   = 100016

	// 认证 11xxxx
	CodeUnauthorized        = 110001
	CodeTokenExpired        = 110002
	CodeTokenRevoked        = 110003
	CodeBadCredentials      = 110004
	CodeAccountLocked       = 110005
	CodeAccountDisabled     = 110006
	CodeCaptchaRequired     = 110007
	CodeCaptchaInvalid      = 110008
	CodeNeed2FA             = 110009
	Code2FAInvalid          = 110010
	CodeSignTimestamp       = 110011
	CodeSignNonceReplay     = 110012
	CodeSignInvalid         = 110013
	CodeTokenIPDenied       = 110014
	CodeTokenExpiredAK      = 110015
	Code2FANotBound         = 110016
	Code2FAAlreadyBound     = 110017
	CodeRecoveryCodeInvalid = 110018
	CodeIPBlocked           = 110019
	CodeEntryPathInvalid    = 110020
	CodeRefreshInvalid      = 110021
	CodePasswordExpired     = 110022
	CodeSessionKicked       = 110023

	// 用户与权限 12xxxx
	CodeUserExists          = 120001
	CodeWeakPassword        = 120002
	CodeForbidden           = 120003
	CodeGrantEscalation     = 120004
	CodeRoleInUse           = 120005
	CodeCannotDeleteSelf    = 120006
	CodeSuperAdminProtected = 120007
	CodeRoleNotFound        = 120008
	CodePermissionUnknown   = 120009
	CodeRoleCodeExists      = 120010
	CodeDataScopeDenied     = 120011
	CodeOldPasswordWrong    = 120012
	CodeLastSuperAdmin      = 120013
)
