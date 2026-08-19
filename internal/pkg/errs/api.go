package errs

import (
	"errors"
	"fmt"
	"net/http"
)

// New 用错误码与用户可见消息构造错误。
func New(code int, message string) *Error {
	return &Error{Code: code, Message: message}
}

// Newf 同 New，message 支持格式化。
func Newf(code int, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// Wrap 包装底层错误并赋予业务错误码。cause 为 nil 时等价于 New。
func Wrap(cause error, code int, message string) *Error {
	if cause == nil {
		return New(code, message)
	}
	var e *Error
	if errors.As(cause, &e) && e.Code == code {
		return e // 同码不重复包装，避免栈式嵌套
	}
	return &Error{Code: code, Message: message, Detail: cause.Error(), cause: cause}
}

// Wrapf 同 Wrap，message 支持格式化。
func Wrapf(cause error, code int, format string, args ...any) *Error {
	return Wrap(cause, code, fmt.Sprintf(format, args...))
}

// Code 提取业务错误码；非 *Error 返回 CodeInternal；nil 返回 0。
func Code(err error) int {
	if err == nil {
		return 0
	}
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return CodeInternal
}

// Message 提取用户可见消息；非 *Error 返回通用兜底文案。
func Message(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Message
	}
	return "服务内部错误"
}

// HTTPStatus 将业务错误码映射为 HTTP 状态码。
func HTTPStatus(code int) int {
	if code == 0 {
		return http.StatusOK
	}
	if s, ok := statusMap[code]; ok {
		return s
	}
	switch code / 10000 { // 按模块兜底
	case 11, 12:
		return http.StatusForbidden
	default:
		return http.StatusBadRequest
	}
}

// As 提取 *Error，便于日志层读取 Detail 与 Fields。
func As(err error) (*Error, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}

// 预定义哨兵错误，供 errors.Is 比较。常量名与 05 号文档错误码表一致。
var (
	ErrInvalidParam    = New(CodeInvalidParam, "请求参数有误")
	ErrInvalidJSON     = New(CodeInvalidJSON, "请求体格式错误")
	ErrNotFound        = New(CodeNotFound, "资源不存在")
	ErrConflict        = New(CodeConflict, "资源已存在或状态冲突")
	ErrInternal        = New(CodeInternal, "服务内部错误")
	ErrTooManyRequests = New(CodeTooManyRequests, "请求过于频繁")
	ErrUnsupported     = New(CodeUnsupported, "当前环境不支持该操作")
	ErrTimeout         = New(CodeTimeout, "操作超时")
	ErrResourceBusy    = New(CodeResourceBusy, "资源正被其他操作占用")

	ErrUnauthorized    = New(CodeUnauthorized, "未登录或登录已过期")
	ErrTokenExpired    = New(CodeTokenExpired, "登录已过期")
	ErrTokenRevoked    = New(CodeTokenRevoked, "登录已失效，请重新登录")
	ErrBadCredentials  = New(CodeBadCredentials, "用户名或密码错误")
	ErrAccountLocked   = New(CodeAccountLocked, "账号已锁定，请稍后再试")
	ErrAccountDisabled = New(CodeAccountDisabled, "账号已停用")
	ErrNeed2FA         = New(CodeNeed2FA, "需要二次验证")
	ErrRefreshInvalid  = New(CodeRefreshInvalid, "刷新凭证无效")
	ErrSessionKicked   = New(CodeSessionKicked, "会话已在其他位置登出")

	ErrWeakPassword      = New(CodeWeakPassword, "密码强度不足")
	ErrForbidden         = New(CodeForbidden, "没有该操作权限")
	ErrPermissionUnknown = New(CodePermissionUnknown, "权限点不存在")
	ErrOldPasswordWrong  = New(CodeOldPasswordWrong, "原密码不正确")
	ErrUserExists        = New(CodeUserExists, "用户名已存在")
)
