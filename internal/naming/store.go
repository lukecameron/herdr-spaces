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

// State is everything the plugin remembers between runs.
type State struct {
	// Owned maps a workspace id to the name the plugin gave it.
	Owned map[string]Owned `json:"owned"`
	// Born maps a workspace id to the label it had when the plugin first saw
	// it appear. Only a workspace that was created while the plugin was
	// running, and still carries that label, is named automatically: Herdr
	// does not record who set a label, so a workspace that already existed
	// when the plugin started may have been named by hand and is left alone.
	Born map[string]string `json:"born"`
}

func newState() State {
	return State{Owned: map[string]Owned{}, Born: map[string]string{}}
}

// Store persists State in state.json under the state directory. Actions and
// the daemon both write it, so every update happens under a file lock.
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

func (s *Store) path() string { return filepath.Join(s.dir, "state.json") }

// Load reads the current state. A state file from version 0.1, which held
// only the owned map, is read as such.
func (s *Store) Load() (State, error) {
	st := newState()
	data, err := os.ReadFile(s.path())
	if errors.Is(err, os.ErrNotExist) {
		return s.loadLegacy(st)
	}
	if err != nil {
		return st, err
	}
	if err := json.Unmarshal(data, &st); err != nil {
		return st, err
	}
	if st.Owned == nil {
		st.Owned = map[string]Owned{}
	}
	if st.Born == nil {
		st.Born = map[string]string{}
	}
	return st, nil
}

func (s *Store) loadLegacy(st State) (State, error) {
	data, err := os.ReadFile(filepath.Join(s.dir, "owned.json"))
	if errors.Is(err, os.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	if err := json.Unmarshal(data, &st.Owned); err != nil {
		return st, err
	}
	if st.Owned == nil {
		st.Owned = map[string]Owned{}
	}
	return st, nil
}

// Update applies fn to the state under the lock and writes it back.
func (s *Store) Update(fn func(*State)) error {
	lock, err := os.OpenFile(filepath.Join(s.dir, "state.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer func() { _ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) }()

	st, err := s.Load()
	if err != nil {
		return err
	}
	fn(&st)

	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path() + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path()); err != nil {
		return err
	}
	_ = os.Remove(filepath.Join(s.dir, "owned.json"))
	return nil
}
