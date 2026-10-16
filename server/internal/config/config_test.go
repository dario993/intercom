package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingFileGivesDefaults(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "none.json"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Settings.HTTPPort != 8080 || c.Settings.UDPPort != 8081 || c.Settings.ServerName != "Intercom" || c.NextID != 1 {
		t.Errorf("defaults = %+v", c)
	}
}

func TestStoreUpdatePersistsAtomically(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	err = s.Update(func(c *Config) error {
		c.Channels = append(c.Channels, Channel{ID: c.NewID(), Name: "Regia", Color: "#ff0000"})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	again, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Channels) != 1 || again.Channels[0].ID != 1 || again.NextID != 2 {
		t.Errorf("reloaded = %+v", again)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("temp files left behind: %v", entries)
	}
}

func TestStoreUpdateErrorKeepsOldConfig(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "c.json"))
	if err != nil {
		t.Fatal(err)
	}
	boom := errors.New("boom")
	err = s.Update(func(c *Config) error {
		c.Settings.ServerName = "Changed"
		return boom
	})
	if err != boom {
		t.Fatalf("err = %v", err)
	}
	s.View(func(c *Config) {
		if c.Settings.ServerName != "Intercom" {
			t.Errorf("failed update leaked: %q", c.Settings.ServerName)
		}
	})
}

func TestPasswordsAndLookups(t *testing.T) {
	h, err := HashPassword("segreta")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(h, "segreta") || CheckPassword(h, "altra") || CheckPassword("", "") {
		t.Error("CheckPassword wrong")
	}
	c := Default()
	c.Users = []User{{ID: 5, Name: "Anna"}}
	c.Channels = []Channel{{ID: 6, Name: "Luci"}}
	if c.UserByName("anna") == nil || c.UserByID(5) == nil || c.UserByID(6) != nil || c.ChannelByID(6) == nil {
		t.Error("lookups wrong")
	}
}
