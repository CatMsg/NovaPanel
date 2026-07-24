package service

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/CatMsg/NovaPanel/database/model"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type WarpService struct{}

var (
	warpAPIBaseURL = "https://api.cloudflareclient.com/v0a2158"
	warpHTTPClient = &http.Client{Timeout: 15 * time.Second}
)

type warpRegistrationResponse struct {
	ID      string `json:"id"`
	Token   string `json:"token"`
	Account struct {
		License string `json:"license"`
	} `json:"account"`
}

type warpInfoResponse struct {
	Config struct {
		ClientID  string `json:"client_id"`
		Interface struct {
			Addresses struct {
				V4 string `json:"v4"`
				V6 string `json:"v6"`
			} `json:"addresses"`
		} `json:"interface"`
		Peers []struct {
			Endpoint struct {
				Host string `json:"host"`
			} `json:"endpoint"`
			PublicKey string `json:"public_key"`
		} `json:"peers"`
	} `json:"config"`
}

type warpLicenseResponse struct {
	Success *bool `json:"success"`
	Errors  []struct {
		Code    interface{} `json:"code"`
		Message string      `json:"message"`
	} `json:"errors"`
}

func doWarpRequest(req *http.Request) ([]byte, error) {
	resp, err := warpHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		detail := strings.TrimSpace(string(body))
		if len(detail) > 512 {
			detail = detail[:512]
		}
		if detail == "" {
			return nil, fmt.Errorf("WARP API returned HTTP %d", resp.StatusCode)
		}
		return nil, fmt.Errorf("WARP API returned HTTP %d: %s", resp.StatusCode, detail)
	}
	return body, nil
}

func (s *WarpService) getWarpInfo(deviceId string, accessToken string) ([]byte, error) {
	url := fmt.Sprintf("%s/reg/%s", warpAPIBaseURL, deviceId)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	return doWarpRequest(req)
}

func (s *WarpService) RegisterWarp(ep *model.Endpoint) error {
	tos := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	privateKey, err := wgtypes.GenerateKey()
	if err != nil {
		return err
	}
	publicKey := privateKey.PublicKey().String()
	hostName, _ := os.Hostname()

	data, err := json.Marshal(map[string]string{
		"key":   publicKey,
		"tos":   tos,
		"type":  "PC",
		"model": "s-ui",
		"name":  hostName,
	})
	if err != nil {
		return err
	}
	url := warpAPIBaseURL + "/reg"

	req, err := http.NewRequest("POST", url, bytes.NewReader(data))
	if err != nil {
		return err
	}

	req.Header.Add("CF-Client-Version", "a-7.21-0721")
	req.Header.Add("Content-Type", "application/json")

	body, err := doWarpRequest(req)
	if err != nil {
		return err
	}
	var registration warpRegistrationResponse
	if err := json.Unmarshal(body, &registration); err != nil {
		return err
	}
	if registration.ID == "" || registration.Token == "" || registration.Account.License == "" {
		return fmt.Errorf("WARP registration response is missing required fields")
	}

	warpInfo, err := s.getWarpInfo(registration.ID, registration.Token)
	if err != nil {
		return err
	}

	var details warpInfoResponse
	if err := json.Unmarshal(warpInfo, &details); err != nil {
		return err
	}
	if details.Config.ClientID == "" || details.Config.Interface.Addresses.V4 == "" ||
		details.Config.Interface.Addresses.V6 == "" || len(details.Config.Peers) == 0 {
		return fmt.Errorf("WARP device response is missing required configuration")
	}
	peer := details.Config.Peers[0]
	if peer.Endpoint.Host == "" || peer.PublicKey == "" {
		return fmt.Errorf("WARP device response is missing peer configuration")
	}
	reserved := s.getReserved(details.Config.ClientID)
	if reserved == nil {
		return fmt.Errorf("WARP device response contains an invalid client ID")
	}
	peerEndpoint := peer.Endpoint.Host
	peerEpAddress, peerEpPort, err := net.SplitHostPort(peerEndpoint)
	if err != nil {
		return err
	}
	peerPort, _ := strconv.Atoi(peerEpPort)
	if peerPort < 1 || peerPort > 65535 {
		return fmt.Errorf("WARP device response contains an invalid peer port")
	}

	peers := []map[string]interface{}{
		{
			"address":     peerEpAddress,
			"port":        peerPort,
			"public_key":  peer.PublicKey,
			"allowed_ips": []string{"0.0.0.0/0", "::/0"},
			"reserved":    reserved,
		},
	}

	warpData := map[string]interface{}{
		"access_token": registration.Token,
		"device_id":    registration.ID,
		"license_key":  registration.Account.License,
	}

	ep.Ext, err = json.MarshalIndent(warpData, "", "  ")
	if err != nil {
		return err
	}

	var epOptions map[string]interface{}
	err = json.Unmarshal(ep.Options, &epOptions)
	if err != nil {
		return err
	}
	epOptions["private_key"] = privateKey.String()
	epOptions["address"] = []string{
		fmt.Sprintf("%s/32", details.Config.Interface.Addresses.V4),
		fmt.Sprintf("%s/128", details.Config.Interface.Addresses.V6),
	}
	epOptions["listen_port"] = 0
	epOptions["peers"] = peers

	ep.Options, err = json.MarshalIndent(epOptions, "", "  ")
	return err
}

func (s *WarpService) getReserved(clientID string) []int {
	var reserved []int
	decoded, err := base64.StdEncoding.DecodeString(clientID)
	if err != nil {
		return nil
	}

	hexString := ""
	for _, char := range decoded {
		hex := fmt.Sprintf("%02x", char)
		hexString += hex
	}

	for i := 0; i < len(hexString); i += 2 {
		hexByte := hexString[i : i+2]
		decValue, err := strconv.ParseInt(hexByte, 16, 32)
		if err != nil {
			return nil
		}
		reserved = append(reserved, int(decValue))
	}

	return reserved
}

func (s *WarpService) SetWarpLicense(old_license string, ep *model.Endpoint) error {
	var warpData map[string]string
	if err := json.Unmarshal(ep.Ext, &warpData); err != nil {
		return err
	}

	if warpData["license_key"] == old_license {
		return nil
	}
	if warpData["device_id"] == "" || warpData["access_token"] == "" || warpData["license_key"] == "" {
		return fmt.Errorf("WARP license data is incomplete")
	}

	url := fmt.Sprintf("%s/reg/%s/account", warpAPIBaseURL, warpData["device_id"])
	data, err := json.Marshal(map[string]string{"license": warpData["license_key"]})
	if err != nil {
		return err
	}

	req, err := http.NewRequest("PUT", url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+warpData["access_token"])
	req.Header.Set("Content-Type", "application/json")

	body, err := doWarpRequest(req)
	if err != nil {
		return err
	}
	var response warpLicenseResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return err
	}

	if response.Success != nil && !*response.Success {
		if len(response.Errors) > 0 {
			return fmt.Errorf("WARP API error %v: %s", response.Errors[0].Code, response.Errors[0].Message)
		}
		return fmt.Errorf("WARP API rejected the license")
	}

	return nil
}
