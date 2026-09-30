package bitrix

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/pro-instrument/pro-instrument/apps/backend/internal/verification"
)

type VerificationNotifier struct {
	client     Client
	repository WorkerRepository
	baseURL    string
}

func NewVerificationNotifier(
	client Client,
	repository WorkerRepository,
	baseURL string,
) *VerificationNotifier {
	return &VerificationNotifier{
		client:     client,
		repository: repository,
		baseURL:    strings.TrimRight(baseURL, "/"),
	}
}

func (notifier *VerificationNotifier) NotifyDocumentsSubmitted(
	ctx context.Context,
	clientID string,
	documents []verification.Document,
) error {
	if len(documents) == 0 || clientID == "" {
		return nil
	}

	data, err := notifier.repository.GetClientSyncData(ctx, clientID)
	if err != nil {
		return fmt.Errorf("get client sync data: %w", err)
	}

	contactID := strings.TrimSpace(data.ContactID)
	if contactID == "" {
		var found bool
		contactID, found, err = notifier.client.FindContactByPhone(ctx, data.Phone)
		if err != nil {
			return fmt.Errorf("find Bitrix contact by phone: %w", err)
		}
		if !found {
			fullName := strings.TrimSpace(data.FullName)
			if fullName == "" {
				fullName = "Клиент " + data.Phone
			}
			contactID, err = notifier.client.CreateContact(ctx, ContactInput{
				FullName: fullName,
				Phone:    data.Phone,
			})
			if err != nil {
				return fmt.Errorf("create Bitrix contact: %w", err)
			}
		}
		if contactID != "" {
			_ = notifier.repository.SaveContactID(ctx, clientID, contactID)
		}
	}

	if contactID == "" {
		return nil
	}

	typeLabels := map[verification.DocumentType]string{
		verification.DocumentPassportMain:         "Главный разворот паспорта",
		verification.DocumentPassportRegistration: "Разворот с регистрацией (пропиской)",
		verification.DocumentSelfieWithPassport:   "Селфи с паспортом",
	}

	clientName := strings.TrimSpace(data.FullName)
	if clientName == "" {
		clientName = "Без имени"
	}

	var builder strings.Builder
	builder.WriteString("📋 Клиент отправил документы на проверку из приложения\n")
	builder.WriteString(fmt.Sprintf("👤 Клиент: %s (%s)\n\n", clientName, data.Phone))
	builder.WriteString("📄 Загруженные фотографии:\n")

	for _, doc := range documents {
		label := typeLabels[doc.DocumentType]
		if label == "" {
			label = string(doc.DocumentType)
		}
		viewURL := fmt.Sprintf("%s/api/v1/verification/documents/%s/view", notifier.baseURL, doc.ID)
		builder.WriteString(fmt.Sprintf("• %s: %s\n", label, viewURL))
	}

	builder.WriteString("\n⚖️ Действия менеджера:\n")
	builder.WriteString("• Чтобы ОДОБРИТЬ: установите в карточке контакта поле \"Проверен\" (UF_CRM_1786619870585) в значение \"Да\".\n")
	builder.WriteString("• Чтобы ОТКЛОНИТЬ: установите значение \"Нет\" и напишите причину в комментарии к контакту.\n")

	_ = notifier.client.AddContactComment(ctx, contactID, builder.String())

	_ = notifier.client.AddContactActivity(
		ctx,
		contactID,
		fmt.Sprintf("Проверить документы: %s", clientName),
		fmt.Sprintf("Клиент %s (%s) загрузил фото паспорта и селфи для верификации.", clientName, data.Phone),
		time.Now().Add(4*time.Hour),
	)

	return nil
}
