package bitrix

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const maximumBitrixResponseSize = 4 << 20

type HTTPClient struct {
	baseURL string
	client  *http.Client
}

func NewHTTPClient(baseURL string, timeout time.Duration) *HTTPClient {
	return &HTTPClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: timeout},
	}
}

func (client *HTTPClient) FindContactByPhone(
	ctx context.Context,
	phone string,
) (string, bool, error) {
	result, err := client.call(ctx, "crm.duplicate.findbycomm", map[string]any{
		"entity_type": "CONTACT",
		"type":        "PHONE",
		"values":      []string{phone},
	})
	if err != nil {
		return "", false, err
	}

	return decodeContactDuplicates(result)
}

func (client *HTTPClient) CreateContact(
	ctx context.Context,
	input ContactInput,
) (string, error) {
	result, err := client.call(ctx, "crm.contact.add", map[string]any{
		"fields": map[string]any{
			"NAME": input.FullName,
			"PHONE": []map[string]string{{
				"VALUE":      input.Phone,
				"VALUE_TYPE": "MOBILE",
			}},
		},
	})
	if err != nil {
		return "", err
	}
	id, err := decodeID(result)
	if err != nil {
		return "", fmt.Errorf("decode created Bitrix contact id: %w", err)
	}
	return id, nil
}

func (client *HTTPClient) FindDealByRentalID(
	ctx context.Context,
	rentalID string,
	categoryID int,
) (string, bool, error) {
	result, err := client.call(ctx, "crm.deal.list", map[string]any{
		"filter": map[string]any{
			"=TITLE":       dealTitle(rentalID),
			"=CATEGORY_ID": categoryID,
		},
		"select": []string{"ID"},
	})
	if err != nil {
		return "", false, err
	}
	var deals []struct {
		ID json.RawMessage `json:"ID"`
	}
	if err := json.Unmarshal(result, &deals); err != nil {
		return "", false, fmt.Errorf("decode Bitrix deal search: %w", err)
	}
	if len(deals) == 0 {
		return "", false, nil
	}
	id, err := decodeID(deals[0].ID)
	if err != nil {
		return "", false, fmt.Errorf("decode found Bitrix deal id: %w", err)
	}
	return id, true, nil
}

func (client *HTTPClient) CreateDeal(
	ctx context.Context,
	fields map[string]any,
) (string, error) {
	result, err := client.call(ctx, "crm.deal.add", map[string]any{"fields": fields})
	if err != nil {
		return "", err
	}
	id, err := decodeID(result)
	if err != nil {
		return "", fmt.Errorf("decode created Bitrix deal id: %w", err)
	}
	return id, nil
}

func (client *HTTPClient) UpdateDeal(
	ctx context.Context,
	dealID string,
	fields map[string]any,
) error {
	_, err := client.call(ctx, "crm.deal.update", map[string]any{
		"id":     dealID,
		"fields": fields,
	})
	return err
}

func (client *HTTPClient) GetDeal(
	ctx context.Context,
	dealID string,
) (DealState, error) {
	result, err := client.call(ctx, "crm.deal.get", map[string]any{"id": dealID})
	if err != nil {
		return DealState{}, err
	}
	var raw struct {
		ID         json.RawMessage `json:"ID"`
		CategoryID json.RawMessage `json:"CATEGORY_ID"`
		StageID    string          `json:"STAGE_ID"`
	}
	if err := json.Unmarshal(result, &raw); err != nil {
		return DealState{}, fmt.Errorf("decode Bitrix deal: %w", err)
	}
	id, err := decodeID(raw.ID)
	if err != nil {
		return DealState{}, fmt.Errorf("decode Bitrix deal id: %w", err)
	}
	categoryID, err := decodeID(raw.CategoryID)
	if err != nil {
		return DealState{}, fmt.Errorf("decode Bitrix deal category: %w", err)
	}
	return DealState{ID: id, CategoryID: categoryID, StageID: raw.StageID}, nil
}

func (client *HTTPClient) GetDealFull(
	ctx context.Context,
	dealID string,
) (DealFull, error) {
	result, err := client.call(ctx, "crm.deal.get", map[string]any{"id": dealID})
	if err != nil {
		return DealFull{}, err
	}
	var raw struct {
		ID          json.RawMessage `json:"ID"`
		Title       string          `json:"TITLE"`
		CategoryID  json.RawMessage `json:"CATEGORY_ID"`
		StageID     string          `json:"STAGE_ID"`
		ContactID   json.RawMessage `json:"CONTACT_ID"`
		Opportunity string          `json:"OPPORTUNITY"`
		BeginDate   string          `json:"BEGINDATE"`
		CloseDate   string          `json:"CLOSEDATE"`
		ToolName    string          `json:"UF_CRM_1755209429146"`
		ClientName  string          `json:"UF_CRM_1755209635979"`
		ClientPhone string          `json:"UF_CRM_1756648546267"`
		Deposit     string          `json:"UF_CRM_1758015711985"`
		Address     string          `json:"UF_CRM_1755209641247"`
	}
	if err := json.Unmarshal(result, &raw); err != nil {
		return DealFull{}, fmt.Errorf("decode full Bitrix deal: %w", err)
	}
	id, err := decodeID(raw.ID)
	if err != nil {
		return DealFull{}, fmt.Errorf("decode Bitrix deal id: %w", err)
	}
	catID, _ := decodeID(raw.CategoryID)
	contactID, _ := decodeID(raw.ContactID)

	return DealFull{
		ID:          id,
		Title:       raw.Title,
		CategoryID:  catID,
		StageID:     raw.StageID,
		ContactID:   contactID,
		Opportunity: raw.Opportunity,
		BeginDate:   raw.BeginDate,
		CloseDate:   raw.CloseDate,
		ToolName:    raw.ToolName,
		ClientName:  raw.ClientName,
		ClientPhone: raw.ClientPhone,
		Deposit:     raw.Deposit,
		Address:     raw.Address,
	}, nil
}

func (client *HTTPClient) GetContact(
	ctx context.Context,
	contactID string,
) (ContactDetails, error) {
	if contactID == "" {
		return ContactDetails{}, errors.New("empty contact id")
	}
	result, err := client.call(ctx, "crm.contact.get", map[string]any{"id": contactID})
	if err != nil {
		return ContactDetails{}, err
	}
	var raw struct {
		ID       json.RawMessage `json:"ID"`
		Name     string          `json:"NAME"`
		LastName string          `json:"LAST_NAME"`
		Phones   []struct {
			Value string `json:"VALUE"`
		} `json:"PHONE"`
		Comments string          `json:"COMMENTS"`
		Verified json.RawMessage `json:"UF_CRM_1786619870585"`
	}
	if err := json.Unmarshal(result, &raw); err != nil {
		return ContactDetails{}, fmt.Errorf("decode Bitrix contact: %w", err)
	}
	id, err := decodeID(raw.ID)
	if err != nil {
		return ContactDetails{}, fmt.Errorf("decode Bitrix contact id: %w", err)
	}
	fullName := strings.TrimSpace(raw.Name + " " + raw.LastName)
	phone := ""
	if len(raw.Phones) > 0 {
		phone = raw.Phones[0].Value
	}

	verifiedStatus := ""
	if len(raw.Verified) > 0 {
		verifiedRaw := strings.Trim(string(raw.Verified), "\" ")
		switch verifiedRaw {
		case "300", "Да", "да", "true", "Y", "1":
			verifiedStatus = "approved"
		case "302", "Нет", "нет", "false", "N", "0":
			verifiedStatus = "rejected"
		}
	}

	return ContactDetails{
		ID:             id,
		FullName:       fullName,
		Phone:          phone,
		Comments:       strings.TrimSpace(raw.Comments),
		VerifiedStatus: verifiedStatus,
	}, nil
}

func (client *HTTPClient) AddDealComment(
	ctx context.Context,
	dealID string,
	comment string,
) error {
	if strings.TrimSpace(dealID) == "" {
		return errors.New("empty deal id")
	}
	if strings.TrimSpace(comment) == "" {
		return nil
	}
	_, err := client.call(ctx, "crm.timeline.comment.add", map[string]any{
		"fields": map[string]any{
			"ENTITY_ID":   dealID,
			"ENTITY_TYPE": "deal",
			"COMMENT":     comment,
		},
	})
	return err
}

func (client *HTTPClient) AddContactComment(
	ctx context.Context,
	contactID string,
	comment string,
) error {
	if strings.TrimSpace(contactID) == "" {
		return errors.New("empty contact id")
	}
	if strings.TrimSpace(comment) == "" {
		return nil
	}
	_, err := client.call(ctx, "crm.timeline.comment.add", map[string]any{
		"fields": map[string]any{
			"ENTITY_ID":   contactID,
			"ENTITY_TYPE": "contact",
			"COMMENT":     comment,
		},
	})
	return err
}

func (client *HTTPClient) AddContactActivity(
	ctx context.Context,
	contactID string,
	title string,
	description string,
	deadline time.Time,
) error {
	if strings.TrimSpace(contactID) == "" {
		return errors.New("empty contact id")
	}
	_, err := client.call(ctx, "crm.activity.todo.add", map[string]any{
		"ownerTypeId": 3,
		"ownerId":     contactID,
		"title":       title,
		"description": description,
		"deadline":    deadline.Format("2006-01-02T15:04:05-07:00"),
	})
	return err
}

func (client *HTTPClient) call(
	ctx context.Context,
	method string,
	requestBody any,
) (json.RawMessage, error) {
	body, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("encode Bitrix %s request: %w", method, err)
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		client.baseURL+"/"+method+".json",
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("create Bitrix %s request: %w", method, err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")

	response, err := client.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("call Bitrix %s: %w", method, err)
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maximumBitrixResponseSize+1))
	if err != nil {
		return nil, fmt.Errorf("read Bitrix %s response: %w", method, err)
	}
	if len(responseBody) > maximumBitrixResponseSize {
		return nil, errors.New("Bitrix response is too large")
	}

	var envelope struct {
		Result           json.RawMessage `json:"result"`
		Error            string          `json:"error"`
		ErrorDescription string          `json:"error_description"`
	}
	if len(responseBody) > 0 {
		if err := json.Unmarshal(responseBody, &envelope); err != nil {
			if response.StatusCode < http.StatusOK ||
				response.StatusCode >= http.StatusMultipleChoices {
				return nil, &APIError{
					StatusCode:  response.StatusCode,
					Description: http.StatusText(response.StatusCode),
					Temporary: response.StatusCode == http.StatusTooManyRequests ||
						response.StatusCode >= 500,
				}
			}
			return nil, fmt.Errorf("decode Bitrix %s response: %w", method, err)
		}
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, &APIError{
			StatusCode:  response.StatusCode,
			Code:        envelope.Error,
			Description: firstNonEmpty(envelope.ErrorDescription, http.StatusText(response.StatusCode)),
			Temporary:   response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500,
		}
	}
	if envelope.Error != "" {
		return nil, &APIError{
			StatusCode:  response.StatusCode,
			Code:        envelope.Error,
			Description: envelope.ErrorDescription,
			Temporary:   temporaryBitrixCode(envelope.Error),
		}
	}
	if envelope.Result == nil && method == "crm.duplicate.findbycomm" {
		return json.RawMessage("null"), nil
	}
	if envelope.Result == nil {
		return nil, fmt.Errorf("Bitrix %s response has no result", method)
	}
	return envelope.Result, nil
}

func decodeContactDuplicates(result json.RawMessage) (string, bool, error) {
	trimmed := bytes.TrimSpace(result)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return "", false, nil
	}

	switch trimmed[0] {
	case '[':
		var values []json.RawMessage
		if err := json.Unmarshal(trimmed, &values); err != nil {
			return "", false, fmt.Errorf("decode Bitrix contact duplicates: %w", err)
		}
		if len(values) == 0 {
			return "", false, nil
		}
		return "", false, errors.New(
			"decode Bitrix contact duplicates: unexpected non-empty array result",
		)
	case '{':
		var duplicates map[string]json.RawMessage
		if err := json.Unmarshal(trimmed, &duplicates); err != nil {
			return "", false, fmt.Errorf("decode Bitrix contact duplicates: %w", err)
		}
		if len(duplicates) == 0 {
			return "", false, nil
		}

		rawContacts, ok := duplicates["CONTACT"]
		if !ok {
			return "", false, errors.New(
				"decode Bitrix contact duplicates: non-empty object has no CONTACT field",
			)
		}
		rawContacts = bytes.TrimSpace(rawContacts)
		if bytes.Equal(rawContacts, []byte("null")) {
			return "", false, nil
		}

		var contacts []json.RawMessage
		if err := json.Unmarshal(rawContacts, &contacts); err != nil {
			return "", false, fmt.Errorf("decode Bitrix contact duplicates CONTACT: %w", err)
		}
		if len(contacts) == 0 {
			return "", false, nil
		}
		for _, contact := range contacts {
			id, err := decodeID(contact)
			numericID, parseErr := strconv.ParseInt(id, 10, 64)
			if err == nil && parseErr == nil && numericID > 0 {
				return id, true, nil
			}
		}
		return "", false, errors.New(
			"decode Bitrix contact duplicates: CONTACT contains no valid contact ID",
		)
	default:
		return "", false, fmt.Errorf(
			"decode Bitrix contact duplicates: unexpected result type %q",
			string(trimmed[0]),
		)
	}
}

func decodeID(raw json.RawMessage) (string, error) {
	var stringID string
	if err := json.Unmarshal(raw, &stringID); err == nil {
		if strings.TrimSpace(stringID) == "" {
			return "", errors.New("empty id")
		}
		return stringID, nil
	}
	var numberID json.Number
	if err := json.Unmarshal(raw, &numberID); err != nil {
		return "", err
	}
	value := numberID.String()
	if _, err := strconv.ParseInt(value, 10, 64); err != nil {
		return "", err
	}
	return value, nil
}

func temporaryBitrixCode(code string) bool {
	switch strings.ToUpper(code) {
	case "QUERY_LIMIT_EXCEEDED", "INTERNAL_ERROR", "TIMEOUT", "OPERATION_TIME_LIMIT":
		return true
	default:
		return false
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func dealTitle(rentalID string) string {
	return "Заявка аренды " + rentalID
}
