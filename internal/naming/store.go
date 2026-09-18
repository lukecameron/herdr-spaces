package naming

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// Owned records a label this plugin wrote. While a workspace still carries
// that label the plugin owns it; once the label differs the user has renamed
// it and the plugin lets go.
type Owned struct {
	Name        string    `json:"name"`
	Fingerprint string    `json:"fingerprint"`
	NamedAt     time.Time `json:"named_at"`
	Model       string    `json:"model,omitempty"`
}

// Store persists ownership in owned.json under the state directory. Actions
// and the daemon both write it, so every update happens under a file lock.
type Store struct {
	dir string
}

// NewStore returns a store rooted at dir, creating it when needed.
func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

func (s *Store) path() string { return filepath.Join(s.dir, "owned.json") }

// Load reads the current ownership map.
func (s *Store) Load() (map[string]Owned, error) {
	data, err := os.ReadFile(s.path())
	if errors.Is(err, os.ErrNotExist) {
		return map[string]Owned{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string]Owned{}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Update applies fn to the ownership map under the lock and writes it back.
func (s *Store) Update(fn func(map[string]Owned)) error {
	lock, err := os.OpenFile(filepath.Join(s.dir, "owned.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer func() { _ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) }()

	owned, err := s.Load()
	if err != nil {
		return err
	}
	fn(owned)

	data, err := json.MarshalIndent(owned, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path() + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path())
}
