// Package store keeps the list of paired devices on disk. Device keys are
// never stored in clear: only their SHA-256 hash is written.
package store

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Device is a paired phone.
type Device struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	KeyHash  string    `json:"key_sha256"`
	Created  time.Time `json:"created"`
	LastSeen time.Time `json:"last_seen,omitempty"`
}

// Store is a file-backed device list. It re-reads the file on every
// lookup so that `tapis revoke` takes effect on a running server.
type Store struct {
	mu   sync.Mutex
	path string
}

func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	os.Chmod(dir, 0o700) // tighten a folder created earlier with looser rights
	return &Store{path: filepath.Join(dir, "devices.json")}, nil
}

func (s *Store) load() ([]Device, error) {
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var d []Device
	if err := json.Unmarshal(b, &d); err != nil {
		return nil, err
	}
	return d, nil
}

func (s *Store) save(d []Device) error {
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// RandomToken returns n random bytes encoded as unpadded base64url.
func RandomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func hashKey(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:])
}

// RandomDigits returns n uniformly random decimal digits.
func RandomDigits(n int) string {
	b := make([]byte, n)
	for i := range b {
		v, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			panic(err)
		}
		b[i] = byte('0' + v.Int64())
	}
	return string(b)
}

// Add registers a new device and returns it with its secret key (the
// key is only ever returned here).
func (s *Store) Add(name string) (Device, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	devs, err := s.load()
	if err != nil {
		return Device{}, "", err
	}
	idb := make([]byte, 8)
	rand.Read(idb)
	key := RandomToken(32)
	d := Device{ID: hex.EncodeToString(idb), Name: name, KeyHash: hashKey(key), Created: time.Now().UTC()}
	devs = append(devs, d)
	return d, key, s.save(devs)
}

// Verify checks a device's credentials and updates its last-seen time.
func (s *Store) Verify(id, key string) (Device, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	devs, err := s.load()
	if err != nil {
		return Device{}, false
	}
	want := hashKey(key)
	for i, d := range devs {
		if d.ID == id && subtle.ConstantTimeCompare([]byte(d.KeyHash), []byte(want)) == 1 {
			devs[i].LastSeen = time.Now().UTC()
			s.save(devs)
			return devs[i], true
		}
	}
	return Device{}, false
}

// Exists reports whether the device is still paired.
func (s *Store) Exists(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	devs, _ := s.load()
	for _, d := range devs {
		if d.ID == id {
			return true
		}
	}
	return false
}

// List returns all devices, oldest first.
func (s *Store) List() ([]Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, err := s.load()
	sort.Slice(d, func(i, j int) bool { return d[i].Created.Before(d[j].Created) })
	return d, err
}

// Revoke removes a device. It returns false if no such device exists.
func (s *Store) Revoke(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	devs, err := s.load()
	if err != nil {
		return false, err
	}
	out := devs[:0]
	found := false
	for _, d := range devs {
		if d.ID == id {
			found = true
			continue
		}
		out = append(out, d)
	}
	if !found {
		return false, nil
	}
	return true, s.save(out)
}

// RevokeAll removes every device.
func (s *Store) RevokeAll() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.save([]Device{})
}
