package postgreslistener

import (
	"context"
	"database/sql"        // Required for sql.Open and db.Ping
	"database/sql/driver" // For driver.ErrBadConn
	"encoding/base64"
	"fmt"
	"net/url" // For URL escaping password
	"os"
	"strings"
	"time"

	"github.com/lib/pq" // PostgreSQL driver
	"github.com/project-flogo/core/data/metadata"
	"github.com/project-flogo/core/support/log"
	"github.com/project-flogo/core/trigger"
)

var triggerMd = trigger.NewMetadata(&Settings{}, &HandlerSettings{}, &Output{})

func init() {
	_ = trigger.Register(&Trigger{}, &Factory{})
}

// Trigger is a Flogo trigger for PostgreSQL LISTEN/NOTIFY
type Trigger struct {
	settings  *Settings
	logger    log.Logger
	handlers  []*Handler
	listeners []*pq.Listener
	db        *sql.DB // Store the database connection for health checks (optional)
}

// Handler is a specific listener for a channel
type Handler struct {
	runner  trigger.Handler
	channel string
}

// Factory is a trigger factory
type Factory struct{}

// Metadata returns the trigger's metadata
func (*Factory) Metadata() *trigger.Metadata {
	return triggerMd
}

// New creates a new trigger instance
func (*Factory) New(config *trigger.Config) (trigger.Trigger, error) {
	s := &Settings{}
	err := metadata.MapToStruct(config.Settings, s, true)
	if err != nil {
		return nil, fmt.Errorf("failed to map trigger settings: %v", err)
	}

	return &Trigger{settings: s}, nil
}

// Initialize initializes the trigger.
func (t *Trigger) Initialize(ctx trigger.InitContext) error {
	t.logger = ctx.Logger()

	// Build the connection string using the trigger's direct settings
	connStr, err := buildConnectionString(t.settings)
	if err != nil {
		return fmt.Errorf("failed to build connection string: %v", err)
	}

	// Optional: Ping the database once to ensure initial connectivity
	// This connection is separate from the pq.Listener connection
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return fmt.Errorf("failed to open initial database connection: %v", err)
	}
	t.db = db // Store for potential health checks or other uses

	// Ping with retry logic
	err = pingDBWithRetry(t.db, t.settings.MaxConnRetryAttempts, t.settings.ConnectionRetryDelay, t.logger)
	if err != nil {
		t.db.Close() // Close the connection if ping fails
		return fmt.Errorf("initial database connection ping failed: %v", err)
	}
	t.logger.Info("Successfully connected to PostgreSQL database for initial ping.")

	for _, handlerCfg := range ctx.GetHandlers() {
		handlerSettings := &HandlerSettings{}
		err := metadata.MapToStruct(handlerCfg.Settings(), handlerSettings, true)
		if err != nil {
			return fmt.Errorf("failed to map handler settings: %v", err)
		}

		handler := &Handler{runner: handlerCfg, channel: handlerSettings.Channel}
		t.handlers = append(t.handlers, handler)

		// Create a new pq.Listener for each channel.
		// The listener will manage its own connection to the database using connStr.
		listener := pq.NewListener(connStr,
			time.Duration(t.settings.ConnectionTimeout)*time.Second, // Fallback for minReconnectInterval
			time.Minute, // Max reconnect interval
			func(ev pq.ListenerEventType, err error) {
				if err != nil {
					t.logger.Errorf("PostgreSQL Listener event error for channel '%s': Type: %d, Error: %v", handler.channel, ev, err)
				} else {
					t.logger.Debugf("PostgreSQL Listener event for channel '%s': Type: %d", handler.channel, ev)
				}
			})

		t.listeners = append(t.listeners, listener)
	}

	t.logger.Info("PostgreSQL Listener trigger initialized successfully")
	return nil
}

// Start starts the trigger
func (t *Trigger) Start() error {
	for i, handler := range t.handlers {
		// Start each listener in a separate goroutine
		go t.listen(handler, t.listeners[i])
	}
	t.logger.Info("PostgreSQL Listener trigger started")
	return nil
}

// Stop stops the trigger
func (t *Trigger) Stop() error {
	t.logger.Info("Stopping PostgreSQL listeners")
	// Close the initial DB connection if it was opened
	if t.db != nil {
		t.db.Close()
	}

	// Close all pq.Listener connections
	for _, listener := range t.listeners {
		if listener != nil {
			listener.Close()
		}
	}
	t.logger.Info("PostgreSQL Listener trigger stopped")
	return nil
}

// listen is the main goroutine for a single channel listener
func (t *Trigger) listen(handler *Handler, listener *pq.Listener) {
	// Start listening on the specified channel
	err := listener.Listen(handler.channel)
	if err != nil {
		t.logger.Errorf("Failed to start listening on channel '%s': %v", handler.channel, err)
		return
	}

	t.logger.Infof("Actively listening for notifications on PostgreSQL channel: '%s'", handler.channel)

	// Loop to receive notifications
	for notification := range listener.Notify {
		if notification == nil {
			t.logger.Warnf("Received nil notification for channel '%s'. Listener might be closing or re-establishing.", handler.channel)
			continue
		}

		t.logger.Debugf("Received notification on channel '%s': %s (PID: %d)", notification.Channel, notification.Extra, notification.BePid)

		// Create the output payload for the Flogo flow
		output := &Output{Payload: notification.Extra} // notification.Extra contains the payload string

		// Handle the trigger event by executing the associated Flogo flow
		_, err := handler.runner.Handle(context.Background(), output.ToMap())
		if err != nil {
			t.logger.Errorf("Error running Flogo flow for channel '%s' with payload '%s': %v", handler.channel, notification.Extra, err)
		} else {
			t.logger.Infof("Successfully processed notification for channel '%s'.", handler.channel)
		}
	}
	t.logger.Warnf("Listener for channel '%s' has stopped receiving notifications.", handler.channel)
}

// buildConnectionString creates a PostgreSQL connection string from the trigger's settings.
func buildConnectionString(s *Settings) (string, error) {
	// Determine sslmode based on TLS settings
	sslMode := s.SSLMode
	if s.TLSConfig {
		switch s.TLSMode {
		case "VerifyCA":
			sslMode = "verify-ca"
		case "VerifyFull":
			sslMode = "verify-full"
		default:
			// If tlsParam is unknown or empty, default to disable for safety
			sslMode = "disable"
		}
	} else if sslMode == "" {
		// If TLSConfig is false and sslmode is not explicitly set, default to disable
		sslMode = "disable"
	}

	// Ensure required parameters are present
	if s.Host == "" || s.Port == 0 || s.User == "" || s.DatabaseName == "" {
		return "", fmt.Errorf("host, port, user, and databaseName are required trigger settings")
	}

	// Properly escape the password for URL inclusion to handle special characters.
	escapedPassword := url.QueryEscape(s.Password)

	// Construct the base connection string
	connStr := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s",
		s.User, escapedPassword, s.Host, s.Port, s.DatabaseName, sslMode)

	// Add connection timeout if specified
	if s.ConnectionTimeout > 0 {
		connStr += fmt.Sprintf("&connect_timeout=%d", s.ConnectionTimeout)
	}

	// Handle TLS certificates if TLSConfig is true and certificates are provided
	if s.TLSConfig {
		// Temporary file paths for certificates
		var caCertPath, clientCertPath, clientKeyPath string
		var err error

		if s.Cacert != "" {
			caCertPath, err = copyCertToTempFile([]byte(s.Cacert), "ca_cert")
			if err != nil {
				return "", fmt.Errorf("failed to process CA certificate: %v", err)
			}
			connStr += fmt.Sprintf("&sslrootcert=%s", caCertPath)
		}
		if s.Clientcert != "" {
			clientCertPath, err = copyCertToTempFile([]byte(s.Clientcert), "client_cert")
			if err != nil {
				return "", fmt.Errorf("failed to process client certificate: %v", err)
			}
			connStr += fmt.Sprintf("&sslcert=%s", clientCertPath)
		}
		if s.Clientkey != "" {
			clientKeyPath, err = copyCertToTempFile([]byte(s.Clientkey), "client_key")
			if err != nil {
				return "", fmt.Errorf("failed to process client key: %v", err)
			}
			connStr += fmt.Sprintf("&sslkey=%s", clientKeyPath)
		}
		// Store paths temporarily for cleanup on stop
		// Note: In a real Flogo trigger, you'd manage these temporary files
		// more robustly, possibly by storing paths in the Trigger struct
		// and cleaning them up in the Stop method. For this example,
		// we'll rely on the OS to clean up temp files on process exit,
		// or you can add a cleanup slice to the Trigger struct.
	}

	return connStr, nil
}

// copyCertToTempFile writes certificate data to a temporary file and returns its path.
// This is a simplified version; in a production trigger, you'd handle base64 decoding
// and robust temporary file management.
func copyCertToTempFile(certData []byte, prefix string) (string, error) {
	// Decode base64 if the input is base64 encoded
	decodedCertData, err := base64.StdEncoding.DecodeString(string(certData))
	if err != nil {
		// If not base64, assume it's raw PEM content
		decodedCertData = certData
	}

	tmpfile, err := os.CreateTemp("", prefix+"_*.pem")
	if err != nil {
		return "", fmt.Errorf("failed to create temporary certificate file: %v", err)
	}
	defer tmpfile.Close() // Ensure file is closed

	if _, err := tmpfile.Write(decodedCertData); err != nil {
		os.Remove(tmpfile.Name()) // Clean up on write error
		return "", fmt.Errorf("failed to write certificate data to temporary file: %v", err)
	}

	// Ensure file permissions are restrictive
	if err := os.Chmod(tmpfile.Name(), 0600); err != nil {
		os.Remove(tmpfile.Name())
		return "", fmt.Errorf("failed to set permissions on temporary certificate file: %v", err)
	}

	return tmpfile.Name(), nil
}

// pingDBWithRetry attempts to ping the database with retries.
func pingDBWithRetry(db *sql.DB, maxAttempts int, retryDelay int, logger log.Logger) error {
	if maxAttempts == 0 {
		return db.Ping() // No retries, just try once
	}

	for i := 0; i <= maxAttempts; i++ {
		err := db.Ping()
		if err == nil {
			return nil // Success
		}

		// Check for specific transient errors that warrant a retry
		if err == driver.ErrBadConn || strings.Contains(err.Error(), "connection refused") ||
			strings.Contains(err.Error(), "network is unreachable") ||
			strings.Contains(err.Error(), "connection reset by peer") ||
			strings.Contains(err.Error(), "dial tcp: lookup") || // DNS resolution issues
			strings.Contains(err.Error(), "timeout") ||
			strings.Contains(err.Error(), "i/o timeout") {

			logger.Warnf("Database ping failed (attempt %d/%d): %v. Retrying in %d seconds...", i+1, maxAttempts+1, err, retryDelay)
			time.Sleep(time.Duration(retryDelay) * time.Second)
			continue
		} else {
			// Non-retriable error (e.g., invalid credentials, syntax error)
			return fmt.Errorf("non-retriable database ping error: %v", err)
		}
	}
	return fmt.Errorf("failed to connect to database after %d attempts", maxAttempts+1)
}
