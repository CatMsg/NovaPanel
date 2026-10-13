package service

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/CatMsg/NovaPanel/database/model"
	"gorm.io/gorm"
)

func TestInboundSubscriptionVisibilityEditKeepsActiveConnection(t *testing.T) {
	inbound := model.Inbound{
		Type:    "socks",
		Tag:     "subscription-visibility-live",
		Addrs:   json.RawMessage(`[]`),
		Options: inboundTestOptions(t, "socks"),
	}
	db, service := setupInboundRuntimeTest(t, inboundRuntimeForwardCall(t, "apply", inbound))

	createPayload := inboundRuntimePayload(t, inbound)
	var postCommit func() error
	if err := db.Transaction(func(tx *gorm.DB) error {
		var err error
		postCommit, err = service.Save(tx, "new", createPayload, "", "127.0.0.1")
		return err
	}); err != nil {
		t.Fatalf("create runtime inbound: %v", err)
	}
	if postCommit == nil {
		t.Fatal("creating inbound returned no runtime action")
	}
	if err := postCommit(); err != nil {
		t.Fatalf("start runtime inbound: %v", err)
	}

	var stored model.Inbound
	if err := db.Where("tag = ?", inbound.Tag).First(&stored).Error; err != nil {
		t.Fatalf("load runtime inbound: %v", err)
	}
	var storedOptions map[string]json.RawMessage
	if err := json.Unmarshal(stored.Options, &storedOptions); err != nil {
		t.Fatalf("decode stored inbound options: %v", err)
	}
	storedOptions["exclude_from_sub"] = json.RawMessage("true")
	legacyOptions, err := json.Marshal(storedOptions)
	if err != nil {
		t.Fatalf("encode legacy inbound options: %v", err)
	}
	if err := db.Model(&model.Inbound{}).Where("id = ?", stored.Id).Update("options", legacyOptions).Error; err != nil {
		t.Fatalf("seed stale panel option: %v", err)
	}
	if err := db.First(&stored, stored.Id).Error; err != nil {
		t.Fatalf("reload inbound with stale panel option: %v", err)
	}
	catalog, err := service.GetAll()
	if err != nil {
		t.Fatalf("read inbound catalog: %v", err)
	}
	var catalogFlag bool
	foundCatalogInbound := false
	for _, entry := range *catalog {
		if entry["tag"] == inbound.Tag {
			foundCatalogInbound = true
			catalogFlag, _ = entry["exclude_from_sub"].(bool)
		}
	}
	if !foundCatalogInbound || catalogFlag {
		t.Fatalf("stale options overrode persisted panel metadata in inbound catalog: found=%v exclude_from_sub=%v", foundCatalogInbound, catalogFlag)
	}
	var options struct {
		ListenPort int `json:"listen_port"`
	}
	if err := json.Unmarshal(stored.Options, &options); err != nil {
		t.Fatalf("decode inbound listen port: %v", err)
	}
	connection, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(options.ListenPort)), 3*time.Second)
	if err != nil {
		t.Fatalf("connect to runtime inbound: %v", err)
	}
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("set SOCKS connection deadline: %v", err)
	}
	if _, err := connection.Write([]byte{5, 1, 0}); err != nil {
		t.Fatalf("send SOCKS greeting: %v", err)
	}
	greeting := make([]byte, 2)
	if _, err := io.ReadFull(connection, greeting); err != nil {
		t.Fatalf("read SOCKS greeting response: %v", err)
	}
	if !bytes.Equal(greeting, []byte{5, 0}) {
		t.Fatalf("SOCKS greeting response = %v, want no-auth response", greeting)
	}

	echoListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("start local echo target: %v", err)
	}
	defer echoListener.Close()
	go func() {
		target, err := echoListener.Accept()
		if err == nil {
			defer target.Close()
			_, _ = io.Copy(target, target)
		}
	}()
	echoPort := uint16(echoListener.Addr().(*net.TCPAddr).Port)
	request := []byte{5, 1, 0, 1, 127, 0, 0, 1, 0, 0}
	binary.BigEndian.PutUint16(request[8:10], echoPort)
	if _, err := connection.Write(request); err != nil {
		t.Fatalf("request SOCKS tunnel before metadata update: %v", err)
	}
	connectResponse := make([]byte, 10)
	if _, err := io.ReadFull(connection, connectResponse); err != nil {
		t.Fatalf("read SOCKS connect response before metadata update: %v", err)
	}
	if connectResponse[1] != 0 {
		t.Fatalf("SOCKS connect response before metadata update = %v, want success", connectResponse)
	}

	full, err := stored.MarshalFull()
	if err != nil {
		t.Fatalf("marshal inbound edit: %v", err)
	}
	(*full)["exclude_from_sub"] = true
	editPayload, err := json.Marshal(full)
	if err != nil {
		t.Fatalf("encode inbound edit: %v", err)
	}
	postCommit = nil
	if err := db.Transaction(func(tx *gorm.DB) error {
		var err error
		postCommit, err = service.Save(tx, "edit", editPayload, "", "127.0.0.1")
		return err
	}); err != nil {
		t.Fatalf("edit subscription visibility: %v", err)
	}
	if postCommit != nil {
		t.Fatal("metadata-only visibility edit returned a runtime or sidecar action")
	}
	assertInboundRuntimeTag(t, inbound.Tag, true)
	var updated model.Inbound
	if err := db.First(&updated, stored.Id).Error; err != nil {
		t.Fatalf("reload inbound after visibility edit: %v", err)
	}
	if !updated.ExcludeFromSub {
		t.Fatal("subscription visibility flag was not persisted")
	}

	marker := []byte("active-session-survived")
	if _, err := connection.Write(marker); err != nil {
		t.Fatalf("write through active SOCKS session after metadata update: %v", err)
	}
	echoed := make([]byte, len(marker))
	if _, err := io.ReadFull(connection, echoed); err != nil {
		t.Fatalf("read through active SOCKS session after metadata update: %v", err)
	}
	if !bytes.Equal(echoed, marker) {
		t.Fatalf("active SOCKS session echoed %q, want %q", echoed, marker)
	}
}
