package auth

import (
	"context"
	"log"
)

type SMSSender interface {
	SendCode(context.Context, string, string) error
}

type DevelopmentSMSSender struct {
	logger *log.Logger
}

func NewDevelopmentSMSSender(logger *log.Logger) *DevelopmentSMSSender {
	return &DevelopmentSMSSender{logger: logger}
}

func (sender *DevelopmentSMSSender) SendCode(_ context.Context, phone string, code string) error {
	sender.logger.Printf("development SMS phone=%s code=%s", phone, code)
	return nil
}
