package orderdocs

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/phpdave11/gofpdf"
	"github.com/skip2/go-qrcode"
	"golang.org/x/image/font/gofont/goregular"
)

type Generator struct{ storagePath string }

func NewGenerator(storagePath string) *Generator { return &Generator{storagePath: storagePath} }

func (generator *Generator) Generate(data RentalData, documentType string) (GeneratedDocument, error) {
	titles := map[string]string{
		RentalContract:  "Договор аренды оборудования",
		Invoice:         "Счёт на оплату",
		PaymentReceipt:  "Подтверждение оплаты",
		TransferAct:     "Акт приёма-передачи оборудования",
		ReturnAct:       "Акт возврата оборудования",
		ClosingDocument: "Универсальный передаточный документ (УПД)",
	}
	title, ok := titles[documentType]
	if !ok {
		return GeneratedDocument{}, fmt.Errorf("unsupported document type %q", documentType)
	}
	key := filepath.Join("order-documents", data.RentalID, documentType+".pdf")
	path := filepath.Join(generator.storagePath, key)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return GeneratedDocument{}, err
	}

	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(18, 16, 18)
	pdf.SetAutoPageBreak(true, 18)
	pdf.AddUTF8FontFromBytes("Go", "", goregular.TTF)
	pdf.AddPage()

	// Header
	pdf.SetTextColor(29, 50, 64)
	pdf.SetFont("Go", "", 11)
	pdf.CellFormat(110, 8, "СЕРВИС ПРОКАТА «ПРО ИНСТРУМЕНТ»", "", 0, "L", false, 0, "")
	pdf.SetTextColor(85, 96, 105)
	pdf.SetFont("Go", "", 9)
	pdf.CellFormat(64, 8, "Документ по заказу", "", 1, "R", false, 0, "")
	pdf.SetDrawColor(221, 226, 230)
	pdf.Line(18, 26, 192, 26)
	pdf.Ln(6)

	// Document Title & Number
	pdf.SetTextColor(20, 28, 35)
	pdf.SetFont("Go", "", 16)
	docNumber := data.OrderNumber
	if docNumber == "" {
		docNumber = data.RentalID[:8]
	}
	pdf.MultiCell(174, 8, title+" № "+docNumber, "", "L", false)
	pdf.SetFont("Go", "", 10)
	pdf.SetTextColor(75, 85, 93)
	pdf.CellFormat(87, 7, "Заказ: № "+docNumber, "", 0, "L", false, 0, "")
	pdf.CellFormat(87, 7, "Дата формирования: "+formatDate(data.CreatedAt), "", 1, "R", false, 0, "")
	pdf.Ln(4)

	// Section 1: Subject / Items Breakdown Table
	section(pdf, "1. Предмет аренды и спецификация")
	row(pdf, "Инструмент", data.ToolName)
	periodText := data.StartDate + " — " + data.EndDate
	if data.RentalDays > 0 {
		periodText += fmt.Sprintf(" (%d дн.)", data.RentalDays)
	}
	row(pdf, "Период аренды", periodText)
	row(pdf, "Способ получения", deliveryLabel(data))
	pdf.Ln(4)

	// Table Breakdown
	section(pdf, "2. Расчёт стоимости и платежей")
	pdf.SetFont("Go", "", 9)
	pdf.SetFillColor(242, 245, 248)
	pdf.SetTextColor(50, 60, 70)
	pdf.CellFormat(12, 7, "№", "1", 0, "C", true, 0, "")
	pdf.CellFormat(82, 7, "Наименование позиции", "1", 0, "L", true, 0, "")
	pdf.CellFormat(26, 7, "Кол-во / срок", "1", 0, "C", true, 0, "")
	pdf.CellFormat(24, 7, "Тариф", "1", 0, "R", true, 0, "")
	pdf.CellFormat(30, 7, "Сумма", "1", 1, "R", true, 0, "")

	// Item 1: Rental
	daysLabel := fmt.Sprintf("%d дн.", data.RentalDays)
	if data.RentalDays <= 0 {
		daysLabel = "1 период"
	}
	dailyRate := ""
	if data.DailyPrice > 0 {
		dailyRate = money(data.DailyPrice) + "/сут."
	}
	tableRow(pdf, "1", "Аренда оборудования: "+data.ToolName, daysLabel, dailyRate, money(data.RentalPrice))

	// Item 2: Security Payment
	tableRow(pdf, "2", "Обеспечительный платеж (возвратный)", "1 оп.", "—", money(data.DepositAmount))

	// Item 3: Delivery
	if data.DeliveryCost > 0 || data.DeliveryMethod == "courier" {
		delMethodName := "Доставка курьером"
		if data.DeliveryMethod != "courier" {
			delMethodName = "Самовывоз из пункта выдачи"
		}
		tableRow(pdf, "3", delMethodName, "1 услуга", "—", money(data.DeliveryCost))
	}

	// Total Row
	pdf.SetFillColor(240, 247, 246)
	pdf.SetTextColor(20, 70, 63)
	pdf.SetFont("Go", "", 10)
	pdf.CellFormat(144, 8, "ИТОГО К ОПЛАТЕ:", "1", 0, "R", true, 0, "")
	pdf.CellFormat(30, 8, money(data.TotalAmount), "1", 1, "R", true, 0, "")
	pdf.Ln(4)

	// Section 3: Parties & Requisites
	section(pdf, "3. Стороны и реквизиты")
	row(pdf, "Арендодатель", "ООО «Про Инструмент» (ИНН 7701234567, КПП 770101001, ОГРН 1237700123456)")
	row(pdf, "Адрес арендодателя", "г. Москва, ул. Складочная, д. 1, стр. 1")
	row(pdf, "Банк арендодателя", "ПАО СБЕРБАНК, БИК 044525225, к/с 30101810400000000225, р/с 40702810938000000001")

	if data.ClientType == "legal_entity" && data.CompanyName != "" {
		row(pdf, "Арендатор (ЮЛ)", data.CompanyName)
		innKpp := data.INN
		if data.KPP != "" {
			innKpp += " / " + data.KPP
		}
		row(pdf, "ИНН / КПП", innKpp)
		if data.OGRN != "" {
			row(pdf, "ОГРН / ОГРНИП", data.OGRN)
		}
		if data.LegalAddress != "" {
			row(pdf, "Юридический адрес", data.LegalAddress)
		}
		if data.ActualAddress != "" {
			row(pdf, "Фактический адрес", data.ActualAddress)
		}
		contactStr := strings.TrimSpace(data.ContactFullName + " " + data.ContactPosition)
		if contactStr != "" {
			row(pdf, "Представитель", contactStr)
		}
		if data.BankName != "" {
			row(pdf, "Банк арендатора", data.BankName)
			if data.SettlementAccount != "" {
				row(pdf, "Расчётный счёт", data.SettlementAccount)
			}
			bikKs := strings.Trim(strings.Join([]string{data.BIK, data.CorrespondentAccount}, " / "), " /")
			if bikKs != "" {
				row(pdf, "БИК / Корр. счёт", bikKs)
			}
		}
	} else {
		clientName := first(data.ClientFullName, "Физическое лицо")
		row(pdf, "Арендатор (ФЛ)", clientName)
		if data.ClientPhone != "" {
			row(pdf, "Телефон", data.ClientPhone)
		}
		if data.Email != "" {
			row(pdf, "Email", data.Email)
		}
		if data.DeliveryAddress != "" {
			row(pdf, "Адрес получения", data.DeliveryAddress)
		}
	}
	pdf.Ln(4)

	// For Invoice: Add Russian Bank QR-Code (ГОСТ Р 56042-2014)
	if documentType == Invoice {
		section(pdf, "4. Оплата по QR-коду через банковское приложение")
		qrPayload := fmt.Sprintf(
			"ST00012|Name=ООО «Про Инструмент»|PersonalAcc=40702810938000000001|BankName=ПАО СБЕРБАНК|BIC=044525225|CorrespAcc=30101810400000000225|PayeeINN=7701234567|KPP=770101001|Sum=%d|Purpose=Оплата по счету № %s за аренду инструмента",
			data.TotalAmount,
			docNumber,
		)
		qrPng, err := qrcode.Encode(qrPayload, qrcode.Medium, 180)
		if err == nil {
			imgName := "qr_" + data.RentalID + "_" + docNumber
			pdf.RegisterImageOptionsReader(imgName, gofpdf.ImageOptions{ImageType: "PNG"}, bytes.NewReader(qrPng))
			curY := pdf.GetY()
			pdf.ImageOptions(imgName, 18, curY, 28, 28, false, gofpdf.ImageOptions{ImageType: "PNG"}, 0, "")
			pdf.SetXY(50, curY+2)
			pdf.SetFont("Go", "", 9)
			pdf.SetTextColor(60, 70, 80)
			pdf.MultiCell(140, 5, "Отсканируйте данный QR-код в мобильном приложении любого банка РФ (Сбер, Т-Банк, ВТБ, Альфа и др.) для моментальной оплаты с автозаполнением реквизитов, назначения платежа и суммы.", "", "L", false)
			pdf.SetY(curY + 32)
		}
	}

	pdf.SetTextColor(90, 100, 108)
	pdf.SetFont("Go", "", 8.5)
	pdf.MultiCell(174, 4.5, documentText(documentType), "", "L", false)
	pdf.Ln(6)

	// Signatures
	pdf.SetDrawColor(170, 178, 184)
	sigY := pdf.GetY() + 4
	if sigY > 260 {
		pdf.AddPage()
		sigY = pdf.GetY() + 4
	}
	pdf.Line(18, sigY, 82, sigY)
	pdf.Line(126, sigY, 192, sigY)
	pdf.SetFont("Go", "", 8)
	pdf.SetXY(18, sigY+1)
	pdf.CellFormat(64, 5, "Арендодатель (подпись / М.П.)", "", 0, "L", false, 0, "")
	pdf.SetXY(126, sigY+1)
	pdf.CellFormat(66, 5, "Арендатор (подпись / М.П.)", "", 1, "R", false, 0, "")

	pdf.SetAutoPageBreak(false, 0)
	pdf.SetY(282)
	pdf.SetTextColor(140, 146, 152)
	pdf.SetFont("Go", "", 7)
	pdf.CellFormat(174, 4, "Документ сформирован сервисом «Про Инструмент». Идентификатор операции: "+data.RentalID, "", 0, "C", false, 0, "")

	if err := pdf.OutputFileAndClose(path); err != nil {
		return GeneratedDocument{}, err
	}
	return GeneratedDocument{Type: documentType, Title: title, StorageKey: filepath.ToSlash(key)}, nil
}

func section(pdf *gofpdf.Fpdf, title string) {
	pdf.SetTextColor(20, 28, 35)
	pdf.SetFont("Go", "", 11)
	pdf.CellFormat(174, 7, title, "B", 1, "L", false, 0, "")
	pdf.Ln(2)
}

func row(pdf *gofpdf.Fpdf, label, value string) {
	pdf.SetFont("Go", "", 8.5)
	pdf.SetTextColor(100, 108, 115)
	pdf.CellFormat(46, 6, label, "", 0, "L", false, 0, "")
	pdf.SetTextColor(35, 43, 49)
	pdf.MultiCell(128, 6, first(value, "—"), "", "L", false)
}

func tableRow(pdf *gofpdf.Fpdf, num, name, qty, rate, sum string) {
	pdf.SetFont("Go", "", 8.5)
	pdf.SetTextColor(45, 55, 65)
	pdf.CellFormat(12, 6.5, num, "1", 0, "C", false, 0, "")
	pdf.CellFormat(82, 6.5, name, "1", 0, "L", false, 0, "")
	pdf.CellFormat(26, 6.5, qty, "1", 0, "C", false, 0, "")
	pdf.CellFormat(24, 6.5, rate, "1", 0, "R", false, 0, "")
	pdf.CellFormat(30, 6.5, sum, "1", 1, "R", false, 0, "")
}

func money(value int64) string { return fmt.Sprintf("%.2f руб.", float64(value)/100) }

func formatDate(value time.Time) string {
	if value.IsZero() {
		value = time.Now()
	}
	return value.Format("02.01.2006")
}

func first(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func deliveryLabel(data RentalData) string {
	if data.DeliveryMethod == "courier" {
		return "Курьерская доставка: " + first(data.DeliveryAddress, "адрес не указан")
	}
	return "Самовывоз из пункта выдачи"
}

func documentText(kind string) string {
	return map[string]string{
		RentalContract:  "Настоящий договор определяет условия передачи и возврата оборудования, порядок внесения арендной платы и обеспечительного платежа, а также взаимные обязательства сторон.",
		Invoice:         "Настоящий счёт является основанием для безналичной оплаты аренды, обеспечительного платежа и сопутствующих услуг по указанному заказу.",
		PaymentReceipt:  "Оплата по заказу успешно подтверждена. Сумма платежа зачислена в счёт исполнения обязательств по договору аренды.",
		TransferAct:     "Оборудование передано арендатору в исправном состоянии, полной комплектности и без видимых дефектов, препятствующих его целевому использованию. Состояние инструмента зафиксировано фотосъемкой (корпус, комплектация, аккумулятор, серийный номер, чистота) и прикреплено к карточке заказа.",
		ReturnAct:       "Оборудование возвращено арендодателю. Комплектность, чистота и техническое состояние проверены уполномоченным сотрудником при приёмке с фиксацией фотоматериалов.",
		ClosingDocument: "Услуги аренды оказаны в полном объёме и в установленные сроки. Стороны взаимных претензий по исполнению обязательств не имеют.",
	}[kind]
}
