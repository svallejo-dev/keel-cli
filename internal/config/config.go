// Package config resolves the platform's own runtime settings. Resolution layers,
// highest priority last: embedded defaults -> environment variables. This mirrors
// the fixed-priority layering described in CONNECTION_INJECTION_PATTERN.md.
package config

import (
	"os"
	"os/exec"
	"path/filepath"
)

// Engine selects which provider backs real work.
type Engine string

const (
	// EngineAuto uses the real substrate when Docker is available, else mock.
	EngineAuto Engine = "auto"
	// EngineReal forces floci/Docker providers.
	EngineReal Engine = "real"
	// EngineMock forces the in-memory simulator.
	EngineMock Engine = "mock"
)

// Config holds resolved platform settings.
type Config struct {
	Home          string // registry/audit root (default ~/.keel)
	Region        string // AWS region reported to floci
	FlociEndpoint string // AWS API endpoint exposed by floci
	DockerNetwork string // shared docker network for backing containers
	Engine        Engine // auto | real | mock
	AWSAccessKey  string // dummy creds accepted by floci
	AWSSecretKey  string
	GCPProject    string // default project id for GCP emulators
}

// Default returns the embedded defaults layer.
func Default() Config {
	return Config{
		Home:   defaultHome(),
		Region: "us-east-1",
		// 127.0.0.1 (not "localhost") forces IPv4: on Windows "localhost" resolves to
		// IPv6 ::1 first, but floci publishes on IPv4, giving "connection refused".
		FlociEndpoint: "http://127.0.0.1:4566",
		DockerNetwork: "keel-net",
		Engine:        EngineAuto,
		AWSAccessKey:  "test",
		AWSSecretKey:  "test",
		// A demo-* project id makes the Firebase/GCP emulators refuse to talk to
		// real Google resources, so local work can never leak to the cloud.
		GCPProject: "demo-keel",
	}
}

// Load returns defaults overridden by environment variables (the winning layer).
func Load() Config {
	c := Default()
	if v := os.Getenv("KEEL_HOME"); v != "" {
		c.Home = v
	}
	if v := os.Getenv("KEEL_REGION"); v != "" {
		c.Region = v
	}
	if v := os.Getenv("KEEL_FLOCI_ENDPOINT"); v != "" {
		c.FlociEndpoint = v
	}
	if v := os.Getenv("KEEL_DOCKER_NETWORK"); v != "" {
		c.DockerNetwork = v
	}
	if v := os.Getenv("KEEL_GCP_PROJECT"); v != "" {
		c.GCPProject = v
	}
	if v := os.Getenv("KEEL_ENGINE"); v != "" {
		c.Engine = Engine(v)
	}
	return c
}

// ResolveEngine turns EngineAuto into a concrete engine based on Docker presence.
func (c Config) ResolveEngine() Engine {
	switch c.Engine {
	case EngineReal, EngineMock:
		return c.Engine
	default:
		if DockerAvailable() {
			return EngineReal
		}
		return EngineMock
	}
}

func defaultHome() string {
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, ".keel")
	}
	return ".keel"
}

// DockerAvailable reports whether a working Docker CLI + daemon is reachable.
func DockerAvailable() bool {
	path, err := exec.LookPath("docker")
	if err != nil {
		return false
	}
	// `docker info` succeeds only when the daemon is actually running.
	return exec.Command(path, "info", "--format", "{{.ServerVersion}}").Run() == nil
}
