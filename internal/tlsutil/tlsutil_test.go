package tlsutil

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newFileManager(t *testing.T) *Manager {
	t.Helper()
	dir := t.TempDir()
	m := NewManager()
	m.dir = dir
	m.certFile = filepath.Join(dir, "cert.pem")
	m.keyFile = filepath.Join(dir, "key.pem")
	return m
}

func TestGenerateSelfSignedAndImportReload(t *testing.T) {
	m := newFileManager(t)
	require.False(t, m.Enabled())

	require.NoError(t, m.RegenerateSelfSigned([]string{"localhost", "127.0.0.1", "example.test"}, 30))
	require.True(t, m.Enabled())

	cert, err := m.GetCertificate(nil)
	require.NoError(t, err)
	require.NotNil(t, cert.Leaf)
	assert.Contains(t, cert.Leaf.DNSNames, "example.test")
	assert.Contains(t, cert.Leaf.DNSNames, "localhost")
	assert.NotEmpty(t, cert.Leaf.IPAddresses)
	assert.Equal(t, cert.Leaf.Issuer.String(), cert.Leaf.Subject.String(), "自签证书 issuer==subject")

	// 导入另一张证书后立即生效（无需重启）。
	otherCert, otherKey, err := GenerateSelfSigned([]string{"other.test"}, 5)
	require.NoError(t, err)
	require.NoError(t, m.Import(otherCert, otherKey))

	reloaded, err := m.GetCertificate(nil)
	require.NoError(t, err)
	require.NotNil(t, reloaded.Leaf)
	assert.Contains(t, reloaded.Leaf.DNSNames, "other.test")

	status := m.Status()
	assert.Equal(t, true, status["enabled"])
	assert.Equal(t, "imported", status["source"])
	assert.NotEmpty(t, status["fingerprint_sha256"])
	assert.NotEmpty(t, status["not_after"])
}

func TestImportRejectsMismatchedPair(t *testing.T) {
	m := newFileManager(t)
	certPEM, _, err := GenerateSelfSigned([]string{"a.test"}, 5)
	require.NoError(t, err)
	_, otherKey, err := GenerateSelfSigned([]string{"b.test"}, 5)
	require.NoError(t, err)
	require.Error(t, m.Import(certPEM, otherKey), "证书与私钥不匹配必须拒绝")
	assert.False(t, m.Enabled())
}
