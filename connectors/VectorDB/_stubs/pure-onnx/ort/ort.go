// Package ort is a no-CGo stub for github.com/amikos-tech/pure-onnx/ort.
// All functions return errNotSupported; no ONNX runtime is linked.
package ort

import "errors"

var errNotSupported = errors.New("ONNX runtime not supported (pure-onnx stub)")

// BootstrapOption configures bootstrap behaviour (stub — no-op).
type BootstrapOption func(*bootstrapConfig) error

type bootstrapConfig struct{}

// WithBootstrapLibraryPath sets a library path (no-op stub).
func WithBootstrapLibraryPath(_ string) BootstrapOption {
	return func(*bootstrapConfig) error { return nil }
}

// WithBootstrapCacheDir sets a cache directory (no-op stub).
func WithBootstrapCacheDir(_ string) BootstrapOption {
	return func(*bootstrapConfig) error { return nil }
}

// WithBootstrapVersion sets a version string (no-op stub).
func WithBootstrapVersion(_ string) BootstrapOption {
	return func(*bootstrapConfig) error { return nil }
}

// WithBootstrapDisableDownload disables download (no-op stub).
func WithBootstrapDisableDownload(_ bool) BootstrapOption {
	return func(*bootstrapConfig) error { return nil }
}

// InitializeEnvironmentWithBootstrap always returns errNotSupported.
func InitializeEnvironmentWithBootstrap(_ ...BootstrapOption) error {
	return errNotSupported
}

// DestroyEnvironment always returns errNotSupported.
func DestroyEnvironment() error {
	return errNotSupported
}

// EnsureOnnxRuntimeSharedLibrary always returns errNotSupported.
func EnsureOnnxRuntimeSharedLibrary(_ ...BootstrapOption) (string, error) {
	return "", errNotSupported
}
