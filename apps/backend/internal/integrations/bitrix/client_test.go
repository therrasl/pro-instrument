package bitrix

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestFindContactByPhoneDecodesBitrixResultShapes(t *testing.T) {
	testCases := []struct {
		name       string
		response   string
		expectedID string
		found      bool
		wantError  bool
	}{
		{
			name:     "empty array",
			response: `{"result":[]}`,
		},
		{
			name:     "null",
			response: `{"result":null}`,
		},
		{
			name:     "empty object",
			response: `{"result":{}}`,
		},
		{
			name:     "missing result",
			response: `{"time":{"duration":0.01}}`,
		},
		{
			name:       "numeric contact id",
			response:   `{"result":{"CONTACT":[123]}}`,
			expectedID: "123",
			found:      true,
		},
		{
			name:       "string contact id",
			response:   `{"result":{"CONTACT":["123"]}}`,
			expectedID: "123",
			found:      true,
		},
		{
			name:      "unexpected non-empty array",
			response:  `{"result":[123]}`,
			wantError: true,
		},
		{
			name:      "unexpected scalar",
			response:  `{"result":"unexpected"}`,
			wantError: true,
		},
		{
			name:      "object without contact",
			response:  `{"result":{"LEAD":[123]}}`,
			wantError: true,
		},
		{
			name:       "first valid contact id",
			response:   `{"result":{"CONTACT":[null,"",123]}}`,
			expectedID: "123",
			found:      true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(
				response http.ResponseWriter,
				_ *http.Request,
			) {
				response.Header().Set("Content-Type", "application/json")
				_, _ = response.Write([]byte(testCase.response))
			}))
			defer server.Close()

			id, found, err := NewHTTPClient(server.URL, time.Second).
				FindContactByPhone(context.Background(), "+79990000001")
			if testCase.wantError {
				if err == nil || !strings.Contains(err.Error(), "decode Bitrix contact duplicates") {
					t.Fatalf("expected explicit decoder error, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("find contact: %v", err)
			}
			if found != testCase.found || id != testCase.expectedID {
				t.Fatalf(
					"unexpected lookup: id=%q found=%t, want id=%q found=%t",
					id,
					found,
					testCase.expectedID,
					testCase.found,
				)
			}
		})
	}
}

func TestFindContactByPhoneDoesNotMaskBitrixError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		response http.ResponseWriter,
		_ *http.Request,
	) {
		_ = json.NewEncoder(response).Encode(map[string]any{
			"error":             "INVALID_REQUEST",
			"error_description": "invalid duplicate lookup",
		})
	}))
	defer server.Close()

	_, _, err := NewHTTPClient(server.URL, time.Second).
		FindContactByPhone(context.Background(), "+79990000001")
	var apiError *APIError
	if !errors.As(err, &apiError) ||
		apiError.Code != "INVALID_REQUEST" ||
		apiError.Description != "invalid duplicate lookup" {
		t.Fatalf("expected original Bitrix REST error, got %v", err)
	}
}

func TestContactFoundByPhone(t *testing.T) {
	var createCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/crm.duplicate.findbycomm.json":
			_ = json.NewEncoder(response).Encode(map[string]any{
				"result": map[string]any{"CONTACT": []string{"42"}},
			})
		case "/crm.contact.add.json":
			createCalls.Add(1)
			_ = json.NewEncoder(response).Encode(map[string]any{"result": "99"})
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, time.Second)
	contactID, found, err := client.FindContactByPhone(context.Background(), "+79990000001")
	if err != nil {
		t.Fatalf("find contact: %v", err)
	}
	if !found || contactID != "42" {
		t.Fatalf("unexpected contact lookup: id=%q found=%t", contactID, found)
	}
	if createCalls.Load() != 0 {
		t.Fatalf("existing contact triggered %d create calls", createCalls.Load())
	}
}

func TestContactCreatedWhenNotFound(t *testing.T) {
	var receivedPhone string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/crm.duplicate.findbycomm.json":
			_ = json.NewEncoder(response).Encode(map[string]any{
				"result": map[string]any{"CONTACT": []string{}},
			})
		case "/crm.contact.add.json":
			var body struct {
				Fields struct {
					Phone []struct {
						Value string `json:"VALUE"`
					} `json:"PHONE"`
				} `json:"fields"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatalf("decode contact request: %v", err)
			}
			receivedPhone = body.Fields.Phone[0].Value
			_ = json.NewEncoder(response).Encode(map[string]any{"result": 99})
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, time.Second)
	_, found, err := client.FindContactByPhone(context.Background(), "+79990000001")
	if err != nil {
		t.Fatalf("find contact: %v", err)
	}
	if found {
		t.Fatal("contact unexpectedly found")
	}
	contactID, err := client.CreateContact(context.Background(), ContactInput{
		FullName: "Иван Иванов",
		Phone:    "+79990000001",
	})
	if err != nil {
		t.Fatalf("create contact: %v", err)
	}
	if contactID != "99" || receivedPhone != "+79990000001" {
		t.Fatalf("unexpected created contact: id=%q phone=%q", contactID, receivedPhone)
	}
}

func TestTemporaryBitrixHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(response).Encode(map[string]any{
			"error":             "INTERNAL_ERROR",
			"error_description": "try later",
		})
	}))
	defer server.Close()

	_, _, err := NewHTTPClient(server.URL, time.Second).
		FindContactByPhone(context.Background(), "+79990000001")
	if err == nil || !IsTemporary(err) {
		t.Fatalf("expected temporary Bitrix error, got %v", err)
	}
}
