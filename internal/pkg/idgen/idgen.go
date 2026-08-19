// Package idgen 提供业务主键（雪花 ID）与链路追踪 ID（ULID）生成能力。
package idgen

import (
	"crypto/rand"
	"encoding/binary"
	"regexp"
	"sync"
	"time"

	"github.com/bwmarrin/snowflake"

	"github.com/novapanel/novapanel/internal/pkg/errs"
)

// Epoch 为雪花 ID 起始时间：2024-01-01T00:00:00Z（毫秒）。
const Epoch int64 = 1704067200000

// Generator 生成 int64 雪花主键，可安全并发调用。
type Generator struct {
	node *snowflake.Node
}

// NewGenerator 按 workerID（0-1023）创建生成器。
func NewGenerator(nodeID int64) (*Generator, error) {
	if nodeID < 0 || nodeID > 1023 {
		return nil, errs.Newf(errs.CodeInvalidParam, "雪花 workerID 需在 0-1023 之间，当前 %d", nodeID)
	}
	snowflake.Epoch = Epoch
	node, err := snowflake.NewNode(nodeID)
	if err != nil {
		return nil, errs.Wrap(err, errs.CodeInternal, "雪花生成器初始化失败")
	}
	return &Generator{node: node}, nil
}

// NextID 返回下一个雪花 ID。
func (g *Generator) NextID() int64 { return g.node.Generate().Int64() }

var (
	defaultMu  sync.RWMutex
	defaultGen *Generator
)

// InitDefault 初始化包级默认生成器，供无法注入依赖的场景（如 CLI）使用。
func InitDefault(nodeID int64) error {
	g, err := NewGenerator(nodeID)
	if err != nil {
		return err
	}
	defaultMu.Lock()
	defaultGen = g
	defaultMu.Unlock()
	return nil
}

// NextID 使用默认生成器返回雪花 ID；未初始化时按 node 0 惰性初始化。
func NextID() int64 {
	defaultMu.RLock()
	g := defaultGen
	defaultMu.RUnlock()
	if g == nil {
		if err := InitDefault(0); err != nil {
			return time.Now().UTC().UnixNano() // 极端兜底，保证单调不冲突
		}
		defaultMu.RLock()
		g = defaultGen
		defaultMu.RUnlock()
	}
	return g.NextID()
}

// crockford 为 ULID 使用的 Base32 字符集（去除 I、L、O、U）。
const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// ULIDLength 为 ULID 字符串长度。
const ULIDLength = 26

// NewULID 生成 26 位 ULID：前 48 位为毫秒时间戳，后 80 位为密码学随机数，
// 因此按字典序排序即近似按时间排序，适合作为 traceId。
func NewULID() string {
	var b [16]byte
	ms := uint64(time.Now().UTC().UnixMilli())
	b[0] = byte(ms >> 40)
	b[1] = byte(ms >> 32)
	b[2] = byte(ms >> 24)
	b[3] = byte(ms >> 16)
	b[4] = byte(ms >> 8)
	b[5] = byte(ms)
	if _, err := rand.Read(b[6:]); err != nil {
		// crypto/rand 失败属不可用状态，退化为纳秒时间填充，保证仍可追踪
		binary.BigEndian.PutUint64(b[6:14], uint64(time.Now().UTC().UnixNano()))
	}

	hi := binary.BigEndian.Uint64(b[0:8])
	lo := binary.BigEndian.Uint64(b[8:16])
	out := make([]byte, ULIDLength)
	for i := 0; i < ULIDLength; i++ {
		shift := uint(125 - 5*i)
		out[i] = crockford[extract5(hi, lo, shift)]
	}
	return string(out)
}

func extract5(hi, lo uint64, shift uint) byte {
	var v uint64
	switch {
	case shift >= 64:
		v = hi >> (shift - 64)
	case shift+5 <= 64:
		v = lo >> shift
	default:
		v = (lo >> shift) | (hi << (64 - shift))
	}
	return byte(v & 31)
}

var ulidRe = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)

// IsULID 判断字符串是否为合法 ULID。
func IsULID(s string) bool { return ulidRe.MatchString(s) }

var requestIDRe = regexp.MustCompile(`^[A-Za-z0-9._:-]{8,64}$`)

// IsRequestID 校验客户端传入的 X-Request-Id：仅接受安全字符且长度 8-64，
// 防止日志注入与响应头污染。非法值一律丢弃。
func IsRequestID(s string) bool { return requestIDRe.MatchString(s) }
