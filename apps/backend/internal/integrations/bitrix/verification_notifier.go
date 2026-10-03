package bitrix

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/pro-instrument/pro-instrument/apps/backend/internal/verification"
)

type VerificationNotifier struct {
	client     Client
	repository WorkerRepository
	baseURL    string
	secret     string
}

func NewVerificationNotifier(
	client Client,
	repository WorkerRepository,
	baseURL string,
	secret string,
) *VerificationNotifier {
	return &VerificationNotifier{
		client:     client,
		repository: repository,
		baseURL:    strings.TrimRight(baseURL, "/"),
		secret:     strings.TrimSpace(secret),
	}
}

func GenerateQuickReviewToken(secret, clientID, decision string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(clientID + ":" + decision))
	return hex.EncodeToString(mac.Sum(nil))
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

	input := ContactInput{
		FullName:       data.FullName,
		Phone:          data.Phone,
		BirthDate:      data.BirthDate,
		Email:          data.Email,
		ClientType:     data.ClientType,
		CompanyName:    data.CompanyName,
		INN:            data.INN,
		KPP:            data.KPP,
		OGRN:           data.OGRN,
		LegalAddress:   data.LegalAddress,
		CompanyContact: data.CompanyContact,
	}

	contactID := strings.TrimSpace(data.ContactID)
	if contactID == "" {
		var found bool
		contactID, found, err = notifier.client.FindContactByPhone(ctx, data.Phone)
		if err != nil {
			return fmt.Errorf("find Bitrix contact by phone: %w", err)
		}
		if found {
			_ = notifier.client.UpdateContact(ctx, contactID, input)
		} else {
			contactID, err = notifier.client.CreateContact(ctx, input)
			if err != nil {
				return fmt.Errorf("create Bitrix contact: %w", err)
			}
		}
		if contactID != "" {
			_ = notifier.repository.SaveContactID(ctx, clientID, contactID)
		}
	} else {
		_ = notifier.client.UpdateContact(ctx, contactID, input)
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
	builder.WriteString(fmt.Sprintf("👤 Клиент: %s (%s)\n", clientName, data.Phone))
	if data.BirthDate != nil {
		builder.WriteString(fmt.Sprintf("🎂 Дата рождения: %s\n", data.BirthDate.Format("02.01.2006")))
	}
	if data.Email != "" {
		builder.WriteString(fmt.Sprintf("✉️ Email: %s\n", data.Email))
	}
	builder.WriteString("📱 Источник: Мобильное приложение\n")

	if data.ClientType == "legal_entity" || data.CompanyName != "" {
		builder.WriteString("\n🏢 Данные организации:\n")
		if data.CompanyName != "" {
			builder.WriteString(fmt.Sprintf("• Компания: %s\n", data.CompanyName))
		}
		if data.INN != "" {
			builder.WriteString(fmt.Sprintf("• ИНН: %s\n", data.INN))
		}
		if data.KPP != "" {
			builder.WriteString(fmt.Sprintf("• КПП: %s\n", data.KPP))
		}
		if data.OGRN != "" {
			builder.WriteString(fmt.Sprintf("• ОГРН: %s\n", data.OGRN))
		}
		if data.LegalAddress != "" {
			builder.WriteString(fmt.Sprintf("• Юр. адрес: %s\n", data.LegalAddress))
		}
		if data.CompanyContact != "" {
			builder.WriteString(fmt.Sprintf("• Контактное лицо: %s\n", data.CompanyContact))
		}
	}

	builder.WriteString("\n📄 Загруженные фотографии:\n")
	for _, doc := range documents {
		label := typeLabels[doc.DocumentType]
		if label == "" {
			label = string(doc.DocumentType)
		}
		viewURL := fmt.Sprintf("%s/api/v1/verification/documents/%s/view", notifier.baseURL, doc.ID)
		builder.WriteString(fmt.Sprintf("• %s: %s\n", label, viewURL))
	}

	builder.WriteString("\n⚖️ Как подтвердить документы (2 способа):\n")
	if notifier.secret != "" && notifier.baseURL != "" {
		approveURL := fmt.Sprintf("%s/api/v1/integrations/bitrix/quick-review?client_id=%s&decision=approved&token=%s",
			notifier.baseURL, clientID, GenerateQuickReviewToken(notifier.secret, clientID, "approved"))
		rejectURL := fmt.Sprintf("%s/api/v1/integrations/bitrix/quick-review?client_id=%s&decision=rejected&token=%s",
			notifier.baseURL, clientID, GenerateQuickReviewToken(notifier.secret, clientID, "rejected"))
		builder.WriteString("⚡ СПОСОБ 1 (В 1 клик прямо из комментария):\n")
		builder.WriteString(fmt.Sprintf("• ✅ ОДОБРИТЬ: %s\n", approveURL))
		builder.WriteString(fmt.Sprintf("• ❌ ОТКЛОНИТЬ: %s\n\n", rejectURL))
	}
	builder.WriteString("📋 СПОСОБ 2 (В карточке контакта в CRM):\n")
	builder.WriteString("• Найдите поле «Документы проверены» и выберите:\n")
	builder.WriteString("  - «Да» — для одобрения (клиенту откроется оплата)\n")
	builder.WriteString("  - «Нет» — для отклонения (напишите причину в комментарии)\n")
	builder.WriteString("*(Если поле скрыто в карточке, нажмите в левой колонке «Выбрать поле» -> отметьте «Документы проверены»)*\n")

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
