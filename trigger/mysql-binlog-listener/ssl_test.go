package mysqlbinloglistener

import (
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"os"
	"testing"

	"github.com/project-flogo/core/support/log"
	"github.com/stretchr/testify/assert"
)

// TestSSLSettingsValidation tests SSL-related settings validation
func TestSSLSettingsValidation(t *testing.T) {

	tests := []struct {
		name        string
		settings    *Settings
		expectError bool
		errorMsg    string
	}{
		{
			name: "SSL disabled",
			settings: &Settings{
				Host:         "localhost",
				Port:         3306,
				User:         "test",
				Password:     "test",
				DatabaseName: "test",
				SSLMode:      "disable",
			},
			expectError: false,
		},
		{
			name: "SSL require mode",
			settings: &Settings{
				Host:         "localhost",
				Port:         3306,
				User:         "test",
				Password:     "test",
				DatabaseName: "test",
				SSLMode:      "require",
			},
			expectError: false,
		},
		{
			name: "SSL verify-ca mode without CA",
			settings: &Settings{
				Host:         "localhost",
				Port:         3306,
				User:         "test",
				Password:     "test",
				DatabaseName: "test",
				SSLMode:      "verify-ca",
			},
			expectError: true,
			errorMsg:    "sslCA is required when sslMode is verify-ca",
		},
		{
			name: "SSL verify-ca mode with CA file",
			settings: &Settings{
				Host:         "localhost",
				Port:         3306,
				User:         "test",
				Password:     "test",
				DatabaseName: "test",
				SSLMode:      "verify-ca",
				SSLCA:        "/path/to/ca.pem",
			},
			expectError: false,
		},
		{
			name: "SSL verify-full mode with CA cert",
			settings: &Settings{
				Host:         "localhost",
				Port:         3306,
				User:         "test",
				Password:     "test",
				DatabaseName: "test",
				SSLMode:      "verify-full",
				SSLCA:        "/path/to/ca.pem",
			},
			expectError: false,
		},
		{
			name: "Client cert without key",
			settings: &Settings{
				Host:         "localhost",
				Port:         3306,
				User:         "test",
				Password:     "test",
				DatabaseName: "test",
				SSLMode:      "require",
				SSLCert:      "/path/to/client.pem",
			},
			expectError: true,
			errorMsg:    "sslKey is required when sslCert is provided",
		},
		{
			name: "Client key without cert",
			settings: &Settings{
				Host:         "localhost",
				Port:         3306,
				User:         "test",
				Password:     "test",
				DatabaseName: "test",
				SSLMode:      "require",
				SSLKey:       "/path/to/client.key",
			},
			expectError: true,
			errorMsg:    "sslCert is required when sslKey is provided",
		},
		{
			name: "Client cert and key provided",
			settings: &Settings{
				Host:         "localhost",
				Port:         3306,
				User:         "test",
				Password:     "test",
				DatabaseName: "test",
				SSLMode:      "require",
				SSLCert:      "/path/to/client.pem",
				SSLKey:       "/path/to/client.key",
			},
			expectError: false,
		},
		{
			name: "Invalid SSL mode",
			settings: &Settings{
				Host:         "localhost",
				Port:         3306,
				User:         "test",
				Password:     "test",
				DatabaseName: "test",
				SSLMode:      "invalid-mode",
			},
			expectError: true,
			errorMsg:    "invalid sslMode: invalid-mode",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.settings.Validate()

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestSSLHelperMethods tests SSL helper methods
func TestSSLHelperMethods(t *testing.T) {
	logger := log.RootLogger()

	tests := []struct {
		name           string
		settings       *Settings
		isSSLRequired  bool
		needsCustomTLS bool
	}{
		{
			name: "SSL disabled",
			settings: &Settings{
				SSLMode: "disable",
			},
			isSSLRequired:  false,
			needsCustomTLS: false,
		},
		{
			name: "SSL require mode",
			settings: &Settings{
				SSLMode: "require",
			},
			isSSLRequired:  true,
			needsCustomTLS: false,
		},
		{
			name: "SSL verify-ca mode",
			settings: &Settings{
				SSLMode: "verify-ca",
				SSLCA:   "/path/to/ca.pem",
			},
			isSSLRequired:  true,
			needsCustomTLS: true,
		},
		{
			name: "SSL with client cert",
			settings: &Settings{
				SSLMode: "require",
				SSLCert: "/path/to/client.pem",
				SSLKey:  "/path/to/client.key",
			},
			isSSLRequired:  true,
			needsCustomTLS: true,
		},
		{
			name: "SSL with file CA",
			settings: &Settings{
				SSLMode: "verify-full",
				SSLCA:   "/path/to/ca.pem",
			},
			isSSLRequired:  true,
			needsCustomTLS: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			listener := NewMySQLBinlogListener(tt.settings, logger)

			assert.Equal(t, tt.isSSLRequired, listener.isSSLRequired())
			assert.Equal(t, tt.needsCustomTLS, listener.needsCustomTLSConfig())
		})
	}
}

// TestTLSConfigBuilding tests TLS config building (without file operations)
func TestTLSConfigBuilding(t *testing.T) {
	logger := log.RootLogger()

	t.Run("No SSL required", func(t *testing.T) {
		settings := &Settings{
			SSLMode: "disable",
		}
		listener := NewMySQLBinlogListener(settings, logger)

		tlsConfig, err := listener.buildTLSConfig()
		assert.NoError(t, err)
		assert.Nil(t, tlsConfig)
	})

	t.Run("SSL require mode", func(t *testing.T) {
		settings := &Settings{
			SSLMode: "require",
		}
		listener := NewMySQLBinlogListener(settings, logger)

		tlsConfig, err := listener.buildTLSConfig()
		assert.NoError(t, err)
		assert.NotNil(t, tlsConfig)
		assert.False(t, tlsConfig.InsecureSkipVerify)
	})

	t.Run("SSL require mode with skip verify", func(t *testing.T) {
		settings := &Settings{
			SSLMode:       "require",
			SkipSSLVerify: true,
		}
		listener := NewMySQLBinlogListener(settings, logger)

		tlsConfig, err := listener.buildTLSConfig()
		assert.NoError(t, err)
		assert.NotNil(t, tlsConfig)
		assert.True(t, tlsConfig.InsecureSkipVerify)
	})

	t.Run("SSL verify-full mode", func(t *testing.T) {
		settings := &Settings{
			Host:    "mysql.example.com",
			SSLMode: "verify-full",
		}
		listener := NewMySQLBinlogListener(settings, logger)

		_, err := listener.buildTLSConfig()
		// This will fail because no CA cert is provided, but we can check the hostname
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "CA certificate is required")
	})
}

// TestSSLConnectionStringBuilding tests connection string building with SSL parameters
func TestSSLConnectionStringBuilding(t *testing.T) {
	logger := log.RootLogger()

	t.Run("SSL disabled", func(t *testing.T) {
		settings := &Settings{
			Host:         "localhost",
			Port:         3306,
			User:         "testuser",
			Password:     "testpass",
			DatabaseName: "testdb",
			SSLMode:      "disable",
		}
		listener := NewMySQLBinlogListener(settings, logger)

		dsn := "testuser:testpass@tcp(localhost:3306)/testdb?parseTime=true"
		err := listener.addSSLConfiguration(&dsn)
		assert.NoError(t, err)
		assert.NotContains(t, dsn, "tls=")
	})

	t.Run("SSL required", func(t *testing.T) {
		settings := &Settings{
			Host:         "localhost",
			Port:         3306,
			User:         "testuser",
			Password:     "testpass",
			DatabaseName: "testdb",
			SSLMode:      "require",
		}
		listener := NewMySQLBinlogListener(settings, logger)

		dsn := "testuser:testpass@tcp(localhost:3306)/testdb?parseTime=true"
		err := listener.addSSLConfiguration(&dsn)
		assert.NoError(t, err)
		assert.Contains(t, dsn, "tls=true")
	})

	t.Run("SSL skip verify", func(t *testing.T) {
		settings := &Settings{
			Host:          "localhost",
			Port:          3306,
			User:          "testuser",
			Password:      "testpass",
			DatabaseName:  "testdb",
			SSLMode:       "require",
			SkipSSLVerify: true,
		}
		listener := NewMySQLBinlogListener(settings, logger)

		dsn := "testuser:testpass@tcp(localhost:3306)/testdb?parseTime=true"
		err := listener.addSSLConfiguration(&dsn)
		assert.NoError(t, err)
		assert.Contains(t, dsn, "tls=skip-verify")
	})
}

// TestSSLModeCompatibility tests SSL mode compatibility
func TestSSLModeCompatibility(t *testing.T) {
	validModes := []string{"disable", "require", "verify-ca", "verify-full"}

	for _, mode := range validModes {
		t.Run(fmt.Sprintf("SSL mode %s", mode), func(t *testing.T) {
			settings := &Settings{
				Host:         "localhost",
				Port:         3306,
				User:         "test",
				Password:     "test",
				DatabaseName: "test",
				SSLMode:      mode,
			}

			// Add required fields for verify modes
			if mode == "verify-ca" || mode == "verify-full" {
				settings.SSLCA = "/path/to/ca.pem"
			}

			err := settings.Validate()
			assert.NoError(t, err)
		})
	}
}

// TestBase64CertificateHandling tests base64 certificate handling
func TestBase64CertificateHandling(t *testing.T) {
	logger := log.RootLogger()

	// Generate a sample certificate for testing
	testCert := `-----BEGIN CERTIFICATE-----
MIICljCCAX4CCQDKn0J8F8s3gTANBgkqhkiG9w0BAQsFADANMQswCQYDVQQGEwJV
UzAeFw0yMTAxMDEwMDAwMDBaFw0yMjAxMDEwMDAwMDBaMA0xCzAJBgNVBAYTAlVT
MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA1mKuzvK...
-----END CERTIFICATE-----`

	t.Run("File CA certificate", func(t *testing.T) {
		// Create a temporary certificate file
		tmpFile, err := ioutil.TempFile("", "ca_cert_*.pem")
		assert.NoError(t, err)
		defer os.Remove(tmpFile.Name())

		_, err = tmpFile.Write([]byte(testCert))
		assert.NoError(t, err)
		tmpFile.Close()

		settings := &Settings{
			SSLMode: "verify-ca",
			SSLCA:   tmpFile.Name(),
		}
		listener := NewMySQLBinlogListener(settings, logger)

		tlsConfig := &tls.Config{}
		err = listener.loadCACertificate(tlsConfig)
		// This might fail due to invalid cert format, but should not fail on file reading
		if err != nil {
			assert.NotContains(t, err.Error(), "failed to read CA certificate file")
		}
	})

	t.Run("Invalid CA certificate file", func(t *testing.T) {
		settings := &Settings{
			SSLMode: "verify-ca",
			SSLCA:   "/nonexistent/file.pem",
		}
		listener := NewMySQLBinlogListener(settings, logger)

		tlsConfig := &tls.Config{}
		err := listener.loadCACertificate(tlsConfig)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to read CA certificate file")
	})
}
