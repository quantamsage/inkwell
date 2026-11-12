package config

import "testing"

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := load(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != defaultAddr || cfg.DatabasePath != defaultDatabasePath || cfg.MaxDocumentBytes != defaultMaxDocumentBytes || cfg.SecureCookies {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func TestLoadOverrides(t *testing.T) {
	cfg, err := load(env(map[string]string{
		"ADDR":               ":9000",
		"DATABASE_PATH":      "/tmp/x.db",
		"MAX_DOCUMENT_BYTES": "2048",
		"SECURE_COOKIES":     "true",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != ":9000" || cfg.DatabasePath != "/tmp/x.db" || cfg.MaxDocumentBytes != 2048 || !cfg.SecureCookies {
		t.Fatalf("overrides not applied: %+v", cfg)
	}
}

func TestLoadInvalid(t *testing.T) {
	for _, m := range []map[string]string{
		{"MAX_DOCUMENT_BYTES": "abc"},
		{"MAX_DOCUMENT_BYTES": "10"},
		{"SECURE_COOKIES": "maybe"},
	} {
		if _, err := load(env(m)); err == nil {
			t.Errorf("expected error for %v", m)
		}
	}
}
