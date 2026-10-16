// Package config holds the server configuration and persists it as JSON.
package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/crypto/bcrypt"
)

type Channel struct {
	ID    uint16 `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

type Assignment struct {
	ChannelID uint16 `json:"channelId"`
	Talk      bool   `json:"talk"`
	Listen    bool   `json:"listen"`
}

type User struct {
	ID           uint16       `json:"id"`
	Name         string       `json:"name"`
	PasswordHash string       `json:"passwordHash"`
	Enabled      bool         `json:"enabled"`
	Channels     []Assignment `json:"channels"`
	AllCall      bool         `json:"allCall"`
	PrivateCall  bool         `json:"privateCall"`
}

type Settings struct {
	ServerName string `json:"serverName"`
	HTTPPort   int    `json:"httpPort"`
	UDPPort    int    `json:"udpPort"`
	AdminHash  string `json:"adminHash"`
}

type Config struct {
	Settings Settings  `json:"settings"`
	Channels []Channel `json:"channels"`
	Users    []User    `json:"users"`
	NextID   uint16    `json:"nextId"`
}

func Default() *Config {
	return &Config{
		Settings: Settings{ServerName: "Intercom", HTTPPort: 8080, UDPPort: 8081},
		NextID:   1,
	}
}

// NewID returns a fresh id for a user or channel. 0 and 0xFFFF are reserved on the wire.
// ponytail: no reuse of freed ids, 65k creations before wrap-around collisions.
func (c *Config) NewID() uint16 {
	if c.NextID == 0 || c.NextID == 0xFFFF {
		c.NextID = 1
	}
	id := c.NextID
	c.NextID++
	return id
}

func (c *Config) UserByName(name string) *User {
	for i := range c.Users {
		if strings.EqualFold(c.Users[i].Name, name) {
			return &c.Users[i]
		}
	}
	return nil
}

func (c *Config) UserByID(id uint16) *User {
	for i := range c.Users {
		if c.Users[i].ID == id {
			return &c.Users[i]
		}
	}
	return nil
}

func (c *Config) ChannelByID(id uint16) *Channel {
	for i := range c.Channels {
		if c.Channels[i].ID == id {
			return &c.Channels[i]
		}
	}
	return nil
}

func HashPassword(p string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(p), bcrypt.DefaultCost)
	return string(b), err
}

func CheckPassword(hash, p string) bool {
	return hash != "" && bcrypt.CompareHashAndPassword([]byte(hash), []byte(p)) == nil
}

// Load reads path; a missing file yields the defaults.
func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return nil, err
	}
	c := Default()
	if err := json.Unmarshal(b, c); err != nil {
		return nil, err
	}
	return c, nil
}

// Save writes c to path atomically: temp file in the same directory, fsync, rename.
func Save(path string, c *Config) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".intercom-config-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Store guards the live configuration. View callbacks must not call back into code that takes other locks.
type Store struct {
	mu   sync.RWMutex
	path string
	cfg  *Config
}

func Open(path string) (*Store, error) {
	c, err := Load(path)
	if err != nil {
		return nil, err
	}
	return &Store{path: path, cfg: c}, nil
}

func (s *Store) View(fn func(c *Config)) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	fn(s.cfg)
}

// Update runs fn on a copy; the copy is saved and kept only if fn and the save both succeed.
func (s *Store) Update(fn func(c *Config) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := clone(s.cfg)
	if err != nil {
		return err
	}
	if err := fn(c); err != nil {
		return err
	}
	if err := Save(s.path, c); err != nil {
		return err
	}
	s.cfg = c
	return nil
}

func clone(c *Config) (*Config, error) {
	b, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	n := &Config{}
	return n, json.Unmarshal(b, n)
}
