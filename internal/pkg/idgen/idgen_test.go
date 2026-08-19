package idgen

import (
	"sort"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/novapanel/novapanel/internal/pkg/errs"
)

func TestNewGeneratorRejectsBadNodeID(t *testing.T) {
	for _, id := range []int64{-1, 1024, 99999} {
		_, err := NewGenerator(id)
		require.Error(t, err)
		assert.Equal(t, errs.CodeInvalidParam, errs.Code(err))
	}
}

func TestGeneratorUniqueAndIncreasing(t *testing.T) {
	g, err := NewGenerator(7)
	require.NoError(t, err)

	const n = 5000
	ids := make([]int64, n)
	for i := range ids {
		ids[i] = g.NextID()
		assert.Positive(t, ids[i], "雪花 ID 必须为正，避免与自增 0 值混淆")
	}

	seen := make(map[int64]struct{}, n)
	for _, id := range ids {
		_, dup := seen[id]
		require.False(t, dup, "雪花 ID 重复：%d", id)
		seen[id] = struct{}{}
	}
	assert.True(t, sort.SliceIsSorted(ids, func(i, j int) bool { return ids[i] < ids[j] }), "同节点 ID 应单调递增")
}

func TestGeneratorConcurrentUnique(t *testing.T) {
	g, err := NewGenerator(1)
	require.NoError(t, err)

	const workers, each = 16, 500
	var mu sync.Mutex
	seen := make(map[int64]struct{}, workers*each)

	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			local := make([]int64, each)
			for i := range local {
				local[i] = g.NextID()
			}
			mu.Lock()
			defer mu.Unlock()
			for _, id := range local {
				seen[id] = struct{}{}
			}
		}()
	}
	wg.Wait()
	assert.Len(t, seen, workers*each, "并发生成不得冲突")
}

func TestInitDefaultAndNextID(t *testing.T) {
	require.NoError(t, InitDefault(3))
	a, b := NextID(), NextID()
	assert.NotEqual(t, a, b)
	assert.Error(t, InitDefault(2048))
}

func TestNewULIDFormat(t *testing.T) {
	const n = 2000
	seen := make(map[string]struct{}, n)
	prev := ""
	for range n {
		id := NewULID()
		require.Len(t, id, ULIDLength)
		require.True(t, IsULID(id), "非法 ULID：%s", id)
		_, dup := seen[id]
		require.False(t, dup, "ULID 重复：%s", id)
		seen[id] = struct{}{}

		if prev != "" {
			assert.GreaterOrEqual(t, id[:10], prev[:10], "时间前缀应单调不减")
		}
		prev = id
	}
}

func TestIsULIDRejectsAmbiguousChars(t *testing.T) {
	cases := map[string]bool{
		"01J9Z3M8Q0ABCDEFGHJKMNPQRS": true,
		"01J9Z3M8Q0ABCDEFGHJKMNPQR":  false, // 长度不足
		"01J9Z3M8Q0ABCDEFGHIKMNPQRS": false, // 含 I
		"01J9Z3M8Q0ABCDEFGHLKMNPQRS": false, // 含 L
		"01J9Z3M8Q0ABCDEFGHOKMNPQRS": false, // 含 O
		"01J9Z3M8Q0ABCDEFGHUKMNPQRS": false, // 含 U
		"01j9z3m8q0abcdefghjkmnpqrs": false, // 小写
	}
	for in, want := range cases {
		assert.Equal(t, want, IsULID(in), in)
	}
}

func TestIsRequestID(t *testing.T) {
	cases := map[string]bool{
		"req-12345678":     true,
		"01J9Z3M8Q0ABCDEF": true,
		"short":            false,
		"has space here":   false,
		"inject\nheader01": false,
		"<script>abc</s>":  false,
	}
	for in, want := range cases {
		assert.Equal(t, want, IsRequestID(in), in)
	}
	assert.False(t, IsRequestID(""))
}

func BenchmarkNewULID(b *testing.B) {
	for range b.N {
		_ = NewULID()
	}
}
