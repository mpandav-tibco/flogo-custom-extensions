package vectordb

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
)

// buildTLSConfig constructs a *tls.Config from the TLS-related fields in
// ConnectionConfig.  Returns nil when UseTLS is false (plaintext connection).
//
// Cert fields may be either a filesystem path or PEM-encoded content.
// If a field starts with "-----BEGIN" it is treated as inline PEM; otherwise
// it is treated as a file path.
func buildTLSConfig(cfg ConnectionConfig) (*tls.Config, error) {
	if !cfg.UseTLS {
		return nil, nil
	}

	tlsCfg := &tls.Config{
		InsecureSkipVerify: cfg.TLSInsecureSkipVerify, //nolint:gosec // opt-in, user-controlled
	}
	if cfg.TLSServerName != "" {
		tlsCfg.ServerName = cfg.TLSServerName
	}

	// CA certificate (server verification)
	if cfg.CACert != "" {
		pem, err := readPEMField(cfg.CACert, "CA certificate")
		if err != nil {
			return nil, err
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("vectordb TLS: failed to parse CA certificate")
		}
		tlsCfg.RootCAs = pool
	}

	// mTLS: client certificate + key
	if cfg.ClientCert != "" || cfg.ClientKey != "" {
		if cfg.ClientCert == "" || cfg.ClientKey == "" {
			return nil, fmt.Errorf("vectordb TLS: clientCert and clientKey must both be set for mTLS")
		}
		certPEM, err := readPEMField(cfg.ClientCert, "client certificate")
		if err != nil {
			return nil, err
		}
		keyPEM, err := readPEMField(cfg.ClientKey, "client key")
		if err != nil {
			return nil, err
		}
		cert, err := tls.X509KeyPair(certPEM, keyPEM)
		if err != nil {
			return nil, fmt.Errorf("vectordb TLS: failed to parse client cert/key pair: %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}

	return tlsCfg, nil
}

// readPEMField returns PEM bytes — from inline content or from a file path.
func readPEMField(value, label string) ([]byte, error) {
	if len(value) >= 5 && value[:5] == "-----" {
		return []byte(value), nil
	}
	data, err := os.ReadFile(value)
	if err != nil {
		return nil, fmt.Errorf("vectordb TLS: cannot read %s file %q: %w", label, value, err)
	}
	return data, nil
}
