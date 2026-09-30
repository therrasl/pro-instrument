package bitrix

import (
	"context"
	"fmt"
	"strings"

	"github.com/pro-instrument/pro-instrument/apps/backend/internal/rentals"
)

type DealPhotoNotifier struct {
	client  Client
	baseURL string
}

func NewDealPhotoNotifier(client Client, baseURL string) *DealPhotoNotifier {
	return &DealPhotoNotifier{
		client:  client,
		baseURL: strings.TrimRight(baseURL, "/"),
	}
}

func (notifier *DealPhotoNotifier) NotifyInspectionPhotos(
	ctx context.Context,
	dealID string,
	phase string,
	orderNumber string,
	photos []rentals.InspectionPhoto,
) error {
	if len(photos) == 0 || dealID == "" {
		return nil
	}

	phaseTitle := "Выдача клиенту"
	if phase == rentals.PhotoPhaseReturn {
		phaseTitle = "Возврат на склад"
	}

	photoTypeTitles := map[string]string{
		rentals.PhotoTypeBody:         "Корпус",
		rentals.PhotoTypeEquipment:    "Комплектация",
		rentals.PhotoTypeBattery:      "Аккумулятор / кабель",
		rentals.PhotoTypeSerialNumber: "Серийный номер",
		rentals.PhotoTypeCleanliness:  "Чистота инструмента",
	}

	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("📸 Фотофиксация инструмента (%s)\n", phaseTitle))
	if orderNumber != "" {
		builder.WriteString(fmt.Sprintf("Заказ: %s\n", orderNumber))
	}
	builder.WriteString("Загруженные фотографии:\n")

	count := 0
	for _, p := range photos {
		if p.Phase != phase {
			continue
		}
		count++
		title := photoTypeTitles[p.PhotoType]
		if title == "" {
			title = p.PhotoType
		}
		link := p.URL
		if !strings.HasPrefix(link, "http") && notifier.baseURL != "" {
			link = notifier.baseURL + link
		}
		builder.WriteString(fmt.Sprintf("• %s: %s\n", title, link))
	}

	if count == 0 {
		return nil
	}

	return notifier.client.AddDealComment(ctx, dealID, builder.String())
}
