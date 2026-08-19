// Package security 提供口令哈希、访问令牌签发校验、刷新令牌生成与吊销名单。
// 本包不访问数据库，所有持久化由 repository 完成。
package security

import (
	"crypto/rand"
	"crypto/sha256"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/crypto/hkdf"

	"github.com/novapanel/novapanel/internal/pkg/errs"
)

// masterKeyLen 为主密钥长度（字节）。
const masterKeyLen = 32

// LoadOrCreateMasterKey 读取主密钥文件；不存在时生成 32 字节随机密钥并以 0600 落盘。
// 主密钥是敏感字段加密与令牌签名密钥派生的根，禁止写入日志或响应。
func LoadOrCreateMasterKey(path string) ([]byte, error) {
	if path == "" {
		return nil, errs.New(errs.CodeInvalidParam, "security.master_key_file 不能为空")
	}
	b, err := os.ReadFile(path) // #nosec G304 -- 路径来自受信配置
	switch {
	case err == nil:
		if len(b) < masterKeyLen {
			return nil, errs.Newf(errs.CodeInvalidParam, "主密钥长度不足 %d 字节：%s", masterKeyLen, path)
		}
		return b[:masterKeyLen], nil
	case !os.IsNotExist(err):
		return nil, errs.Wrapf(err, errs.CodeInternal, "主密钥读取失败：%s", path)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, errs.Wrap(err, errs.CodeInternal, "主密钥目录创建失败")
	}
	key := make([]byte, masterKeyLen)
	if _, err := rand.Read(key); err != nil {
		return nil, errs.Wrap(err, errs.CodeInternal, "主密钥生成失败")
	}
	if err := os.WriteFile(path, key, 0o600); err != nil {
		return nil, errs.Wrapf(err, errs.CodeInternal, "主密钥写入失败：%s", path)
	}
	return key, nil
}

// DeriveKey 用 HKDF-SHA256 从主密钥派生用途隔离的子密钥，
// 使令牌签名密钥与数据加密密钥互不复用。
func DeriveKey(master []byte, info string, size int) ([]byte, error) {
	if len(master) == 0 {
		return nil, errs.New(errs.CodeInvalidParam, "主密钥为空")
	}
	if size <= 0 {
		size = masterKeyLen
	}
	out := make([]byte, size)
	r := hkdf.New(sha256.New, master, nil, []byte(info))
	if _, err := io.ReadFull(r, out); err != nil {
		return nil, errs.Wrap(err, errs.CodeInternal, "子密钥派生失败")
	}
	return out, nil
}
