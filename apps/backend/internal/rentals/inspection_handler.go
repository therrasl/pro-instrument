package rentals

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pro-instrument/pro-instrument/apps/backend/internal/config"
)

type BitrixInspectionClient interface {
	UpdateDeal(context.Context, string, map[string]any) error
	AddDealComment(context.Context, string, string) error
}

type DepositSettler interface {
	SettleDeposit(ctx context.Context, rentalID string, refundAmount int64, withholdAmount int64, reason string) error
}

type DepositSettlerFunc func(ctx context.Context, rentalID string, refundAmount int64, withholdAmount int64, reason string) error

func (f DepositSettlerFunc) SettleDeposit(ctx context.Context, rentalID string, refundAmount int64, withholdAmount int64, reason string) error {
	return f(ctx, rentalID, refundAmount, withholdAmount, reason)
}

type InspectionRepository interface {
	GetInspectionViewData(ctx context.Context, rentalID string) (InspectionViewData, error)
	UpdateRentalStatus(ctx context.Context, rentalID string, targetStatus string, actorType string, actorID string, now time.Time) error
}

type InspectionHandler struct {
	repository     InspectionRepository
	depositSettler DepositSettler
	bitrixClient   BitrixInspectionClient
	bitrixStages   config.BitrixStages
	bitrixPortal   string
	secret         string
	logger         *log.Logger
	now            func() time.Time
}

func NewInspectionHandler(
	repository InspectionRepository,
	depositSettler DepositSettler,
	bitrixClient BitrixInspectionClient,
	bitrixStages config.BitrixStages,
	bitrixBaseURL string,
	secret string,
	logger *log.Logger,
) *InspectionHandler {
	portal := strings.TrimRight(bitrixBaseURL, "/")
	if idx := strings.Index(portal, "/rest/"); idx != -1 {
		portal = portal[:idx]
	}

	return &InspectionHandler{
		repository:     repository,
		depositSettler: depositSettler,
		bitrixClient:   bitrixClient,
		bitrixStages:   bitrixStages,
		bitrixPortal:   portal,
		secret:         secret,
		logger:         logger,
		now:            time.Now,
	}
}

func (handler *InspectionHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/rentals/{id}/inspection-view", handler.handleInspectionView)
	mux.HandleFunc("POST /api/v1/rentals/{id}/inspection-action", handler.handleInspectionAction)
}

func (handler *InspectionHandler) handleInspectionView(response http.ResponseWriter, request *http.Request) {
	rentalID := request.PathValue("id")
	if !validUUID(rentalID) {
		http.Error(response, "Некорректный ID заказа", http.StatusBadRequest)
		return
	}

	token := strings.TrimSpace(request.URL.Query().Get("token"))
	if !ValidateInspectionToken(handler.secret, rentalID, token) {
		response.Header().Set("Content-Type", "text/html; charset=utf-8")
		response.WriteHeader(http.StatusForbidden)
		_, _ = response.Write([]byte(renderErrorPage("Доступ ограничен", "Ссылка для проверки инструмента устарела или содержит неверный ключ безопасности.")))
		return
	}

	data, err := handler.repository.GetInspectionViewData(request.Context(), rentalID)
	if err != nil {
		handler.logger.Printf("get inspection view data error for %s: %v", rentalID, err)
		response.Header().Set("Content-Type", "text/html; charset=utf-8")
		response.WriteHeader(http.StatusNotFound)
		_, _ = response.Write([]byte(renderErrorPage("Заказ не найден", "Информация по указанному заказу аренды не найдена в базе данных.")))
		return
	}

	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	response.WriteHeader(http.StatusOK)
	_, _ = response.Write([]byte(handler.renderInspectionHTML(data, token)))
}

func (handler *InspectionHandler) handleInspectionAction(response http.ResponseWriter, request *http.Request) {
	rentalID := request.PathValue("id")
	if !validUUID(rentalID) {
		http.Error(response, "Некорректный ID заказа", http.StatusBadRequest)
		return
	}

	_ = request.ParseForm()
	token := strings.TrimSpace(request.URL.Query().Get("token"))
	if token == "" {
		token = strings.TrimSpace(request.PostForm.Get("token"))
	}

	if !ValidateInspectionToken(handler.secret, rentalID, token) {
		http.Error(response, "Недействительный токен", http.StatusForbidden)
		return
	}

	action := strings.TrimSpace(request.PostForm.Get("action"))
	reason := strings.TrimSpace(request.PostForm.Get("reason"))
	deductionStr := strings.TrimSpace(request.PostForm.Get("deduction_amount"))

	data, err := handler.repository.GetInspectionViewData(request.Context(), rentalID)
	if err != nil {
		http.Error(response, "Заказ не найден", http.StatusNotFound)
		return
	}

	now := handler.now().UTC()
	var successTitle string
	var successMessage string

	switch action {
	case "approve_full":
		if data.RefundableAmount > 0 && handler.depositSettler != nil {
			err := handler.depositSettler.SettleDeposit(request.Context(), rentalID, data.RefundableAmount, 0, "Осмотр пройден без замечаний")
			if err != nil {
				handler.logger.Printf("settle deposit 100%% failed for %s: %v", rentalID, err)
				http.Error(response, "Ошибка при оформлении возврата залога: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}

		_ = handler.repository.UpdateRentalStatus(request.Context(), rentalID, StatusCompleted, "integration", "bitrix_manager", now)

		if handler.bitrixClient != nil && data.BitrixDealID != "" {
			_ = handler.bitrixClient.UpdateDeal(request.Context(), data.BitrixDealID, map[string]any{
				"STAGE_ID": handler.bitrixStages.Completed,
			})
			comment := fmt.Sprintf("✅ Осмотр инструмента пройден успешно без замечаний.\n• Произведён полный (100%%) возврат залога: %s ₽.\n• Заказ успешно завершён.", formatRubles(data.RefundableAmount))
			_ = handler.bitrixClient.AddDealComment(request.Context(), data.BitrixDealID, comment)
		}

		successTitle = "Возврат 100% залога успешно одобрен"
		successMessage = fmt.Sprintf("Осмотр завершён без претензий. Сумма %s ₽ отправлена на карту клиента. Заказ переведён в статус «Завершён».", formatRubles(data.RefundableAmount))

	case "partial_refund":
		if deductionStr == "" {
			http.Error(response, "Не указана сумма удержания", http.StatusBadRequest)
			return
		}
		deductionRubles, parseErr := strconv.ParseInt(deductionStr, 10, 64)
		if parseErr != nil || deductionRubles <= 0 {
			http.Error(response, "Некорректная сумма удержания", http.StatusBadRequest)
			return
		}
		deductionKopecks := deductionRubles * 100
		if deductionKopecks > data.RefundableAmount {
			http.Error(response, fmt.Sprintf("Сумма удержания (%s ₽) превышает доступный залог (%s ₽)", formatRubles(deductionKopecks), formatRubles(data.RefundableAmount)), http.StatusBadRequest)
			return
		}
		if reason == "" {
			http.Error(response, "Необходимо указать причину удержания", http.StatusBadRequest)
			return
		}

		refundKopecks := data.RefundableAmount - deductionKopecks
		if handler.depositSettler != nil {
			err := handler.depositSettler.SettleDeposit(request.Context(), rentalID, refundKopecks, deductionKopecks, reason)
			if err != nil {
				handler.logger.Printf("settle deposit partial failed for %s: %v", rentalID, err)
				http.Error(response, "Ошибка при оформлении удержания: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}

		_ = handler.repository.UpdateRentalStatus(request.Context(), rentalID, StatusCompleted, "integration", "bitrix_manager", now)

		if handler.bitrixClient != nil && data.BitrixDealID != "" {
			_ = handler.bitrixClient.UpdateDeal(request.Context(), data.BitrixDealID, map[string]any{
				"STAGE_ID": handler.bitrixStages.Completed,
			})
			comment := fmt.Sprintf("⚠️ Осмотр инструмента: частичный возврат залога с удержанием.\n• Удержано: %s ₽ (Причина: %s)\n• Возвращено клиенту на карту: %s ₽\n• Заказ завершён.", formatRubles(deductionKopecks), reason, formatRubles(refundKopecks))
			_ = handler.bitrixClient.AddDealComment(request.Context(), data.BitrixDealID, comment)
		}

		successTitle = "Частичный возврат залога оформлен"
		successMessage = fmt.Sprintf("Удержано: %s ₽ (Причина: %s). Возвращено клиенту: %s ₽. Заказ успешно завершён.", formatRubles(deductionKopecks), reason, formatRubles(refundKopecks))

	case "dispute":
		if reason == "" {
			http.Error(response, "Необходимо указать описание дефекта/претензии", http.StatusBadRequest)
			return
		}

		_ = handler.repository.UpdateRentalStatus(request.Context(), rentalID, StatusInspection, "integration", "bitrix_manager", now)

		if handler.bitrixClient != nil && data.BitrixDealID != "" {
			_ = handler.bitrixClient.UpdateDeal(request.Context(), data.BitrixDealID, map[string]any{
				"STAGE_ID": handler.bitrixStages.Inspection,
			})
			comment := fmt.Sprintf("🚨 Претензия по возврату инструмента!\n• Причина / повреждения: %s\n• Залог заморожен до окончания выяснения обстоятельств.", reason)
			_ = handler.bitrixClient.AddDealComment(request.Context(), data.BitrixDealID, comment)
		}

		successTitle = "Претензия зафиксирована"
		successMessage = fmt.Sprintf("Залог заморожен. Заказ переведён на этап «Осмотр инструмента». Зафиксированная причина: %s.", reason)

	default:
		http.Error(response, "Неизвестное действие", http.StatusBadRequest)
		return
	}

	if strings.Contains(request.Header.Get("Accept"), "application/json") {
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(map[string]any{
			"ok":      true,
			"action":  action,
			"title":   successTitle,
			"message": successMessage,
		})
		return
	}

	bitrixDealLink := ""
	if handler.bitrixPortal != "" && data.BitrixDealID != "" {
		bitrixDealLink = fmt.Sprintf("%s/crm/deal/details/%s/", handler.bitrixPortal, data.BitrixDealID)
	}
	inspectionViewLink := fmt.Sprintf("/api/v1/rentals/%s/inspection-view?token=%s", rentalID, token)

	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	response.WriteHeader(http.StatusOK)
	_, _ = response.Write([]byte(renderSuccessPage(successTitle, successMessage, bitrixDealLink, inspectionViewLink)))
}

func formatRubles(kopecks int64) string {
	rub := kopecks / 100
	kop := kopecks % 100
	if kop == 0 {
		return fmt.Sprintf("%d", rub)
	}
	return fmt.Sprintf("%d.%02d", rub, kop)
}

func statusBadge(status string) (string, string) {
	switch status {
	case StatusCompleted:
		return "Завершён", "#10b981"
	case StatusInspection:
		return "Осмотр инструмента", "#f59e0b"
	case StatusAwaitingReturn:
		return "Ожидает возврата", "#3b82f6"
	case StatusRented:
		return "В аренде", "#6366f1"
	default:
		return status, "#64748b"
	}
}

func depositBadge(status string) (string, string) {
	switch status {
	case "refunded":
		return "Возвращён (100%)", "#10b981"
	case "partially_refunded":
		return "Частично удержан", "#f59e0b"
	case "withheld":
		return "Удержан (100%)", "#ef4444"
	case "paid":
		return "Оплачен (на удержании)", "#3b82f6"
	default:
		return status, "#64748b"
	}
}

func (handler *InspectionHandler) renderInspectionHTML(data InspectionViewData, token string) string {
	statusLabel, statusColor := statusBadge(data.Status)
	depLabel, depColor := depositBadge(data.DepositStatus)

	bitrixLinkHTML := ""
	if handler.bitrixPortal != "" && data.BitrixDealID != "" {
		bitrixLinkHTML = fmt.Sprintf(`<a href="%s/crm/deal/details/%s/" target="_blank" class="bitrix-link">📋 Сделка в Bitrix24 #%s ↗</a>`,
			handler.bitrixPortal, data.BitrixDealID, data.BitrixDealID)
	}

	slotDefs := []struct {
		Slot  string
		Title string
	}{
		{PhotoTypeBody, "1. Корпус инструмента"},
		{PhotoTypeEquipment, "2. Комплектация и оснастка"},
		{PhotoTypeBattery, "3. Аккумулятор / кабель питания"},
		{PhotoTypeSerialNumber, "4. Серийный номер / шильдик"},
		{PhotoTypeCleanliness, "5. Чистота инструмента"},
	}

	var rowsHTML strings.Builder
	for _, s := range slotDefs {
		handoverPhoto, hasHandover := data.HandoverPhotos[s.Slot]
		returnPhoto, hasReturn := data.ReturnPhotos[s.Slot]

		handoverCard := `<div class="photo-cell empty"><div class="empty-icon">📷</div><span>Фото при выдаче отсутствует</span></div>`
		if hasHandover {
			commentHTML := ""
			if handoverPhoto.Comment != "" {
				commentHTML = fmt.Sprintf(`<div class="photo-comment">💬 %s</div>`, html.EscapeString(handoverPhoto.Comment))
			}
			handoverCard = fmt.Sprintf(`
				<div class="photo-cell filled">
					<div class="img-wrapper" onclick="openLightbox('%s', 'Выдача: %s')">
						<img src="%s" alt="Выдача" loading="lazy">
						<span class="zoom-badge">🔍 Увеличить</span>
					</div>
					<div class="photo-meta">
						<span class="photo-time">🕒 %s</span>
						%s
					</div>
				</div>`,
				handoverPhoto.URL, html.EscapeString(s.Title),
				handoverPhoto.URL,
				handoverPhoto.CreatedAt.Format("02.01.2006 15:04"),
				commentHTML,
			)
		}

		returnCard := `<div class="photo-cell empty warning"><div class="empty-icon">⏳</div><span>Ожидает фото при возврате</span></div>`
		if hasReturn {
			commentHTML := ""
			if returnPhoto.Comment != "" {
				commentHTML = fmt.Sprintf(`<div class="photo-comment">💬 %s</div>`, html.EscapeString(returnPhoto.Comment))
			}
			returnCard = fmt.Sprintf(`
				<div class="photo-cell filled">
					<div class="img-wrapper" onclick="openLightbox('%s', 'Возврат: %s')">
						<img src="%s" alt="Возврат" loading="lazy">
						<span class="zoom-badge">🔍 Увеличить</span>
					</div>
					<div class="photo-meta">
						<span class="photo-time">🕒 %s</span>
						%s
					</div>
				</div>`,
				returnPhoto.URL, html.EscapeString(s.Title),
				returnPhoto.URL,
				returnPhoto.CreatedAt.Format("02.01.2006 15:04"),
				commentHTML,
			)
		}

		rowsHTML.WriteString(fmt.Sprintf(`
			<div class="matrix-row">
				<div class="matrix-title">%s</div>
				<div class="matrix-columns">
					<div class="col-half">
						<div class="col-header handover">📤 Выдача клиенту</div>
						%s
					</div>
					<div class="col-half">
						<div class="col-header return">📥 Возврат на склад</div>
						%s
					</div>
				</div>
			</div>`,
			s.Title,
			handoverCard,
			returnCard,
		))
	}

	refundableRubles := data.RefundableAmount / 100
	actionSectionHTML := ""

	if data.RefundableAmount > 0 && data.Status != StatusCompleted {
		actionSectionHTML = fmt.Sprintf(`
			<section class="actions-card">
				<h2>⚖️ Решение менеджера по возврату залога</h2>
				<p class="section-desc">Сравните фотографии инструмента при выдаче и возврате, затем выберите соответствующее действие:</p>

				<div class="action-grid">
					<!-- Option 1: Full Refund -->
					<div class="action-box approve">
						<div class="box-icon">✅</div>
						<h3>Возврат 100%% залога</h3>
						<p>Инструмент сдан чистым, исправным и в полной комплектации. Претензий нет.</p>
						<form method="POST" action="/api/v1/rentals/%s/inspection-action?token=%s" onsubmit="return confirm('Подтверждаете 100%%%% возврат залога (%s ₽) клиенту?');">
							<input type="hidden" name="action" value="approve_full">
							<input type="hidden" name="token" value="%s">
							<button type="submit" class="btn btn-approve">
								Вернуть %s ₽ (100%%)
							</button>
						</form>
					</div>

					<!-- Option 2: Partial Refund / Deduction -->
					<div class="action-box partial">
						<div class="box-icon">⚠️</div>
						<h3>Частичный возврат с удержанием</h3>
						<p>Удержите фиксированную сумму за мойку или мелкий дефект, остаток залога вернётся клиенту.</p>
						
						<div class="presets-row">
							<button type="button" class="preset-btn" onclick="setDeduction(500, 'Загрязнение инструмента, удержание за мойку')">Мойка 500 ₽</button>
							<button type="button" class="preset-btn" onclick="setDeduction(1000, 'Сильное загрязнение, проф. чистка')">Мойка 1 000 ₽</button>
							<button type="button" class="preset-btn" onclick="setDeduction(1500, 'Незначительное повреждение')">Дефект 1 500 ₽</button>
							<button type="button" class="preset-btn" onclick="setDeduction(%d, 'Полное удержание залога за ущерб')">100%%%% (%s ₽)</button>
						</div>

						<form method="POST" action="/api/v1/rentals/%s/inspection-action?token=%s" onsubmit="return validateDeduction();">
							<input type="hidden" name="action" value="partial_refund">
							<input type="hidden" name="token" value="%s">

							<div class="form-group">
								<label for="deduction_amount">Сумма удержания (₽):</label>
								<input type="number" id="deduction_amount" name="deduction_amount" min="1" max="%d" required oninput="calcRefund()" placeholder="Например: 1000">
							</div>

							<div id="calc-preview" class="calc-preview">
								К возврату клиенту: <strong id="preview-refund">%s ₽</strong> | Будет удержано: <strong id="preview-withheld">0 ₽</strong>
							</div>

							<div class="form-group">
								<label for="reason">Причина удержания (для клиента и CRM):</label>
								<textarea id="reason" name="reason" rows="2" required placeholder="Например: Инструмент возвращён в строительном растворе, требуется химчистка"></textarea>
							</div>

							<button type="submit" class="btn btn-partial">
								Оформить удержание и вернуть остаток
							</button>
						</form>
					</div>

					<!-- Option 3: Dispute / Damage -->
					<div class="action-box dispute">
						<div class="box-icon">🚨</div>
						<h3>Зафиксировать ущерб / Спор</h3>
						<p>Инструмент сломан, не работает или подменён. Залог блокируется, сделка переводится на экспертизу.</p>
						<form method="POST" action="/api/v1/rentals/%s/inspection-action?token=%s" onsubmit="return confirm('Заблокировать возврат залога и отправить инструмент на экспертизу?');">
							<input type="hidden" name="action" value="dispute">
							<input type="hidden" name="token" value="%s">

							<div class="form-group">
								<label for="dispute_reason">Описание дефекта / проблемы:</label>
								<textarea id="dispute_reason" name="reason" rows="3" required placeholder="Опишите обнаруженные дефекты или повреждения"></textarea>
							</div>

							<button type="submit" class="btn btn-dispute">
								Зафиксировать ущерб (Залог заморожен)
							</button>
						</form>
					</div>
				</div>
			</section>`,
			data.RentalID, token, formatRubles(data.RefundableAmount), token, formatRubles(data.RefundableAmount),
			refundableRubles, formatRubles(data.RefundableAmount),
			data.RentalID, token, token,
			refundableRubles,
			formatRubles(data.RefundableAmount),
			data.RentalID, token, token,
		)
	} else {
		actionSectionHTML = fmt.Sprintf(`
			<section class="actions-card settled">
				<div class="settled-content">
					<div class="settled-icon">ℹ️</div>
					<div>
						<h3>Залог по заказу урегулирован</h3>
						<p>Возвращено на карту клиента: <strong>%s ₽</strong> | Удержано сервисом: <strong>%s ₽</strong></p>
						<p>Текущий статус заказа: <strong>%s</strong>.</p>
					</div>
				</div>
			</section>`,
			formatRubles(data.RefundedAmount),
			formatRubles(data.WithheldAmount),
			statusLabel,
		)
	}

	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="ru">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Осмотр инструмента — Заказ %s</title>
<style>
:root {
	--bg: #f8fafc;
	--card-bg: #ffffff;
	--text: #0f172a;
	--text-muted: #64748b;
	--border: #e2e8f0;
	--primary: #2563eb;
	--success: #10b981;
	--warning: #f59e0b;
	--danger: #ef4444;
}
* { box-sizing: border-box; }
body {
	font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
	background: var(--bg);
	color: var(--text);
	margin: 0;
	padding: 1.5rem;
	line-height: 1.5;
}
.container {
	max-width: 1100px;
	margin: 0 auto;
}
header {
	display: flex;
	justify-content: space-between;
	align-items: center;
	flex-wrap: wrap;
	gap: 1rem;
	margin-bottom: 1.5rem;
}
.brand {
	font-size: 1.25rem;
	font-weight: 700;
	color: #1e293b;
	display: flex;
	align-items: center;
	gap: 0.5rem;
}
.brand span {
	color: var(--primary);
}
.bitrix-link {
	display: inline-block;
	background: #f1f5f9;
	color: #334155;
	padding: 0.5rem 1rem;
	border-radius: 0.5rem;
	text-decoration: none;
	font-size: 0.9rem;
	font-weight: 600;
	border: 1px solid #cbd5e1;
	transition: background 0.15s;
}
.bitrix-link:hover {
	background: #e2e8f0;
}
.card {
	background: var(--card-bg);
	border-radius: 0.75rem;
	border: 1px solid var(--border);
	box-shadow: 0 4px 12px rgba(0,0,0,0.03);
	padding: 1.5rem;
	margin-bottom: 1.5rem;
}
.info-grid {
	display: grid;
	grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
	gap: 1rem;
}
.info-item {
	display: flex;
	flex-direction: column;
}
.info-label {
	font-size: 0.8rem;
	color: var(--text-muted);
	text-transform: uppercase;
	letter-spacing: 0.03em;
	font-weight: 600;
	margin-bottom: 0.25rem;
}
.info-val {
	font-size: 1rem;
	font-weight: 600;
	color: var(--text);
}
.badge {
	display: inline-block;
	padding: 0.2rem 0.6rem;
	border-radius: 9999px;
	color: #fff;
	font-size: 0.8rem;
	font-weight: 600;
	width: fit-content;
}
.deposit-bar {
	display: flex;
	flex-wrap: wrap;
	gap: 1.5rem;
	background: #f8fafc;
	padding: 1rem 1.25rem;
	border-radius: 0.5rem;
	border: 1px solid #e2e8f0;
	margin-top: 1rem;
	align-items: center;
}
.matrix-row {
	background: var(--card-bg);
	border-radius: 0.75rem;
	border: 1px solid var(--border);
	box-shadow: 0 2px 8px rgba(0,0,0,0.02);
	margin-bottom: 1.5rem;
	overflow: hidden;
}
.matrix-title {
	background: #f1f5f9;
	padding: 0.75rem 1.25rem;
	font-size: 1.05rem;
	font-weight: 700;
	color: #1e293b;
	border-bottom: 1px solid var(--border);
}
.matrix-columns {
	display: flex;
	flex-wrap: wrap;
}
.col-half {
	flex: 1;
	min-width: 280px;
	padding: 1rem 1.25rem;
	border-right: 1px solid var(--border);
}
.col-half:last-child {
	border-right: none;
}
.col-header {
	font-size: 0.85rem;
	font-weight: 700;
	text-transform: uppercase;
	margin-bottom: 0.75rem;
	display: flex;
	align-items: center;
	gap: 0.35rem;
}
.col-header.handover { color: #0284c7; }
.col-header.return { color: #ea580c; }
.photo-cell.empty {
	background: #f8fafc;
	border: 2px dashed #cbd5e1;
	border-radius: 0.5rem;
	padding: 2.5rem 1rem;
	text-align: center;
	color: var(--text-muted);
	font-size: 0.9rem;
}
.photo-cell.empty.warning {
	background: #fffbeb;
	border-color: #fde68a;
	color: #b45309;
}
.empty-icon {
	font-size: 2rem;
	margin-bottom: 0.5rem;
}
.img-wrapper {
	position: relative;
	border-radius: 0.5rem;
	overflow: hidden;
	cursor: pointer;
	max-height: 280px;
	background: #000;
	display: flex;
	align-items: center;
	justify-content: center;
}
.img-wrapper img {
	width: 100%%;
	height: auto;
	max-height: 280px;
	object-fit: contain;
	display: block;
	transition: transform 0.2s;
}
.img-wrapper:hover img {
	transform: scale(1.02);
}
.zoom-badge {
	position: absolute;
	bottom: 0.5rem;
	right: 0.5rem;
	background: rgba(0,0,0,0.7);
	color: #fff;
	padding: 0.25rem 0.5rem;
	border-radius: 0.25rem;
	font-size: 0.75rem;
	font-weight: 600;
	pointer-events: none;
}
.photo-meta {
	margin-top: 0.5rem;
	font-size: 0.85rem;
}
.photo-time {
	color: var(--text-muted);
}
.photo-comment {
	background: #f8fafc;
	border-left: 3px solid var(--primary);
	padding: 0.4rem 0.6rem;
	margin-top: 0.35rem;
	border-radius: 0 0.25rem 0.25rem 0;
	font-size: 0.85rem;
	color: #334155;
}
.actions-card {
	background: var(--card-bg);
	border-radius: 0.75rem;
	border: 1px solid var(--border);
	box-shadow: 0 10px 25px rgba(0,0,0,0.05);
	padding: 1.5rem;
	margin-top: 2rem;
}
.actions-card h2 {
	margin-top: 0;
	margin-bottom: 0.25rem;
	font-size: 1.35rem;
}
.section-desc {
	color: var(--text-muted);
	font-size: 0.95rem;
	margin-bottom: 1.5rem;
}
.action-grid {
	display: grid;
	grid-template-columns: repeat(auto-fit, minmax(300px, 1fr));
	gap: 1.5rem;
}
.action-box {
	border-radius: 0.75rem;
	padding: 1.25rem;
	display: flex;
	flex-direction: column;
	background: #f8fafc;
	border: 1px solid var(--border);
}
.action-box.approve {
	background: #f0fdf4;
	border-color: #bbf7d0;
}
.action-box.partial {
	background: #fffbeb;
	border-color: #fef08a;
}
.action-box.dispute {
	background: #fef2f2;
	border-color: #fecaca;
}
.box-icon {
	font-size: 1.75rem;
	margin-bottom: 0.5rem;
}
.action-box h3 {
	margin: 0 0 0.5rem 0;
	font-size: 1.15rem;
}
.action-box p {
	font-size: 0.9rem;
	color: #475569;
	margin-bottom: 1rem;
	flex-grow: 1;
}
.btn {
	display: block;
	width: 100%%;
	padding: 0.75rem 1rem;
	border-radius: 0.5rem;
	font-size: 0.95rem;
	font-weight: 700;
	border: none;
	cursor: pointer;
	text-align: center;
	transition: filter 0.15s;
}
.btn:hover { filter: brightness(0.95); }
.btn-approve { background: var(--success); color: #fff; }
.btn-partial { background: #d97706; color: #fff; }
.btn-dispute { background: var(--danger); color: #fff; }
.presets-row {
	display: flex;
	flex-wrap: wrap;
	gap: 0.35rem;
	margin-bottom: 0.75rem;
}
.preset-btn {
	background: #fff;
	border: 1px solid #cbd5e1;
	border-radius: 0.35rem;
	padding: 0.3rem 0.55rem;
	font-size: 0.78rem;
	font-weight: 600;
	cursor: pointer;
	color: #334155;
	transition: all 0.15s;
}
.preset-btn:hover {
	background: #f1f5f9;
	border-color: #94a3b8;
}
.form-group {
	margin-bottom: 0.75rem;
}
.form-group label {
	display: block;
	font-size: 0.82rem;
	font-weight: 600;
	color: #334155;
	margin-bottom: 0.25rem;
}
.form-group input, .form-group textarea {
	width: 100%%;
	padding: 0.55rem;
	border: 1px solid #cbd5e1;
	border-radius: 0.375rem;
	font-size: 0.9rem;
	font-family: inherit;
}
.calc-preview {
	background: #fff;
	border: 1px dashed #d97706;
	padding: 0.5rem;
	border-radius: 0.35rem;
	font-size: 0.82rem;
	margin-bottom: 0.75rem;
	color: #92400e;
}
.actions-card.settled {
	background: #f0fdf4;
	border-color: #bbf7d0;
}
.settled-content {
	display: flex;
	align-items: center;
	gap: 1rem;
}
.settled-icon {
	font-size: 2rem;
}
.settled-content h3 {
	margin: 0 0 0.25rem 0;
	color: #166534;
}
.settled-content p {
	margin: 0;
	color: #15803d;
	font-size: 0.95rem;
}
/* Lightbox modal */
.lightbox {
	display: none;
	position: fixed;
	top: 0; left: 0; width: 100%%; height: 100%%;
	background: rgba(0,0,0,0.85);
	z-index: 9999;
	align-items: center;
	justify-content: center;
	flex-direction: column;
	padding: 1rem;
}
.lightbox.active { display: flex; }
.lightbox img {
	max-width: 90vw;
	max-height: 80vh;
	object-fit: contain;
	border-radius: 0.5rem;
	box-shadow: 0 10px 30px rgba(0,0,0,0.5);
}
.lightbox-caption {
	color: #fff;
	margin-top: 1rem;
	font-size: 1.1rem;
	font-weight: 600;
}
.lightbox-close {
	position: absolute;
	top: 1rem;
	right: 1.5rem;
	color: #fff;
	font-size: 2.5rem;
	cursor: pointer;
	user-select: none;
}
</style>
</head>
<body>
<div class="container">
	<header>
		<div class="brand">
			🛠️ <span>PRO</span> Инструмент &bull; Осмотр инструмента
		</div>
		%s
	</header>

	<section class="card">
		<div class="info-grid">
			<div class="info-item">
				<span class="info-label">Заказ</span>
				<span class="info-val">%s</span>
			</div>
			<div class="info-item">
				<span class="info-label">Статус</span>
				<span class="badge" style="background: %s">%s</span>
			</div>
			<div class="info-item">
				<span class="info-label">Инструмент</span>
				<span class="info-val">%s</span>
			</div>
			<div class="info-item">
				<span class="info-label">Инвентарный №</span>
				<span class="info-val">%s</span>
			</div>
			<div class="info-item">
				<span class="info-label">Клиент</span>
				<span class="info-val">%s &bull; %s</span>
			</div>
			<div class="info-item">
				<span class="info-label">Период аренды</span>
				<span class="info-val">%s — %s (%d дн.)</span>
			</div>
		</div>

		<div class="deposit-bar">
			<div>
				<span class="info-label">Залог по договору:</span>
				<strong>%s ₽</strong>
			</div>
			<div>
				<span class="info-label">К возврату:</span>
				<strong style="color: %s;">%s ₽</strong>
			</div>
			<div>
				<span class="info-label">Уже возвращено:</span>
				<strong>%s ₽</strong>
			</div>
			<div>
				<span class="info-label">Удержано:</span>
				<strong>%s ₽</strong>
			</div>
			<div>
				<span class="info-label">Статус залога:</span>
				<span class="badge" style="background: %s">%s</span>
			</div>
		</div>
	</section>

	<main>
		%s
	</main>

	%s
</div>

<!-- Lightbox Modal -->
<div id="lightbox" class="lightbox" onclick="closeLightbox()">
	<span class="lightbox-close">&times;</span>
	<img id="lightbox-img" src="" alt="Фото">
	<div id="lightbox-caption" class="lightbox-caption"></div>
</div>

<script>
const maxRefundable = %d;

function openLightbox(src, caption) {
	const lb = document.getElementById('lightbox');
	const img = document.getElementById('lightbox-img');
	const cap = document.getElementById('lightbox-caption');
	img.src = src;
	cap.innerText = caption;
	lb.classList.add('active');
}

function closeLightbox() {
	document.getElementById('lightbox').classList.remove('active');
}

document.addEventListener('keydown', function(e) {
	if (e.key === 'Escape') closeLightbox();
});

function setDeduction(amount, reasonText) {
	const input = document.getElementById('deduction_amount');
	const reason = document.getElementById('reason');
	if (input) input.value = amount;
	if (reason) reason.value = reasonText;
	calcRefund();
}

function calcRefund() {
	const input = document.getElementById('deduction_amount');
	const previewRefund = document.getElementById('preview-refund');
	const previewWithheld = document.getElementById('preview-withheld');
	if (!input || !previewRefund || !previewWithheld) return;

	let deduction = parseInt(input.value, 10);
	if (isNaN(deduction) || deduction < 0) deduction = 0;
	if (deduction > maxRefundable) deduction = maxRefundable;

	const remaining = maxRefundable - deduction;
	previewRefund.innerText = remaining.toLocaleString('ru-RU') + ' ₽';
	previewWithheld.innerText = deduction.toLocaleString('ru-RU') + ' ₽';
}

function validateDeduction() {
	const input = document.getElementById('deduction_amount');
	let deduction = parseInt(input.value, 10);
	if (isNaN(deduction) || deduction <= 0) {
		alert('Пожалуйста, укажите корректную сумму удержания больше 0');
		return false;
	}
	if (deduction > maxRefundable) {
		alert('Сумма удержания не может превышать ' + maxRefundable + ' ₽');
		return false;
	}
	const remaining = maxRefundable - deduction;
	return confirm('Подтверждаете удержание ' + deduction + ' ₽ и возврат клиенту остатка ' + remaining + ' ₽?');
}
</script>
</body>
</html>`,
		html.EscapeString(data.OrderNumber),
		bitrixLinkHTML,
		html.EscapeString(data.OrderNumber),
		statusColor, html.EscapeString(statusLabel),
		html.EscapeString(data.ToolName),
		html.EscapeString(data.InventoryNumber),
		html.EscapeString(data.ClientName), html.EscapeString(data.ClientPhone),
		data.StartDate, data.EndDate, data.RentalDays,
		formatRubles(data.DepositAmount),
		statusColor, formatRubles(data.RefundableAmount),
		formatRubles(data.RefundedAmount),
		formatRubles(data.WithheldAmount),
		depColor, html.EscapeString(depLabel),
		rowsHTML.String(),
		actionSectionHTML,
		refundableRubles,
	)
}

func renderErrorPage(title, message string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="ru">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>%s</title>
<style>
body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; background: #f8fafc; display: flex; align-items: center; justify-content: center; min-height: 100vh; margin: 0; padding: 1rem; }
.card { background: #fff; padding: 2rem; border-radius: 1rem; box-shadow: 0 10px 25px rgba(0,0,0,0.06); max-width: 480px; width: 100%%; text-align: center; }
h2 { margin-top: 0; color: #ef4444; }
p { color: #64748b; font-size: 1rem; line-height: 1.5; }
</style>
</head>
<body>
<div class="card">
  <h2>%s</h2>
  <p>%s</p>
</div>
</body>
</html>`, html.EscapeString(title), html.EscapeString(title), html.EscapeString(message))
}

func renderSuccessPage(title, message, bitrixDealLink, inspectionViewLink string) string {
	bitrixBtnHTML := ""
	if bitrixDealLink != "" {
		bitrixBtnHTML = fmt.Sprintf(`<a href="%s" target="_blank" class="btn btn-bitrix">Открыть сделку в Bitrix24 ↗</a>`, bitrixDealLink)
	}

	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="ru">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>%s</title>
<style>
body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; background: #f0fdf4; display: flex; align-items: center; justify-content: center; min-height: 100vh; margin: 0; padding: 1rem; box-sizing: border-box; }
.card { background: #fff; padding: 2.5rem; border-radius: 1rem; box-shadow: 0 10px 30px rgba(0,0,0,0.06); max-width: 520px; width: 100%%; text-align: center; }
.icon { font-size: 3rem; margin-bottom: 1rem; }
h2 { margin-top: 0; color: #166534; font-size: 1.5rem; }
p { color: #4b5563; font-size: 1rem; line-height: 1.5; margin-bottom: 2rem; }
.btn-group { display: flex; flex-direction: column; gap: 0.75rem; }
.btn { display: block; padding: 0.85rem 1.25rem; border-radius: 0.5rem; font-size: 1rem; font-weight: 600; text-decoration: none; text-align: center; }
.btn-bitrix { background: #2563eb; color: #fff; }
.btn-back { background: #f1f5f9; color: #334155; border: 1px solid #cbd5e1; }
</style>
</head>
<body>
<div class="card">
  <div class="icon">✅</div>
  <h2>%s</h2>
  <p>%s</p>
  <div class="btn-group">
    %s
    <a href="%s" class="btn btn-back">Вернуться к осмотру инструмента</a>
  </div>
</div>
</body>
</html>`,
		html.EscapeString(title),
		html.EscapeString(title),
		html.EscapeString(message),
		bitrixBtnHTML,
		inspectionViewLink,
	)
}
