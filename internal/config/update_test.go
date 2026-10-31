package config

import (
	"strings"
	"testing"
)

func TestUpdateCheckDefaultsOn(t *testing.T) {
	cfg, err := Load(writeConfig(t, minimal))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Update.Checks() {
		t.Error("update.check defaults to false")
	}
	cfg, err = Load(writeConfig(t, minimal+"update:\n  check: false\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Update.Checks() {
		t.Error("update.check: false is ignored")
	}
}

func TestUpdateRejectsUnknownKeysAndBadValues(t *testing.T) {
	for block, want := range map[string]string{
		"update:\n  chek: true\n":   "field chek not found",
		"update:\n  check: daily\n": "cannot unmarshal",
	} {
		if _, err := Load(writeConfig(t, minimal+block)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: err = %v, want %q", block, err, want)
		}
	}
}
