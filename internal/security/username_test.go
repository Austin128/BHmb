package security

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckUsernameAccepts(t *testing.T) {
	for _, name := range []string{
		"admin",
		"ops",
		"a.b",
		"user_01",
		"web-master",
		"Austin",
		strings.Repeat("a", UsernameMaxLen),
	} {
		assert.NoError(t, CheckUsername(name), "应接受 %q", name)
	}
}

func TestCheckUsernameRejects(t *testing.T) {
	cases := map[string]string{
		"太短":     "ab",
		"超长":     strings.Repeat("a", UsernameMaxLen+1),
		"数字开头":   "1admin",
		"标点开头":   "_admin",
		"标点结尾":   "admin-",
		"连续标点":   "ad..min",
		"含空格":    "ad min",
		"首尾空白":   " admin",
		"中文":     "管理员",
		"含 @":    "admin@host",
		"保留名":    "system",
		"保留名大小写": "NovaPanel",
	}
	for label, name := range cases {
		require.Error(t, CheckUsername(name), "应拒绝 %s：%q", label, name)
	}
}

func TestSameUsernameIgnoresCase(t *testing.T) {
	assert.True(t, SameUsername("admin", "Admin"))
	assert.True(t, SameUsername("Ops.Team", "ops.team"))
	assert.False(t, SameUsername("admin", "admin1"))
}
