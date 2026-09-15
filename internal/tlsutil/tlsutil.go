package tlsutil

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// 本包提供 HTTPS 支持：自签证书生成、证书导入、运行期热加载。
//
// 配置（环境变量）：
//   TLS_ENABLED=true            开启 HTTPS（默认 false，保持向后兼容的 HTTP）
//   TLS_DIR=./tls               证书落盘目录（默认 cert.pem / key.pem）
//   TLS_CERT_FILE / TLS_KEY_FILE 自定义证书路径
//   TLS_AUTO_SELF_SIGNED=true   证书缺失时自动生成自签（默认 true）
//   TLS_HOSTS=a,b,c             自签证书额外 SAN（默认含本机名/回环/本机 IP）
//   TLS_SELF_SIGNED_DAYS=825    自签有效期天数
//
// 导入证书：管理面 PUT /api/v1/tls/certificate {cert_pem,key_pem}，写盘并立即热加载。

// Manager 持有当前证书，支持运行期替换（GetCertificate 每次握手读取）。
type Manager struct {
	mu       sync.RWMutex
	cert     *tls.Certificate
	source   string
	dir      string
	certFile string
	keyFile  string
}

// Default 是进程级证书管理器，main 在启动时替换为按环境配置好的实例。
var Default = NewManager()

func NewManager() *Manager {
	return &Manager{dir: "./tls"}
}

func (m *Manager) Enabled() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cert != nil
}

func (m *Manager) Source() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.source
}

// GetCertificate 是 tls.Config 的回调：每次握手读取当前证书，替换后无需重启。
func (m *Manager) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.cert == nil {
		return nil, errors.New("tls: no certificate configured")
	}
	return m.cert, nil
}

func (m *Manager) set(cert tls.Certificate, source string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cert = &cert
	m.source = source
}

func (m *Manager) loadFiles(certFile, keyFile, source string) error {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return err
	}
	cert.Leaf, _ = x509.ParseCertificate(cert.Certificate[0])
	m.mu.Lock()
	m.dir = filepath.Dir(certFile)
	m.certFile = certFile
	m.keyFile = keyFile
	m.mu.Unlock()
	m.set(cert, source)
	return nil
}

// Import 校验并保存运维导入的证书/私钥，随后立即生效。
func (m *Manager) Import(certPEM, keyPEM []byte) error {
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return fmt.Errorf("certificate/key pair is invalid: %w", err)
	}
	cert.Leaf, _ = x509.ParseCertificate(cert.Certificate[0])
	m.mu.RLock()
	certFile, keyFile := m.certFile, m.keyFile
	dir := m.dir
	m.mu.RUnlock()
	if certFile == "" {
		certFile = filepath.Join(dir, "cert.pem")
	}
	if keyFile == "" {
		keyFile = filepath.Join(dir, "key.pem")
	}
	if err := os.MkdirAll(filepath.Dir(certFile), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(certFile, certPEM, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		return err
	}
	m.mu.Lock()
	m.dir = filepath.Dir(certFile)
	m.certFile, m.keyFile = certFile, keyFile
	m.mu.Unlock()
	m.set(cert, "imported")
	return nil
}

// RegenerateSelfSigned 生成新的自签证书并落盘、生效。
func (m *Manager) RegenerateSelfSigned(hosts []string, days int) error {
	certPEM, keyPEM, err := GenerateSelfSigned(hosts, days)
	if err != nil {
		return err
	}
	if err := m.Import(certPEM, keyPEM); err != nil {
		return err
	}
	m.mu.Lock()
	m.source = "self-signed"
	m.mu.Unlock()
	return nil
}

// Status 返回当前证书状态（无证书时只含 enabled=false）。
func (m *Manager) Status() map[string]any {
	m.mu.RLock()
	cert := m.cert
	source := m.source
	certFile, keyFile := m.certFile, m.keyFile
	m.mu.RUnlock()
	if cert == nil {
		return map[string]any{"enabled": false, "cert_file": certFile, "key_file": keyFile}
	}
	leaf := cert.Leaf
	if leaf == nil && len(cert.Certificate) > 0 {
		leaf, _ = x509.ParseCertificate(cert.Certificate[0])
	}
	status := map[string]any{
		"enabled":   true,
		"source":    source,
		"cert_file": certFile,
		"key_file":  keyFile,
	}
	if leaf != nil {
		ips := make([]string, 0, len(leaf.IPAddresses))
		for _, ip := range leaf.IPAddresses {
			ips = append(ips, ip.String())
		}
		sum := sha256.Sum256(leaf.Raw)
		status["subject"] = leaf.Subject.CommonName
		status["dns_names"] = append([]string{}, leaf.DNSNames...)
		status["ip_addresses"] = ips
		status["not_before"] = leaf.NotBefore.UTC().Format(time.RFC3339)
		status["not_after"] = leaf.NotAfter.UTC().Format(time.RFC3339)
		status["self_signed"] = leaf.Issuer.String() == leaf.Subject.String() && leaf.CheckSignature(leaf.SignatureAlgorithm, leaf.RawTBSCertificate, leaf.Signature) == nil
		status["fingerprint_sha256"] = formatFingerprint(sum[:])
	}
	return status
}

func formatFingerprint(raw []byte) string {
	hexed := strings.ToUpper(hex.EncodeToString(raw))
	parts := make([]string, 0, len(hexed)/2)
	for i := 0; i+2 <= len(hexed); i += 2 {
		parts = append(parts, hexed[i:i+2])
	}
	return strings.Join(parts, ":")
}

// DefaultHosts 返回自签证书的默认 SAN：本机名、回环与本机所有单播 IP。
func DefaultHosts() []string {
	hosts := []string{"localhost", "127.0.0.1", "::1"}
	if name, err := os.Hostname(); err == nil && strings.TrimSpace(name) != "" {
		hosts = append(hosts, name)
	}
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, addr := range addrs {
			if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
				if ip4 := ipNet.IP.To4(); ip4 != nil {
					hosts = append(hosts, ip4.String())
				}
			}
		}
	}
	return dedupeStrings(hosts)
}

func dedupeStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

// GenerateSelfSigned 生成 ECDSA P-256 自签证书；hosts 支持 DNS 名与 IP。
func GenerateSelfSigned(hosts []string, days int) ([]byte, []byte, error) {
	if days <= 0 {
		days = 825
	}
	hosts = dedupeStrings(hosts)
	if len(hosts) == 0 {
		hosts = DefaultHosts()
	}
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, err
	}
	template := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: hosts[0], Organization: []string{"PowerBarRations"}},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().AddDate(0, 0, days),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	for _, host := range hosts {
		if ip := net.ParseIP(host); ip != nil {
			template.IPAddresses = append(template.IPAddresses, ip)
		} else {
			template.DNSNames = append(template.DNSNames, host)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return nil, nil, err
	}
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return nil, nil, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM, nil
}

func envBool(name string, fallback bool) bool {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	if raw == "" {
		return fallback
	}
	return raw == "1" || raw == "true" || raw == "yes" || raw == "on"
}

func envInt(name string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	var value int
	if _, err := fmt.Sscanf(raw, "%d", &value); err != nil || value <= 0 {
		return fallback
	}
	return value
}

func splitCSV(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	return dedupeStrings(strings.Split(raw, ","))
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// SetupFromEnv 按环境变量构建证书管理器；TLS_ENABLED 未开时返回未启用的 manager。
func SetupFromEnv() (*Manager, error) {
	manager := NewManager()
	dir := strings.TrimSpace(os.Getenv("TLS_DIR"))
	if dir == "" {
		dir = "./tls"
	}
	manager.dir = dir
	if !envBool("TLS_ENABLED", false) {
		return manager, nil
	}
	certFile := strings.TrimSpace(os.Getenv("TLS_CERT_FILE"))
	keyFile := strings.TrimSpace(os.Getenv("TLS_KEY_FILE"))
	if certFile == "" {
		certFile = filepath.Join(dir, "cert.pem")
	}
	if keyFile == "" {
		keyFile = filepath.Join(dir, "key.pem")
	}
	if fileExists(certFile) && fileExists(keyFile) {
		if err := manager.loadFiles(certFile, keyFile, "file"); err != nil {
			return nil, err
		}
		return manager, nil
	}
	if !envBool("TLS_AUTO_SELF_SIGNED", true) {
		return nil, fmt.Errorf("TLS_ENABLED=true 但未找到证书 %s / %s，且 TLS_AUTO_SELF_SIGNED=false", certFile, keyFile)
	}
	hosts := append(splitCSV(os.Getenv("TLS_HOSTS")), DefaultHosts()...)
	certPEM, keyPEM, err := GenerateSelfSigned(hosts, envInt("TLS_SELF_SIGNED_DAYS", 825))
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(certFile), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(certFile, certPEM, 0o644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		return nil, err
	}
	if err := manager.loadFiles(certFile, keyFile, "self-signed"); err != nil {
		return nil, err
	}
	return manager, nil
}

// RunCLI 处理 "pbr tls" 子命令：
//
//	pbr tls gen --out ./tls [--host a,b] [--days 825]
//	pbr tls show --cert ./tls/cert.pem
func RunCLI(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: pbr tls <gen|show> ...")
		return 2
	}
	switch args[0] {
	case "gen":
		fs := flag.NewFlagSet("tls gen", flag.ContinueOnError)
		out := fs.String("out", "./tls", "输出目录")
		hosts := fs.String("host", "", "额外 SAN（逗号分隔的主机名或 IP）")
		days := fs.Int("days", 825, "有效期天数")
		if err := fs.Parse(args[1:]); err != nil {
			return 2
		}
		list := append(splitCSV(*hosts), DefaultHosts()...)
		certPEM, keyPEM, err := GenerateSelfSigned(list, *days)
		if err != nil {
			fmt.Fprintln(os.Stderr, "生成失败: "+err.Error())
			return 1
		}
		if err := os.MkdirAll(*out, 0o700); err != nil {
			fmt.Fprintln(os.Stderr, "创建目录失败: "+err.Error())
			return 1
		}
		certFile := filepath.Join(*out, "cert.pem")
		keyFile := filepath.Join(*out, "key.pem")
		if err := os.WriteFile(certFile, certPEM, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "写证书失败: "+err.Error())
			return 1
		}
		if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
			fmt.Fprintln(os.Stderr, "写私钥失败: "+err.Error())
			return 1
		}
		fmt.Println("自签证书已生成：")
		fmt.Println("  cert: " + certFile)
		fmt.Println("  key:  " + keyFile)
		fmt.Println("  SAN:  " + strings.Join(list, ", "))
		fmt.Println("启用：TLS_ENABLED=true TLS_CERT_FILE=" + certFile + " TLS_KEY_FILE=" + keyFile)
		return 0
	case "show":
		fs := flag.NewFlagSet("tls show", flag.ContinueOnError)
		certFile := fs.String("cert", "./tls/cert.pem", "证书路径")
		if err := fs.Parse(args[1:]); err != nil {
			return 2
		}
		raw, err := os.ReadFile(*certFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, "读取证书失败: "+err.Error())
			return 1
		}
		block, _ := pem.Decode(raw)
		if block == nil {
			fmt.Fprintln(os.Stderr, "不是 PEM 证书")
			return 1
		}
		leaf, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			fmt.Fprintln(os.Stderr, "解析证书失败: "+err.Error())
			return 1
		}
		sum := sha256.Sum256(leaf.Raw)
		fmt.Println("subject: " + leaf.Subject.CommonName)
		fmt.Println("not_after: " + leaf.NotAfter.UTC().Format(time.RFC3339))
		fmt.Println("fingerprint_sha256: " + formatFingerprint(sum[:]))
		return 0
	default:
		fmt.Fprintln(os.Stderr, "unknown tls subcommand: "+args[0])
		return 2
	}
}
