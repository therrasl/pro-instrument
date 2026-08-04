.PHONY: run test migrate-up migrate-down expire-holds bitrix-worker reconcile-bitrix yookassa-worker

run:
	go run ./cmd/api

test:
	go test ./...

migrate-up:
	go run ./cmd/migrate up

migrate-down:
	go run ./cmd/migrate down

expire-holds:
	go run ./cmd/expire-holds

bitrix-worker:
	go run ./cmd/bitrix-worker

reconcile-bitrix:
	go run ./cmd/reconcile-bitrix

yookassa-worker:
	go run ./cmd/yookassa-worker
