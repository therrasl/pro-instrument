package push

import (
	"context"
	"fmt"
	"strings"
)

type VerificationPushNotifier struct {
	tokenFinder TokenFinder
	sender      Sender
}

type TokenFinder interface {
	GetActiveTokensForClient(ctx context.Context, clientID string) ([]string, error)
}

func NewVerificationPushNotifier(tokenFinder TokenFinder, sender Sender) *VerificationPushNotifier {
	return &VerificationPushNotifier{
		tokenFinder: tokenFinder,
		sender:      sender,
	}
}

func (notifier *VerificationPushNotifier) NotifyVerificationApproved(
	ctx context.Context,
	clientID string,
) error {
	if notifier == nil || notifier.tokenFinder == nil || notifier.sender == nil {
		return nil
	}
	tokens, err := notifier.tokenFinder.GetActiveTokensForClient(ctx, clientID)
	if err != nil || len(tokens) == 0 {
		return err
	}

	for _, token := range tokens {
		_, _ = notifier.sender.Send(ctx, Message{
			To:        token,
			Sound:     "default",
			Title:     "Документы подтверждены!",
			Body:      "Ваши документы успешно проверены. Теперь вы можете оформить аренду любого инструмента.",
			Data:      map[string]any{"type": "verification_status", "status": "verified"},
			ChannelID: defaultChannelID,
			Priority:  "high",
		})
	}
	return nil
}

func (notifier *VerificationPushNotifier) NotifyVerificationRejected(
	ctx context.Context,
	clientID string,
	reason string,
) error {
	if notifier == nil || notifier.tokenFinder == nil || notifier.sender == nil {
		return nil
	}
	tokens, err := notifier.tokenFinder.GetActiveTokensForClient(ctx, clientID)
	if err != nil || len(tokens) == 0 {
		return err
	}

	trimmedReason := strings.TrimSpace(reason)
	body := "Документы не прошли проверку. Пожалуйста, загрузите новые фотографии в приложении."
	if trimmedReason != "" {
		body = fmt.Sprintf("Документы отклонены: %s. Пожалуйста, загрузите новые фотографии в приложении.", trimmedReason)
	}

	for _, token := range tokens {
		_, _ = notifier.sender.Send(ctx, Message{
			To:        token,
			Sound:     "default",
			Title:     "Документы отклонены",
			Body:      body,
			Data:      map[string]any{"type": "verification_status", "status": "verification_rejected", "reason": trimmedReason},
			ChannelID: defaultChannelID,
			Priority:  "high",
		})
	}
	return nil
}
