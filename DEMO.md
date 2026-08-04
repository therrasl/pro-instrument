# Демонстрация аренды, Битрикс24 и ЮKassa

## Что понадобится

- Node.js 22+, pnpm 11, Go и Docker;
- тестовый магазин ЮKassa;
- воронка Битрикс24 со стадиями из `apps/backend/.env.example`;
- публичный HTTPS URL для локального backend (например, ngrok или Cloudflare
  Tunnel);
- Expo development build. `expo prebuild --clean` для демонстрации не нужен.

Секреты хранятся только в окружении backend. В mobile допустима единственная
публичная настройка `EXPO_PUBLIC_API_URL`. Локальные реквизиты тестовой ЮKassa
можно вынести в игнорируемый файл `apps/backend/.env.yookassa`.

## PostgreSQL

Запустите PostgreSQL на порту, указанном в примере backend:

```bash
docker run --name pro-instrument-postgres \
  -e POSTGRES_USER=postgres \
  -e POSTGRES_PASSWORD=postgres \
  -e POSTGRES_DB=pro_instrument \
  -p 5434:5432 \
  -d postgres:16
```

При повторном запуске уже созданного контейнера:

```bash
docker start pro-instrument-postgres
```

## Backend

Создайте локальный файл настроек, не добавляя его в git:

```bash
cd apps/backend
cp .env.example .env
```

Минимальные настройки:

```dotenv
PORT=8080
DATABASE_URL=postgres://postgres:postgres@localhost:5434/pro_instrument?sslmode=disable
APP_ENV=development
OTP_HASH_SECRET=<случайная-строка-не-короче-32-символов>

BITRIX_ENABLED=true
BITRIX_BASE_URL=https://<ваш-портал>.bitrix24.ru/rest/<user>/<webhook-token>
BITRIX_WEBHOOK_SECRET=<отдельная-случайная-строка-не-короче-32-символов>

YOOKASSA_ENABLED=true
YOOKASSA_SHOP_ID=<test-shop-id>
YOOKASSA_SECRET_KEY=<test-secret-key>
YOOKASSA_RETURN_URL=pro-instrument://payment-return
YOOKASSA_RECEIPTS_ENABLED=false
PUBLIC_BASE_URL=https://<публичный-домен-туннеля>
```

Проверьте и при необходимости замените `BITRIX_STAGE_*` на коды стадий
демонстрационной воронки. `COURIER_DELIVERY_FEE_KOPECKS` задаёт фиксированную
стоимость курьера в копейках; интеграция с Яндекс Доставкой не используется.

Загрузите переменные, примените миграции и запустите API:

```bash
set -a
source .env
test ! -f .env.yookassa || source .env.yookassa
set +a
make migrate-up
make run
```

В отдельных терминалах с тем же окружением запустите фоновые процессы:

```bash
cd apps/backend
set -a && source .env && set +a
make bitrix-worker
```

```bash
cd apps/backend
set -a
source .env
test ! -f .env.yookassa || source .env.yookassa
set +a
make yookassa-worker
```

Проверка API:

```bash
curl http://localhost:8080/health
```

## Публичные webhook URL

Поднимите HTTPS-туннель к `localhost:8080`, например:

```bash
ngrok http 8080
```

Значение `PUBLIC_BASE_URL` должно совпадать с выданным HTTPS origin.

В тестовом кабинете ЮKassa укажите URL уведомлений:

```text
https://<публичный-домен-туннеля>/api/v1/integrations/yookassa/webhook
```

Подпишите события `payment.succeeded` и `payment.canceled`. Backend не доверяет
телу webhook безусловно: после уведомления он повторно получает платёж у ЮKassa
и сверяет идентификатор, сумму, валюту и metadata.

Для исходящих событий Битрикс24 настройте обработчик изменения сделки:

```text
POST https://<публичный-домен-туннеля>/api/v1/integrations/bitrix/events
X-Bitrix-Webhook-Secret: <BITRIX_WEBHOOK_SECRET>
```

Если штатный робот портала не умеет добавлять заголовок, используйте
промежуточный webhook/робот, который добавляет этот заголовок. В payload должен
приходить ID сделки (`deal_id` либо стандартный `data[FIELDS][ID]`). После
изменения стадии событие сохраняется в PostgreSQL, а `bitrix-worker` обновляет
статус заявки. Для ручной сверки доступна команда `make reconcile-bitrix`.

## Mobile

В `apps/mobile/.env` хранится только публичный адрес API:

```dotenv
EXPO_PUBLIC_API_URL=http://<LAN-IP-компьютера>:8080
```

Для Android Emulator можно использовать `http://10.0.2.2:8080`. Физическое
устройство и компьютер должны видеть друг друга в локальной сети.

Запуск development server:

```bash
pnpm --filter @pro-instrument/mobile start
```

Откройте установленный development build. Scheme
`pro-instrument://payment-return` уже объявлен в `app.json` и возвращает
пользователя на экран backend-проверки платежа.

## Тестовый пользователь

Используйте номер `+7 999 000-00-09`. В development-режиме одноразовый код
печатается в терминале backend:

```text
development SMS phone=+79990000009 code=123456
```

После первого входа можно пройти существующий onboarding штатно. Для короткой
демонстрации допустимо подготовить этого локального пользователя в тестовой БД:

```sql
UPDATE clients
SET
  full_name = 'Демо Пользователь',
  birth_date = '1990-01-01',
  email = 'demo@example.test',
  status = 'verified',
  updated_at = NOW()
WHERE phone = '+79990000009';

INSERT INTO consent_acceptances (
  client_id,
  offer_version,
  privacy_version,
  offer_accepted,
  privacy_accepted,
  data_accuracy_confirmed,
  rental_rules_accepted,
  accepted_at,
  ip_address,
  user_agent
)
SELECT
  id,
  'demo-v1',
  'demo-v1',
  TRUE,
  TRUE,
  TRUE,
  TRUE,
  NOW(),
  '127.0.0.1',
  'demo-seed'
FROM clients
WHERE phone = '+79990000009'
ON CONFLICT (client_id, offer_version, privacy_version) DO NOTHING;
```

Выполнить SQL можно так:

```bash
docker exec -i pro-instrument-postgres \
  psql -U postgres -d pro_instrument
```

После подготовки перезапустите приложение или войдите заново, чтобы обновить
профиль в сессии. Эта операция предназначена только для локальной demo-БД.

## Сценарий демонстрации

1. Войдите тестовым пользователем и откройте доступный инструмент.
2. Нажмите «Оформить аренду», выберите даты и способ получения.
3. Для курьера укажите адрес; для самовывоза курьерские поля скрыты.
4. Убедитесь, что разбивка аренды, залога, доставки и итога пришла из backend
   quote. Нажмите «Отправить заявку» один раз.
5. Откроется деталь аренды со статусом «Проверяется менеджером». В Битрикс24
   должна появиться сделка.
6. В Битрикс24 переведите сделку в стадию «Ожидает оплаты». Вернитесь в
   приложение или потяните экран вниз: появится статус «Ожидает оплаты» и
   единственная кнопка «Оплатить».
7. Нажмите «Оплатить». Backend создаст идемпотентный платёж, а приложение
   откроет `confirmation_url` в системном браузере.
8. Завершите тестовую оплату ЮKassa и вернитесь по deep link. Экран не показывает
   успех по факту возврата — он опрашивает `GET /rentals/:id`.
9. После проверенного webhook backend меняет заявку на `paid`, ставит событие
   синхронизации, а Битрикс24 получает стадию «Оплачено». Только после ответа
   backend приложение показывает «Оплачено».

Если webhook задержался, оставьте `yookassa-worker` запущенным и обновите экран
жестом pull-to-refresh. Не нажимайте оплату повторно: backend вернёт существующий
платёж для той же заявки.

## Проверки перед демонстрацией

Из корня репозитория:

```bash
pnpm --filter @pro-instrument/mobile typecheck
pnpm --filter @pro-instrument/mobile lint
pnpm --filter @pro-instrument/mobile test
go -C apps/backend test ./...
```
