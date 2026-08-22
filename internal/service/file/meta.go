// Package file 实现主机文件管理（docs/08）。
// 所有对外方法的路径入参都必须先经过 pathguard 校验，
// 服务层只做业务判定与文件系统操作，不感知 HTTP。
package file

import (
	"fmt"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/novapanel/novapanel/internal/model"
)

// idCacheTTL 为 uid/gid 到名称的缓存时长（docs/08 8.2.2）。
const idCacheTTL = 60 * time.Second

type idCacheEntry struct {
	name   string
	expire time.Time
}

// idCache 缓存 /etc/passwd 与 /etc/group 的查表结果，避免列目录时逐条查询。
type idCache struct {
	mu     sync.RWMutex
	users  map[int]idCacheEntry
	groups map[int]idCacheEntry
}

func newIDCache() *idCache {
	return &idCache{
		users:  make(map[int]idCacheEntry),
		groups: make(map[int]idCacheEntry),
	}
}

func (c *idCache) userName(uid int) string {
	return c.lookup(c.users, uid, func(id string) (string, error) {
		u, err := user.LookupId(id)
		if err != nil {
			return "", err
		}
		return u.Username, nil
	})
}

func (c *idCache) groupName(gid int) string {
	return c.lookup(c.groups, gid, func(id string) (string, error) {
		g, err := user.LookupGroupId(id)
		if err != nil {
			return "", err
		}
		return g.Name, nil
	})
}

// lookup 命中缓存直接返回；未命中时查表，失败则回落为数字串并同样缓存，
// 避免容器内无 passwd 记录时每次都重复查找。
func (c *idCache) lookup(m map[int]idCacheEntry, id int, fn func(string) (string, error)) string {
	now := time.Now()
	c.mu.RLock()
	e, ok := m[id]
	c.mu.RUnlock()
	if ok && now.Before(e.expire) {
		return e.name
	}

	name, err := fn(strconv.Itoa(id))
	if err != nil || name == "" {
		name = strconv.Itoa(id)
	}
	c.mu.Lock()
	m[id] = idCacheEntry{name: name, expire: now.Add(idCacheTTL)}
	c.mu.Unlock()
	return name
}

// buildEntry 由 lstat 结果组装元数据。full 必须是已通过校验的真实路径。
// lstat 保证符号链接本身被描述，而不是它指向的目标。
func (s *Service) buildEntry(full string, info fs.FileInfo) model.FileEntry {
	mode := info.Mode()
	isSymlink := mode&fs.ModeSymlink != 0

	e := model.FileEntry{
		Name:      info.Name(),
		Path:      full,
		IsDir:     mode.IsDir(),
		Mode:      mode.String(),
		ModeOctal: fmt.Sprintf("0%o", mode.Perm()),
		Mtime:     info.ModTime().UnixMilli(),
		IsSymlink: isSymlink,
		Ext:       strings.ToLower(strings.TrimPrefix(filepath.Ext(info.Name()), ".")),
	}
	if !e.IsDir {
		e.Size = info.Size()
	}
	if uid, gid, ok := ownership(info); ok {
		e.UID, e.GID = uid, gid
		e.Owner = s.ids.userName(uid)
		e.Group = s.ids.groupName(gid)
	}

	if isSymlink {
		if target, err := os.Readlink(full); err == nil {
			e.LinkTarget = target
		}
		// 悬空或成环的链接 Stat 会失败，前端据此置灰
		st, err := os.Stat(full)
		if err != nil {
			e.LinkBroken = true
		} else {
			e.IsDir = st.IsDir()
			if !e.IsDir {
				e.Size = st.Size()
			}
		}
	}

	e.Mime = mimeByExt(e.Ext, e.IsDir)
	e.Icon = iconOf(e.Ext, e.IsDir, isSymlink)
	e.Writable = writable(full)
	return e
}
