package service

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/CatMsg/NovaPanel/database/model"
	"gorm.io/gorm"
)

func TestInboundVisibilityOnlyPreservesRuntimeAndConnection(t *testing.T) {
	db, svc := setupInboundRuntimeTest(t)
	inbound := model.Inbound{Type: "mixed", Tag: "visibility-live", Options: inboundTestOptions(t, "mixed")}
	if err := db.Create(&inbound).Error; err != nil {
		t.Fatal(err)
	}
	client := seedInboundRuntimeClient(t, db, inbound.Id, true)
	config, err := inbound.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if err := corePtr.AddInbound(config); err != nil {
		t.Fatal(err)
	}
	before, _ := corePtr.GetInstance().Inbound().Get(inbound.Tag)
	var options struct {
		Port int `json:"listen_port"`
	}
	if err := json.Unmarshal(inbound.Options, &options); err != nil {
		t.Fatal(err)
	}
	echo, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echo.Close()
	go func() {
		conn, err := echo.Accept()
		if err == nil {
			defer conn.Close()
			io.Copy(conn, conn)
		}
	}()
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", options.Port), 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", echo.Addr(), echo.Addr())
	reader := bufio.NewReader(conn)
	status, err := reader.ReadString('\n')
	if err != nil || status != "HTTP/1.1 200 Connection established\r\n" {
		t.Fatalf("proxy CONNECT: %q, %v", status, err)
	}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if line == "\r\n" {
			break
		}
	}
	for _, hidden := range []bool{true, false} {
		inbound.ExcludeFromSub = hidden
		var action func() error
		if err := db.Transaction(func(tx *gorm.DB) error {
			var err error
			action, err = svc.Save(tx, "edit", inboundRuntimePayload(t, inbound), "", "localhost")
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if action != nil {
			t.Fatal("visibility-only edit scheduled runtime, forwarding or sidecar work")
		}
		after, _ := corePtr.GetInstance().Inbound().Get(inbound.Tag)
		if before != after {
			t.Fatal("visibility-only edit replaced the active listener")
		}
		payload := []byte(fmt.Sprintf("still-connected-hidden=%t", hidden))
		if _, err := conn.Write(payload); err != nil {
			t.Fatal(err)
		}
		response := make([]byte, len(payload))
		if _, err := io.ReadFull(reader, response); err != nil || !bytes.Equal(response, payload) {
			t.Fatalf("existing connection changed: %q, %v", response, err)
		}
		assertInboundRuntimeClientHistory(t, db, client)
		var stored model.Inbound
		if err := db.First(&stored, inbound.Id).Error; err != nil || stored.ExcludeFromSub != hidden {
			t.Fatalf("visibility not persisted: %+v, %v", stored, err)
		}
		all, err := svc.GetAll()
		if err != nil || len(*all) != 1 || (*all)[0]["exclude_from_sub"] != hidden {
			t.Fatalf("visibility list value: %+v, %v", all, err)
		}
	}
}

func TestInboundVisibilityOnlyDoesNotNormalizeOrSyncSidecars(t *testing.T) {
	for _, protocol := range []string{"masque", "mieru", "http"} {
		t.Run(protocol, func(t *testing.T) {
			db, svc := setupInboundRuntimeTest(t)
			inbound := model.Inbound{
				Type: protocol, Tag: "visibility-sidecar", Options: json.RawMessage(`{"listen_port":12345}`),
				OutJson: json.RawMessage(`{"stored":"do-not-regenerate"}`),
			}
			if err := db.Create(&inbound).Error; err != nil {
				t.Fatal(err)
			}
			inbound.ExcludeFromSub = true
			if err := db.Transaction(func(tx *gorm.DB) error {
				action, err := svc.Save(tx, "edit", inboundRuntimePayload(t, inbound), "", "localhost")
				if action != nil {
					t.Fatal("visibility-only sidecar edit scheduled runtime work")
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}
			var stored model.Inbound
			if err := db.First(&stored, inbound.Id).Error; err != nil {
				t.Fatal(err)
			}
			if !stored.ExcludeFromSub || !bytes.Equal(stored.Options, inbound.Options) || !bytes.Equal(stored.OutJson, inbound.OutJson) {
				t.Fatalf("visibility-only edit mutated runtime configuration: %+v", stored)
			}
		})
	}
}
