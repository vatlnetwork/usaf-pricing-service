package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	for _, tt := range []struct {
		name  string
		body  string
		valid bool
	}{
		{"defaults", `{}`, true},
		{"custom", `{"server":{"address":"127.0.0.1:9090"},"mongodb":{"uri":"mongodb://db:27017","database":"pricing","collection":"programs","connect_timeout_seconds":2,"operation_timeout_seconds":3}}`, true},
		{"unknown field", `{"mongo_db":{}}`, false},
		{"unknown nested field", `{"mongodb":{"databse":"pricing"}}`, false},
		{"trailing object", `{} {}`, false},
		{"null", `null`, false},
		{"malformed", `{`, false},
		{"blank database", `{"mongodb":{"database":" "}}`, false},
		{"blank collection", `{"mongodb":{"collection":""}}`, false},
		{"invalid URI", `{"mongodb":{"uri":"https://localhost"}}`, false},
		{"zero timeout", `{"mongodb":{"operation_timeout_seconds":0}}`, false},
		{"negative timeout", `{"mongodb":{"connect_timeout_seconds":-1}}`, false},
		{"large timeout", `{"mongodb":{"operation_timeout_seconds":301}}`, false},
		{"invalid address", `{"server":{"address":"localhost"}}`, false},
		{"invalid port", `{"server":{"address":":70000"}}`, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte(tt.body), 0600); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(path)
			if (err == nil) != tt.valid {
				t.Fatalf("Load() error = %v, want valid = %v", err, tt.valid)
			}
			if tt.name == "defaults" && (cfg.Server.Address != ":8080" || cfg.MongoDB.Database != "usaf_pricing") {
				t.Fatalf("unexpected defaults: %+v", cfg)
			}
			if tt.name == "custom" && (cfg.Server.Address != "127.0.0.1:9090" || cfg.MongoDB.Database != "pricing" || cfg.MongoDB.OperationTimeoutSeconds != 3) {
				t.Fatalf("custom configuration not applied: %+v", cfg)
			}
		})
	}
	if _, err := Load(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("expected missing-file error")
	}
}
