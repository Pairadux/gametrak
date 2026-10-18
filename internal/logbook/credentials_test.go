package logbook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeCredentials(t *testing.T, contents string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "logbook-credentials.yaml")
	if err := os.WriteFile(path, []byte(contents), mode); err != nil {
		t.Fatalf("failed to write credentials: %v", err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("failed to chmod credentials: %v", err)
	}
	return path
}

func TestLoadCredentials(t *testing.T) {
	path := writeCredentials(t, "client_id: abc.access\nclient_secret: cfast_xyz\n", 0600)

	creds, err := LoadCredentials(path)
	if err != nil {
		t.Fatalf("LoadCredentials returned error: %v", err)
	}
	if creds != (Credentials{ClientID: "abc.access", ClientSecret: "cfast_xyz"}) {
		t.Errorf("credentials = %+v", creds)
	}
}

func TestLoadCredentialsRefusesAFileOthersCanRead(t *testing.T) {
	path := writeCredentials(t, "client_id: a\nclient_secret: b\n", 0644)

	_, err := LoadCredentials(path)
	if err == nil || !strings.Contains(err.Error(), "chmod 600") {
		t.Errorf("error = %v, want advice to chmod 600", err)
	}
}

func TestLoadCredentialsNeedsBothValues(t *testing.T) {
	path := writeCredentials(t, "client_id: a\n", 0600)

	if _, err := LoadCredentials(path); err == nil {
		t.Error("LoadCredentials accepted a file without client_secret")
	}
	if _, err := LoadCredentials(filepath.Join(t.TempDir(), "absent.yaml")); err == nil {
		t.Error("LoadCredentials accepted a missing file")
	}
}
