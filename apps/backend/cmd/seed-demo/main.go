package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/pro-instrument/pro-instrument/apps/backend/internal/config"
	"github.com/pro-instrument/pro-instrument/apps/backend/internal/database"
	"github.com/pro-instrument/pro-instrument/apps/backend/internal/orderdocs"
)

const (
	demoClientID     = "d0000000-0000-4000-8000-000000000077"
	demoRentalID     = "d1000000-0000-4000-8000-000000000077"
	demoPhone        = "+79990000077"
	demoOrganization = "ООО «Демо Инструмент»"
)

func main() {
	logger := log.New(os.Stdout, "", log.LstdFlags)
	if err := run(logger); err != nil {
		logger.Fatal(err)
	}
}

func run(logger *log.Logger) error {
	if strings.ToLower(strings.TrimSpace(os.Getenv("DEMO_MODE_ENABLED"))) != "true" {
		return errors.New("demo seed requires DEMO_MODE_ENABLED=true")
	}
	databaseURL, err := config.LoadDatabaseURL(os.Getenv)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := database.Open(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `INSERT INTO clients (id,phone,phone_verified_at,full_name,email,status,client_type,company_name,inn,kpp,ogrn,legal_address,company_contact)
		VALUES ($1::uuid,$2,NOW(),'Анна Викторовна Соколова','demo-company@example.test','verified','legal_entity',$3,'7705432109','770501001','1157746123456','115054, г. Москва, ул. Дубининская, д. 57, стр. 1','Анна Викторовна Соколова')
		ON CONFLICT (id) DO UPDATE SET client_type='legal_entity',company_name=EXCLUDED.company_name,inn=EXCLUDED.inn,kpp=EXCLUDED.kpp,ogrn=EXCLUDED.ogrn,legal_address=EXCLUDED.legal_address,company_contact=EXCLUDED.company_contact,status='verified',updated_at=NOW()`, demoClientID, demoPhone, demoOrganization)
	if err != nil {
		return fmt.Errorf("seed demo client: %w", err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO client_organizations (client_id,company_name,inn,kpp,ogrn,legal_address,actual_address,email,phone,contact_full_name,contact_position)
		VALUES ($1::uuid,$2,'7705432109','770501001','1157746123456','115054, г. Москва, ул. Дубининская, д. 57, стр. 1','115054, г. Москва, ул. Дубининская, д. 57, офис 14','demo-company@example.test',$3,'Анна Викторовна Соколова','Руководитель проектов')
		ON CONFLICT (client_id) DO UPDATE SET company_name=EXCLUDED.company_name,inn=EXCLUDED.inn,kpp=EXCLUDED.kpp,ogrn=EXCLUDED.ogrn,legal_address=EXCLUDED.legal_address,actual_address=EXCLUDED.actual_address,email=EXCLUDED.email,phone=EXCLUDED.phone,contact_full_name=EXCLUDED.contact_full_name,contact_position=EXCLUDED.contact_position,updated_at=NOW()`, demoClientID, demoOrganization, demoPhone)
	if err != nil {
		return fmt.Errorf("seed demo organization: %w", err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO rental_requests (id,order_number,client_id,tool_id,tool_unit_id,start_date,end_date,rental_days,rental_price,deposit_amount,delivery_cost,total_amount,delivery_method,delivery_address,status,expires_at,created_at,updated_at)
		VALUES ($1::uuid,'PI-DEMO-UL-001',$2::uuid,'20000000-0000-4000-8000-000000000001','30000000-0000-4000-8000-000000000001',CURRENT_DATE-12,CURRENT_DATE-10,3,360000,1000000,100000,1460000,'courier','115054, г. Москва, ул. Дубининская, д. 57, офис 14','completed',NOW()+INTERVAL '1 day',NOW()-INTERVAL '14 days',NOW())
		ON CONFLICT (id) DO UPDATE SET status='completed',updated_at=NOW()`, demoRentalID, demoClientID)
	if err != nil {
		return fmt.Errorf("seed demo rental: %w", err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO rental_customer_snapshots (rental_request_id,order_number,client_type,full_name,email,company_name,inn,kpp,ogrn,legal_address,company_contact,actual_address,organization_phone,contact_position,created_at)
		VALUES ($1::uuid,'PI-DEMO-UL-001','legal_entity','Анна Викторовна Соколова','demo-company@example.test',$2,'7705432109','770501001','1157746123456','115054, г. Москва, ул. Дубининская, д. 57, стр. 1','Анна Викторовна Соколова','115054, г. Москва, ул. Дубининская, д. 57, офис 14',$3,'Руководитель проектов',NOW()-INTERVAL '14 days')
		ON CONFLICT (rental_request_id) DO UPDATE SET company_name=EXCLUDED.company_name,inn=EXCLUDED.inn,kpp=EXCLUDED.kpp,ogrn=EXCLUDED.ogrn,legal_address=EXCLUDED.legal_address,company_contact=EXCLUDED.company_contact,actual_address=EXCLUDED.actual_address,organization_phone=EXCLUDED.organization_phone,contact_position=EXCLUDED.contact_position`, demoRentalID, demoOrganization, demoPhone)
	if err != nil {
		return fmt.Errorf("seed demo snapshot: %w", err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO payments (rental_request_id,provider_payment_id,idempotency_key,rental_amount,deposit_amount,delivery_amount,total_amount,status,provider_payload,succeeded_at)
		VALUES ($1::uuid,'demo-payment-ul-001','demo-payment-ul-001',360000,1000000,100000,1460000,'succeeded','{"demo":true}'::jsonb,NOW()-INTERVAL '10 days')
		ON CONFLICT (idempotency_key) DO UPDATE SET status='succeeded',succeeded_at=EXCLUDED.succeeded_at`, demoRentalID)
	if err != nil {
		return fmt.Errorf("seed demo payment: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	storage := os.Getenv("STORAGE_PATH")
	if storage == "" {
		storage = "./storage"
	}
	service := orderdocs.NewService(orderdocs.NewPostgresRepository(pool), orderdocs.NewGenerator(storage), logger)
	if err := service.EnsureRental(ctx, demoRentalID); err != nil {
		return err
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM order_documents WHERE rental_request_id=$1::uuid`, demoRentalID).Scan(&count); err != nil {
		return err
	}
	logger.Printf("demo legal entity ready: phone=%s organization=%s order=PI-DEMO-UL-001 documents=%d", demoPhone, demoOrganization, count)
	return nil
}
