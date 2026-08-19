// Package migrate 实现基于 SQL 文件的迁移器：版本有序、up/down 成对、
// 校验和防篡改、库级锁防并发，支持 sqlite / mysql / postgres 三方言。
package migrate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/novapanel/novapanel/internal/pkg/errs"
	"github.com/novapanel/novapanel/internal/pkg/logx"
)

// 迁移记录表名。该表由迁移器自身维护，不出现在迁移脚本中。
const tableName = "sys_schema_migration"

// lockKey 为库级迁移锁的名称。
const lockKey = "novapanel_migrate"

// fileRe 匹配 <14 位版本>_<描述>.<up|down>.sql。
var fileRe = regexp.MustCompile(`^([0-9]{14})_([a-z0-9_]+)\.(up|down)\.sql$`)

// Migration 描述一个版本的迁移脚本。
type Migration struct {
	Version  int64
	Name     string
	UpPath   string
	DownPath string
}

// Record 为迁移记录表中的一行。
type Record struct {
	Version    int64     `gorm:"column:version"`
	Name       string    `gorm:"column:name"`
	AppliedAt  time.Time `gorm:"column:applied_at"`
	Checksum   string    `gorm:"column:checksum"`
	DurationMs int64     `gorm:"column:duration_ms"`
}

// Status 汇总脚本与数据库记录的比对结果。
type Status struct {
	Version   int64
	Name      string
	Applied   bool
	AppliedAt time.Time
	Dirty     bool // 已应用但校验和与当前脚本不一致
}

// Migrator 执行迁移。fsys 通常为 migrations.FS。
type Migrator struct {
	db      *gorm.DB
	fsys    fs.FS
	dialect string
}

// New 创建迁移器，dialect 取 sqlite / mysql / postgres。
func New(db *gorm.DB, fsys fs.FS, dialect string) *Migrator {
	return &Migrator{db: db, fsys: fsys, dialect: dialect}
}

// Up 应用全部未执行的迁移，返回本次应用的版本列表。
func (m *Migrator) Up(ctx context.Context) ([]Migration, error) {
	migrations, err := m.load()
	if err != nil {
		return nil, err
	}
	if err := m.ensureTable(ctx); err != nil {
		return nil, err
	}

	unlock, err := m.lock(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()

	applied, err := m.records(ctx)
	if err != nil {
		return nil, err
	}
	if err := m.verify(migrations, applied); err != nil {
		return nil, err
	}

	var done []Migration
	for _, mg := range migrations {
		if _, ok := applied[mg.Version]; ok {
			continue
		}
		sqlText, checksum, err := m.read(mg.UpPath)
		if err != nil {
			return nil, err
		}
		start := time.Now()
		if err := m.execTx(ctx, sqlText, func(tx *gorm.DB) error {
			return tx.WithContext(ctx).Exec(
				fmt.Sprintf("INSERT INTO %s (version, name, applied_at, checksum, duration_ms) VALUES (?, ?, ?, ?, ?)", tableName),
				mg.Version, mg.Name, time.Now().UTC(), checksum, time.Since(start).Milliseconds(),
			).Error
		}); err != nil {
			return done, errs.Wrap(err, errs.CodeInternal, "迁移执行失败").
				WithField("version", mg.Version).WithField("name", mg.Name)
		}
		logx.FromContext(ctx).Info("迁移已应用", "module", "migrate",
			"version", mg.Version, "name", mg.Name, "costMs", time.Since(start).Milliseconds())
		done = append(done, mg)
	}
	return done, nil
}

// Down 回滚最近 steps 个已应用迁移；steps <= 0 时回滚全部。
func (m *Migrator) Down(ctx context.Context, steps int) ([]Migration, error) {
	migrations, err := m.load()
	if err != nil {
		return nil, err
	}
	if err := m.ensureTable(ctx); err != nil {
		return nil, err
	}

	unlock, err := m.lock(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()

	applied, err := m.records(ctx)
	if err != nil {
		return nil, err
	}

	byVersion := make(map[int64]Migration, len(migrations))
	for _, mg := range migrations {
		byVersion[mg.Version] = mg
	}

	versions := make([]int64, 0, len(applied))
	for v := range applied {
		versions = append(versions, v)
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i] > versions[j] })
	if steps > 0 && steps < len(versions) {
		versions = versions[:steps]
	}

	var done []Migration
	for _, v := range versions {
		mg, ok := byVersion[v]
		if !ok {
			return done, errs.Newf(errs.CodeInternal, "缺少版本 %d 的回滚脚本，无法安全回滚", v)
		}
		sqlText, _, err := m.read(mg.DownPath)
		if err != nil {
			return done, err
		}
		start := time.Now()
		if err := m.execTx(ctx, sqlText, func(tx *gorm.DB) error {
			return tx.WithContext(ctx).Exec(
				fmt.Sprintf("DELETE FROM %s WHERE version = ?", tableName), mg.Version).Error
		}); err != nil {
			return done, errs.Wrap(err, errs.CodeInternal, "迁移回滚失败").
				WithField("version", mg.Version).WithField("name", mg.Name)
		}
		logx.FromContext(ctx).Info("迁移已回滚", "module", "migrate",
			"version", mg.Version, "name", mg.Name, "costMs", time.Since(start).Milliseconds())
		done = append(done, mg)
	}
	return done, nil
}

// Status 返回脚本与数据库记录的比对结果，按版本升序。
func (m *Migrator) Status(ctx context.Context) ([]Status, error) {
	migrations, err := m.load()
	if err != nil {
		return nil, err
	}
	if err := m.ensureTable(ctx); err != nil {
		return nil, err
	}
	applied, err := m.records(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]Status, 0, len(migrations))
	for _, mg := range migrations {
		st := Status{Version: mg.Version, Name: mg.Name}
		if rec, ok := applied[mg.Version]; ok {
			_, checksum, err := m.read(mg.UpPath)
			if err != nil {
				return nil, err
			}
			st.Applied = true
			st.AppliedAt = rec.AppliedAt
			st.Dirty = rec.Checksum != checksum
		}
		out = append(out, st)
	}
	return out, nil
}

// Pending 返回尚未应用的版本数量。
func (m *Migrator) Pending(ctx context.Context) (int, error) {
	list, err := m.Status(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, s := range list {
		if !s.Applied {
			n++
		}
	}
	return n, nil
}

// load 解析脚本目录，方言目录中的同名文件优先。
func (m *Migrator) load() ([]Migration, error) {
	entries, err := fs.ReadDir(m.fsys, ".")
	if err != nil {
		return nil, errs.Wrap(err, errs.CodeInternal, "迁移脚本目录读取失败")
	}

	type pair struct {
		name string
		up   string
		down string
	}
	found := map[int64]*pair{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		mt := fileRe.FindStringSubmatch(e.Name())
		if mt == nil {
			continue
		}
		version, err := strconv.ParseInt(mt[1], 10, 64)
		if err != nil {
			return nil, errs.Wrapf(err, errs.CodeInternal, "迁移版本号非法：%s", e.Name())
		}
		p, ok := found[version]
		if !ok {
			p = &pair{name: mt[2]}
			found[version] = p
		}
		if p.name != mt[2] {
			return nil, errs.Newf(errs.CodeInternal, "版本 %d 存在多个不同描述的脚本：%s / %s", version, p.name, mt[2])
		}
		if mt[3] == "up" {
			p.up = m.resolve(e.Name())
		} else {
			p.down = m.resolve(e.Name())
		}
	}

	out := make([]Migration, 0, len(found))
	for version, p := range found {
		if p.up == "" || p.down == "" {
			return nil, errs.Newf(errs.CodeInternal, "版本 %d(%s) 缺少 up 或 down 脚本，迁移必须成对", version, p.name)
		}
		out = append(out, Migration{Version: version, Name: p.name, UpPath: p.up, DownPath: p.down})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	if len(out) == 0 {
		return nil, errs.New(errs.CodeInternal, "未找到任何迁移脚本")
	}
	return out, nil
}

// resolve 优先返回 dialect/<driver>/<file>，不存在时回落到根目录同名文件。
func (m *Migrator) resolve(name string) string {
	if m.dialect != "" {
		p := path.Join("dialect", m.dialect, name)
		if _, err := fs.Stat(m.fsys, p); err == nil {
			return p
		}
	}
	return name
}

// read 返回脚本内容及其 SHA-256 校验和。
func (m *Migrator) read(p string) (string, string, error) {
	b, err := fs.ReadFile(m.fsys, p)
	if err != nil {
		return "", "", errs.Wrapf(err, errs.CodeInternal, "迁移脚本读取失败：%s", p)
	}
	sum := sha256.Sum256(b)
	return string(b), hex.EncodeToString(sum[:]), nil
}

// verify 校验已应用脚本未被修改，防止线上库与脚本漂移。
func (m *Migrator) verify(migrations []Migration, applied map[int64]Record) error {
	var dirty []string
	for _, mg := range migrations {
		rec, ok := applied[mg.Version]
		if !ok {
			continue
		}
		_, checksum, err := m.read(mg.UpPath)
		if err != nil {
			return err
		}
		if rec.Checksum != checksum {
			dirty = append(dirty, fmt.Sprintf("%d_%s", mg.Version, mg.Name))
		}
	}
	if len(dirty) > 0 {
		return errs.Newf(errs.CodeInternal,
			"已应用的迁移脚本被修改，禁止继续：%s。请新增迁移而不是改历史脚本", strings.Join(dirty, ", "))
	}
	return nil
}

// records 读取已应用记录。
func (m *Migrator) records(ctx context.Context) (map[int64]Record, error) {
	var rows []Record
	if err := m.db.WithContext(ctx).Table(tableName).Find(&rows).Error; err != nil {
		return nil, errs.Wrap(err, errs.CodeInternal, "迁移记录读取失败")
	}
	out := make(map[int64]Record, len(rows))
	for _, r := range rows {
		out[r.Version] = r
	}
	return out, nil
}

// execTx 在单个事务内执行脚本语句并记账，任何语句失败即整体回滚。
// SQLite 的 DSN 使用 _txlock=immediate，事务开始即取写锁。
func (m *Migrator) execTx(ctx context.Context, sqlText string, bookkeep func(tx *gorm.DB) error) error {
	stmts, err := SplitStatements(sqlText)
	if err != nil {
		return err
	}
	return m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, s := range stmts {
			if err := tx.WithContext(ctx).Exec(s).Error; err != nil {
				return errs.Wrap(err, errs.CodeInternal, "SQL 语句执行失败").WithField("sql", s)
			}
		}
		return bookkeep(tx)
	})
}

// ensureTable 创建迁移记录表。该表结构随方言而异，故不走迁移脚本。
func (m *Migrator) ensureTable(ctx context.Context) error {
	var ddl string
	switch m.dialect {
	case "mysql":
		ddl = "CREATE TABLE IF NOT EXISTS " + tableName + " (" +
			"`version` BIGINT NOT NULL, `name` VARCHAR(128) NOT NULL, `applied_at` DATETIME(3) NOT NULL, " +
			"`checksum` VARCHAR(64) NOT NULL, `duration_ms` BIGINT NOT NULL DEFAULT 0, PRIMARY KEY (`version`)" +
			") ENGINE=InnoDB DEFAULT CHARSET=utf8mb4"
	case "postgres":
		ddl = "CREATE TABLE IF NOT EXISTS " + tableName + " (" +
			"version BIGINT NOT NULL, name VARCHAR(128) NOT NULL, applied_at TIMESTAMP(3) NOT NULL, " +
			"checksum VARCHAR(64) NOT NULL, duration_ms BIGINT NOT NULL DEFAULT 0, PRIMARY KEY (version))"
	default:
		ddl = "CREATE TABLE IF NOT EXISTS " + tableName + " (" +
			"version BIGINT NOT NULL, name VARCHAR(128) NOT NULL, applied_at DATETIME NOT NULL, " +
			"checksum VARCHAR(64) NOT NULL, duration_ms BIGINT NOT NULL DEFAULT 0, PRIMARY KEY (version))"
	}
	if err := m.db.WithContext(ctx).Exec(ddl).Error; err != nil {
		return errs.Wrap(err, errs.CodeInternal, "迁移记录表创建失败")
	}
	return nil
}

// lock 获取库级迁移锁，返回释放函数。多实例同时启动时只有一个能执行迁移。
func (m *Migrator) lock(ctx context.Context) (func(), error) {
	switch m.dialect {
	case "mysql":
		var got int
		if err := m.db.WithContext(ctx).Raw("SELECT GET_LOCK(?, ?)", lockKey, 30).Scan(&got).Error; err != nil {
			return nil, errs.Wrap(err, errs.CodeInternal, "获取迁移锁失败")
		}
		if got != 1 {
			return nil, errs.New(errs.CodeConflict, "另一个实例正在执行迁移，请稍后重试")
		}
		return func() {
			if err := m.db.Exec("SELECT RELEASE_LOCK(?)", lockKey).Error; err != nil {
				logx.L().Warn("迁移锁释放失败", "module", "migrate", "err", err.Error())
			}
		}, nil
	case "postgres":
		var got bool
		if err := m.db.WithContext(ctx).Raw("SELECT pg_try_advisory_lock(hashtext(?))", lockKey).Scan(&got).Error; err != nil {
			return nil, errs.Wrap(err, errs.CodeInternal, "获取迁移锁失败")
		}
		if !got {
			return nil, errs.New(errs.CodeConflict, "另一个实例正在执行迁移，请稍后重试")
		}
		return func() {
			if err := m.db.Exec("SELECT pg_advisory_unlock(hashtext(?))", lockKey).Error; err != nil {
				logx.L().Warn("迁移锁释放失败", "module", "migrate", "err", err.Error())
			}
		}, nil
	default:
		// SQLite：写事务本身即库级独占锁（DSN _txlock=immediate + busy_timeout）
		return func() {}, nil
	}
}

// SplitStatements 按分号切分 SQL，忽略 -- 行注释与空语句。
// 迁移脚本不允许包含存储过程等含内嵌分号的结构。
func SplitStatements(sqlText string) ([]string, error) {
	var (
		out []string
		cur strings.Builder
	)
	for _, rawLine := range strings.Split(sqlText, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "--") {
			continue
		}
		cur.WriteString(line)
		cur.WriteString("\n")
		if strings.HasSuffix(line, ";") {
			stmt := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(cur.String()), ";"))
			if stmt != "" {
				out = append(out, stmt)
			}
			cur.Reset()
		}
	}
	if rest := strings.TrimSpace(cur.String()); rest != "" {
		return nil, errs.Newf(errs.CodeInternal, "SQL 结尾缺少分号：%.60s", rest)
	}
	if len(out) == 0 {
		return nil, errs.New(errs.CodeInternal, "迁移脚本为空")
	}
	return out, nil
}
