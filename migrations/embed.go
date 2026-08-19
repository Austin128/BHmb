// Package migrations 通过 embed 暴露迁移脚本，使二进制自带全部 SQL，
// 无需随包分发 migrations 目录。
package migrations

import "embed"

// FS 包含根目录下的 *.sql 与各方言覆盖目录 dialect/<driver>/*.sql。
//
//go:embed *.sql dialect
var FS embed.FS
