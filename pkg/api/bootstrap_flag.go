package api

import (
	"encoding/json"
	"os"
)

// BootstrapFlag is the one-time payload written to disk when the first
// global ADMIN account is auto-created (see cmd/tricms/main.go), so the
// login page can display the generated credentials until they are used to
// log in for the first time (see clearBootstrapFlag).
type BootstrapFlag struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// WriteBootstrapFlag persists flag as JSON at path, readable only by the
// process owner since it holds a plaintext password.
func WriteBootstrapFlag(path string, flag BootstrapFlag) error {
	data, err := json.Marshal(flag)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// readBootstrapFlag reads back a flag written by WriteBootstrapFlag. A
// missing or unreadable file just means there's nothing to display -- not
// an error, since that's the expected state once the flag is cleared.
func readBootstrapFlag(path string) (*BootstrapFlag, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var flag BootstrapFlag
	if err := json.Unmarshal(data, &flag); err != nil {
		return nil, false
	}
	return &flag, true
}
