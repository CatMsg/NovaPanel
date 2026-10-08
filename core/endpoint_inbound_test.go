package core

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/logger"
	"github.com/op/go-logging"
)

func TestAddInboundNaiveRequiresUsers(t *testing.T) {
	logger.InitLogger(logging.ERROR)
	if err := database.InitDB(filepath.Join(t.TempDir(), "naive-core.db")); err != nil {
		t.Fatal(err)
	}
	runtime := NewCore()
	t.Cleanup(func() {
		if err := runtime.Stop(); err != nil {
			t.Errorf("stop core: %v", err)
		}
	})
	if err := runtime.Start([]byte(`{"log":{"disabled":true},"outbounds":[{"type":"direct","tag":"direct"}]}`)); err != nil {
		t.Fatal(err)
	}
	empty := []byte(`{"type":"naive","tag":"naive-test","listen":"127.0.0.1","network":"tcp","users":[]}`)
	if err := runtime.AddInbound(empty); err == nil || !strings.Contains(err.Error(), "missing users") {
		t.Fatalf("expected core to reject empty Naive, got %v", err)
	}
	if _, exists := runtime.GetInstance().Inbound().Get("naive-test"); exists {
		t.Fatal("failed empty Naive was installed")
	}
	withUser := []byte(`{"type":"naive","tag":"naive-test","listen":"127.0.0.1","network":"tcp","users":[{"username":"test-user","password":"test-pass"}]}`)
	if err := runtime.AddInbound(withUser); err != nil {
		t.Fatalf("add first Naive user: %v", err)
	}
	if _, exists := runtime.GetInstance().Inbound().Get("naive-test"); !exists {
		t.Fatal("Naive with user was not installed")
	}
	if err := runtime.RemoveInbound("naive-test"); err != nil {
		t.Fatal(err)
	}
	if err := runtime.AddInbound([]byte(`{"type":"mixed","tag":"mixed-test","listen":"127.0.0.1","users":[]}`)); err != nil {
		t.Fatalf("non-Naive empty users must still work: %v", err)
	}
}
