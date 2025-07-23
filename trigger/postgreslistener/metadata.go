package postgreslistener

import (
	"github.com/project-flogo/core/data/coerce"
)

// Settings structure for the PostgreSQL Listener Trigger
type Settings struct {
	Host                 string `md:"host,required"`
	Port                 int    `md:"port,required"`
	User                 string `md:"user,required"`
	Password             string `md:"password,required"`
	DatabaseName         string `md:"databaseName,required"` // Changed from DbName to DatabaseName for consistency
	SSLMode              string `md:"sslmode"`
	ConnectionTimeout    int    `md:"connectionTimeout"`    // In seconds
	MaxConnRetryAttempts int    `md:"maxConnectAttempts"`   // Max connection retry attempts
	TLSConfig            bool   `md:"tlsConfig"`            // Whether TLS is configured
	TLSMode              string `md:"tlsParam"`             // TLS parameter (e.g., VerifyCA, VerifyFull)
	Cacert               string `md:"cacert"`               // CA Certificate content (base64 encoded)
	Clientcert           string `md:"clientCert"`           // Client Certificate content (base64 encoded)
	Clientkey            string `md:"clientKey"`            // Client Key content (base64 encoded)
	ConnectionRetryDelay int    `md:"connectionRetryDelay"` // Delay between connection retry attempts in seconds
}

// HandlerSettings structure for individual channels to listen on
type HandlerSettings struct {
	Channel string `md:"channel,required"` // The PostgreSQL channel to listen on
}

// Output structure for the data received from PostgreSQL NOTIFY
type Output struct {
	Payload string `md:"payload"` // The payload string from the NOTIFY message
}

// ToMap method for Output (required by Flogo)
func (o *Output) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"payload": o.Payload,
	}
}

// FromMap method for Output (required by Flogo)
func (o *Output) FromMap(values map[string]interface{}) error {
	var err error
	o.Payload, err = coerce.ToString(values["payload"])
	if err != nil {
		return err
	}
	return nil
}
