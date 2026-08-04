package push

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const DefaultExpoPushURL = "https://exp.host/--/api/v2/push/send"
const defaultExpoReceiptsURL = "https://exp.host/--/api/v2/push/getReceipts"

var ErrDeviceNotRegistered = errors.New("push device is not registered")

type Sender interface {
	Send(context.Context, Message) (string, error)
	CheckReceipt(context.Context, string) (bool, error)
}

func (client *ExpoClient) CheckReceipt(ctx context.Context, ticketID string) (bool, error) {
	body, err := json.Marshal(map[string][]string{"ids": {ticketID}})
	if err != nil {
		return false, fmt.Errorf("encode Expo push receipt request: %w", err)
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		defaultExpoReceiptsURL,
		bytes.NewReader(body),
	)
	if err != nil {
		return false, fmt.Errorf("create Expo push receipt request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")

	response, err := client.client.Do(request)
	if err != nil {
		return false, fmt.Errorf("send Expo push receipt request: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return false, fmt.Errorf("read Expo push receipt response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return false, fmt.Errorf("Expo push receipts returned HTTP %d", response.StatusCode)
	}

	var result struct {
		Data map[string]struct {
			Status  string `json:"status"`
			Message string `json:"message"`
			Details struct {
				Error string `json:"error"`
			} `json:"details"`
		} `json:"data"`
	}
	if err := json.Unmarshal(responseBody, &result); err != nil {
		return false, fmt.Errorf("decode Expo push receipt response: %w", err)
	}
	receipt, ok := result.Data[ticketID]
	if !ok {
		return false, nil
	}
	if receipt.Status == "ok" {
		return true, nil
	}
	if receipt.Details.Error == "DeviceNotRegistered" {
		return true, ErrDeviceNotRegistered
	}
	messageText := strings.TrimSpace(receipt.Message)
	if messageText == "" {
		messageText = "Expo rejected push receipt"
	}
	return true, errors.New(messageText)
}

type ExpoClient struct {
	url    string
	client *http.Client
}

func NewExpoClient(url string, client *http.Client) *ExpoClient {
	return &ExpoClient{url: url, client: client}
}

func (client *ExpoClient) Send(ctx context.Context, message Message) (string, error) {
	body, err := json.Marshal(message)
	if err != nil {
		return "", fmt.Errorf("encode Expo push message: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create Expo push request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")

	response, err := client.client.Do(request)
	if err != nil {
		return "", fmt.Errorf("send Expo push request: %w", err)
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read Expo push response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("Expo push returned HTTP %d", response.StatusCode)
	}

	var result struct {
		Data struct {
			Status  string `json:"status"`
			ID      string `json:"id"`
			Message string `json:"message"`
			Details struct {
				Error string `json:"error"`
			} `json:"details"`
		} `json:"data"`
	}
	if err := json.Unmarshal(responseBody, &result); err != nil {
		return "", fmt.Errorf("decode Expo push response: %w", err)
	}
	if result.Data.Status == "ok" && result.Data.ID != "" {
		return result.Data.ID, nil
	}
	if result.Data.Details.Error == "DeviceNotRegistered" {
		return "", ErrDeviceNotRegistered
	}
	messageText := strings.TrimSpace(result.Data.Message)
	if messageText == "" {
		messageText = "Expo rejected push message"
	}
	return "", errors.New(messageText)
}
