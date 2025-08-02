package mysqlbinloglistener

import (
	"testing"

	"github.com/project-flogo/core/support/log"
	"github.com/stretchr/testify/assert"
)

// TestSSLCornerCases tests SSL/TLS corner cases and edge cases
func TestSSLCornerCases(t *testing.T) {
	logger := log.RootLogger()

	t.Run("Empty SSLMode defaults to disable", func(t *testing.T) {
		settings := &Settings{
			Host:         "localhost",
			Port:         3306,
			User:         "test",
			Password:     "test",
			DatabaseName: "test",
			SSLMode:      "", // Empty should default
		}

		err := settings.Validate()
		assert.NoError(t, err)
		assert.Equal(t, "disable", settings.SSLMode)
	})

	t.Run("Case sensitivity of SSL modes", func(t *testing.T) {
		invalidModes := []string{"DISABLE", "Require", "VERIFY-CA", "Verify-Full", "disabled"}
		for _, mode := range invalidModes {
			settings := &Settings{
				Host:         "localhost",
				Port:         3306,
				User:         "test",
				Password:     "test",
				DatabaseName: "test",
				SSLMode:      mode,
			}

			err := settings.Validate()
			assert.Error(t, err, "Should reject case-sensitive SSL mode: %s", mode)
			assert.Contains(t, err.Error(), "invalid sslMode")
		}
	})

	t.Run("SkipSSLVerify with verify-full mode conflict", func(t *testing.T) {
		// This is a logical conflict but should still validate
		settings := &Settings{
			Host:          "localhost",
			Port:          3306,
			User:          "test",
			Password:      "test",
			DatabaseName:  "test",
			SSLMode:       "verify-full",
			SSLCA:         "/path/to/ca.pem",
			SkipSSLVerify: true, // Conflicts with verify-full intent
		}

		err := settings.Validate()
		assert.NoError(t, err) // Should validate but with logical conflict

		listener := NewMySQLBinlogListener(settings, logger)
		assert.True(t, listener.isSSLRequired())
		assert.True(t, listener.needsCustomTLSConfig())

		// Note: buildTLSConfig would fail due to file not existing,
		// but the settings validation and SSL requirement detection work correctly
	})

	t.Run("SSL require mode without any certificates", func(t *testing.T) {
		// This should be valid - require mode doesn't need certificates
		settings := &Settings{
			Host:         "localhost",
			Port:         3306,
			User:         "test",
			Password:     "test",
			DatabaseName: "test",
			SSLMode:      "require",
			// No certificates provided - should be OK
		}

		err := settings.Validate()
		assert.NoError(t, err)

		listener := NewMySQLBinlogListener(settings, logger)
		assert.True(t, listener.isSSLRequired())
		assert.False(t, listener.needsCustomTLSConfig())
	})

	t.Run("Multiple SSL mode synonyms", func(t *testing.T) {
		// Test exact mode strings (no synonyms should be accepted)
		validModes := []string{"disable", "require", "verify-ca", "verify-full"}

		for _, mode := range validModes {
			settings := &Settings{
				Host:         "localhost",
				Port:         3306,
				User:         "test",
				Password:     "test",
				DatabaseName: "test",
				SSLMode:      mode,
			}

			if mode == "verify-ca" || mode == "verify-full" {
				settings.SSLCA = "/path/to/ca.pem"
			}

			err := settings.Validate()
			assert.NoError(t, err, "Valid SSL mode should pass: %s", mode)
		}
	})

	t.Run("SSL with client cert but disabled SSL mode", func(t *testing.T) {
		// Edge case: certificates provided but SSL disabled
		settings := &Settings{
			Host:         "localhost",
			Port:         3306,
			User:         "test",
			Password:     "test",
			DatabaseName: "test",
			SSLMode:      "disable",
			SSLCert:      "/path/to/client.pem", // Provided but SSL disabled
			SSLKey:       "/path/to/client.key",
		}

		err := settings.Validate()
		assert.NoError(t, err) // Should validate - certificates ignored when SSL disabled

		listener := NewMySQLBinlogListener(settings, logger)
		assert.False(t, listener.isSSLRequired())
	})

	t.Run("SSL require with SkipSSLVerify true", func(t *testing.T) {
		// Valid combination for testing environments
		settings := &Settings{
			Host:          "localhost",
			Port:          3306,
			User:          "test",
			Password:      "test",
			DatabaseName:  "test",
			SSLMode:       "require",
			SkipSSLVerify: true,
		}

		err := settings.Validate()
		assert.NoError(t, err)

		listener := NewMySQLBinlogListener(settings, logger)
		tlsConfig, err := listener.buildTLSConfig()
		assert.NoError(t, err)
		assert.NotNil(t, tlsConfig)
		assert.True(t, tlsConfig.InsecureSkipVerify)
	})

	t.Run("Host field used for ServerName in verify-full", func(t *testing.T) {
		hostname := "mysql.production.com"
		settings := &Settings{
			Host:         hostname,
			Port:         3306,
			User:         "test",
			Password:     "test",
			DatabaseName: "test",
			SSLMode:      "verify-full",
			SSLCA:        "/path/to/ca.pem",
		}

		err := settings.Validate()
		assert.NoError(t, err)

		listener := NewMySQLBinlogListener(settings, logger)
		assert.True(t, listener.isSSLRequired())
		assert.True(t, listener.needsCustomTLSConfig())

		// Note: Full TLS config testing would require actual certificate files
		// This test verifies the SSL requirement and configuration detection
	})

	t.Run("Invalid SSL certificate file paths", func(t *testing.T) {
		// Test that our validation allows non-existent paths (file existence checked later)
		settings := &Settings{
			Host:         "localhost",
			Port:         3306,
			User:         "test",
			Password:     "test",
			DatabaseName: "test",
			SSLMode:      "verify-ca",
			SSLCA:        "/nonexistent/path/ca.pem",
			SSLCert:      "/nonexistent/path/client.pem",
			SSLKey:       "/nonexistent/path/client.key",
		}

		err := settings.Validate()
		assert.NoError(t, err) // Validation should pass - file existence checked during connection

		listener := NewMySQLBinlogListener(settings, logger)
		assert.True(t, listener.isSSLRequired())
		assert.True(t, listener.needsCustomTLSConfig())
	})
}
