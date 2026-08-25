package main

import "testing"

func TestAddressValidation(t *testing.T) {
	for _, address := range []string{"127.0.0.1:19081", "[::1]:19081"} {
		if err := validateAddress(address); err != nil {
			t.Fatalf("应接受 %s: %v", address, err)
		}
	}
	for _, address := range []string{"0.0.0.0:19081", "127.0.0.1", "localhost:19081", "127.0.0.1:0"} {
		if err := validateAddress(address); err == nil {
			t.Fatalf("应拒绝 %s", address)
		}
	}
}

func TestPortEnvironmentConfig(t *testing.T) {
	t.Setenv("PORT", "19123")
	cfg, err := parseConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.address != "127.0.0.1:19123" {
		t.Fatalf("PORT 未生效: %s", cfg.address)
	}
	if cfg.selfcheck {
		t.Fatal("默认不应进入 selfcheck")
	}
}
