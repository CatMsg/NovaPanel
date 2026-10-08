package core

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/logger"
	"github.com/op/go-logging"
	E "github.com/sagernet/sing/common/exceptions"
)

func TestNaiveCloseErrorOnlyNormalizesClosedLeaves(t *testing.T) {
	realFailure := errors.New("TLS teardown failed")
	closedListener := &net.OpError{Op: "close", Net: "tcp", Err: net.ErrClosed}
	for _, test := range []struct {
		name     string
		closeErr error
		wantNil  bool
	}{
		{"success", nil, true},
		{"closed-listener", closedListener, true},
		{"wrapped-closed-listener", E.Cause(closedListener, "close Naive"), true},
		{"multiple-closed-listeners", E.Errors(closedListener, fmt.Errorf("HTTP: %w", net.ErrClosed)), true},
		{"joined-closed-listeners", errors.Join(closedListener, net.ErrClosed), true},
		{"real-failure", realFailure, false},
		{"mixed-close-errors", E.Errors(closedListener, realFailure), false},
		{"wrapped-mixed-close-errors", fmt.Errorf("close: %w", errors.Join(net.ErrClosed, realFailure)), false},
		{"upstream-mixed-cause", E.Cause(realFailure, net.ErrClosed), false},
		{"file-closed-is-not-network-closed", os.ErrClosed, false},
		{"matching-error-text-is-not-network-closed", errors.New(net.ErrClosed.Error()), false},
		{"unknown-tag", os.ErrInvalid, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := normalizeNaiveCloseError(test.closeErr)
			if test.wantNil {
				if got != nil {
					t.Fatalf("idempotent teardown: %v", got)
				}
			} else if got != test.closeErr {
				t.Fatalf("original error must be preserved: got %v, want %v", got, test.closeErr)
			}
		})
	}
}

func TestNaiveInboundRuntimeClosePaths(t *testing.T) {
	logger.InitLogger(logging.ERROR)
	if err := database.InitDB(filepath.Join(t.TempDir(), "naive-close.db")); err != nil {
		t.Fatal(err)
	}
	config := []byte(`{"type":"naive","tag":"naive-close","listen":"127.0.0.1","network":"tcp","users":[{"username":"test-user","password":"test-pass"}]}`)
	for _, path := range []string{"remove", "replace", "stop"} {
		t.Run(path, func(t *testing.T) {
			runtime := NewCore()
			t.Cleanup(func() {
				if err := runtime.Stop(); err != nil {
					t.Errorf("stop core: %v", err)
				}
			})
			if err := runtime.Start([]byte(`{"log":{"disabled":true},"outbounds":[{"type":"direct","tag":"direct"}]}`)); err != nil {
				t.Fatal(err)
			}
			if err := runtime.AddInbound(config); err != nil {
				t.Fatal(err)
			}
			original, found := runtime.GetInstance().Inbound().Get("naive-close")
			if _, wrapped := original.(*naiveInbound); !found || !wrapped {
				t.Fatal("runtime registry did not install the Naive lifecycle wrapper")
			}
			switch path {
			case "remove":
				if err := runtime.RemoveInbound("naive-close"); err != nil {
					t.Fatal(err)
				}
				if _, found := runtime.GetInstance().Inbound().Get("naive-close"); found {
					t.Fatal("removed Naive is still registered")
				}
			case "replace":
				if err := runtime.AddInbound(config); err != nil {
					t.Fatalf("replace Naive: %v", err)
				}
				current, found := runtime.GetInstance().Inbound().Get("naive-close")
				if _, wrapped := current.(*naiveInbound); !found || !wrapped || current == original {
					t.Fatal("replacement did not install a new wrapped Naive")
				}
			case "stop":
				if err := runtime.Stop(); err != nil {
					t.Fatalf("stop active Naive: %v", err)
				}
			}
		})
	}
}
