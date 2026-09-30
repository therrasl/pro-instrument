const configuredURL = process.env.EXPO_PUBLIC_API_URL?.trim();

export const API_URL = configuredURL?.replace(/\/+$/, '') ?? '';

export function resolveAPIAssetURL(value: string): string {
  if (!value || /^(?:https?:|data:|file:)/u.test(value)) return value;
  return `${API_URL}${value.startsWith('/') ? value : `/${value}`}`;
}

export class ApiError extends Error {
  constructor(
    public readonly status: number,
    message: string,
  ) {
    super(message);
    this.name = 'ApiError';
  }
}

function userMessage(status: number, serverMessage?: string): string {
  if (serverMessage === 'invalid or expired code') return 'Неверный или просроченный код.';
  if (serverMessage === 'invalid profile') return 'Проверьте данные профиля.';
  if (serverMessage === 'invalid document') return 'Файл не прошёл проверку.';
  if (serverMessage === 'documents incomplete') {
    return 'Сначала загрузите все обязательные документы.';
  }
  if (serverMessage === 'document submission not allowed') {
    return 'Документы сейчас нельзя отправить на проверку.';
  }
  if (serverMessage === 'all consents are required') {
    return 'Подтвердите все обязательные условия.';
  }
  if (serverMessage === 'invalid rental request') {
    return 'Проверьте даты и способ получения.';
  }
  if (serverMessage === 'rental onboarding is incomplete' || serverMessage === 'client verification is incomplete') {
    return 'Для аренды сначала завершите проверку документов.';
  }
  if (serverMessage === 'no tool unit is available for selected dates') {
    return 'На выбранные даты инструмент уже недоступен.';
  }
  if (serverMessage === 'rental request is not awaiting payment') {
    return 'Эта заявка больше не ожидает оплаты.';
  }
  if (serverMessage === 'payment deadline has expired') {
    return 'Время на оплату истекло.';
  }
  if (serverMessage === 'rental request cannot be cancelled') {
    return 'Этот заказ уже нельзя отменить.';
  }
  if (serverMessage === 'payments are disabled') {
    return 'Оплата временно недоступна. Попробуйте позже.';
  }
  if (serverMessage === 'email is required for fiscal receipt') {
    return 'Добавьте email в профиль для получения чека.';
  }
  if (serverMessage === 'payment provider is unavailable') {
    return 'Платёжный сервис временно недоступен. Попробуйте позже.';
  }
  if (serverMessage === 'payment requires reconciliation') {
    return 'Проверяем платёж вручную. Повторно платить не нужно.';
  }
  if (serverMessage === 'rental request not found') {
    return 'Заявка не найдена или недоступна.';
  }
  if (serverMessage === 'tool is not available for requested extension period') {
    return 'На выбранные даты инструмент уже забронирован следующим клиентом.';
  }
  if (serverMessage === 'new end date must be after current end date') {
    return 'Новая дата возврата должна быть позже текущей даты возврата.';
  }
  if (serverMessage === 'rental is not active for extension') {
    return 'Продление возможно только для активной аренды.';
  }
  if (serverMessage === 'invalid inspection photo file') {
    return 'Файл фотографии некорректен или поврежден.';
  }
  if (serverMessage && serverMessage.trim()) {
    return serverMessage;
  }
  if (status === 401) return 'Войдите в аккаунт ещё раз.';
  if (status === 403) return 'Действие недоступно. Проверьте статус верификации.';
  if (status === 409) return 'Конфликт состояния заявки. Обновите страницу.';
  if (status === 429) return 'Слишком много попыток. Попробуйте позже.';
  if (status >= 500) return 'Сервер временно недоступен. Попробуйте позже.';
  return 'Не удалось выполнить запрос.';
}

export interface ApiRequestOptions {
  method?: string;
  token?: string;
  body?: unknown;
  headers?: Record<string, string>;
  timeoutMs?: number;
}

export async function apiRequest<T>(
  path: string,
  options: ApiRequestOptions = {},
): Promise<T> {
  const url = `${API_URL}${path}`;
  const headers = new Headers(options.headers);

  if (options.token) {
    headers.set('Authorization', `Bearer ${options.token}`);
  }

  let requestBody: BodyInit | undefined;

  if (options.body !== undefined && options.body !== null) {
    if (options.body instanceof FormData) {
      // Content-Type вручную не ставим:
      // fetch сам добавит multipart boundary.
      requestBody = options.body;
    } else if (typeof options.body === 'string') {
      requestBody = options.body;

      if (!headers.has('Content-Type')) {
        headers.set('Content-Type', 'application/json');
      }
    } else {
      headers.set('Content-Type', 'application/json');
      requestBody = JSON.stringify(options.body);
    }
  }

  let response: Response;
  const controller = options.timeoutMs ? new AbortController() : null;
  const timeout = controller
    ? setTimeout(() => controller.abort(), options.timeoutMs)
    : null;

  try {
    response = await fetch(url, {
      method: options.method ?? 'GET',
      headers,
      body: requestBody,
      signal: controller?.signal,
    });
  } catch (cause) {
    if (cause instanceof Error && cause.name === 'AbortError') {
      const timeoutError = new Error(
        'Сервер не ответил вовремя. Попробуйте ещё раз.',
      ) as Error & { cause?: unknown };
      timeoutError.cause = cause;
      throw timeoutError;
    }

    console.warn('FETCH FAILED:', {
      url,
      message: cause instanceof Error ? cause.message : String(cause),
      cause,
    });

    const error = new Error(
      cause instanceof Error
        ? `Нет соединения с сервером: ${cause.message}`
        : 'Нет соединения с сервером.',
    ) as Error & { cause?: unknown };

    error.cause = cause;
    throw error;
  } finally {
    if (timeout) clearTimeout(timeout);
  }

  const raw = await response.text();

  let data: unknown = null;

  if (raw.trim()) {
    try {
      data = JSON.parse(raw);
    } catch {
      data = raw;
    }
  }

  if (!response.ok) {
    let serverMessage: string | undefined;

    if (typeof data === 'string' && data.trim()) {
      serverMessage = data;
    }

    if (data && typeof data === 'object') {
      const errorBody = data as {
        error?: unknown;
        message?: unknown;
      };

      if (typeof errorBody.error === 'string') {
        serverMessage = errorBody.error;
      } else if (typeof errorBody.message === 'string') {
        serverMessage = errorBody.message;
      }
    }

    throw new ApiError(response.status, userMessage(response.status, serverMessage));
  }

  return data as T;
}
