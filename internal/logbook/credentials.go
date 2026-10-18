package logbook

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Credentials is a Cloudflare Access service token, sent with every request.
type Credentials struct {
	ClientID     string `yaml:"client_id"`
	ClientSecret string `yaml:"client_secret"`
}

// LoadCredentials reads the service token from a YAML file that only its owner
// can read, since anyone holding it can write to the Logbook.
func LoadCredentials(path string) (Credentials, error) {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return Credentials{}, fmt.Errorf("no Logbook credentials at %s; see `gametrak sync --help`", path)
	}
	if err != nil {
		return Credentials{}, fmt.Errorf("failed to read Logbook credentials: %w", err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		return Credentials{}, fmt.Errorf("%s can be read by other users (mode %04o); run: chmod 600 %s", path, perm, path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return Credentials{}, fmt.Errorf("failed to read Logbook credentials: %w", err)
	}
	var creds Credentials
	if err := yaml.Unmarshal(data, &creds); err != nil {
		return Credentials{}, fmt.Errorf("failed to parse %s: %w", path, err)
	}
	if creds.ClientID == "" || creds.ClientSecret == "" {
		return Credentials{}, fmt.Errorf("%s needs both client_id and client_secret", path)
	}
	return creds, nil
}
