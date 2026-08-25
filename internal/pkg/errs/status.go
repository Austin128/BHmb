package errs

import "net/http"

// statusMap 为业务错误码到 HTTP 状态码的唯一映射来源。
// 映射结果由 testdata/status-mapping.golden 固定，改动必须同步 golden 文件。
var statusMap = map[int]int{
	CodeInvalidParam:     http.StatusBadRequest,
	CodeInvalidJSON:      http.StatusBadRequest,
	CodeNotFound:         http.StatusNotFound,
	CodeConflict:         http.StatusConflict,
	CodeInternal:         http.StatusInternalServerError,
	CodeTooManyRequests:  http.StatusTooManyRequests,
	CodeUnsupported:      http.StatusBadRequest,
	CodeNoSpace:          http.StatusInsufficientStorage,
	CodeTimeout:          http.StatusGatewayTimeout,
	CodeIdempotentReplay: http.StatusBadRequest,
	CodeResourceBusy:     http.StatusConflict,
	CodeBadPagination:    http.StatusBadRequest,
	CodeMaintenance:      http.StatusServiceUnavailable,
	CodeNeedConfirm:      http.StatusForbidden,
	CodeBodyTooLarge:     http.StatusRequestEntityTooLarge,
	CodeExecutorFailed:   http.StatusInternalServerError,

	CodeUnauthorized:        http.StatusUnauthorized,
	CodeTokenExpired:        http.StatusUnauthorized,
	CodeTokenRevoked:        http.StatusUnauthorized,
	CodeBadCredentials:      http.StatusBadRequest,
	CodeAccountLocked:       http.StatusLocked,
	CodeAccountDisabled:     http.StatusForbidden,
	CodeCaptchaRequired:     http.StatusBadRequest,
	CodeCaptchaInvalid:      http.StatusBadRequest,
	CodeNeed2FA:             http.StatusUnauthorized,
	Code2FAInvalid:          http.StatusBadRequest,
	CodeSignTimestamp:       http.StatusUnauthorized,
	CodeSignNonceReplay:     http.StatusUnauthorized,
	CodeSignInvalid:         http.StatusUnauthorized,
	CodeTokenIPDenied:       http.StatusForbidden,
	CodeTokenExpiredAK:      http.StatusForbidden,
	Code2FANotBound:         http.StatusBadRequest,
	Code2FAAlreadyBound:     http.StatusConflict,
	CodeRecoveryCodeInvalid: http.StatusBadRequest,
	CodeIPBlocked:           http.StatusForbidden,
	CodeEntryPathInvalid:    http.StatusForbidden,
	CodeRefreshInvalid:      http.StatusUnauthorized,
	CodePasswordExpired:     http.StatusConflict,
	CodeSessionKicked:       http.StatusForbidden,

	CodeUserExists:          http.StatusConflict,
	CodeWeakPassword:        http.StatusBadRequest,
	CodeForbidden:           http.StatusForbidden,
	CodeGrantEscalation:     http.StatusForbidden,
	CodeRoleInUse:           http.StatusConflict,
	CodeCannotDeleteSelf:    http.StatusBadRequest,
	CodeSuperAdminProtected: http.StatusForbidden,
	CodeRoleNotFound:        http.StatusNotFound,
	CodePermissionUnknown:   http.StatusBadRequest,
	CodeRoleCodeExists:      http.StatusConflict,
	CodeDataScopeDenied:     http.StatusForbidden,
	CodeOldPasswordWrong:    http.StatusBadRequest,
	CodeLastSuperAdmin:      http.StatusConflict,

	CodePathEscape:         http.StatusForbidden,
	CodeFileNotFound:       http.StatusNotFound,
	CodeFileExists:         http.StatusConflict,
	CodeFilePermission:     http.StatusForbidden,
	CodeFileTooLarge:       http.StatusRequestEntityTooLarge,
	CodeChunkChecksum:      http.StatusBadRequest,
	CodeUploadExpired:      http.StatusConflict,
	CodeArchiveUnsupported: http.StatusBadRequest,
	CodeArchiveCorrupt:     http.StatusBadRequest,
	CodeNotUTF8:            http.StatusBadRequest,
	CodeFileChanged:        http.StatusConflict,
	CodeProtectedPath:      http.StatusForbidden,
	CodeDiskFull:           http.StatusInsufficientStorage,

	CodeDomainExists:        http.StatusConflict,
	CodeDomainInvalid:       http.StatusBadRequest,
	CodeSiteNotFound:        http.StatusNotFound,
	CodeSiteRootConflict:    http.StatusConflict,
	CodeNginxTestFailed:     http.StatusBadRequest,
	CodePortOccupied:        http.StatusConflict,
	CodeRewriteInvalid:      http.StatusBadRequest,
	CodeSiteStopped:         http.StatusConflict,
	CodeUpstreamUnreachable: http.StatusBadRequest,
	CodeRuntimeInUse:        http.StatusConflict,
}

// Codes 返回所有已登记的错误码，供一致性校验与 golden 测试使用。
func Codes() []int {
	out := make([]int, 0, len(statusMap))
	for c := range statusMap {
		out = append(out, c)
	}
	return out
}
