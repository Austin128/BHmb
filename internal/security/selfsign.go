package security

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/novapanel/novapanel/internal/pkg/errs"
)

// selfSignedValidity 为自签证书有效期。825 天是浏览器对服务端证书接受的上限。
const selfSignedValidity = 825 * 24 * time.Hour

// EnsureSelfSignedCert 在证书或私钥缺失时生成一张自签服务端证书。
// 两个文件都已存在时直接返回，不做任何覆盖；返回 true 表示本次新生成。
// 自签证书仅用于首次启动可访问，生产应替换为受信 CA 签发的证书。
func EnsureSelfSignedCert(certFile, keyFile string, hosts []string) (bool, error) {
	if certFile == "" || keyFile == "" {
		return false, errs.New(errs.CodeInvalidParam, "证书与私钥路径不能为空")
	}
	if fileExists(certFile) && fileExists(keyFile) {
		return false, nil
	}
	for _, dir := range []string{filepath.Dir(certFile), filepath.Dir(keyFile)} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return false, errs.Wrap(err, errs.CodeInternal, "创建证书目录失败").
				WithField("dir", dir)
		}
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return false, errs.Wrap(err, errs.CodeInternal, "生成证书私钥失败")
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return false, errs.Wrap(err, errs.CodeInternal, "生成证书序列号失败")
	}

	now := time.Now()
	tmpl := x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"Qingyuan Panel"},
			CommonName:   "Qingyuan Panel Self-Signed",
		},
		// 回拨 1 小时，容忍机器间的时钟偏差
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(selfSignedValidity),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	for _, h := range normalizeHosts(hosts) {
		if ip := net.ParseIP(h); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
			continue
		}
		tmpl.DNSNames = append(tmpl.DNSNames, h)
	}

	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		return false, errs.Wrap(err, errs.CodeInternal, "签发自签证书失败")
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return false, errs.Wrap(err, errs.CodeInternal, "序列化证书私钥失败")
	}

	if err := writePEM(certFile, "CERTIFICATE", der, 0o644); err != nil {
		return false, err
	}
	// 私钥必须 0600：证书目录通常与配置同级，权限放宽等于泄露私钥
	if err := writePEM(keyFile, "EC PRIVATE KEY", keyDER, 0o600); err != nil {
		return false, err
	}
	return true, nil
}

// normalizeHosts 保证至少包含 localhost 与回环地址，并去重。
func normalizeHosts(hosts []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(hosts)+3)
	add := func(h string) {
		if h == "" || h == "0.0.0.0" || h == "::" {
			return
		}
		if _, ok := seen[h]; ok {
			return
		}
		seen[h] = struct{}{}
		out = append(out, h)
	}
	for _, h := range hosts {
		add(h)
	}
	add("localhost")
	add("127.0.0.1")
	add("::1")
	return out
}

func writePEM(path, blockType string, der []byte, perm os.FileMode) error {
	buf := pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: der})
	if buf == nil {
		return errs.New(errs.CodeInternal, "PEM 编码失败").WithField("file", path)
	}
	if err := os.WriteFile(path, buf, perm); err != nil {
		return errs.Wrap(err, errs.CodeInternal, "写入证书文件失败").WithField("file", path)
	}
	return os.Chmod(path, perm)
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}
