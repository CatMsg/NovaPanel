package api

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/service"
	"github.com/gin-gonic/gin"
)

func TestGetDbPreventsCachingSensitiveBackup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	if err := database.InitDB(filepath.Join(t.TempDir(), "backup.db")); err != nil {
		t.Fatalf("initialize database: %v", err)
	}

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest("GET", "/api/getdb", nil)

	(&ApiService{}).GetDb(context)

	if recorder.Code != 200 {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("Cache-Control = %q, want private, no-store", got)
	}
	if got := recorder.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
}

func TestDatabaseUploadHandlersRejectOversizedDeclaredBodiesBeforeReading(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handlers := []struct {
		name    string
		handler func(*gin.Context)
	}{
		{name: "importdb", handler: (&ApiService{}).ImportDb},
		{name: "validateBackup", handler: (&ApiService{}).ValidateDb},
	}
	for _, tc := range handlers {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(recorder)
			request := httptest.NewRequest("POST", "/api/"+tc.name, nil)
			body := &readTrackingBody{}
			request.Body = body
			request.ContentLength = maxDatabaseUploadBodySize + 1
			context.Request = request

			tc.handler(context)

			if body.read {
				t.Fatal("oversized request body was read before rejection")
			}
			if !strings.Contains(recorder.Body.String(), "database upload exceeds the 1 GiB limit") {
				t.Fatalf("response = %q, want an upload-size error", recorder.Body.String())
			}
		})
	}
}

func TestDatabaseUploadBodyLimitRejectsOversizedStreamingBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest("POST", "/api/importdb", strings.NewReader("123456789"))
	request.ContentLength = -1
	context.Request = request
	limitRequestBody(context, 4)

	_, err := io.Copy(io.Discard, context.Request.Body)
	var maxBytesErr *http.MaxBytesError
	if !errors.As(err, &maxBytesErr) {
		t.Fatalf("streaming body error = %v, want *http.MaxBytesError", err)
	}
}

func TestDatabaseUploadReadDeadlineBoundsIdleAndTotalDuration(t *testing.T) {
	startedAt := time.Unix(1_800_000_000, 0)
	for _, tc := range []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{
			name: "idle timeout",
			now:  startedAt.Add(10 * time.Minute),
			want: startedAt.Add(10*time.Minute + databaseUploadReadIdleTimeout),
		},
		{
			name: "total timeout caps idle timeout",
			now:  startedAt.Add(databaseUploadMaxDuration - 10*time.Second),
			want: startedAt.Add(databaseUploadMaxDuration),
		},
		{
			name: "total timeout elapsed",
			now:  startedAt.Add(databaseUploadMaxDuration + time.Second),
			want: startedAt.Add(databaseUploadMaxDuration),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := databaseUploadReadDeadline(startedAt, tc.now); !got.Equal(tc.want) {
				t.Fatalf("read deadline = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestDatabaseUploadHandlersRejectConcurrentWorkBeforeReading(t *testing.T) {
	gin.SetMode(gin.TestMode)
	release, err := acquireDatabaseUpload()
	if err != nil {
		t.Fatalf("occupy database upload gate: %v", err)
	}
	defer release()

	for _, tc := range []struct {
		name    string
		handler func(*gin.Context)
	}{
		{name: "importdb", handler: (&ApiService{}).ImportDb},
		{name: "validateBackup", handler: (&ApiService{}).ValidateDb},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(recorder)
			request := httptest.NewRequest("POST", "/api/"+tc.name, nil)
			body := &readTrackingBody{}
			request.Body = body
			request.ContentLength = 1
			context.Request = request

			tc.handler(context)

			if body.read {
				t.Fatal("concurrent database request body was read before rejection")
			}
			if !strings.Contains(recorder.Body.String(), "another database upload or validation is in progress") {
				t.Fatalf("response = %q, want concurrent-operation error", recorder.Body.String())
			}
		})
	}
}

func TestDatabaseImportChecksRemoteBackupGuardBeforeReading(t *testing.T) {
	gin.SetMode(gin.TestMode)
	releaseBackup, err := service.AcquireRemoteBackupRestoreGuard()
	if err != nil {
		t.Fatalf("occupy remote backup guard: %v", err)
	}
	defer releaseBackup()

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest("POST", "/api/importdb", nil)
	body := &readTrackingBody{}
	request.Body = body
	request.ContentLength = 1
	context.Request = request

	(&ApiService{}).ImportDb(context)

	if body.read {
		t.Fatal("database import body was read while remote backup guard was busy")
	}
	if !strings.Contains(recorder.Body.String(), "another remote backup operation is running") {
		t.Fatalf("response = %q, want remote-backup-busy error", recorder.Body.String())
	}
}

func TestDatabaseUploadLocksReleaseAfterMalformedMultipart(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name    string
		handler func(*gin.Context)
	}{
		{name: "importdb", handler: (&ApiService{}).ImportDb},
		{name: "validateBackup", handler: (&ApiService{}).ValidateDb},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(recorder)
			context.Request = httptest.NewRequest("POST", "/api/"+tc.name, nil)
			tc.handler(context)

			releaseUpload, err := acquireDatabaseUpload()
			if err != nil {
				t.Fatalf("database upload gate was not released: %v", err)
			}
			releaseUpload()

			if tc.name == "importdb" {
				releaseBackup, err := service.AcquireRemoteBackupRestoreGuard()
				if err != nil {
					t.Fatalf("remote backup guard was not released: %v", err)
				}
				releaseBackup()
			}
		})
	}
}

type readTrackingBody struct {
	read bool
}

func (b *readTrackingBody) Read([]byte) (int, error) {
	b.read = true
	return 0, io.EOF
}

func (*readTrackingBody) Close() error { return nil }
