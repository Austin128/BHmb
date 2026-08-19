package security

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnsureSelfSignedCertCreatesUsablePair(t *testing.T) {
	dir := t.TempDir()
	certFile := filepath.Join(dir, "certs", "panel.crt")
	keyFile := filepath.Join(dir, "certs", "panel.key")

	created, err := EnsureSelfSignedCert(certFile, keyFile, []string{"0.0.0.0", "panel.local"})
	require.NoError(t, err)
	assert.True(t, created)

	// 能被 TLS 栈加载，说明证书与私钥匹配且编码正确
	pair, err := tls.LoadX509KeyPair(certFile, keyFile)
	require.NoError(t, err)
	require.NotEmpty(t, pair.Certificate)

	block, _ := pem.Decode(mustRead(t, certFile))
	require.NotNil(t, block)
	cert, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)

	// 0.0.0.0 是监听通配地址而非可校验主机名，必须被剔除
	assert.NotContains(t, cert.DNSNames, "0.0.0.0")
	assert.Contains(t, cert.DNSNames, "panel.local")
	assert.Contains(t, cert.DNSNames, "localhost")
	assert.NoError(t, cert.VerifyHostname("localhost"))
	assert.NoError(t, cert.VerifyHostname("127.0.0.1"))
	assert.True(t, cert.NotAfter.After(cert.NotBefore))

	st, err := os.Stat(keyFile)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), st.Mode().Perm(), "私钥必须仅所有者可读写")
}

func TestEnsureSelfSignedCertKeepsExistingFiles(t *testing.T) {
	dir := t.TempDir()
	certFile := filepath.Join(dir, "panel.crt")
	keyFile := filepath.Join(dir, "panel.key")

	created, err := EnsureSelfSignedCert(certFile, keyFile, nil)
	require.NoError(t, err)
	require.True(t, created)
	before := mustRead(t, certFile)

	created, err = EnsureSelfSignedCert(certFile, keyFile, nil)
	require.NoError(t, err)
	assert.False(t, created, "已存在证书不应重新生成")
	assert.Equal(t, before, mustRead(t, certFile))
}

func TestEnsureSelfSignedCertRejectsEmptyPath(t *testing.T) {
	_, err := EnsureSelfSignedCert("", "", nil)
	assert.Error(t, err)
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	return b
}
