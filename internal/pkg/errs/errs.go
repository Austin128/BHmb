// Package errs 提供全项目统一的错误类型与错误码。
// 约定：跨层传递的错误必须是 *Error；构造只能通过 New / Wrap / Newf。
package errs

import (
	"errors"
	"fmt"
)

// Error 是全项目统一错误类型。
// Code 为 6 位业务错误码；Message 面向最终用户（可直接展示）；
// Detail 面向排查（仅写日志与管理员可见）；Fields 为结构化上下文。
type Error struct {
	Code    int
	Message string
	Detail  string
	Fields  map[string]any
	cause   error
}

func (e *Error) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("code=%d msg=%s detail=%s cause=%v", e.Code, e.Message, e.Detail, e.cause)
	}
	return fmt.Sprintf("code=%d msg=%s detail=%s", e.Code, e.Message, e.Detail)
}

func (e *Error) Unwrap() error { return e.cause }

// Is 支持按错误码比较：errors.Is(err, errs.ErrNotFound)。
func (e *Error) Is(target error) bool {
	var t *Error
	if errors.As(target, &t) {
		return e.Code == t.Code
	}
	return false
}

// WithDetail 补充排查信息，返回副本，不修改原错误。
func (e *Error) WithDetail(format string, args ...any) *Error {
	c := *e
	c.Detail = fmt.Sprintf(format, args...)
	return &c
}

// WithField 附加结构化上下文，返回副本。
func (e *Error) WithField(key string, val any) *Error {
	c := *e
	c.Fields = make(map[string]any, len(e.Fields)+1)
	for k, v := range e.Fields {
		c.Fields[k] = v
	}
	c.Fields[key] = val
	return &c
}

// WithCause 绑定底层错误，返回副本，用于哨兵错误补充 cause。
func (e *Error) WithCause(cause error) *Error {
	c := *e
	c.cause = cause
	return &c
}
