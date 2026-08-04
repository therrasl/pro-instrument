import { apiRequest } from './client';

export type PublicAppConfig =
  | { demo_mode: false }
  | { demo_mode: true; message: string; demo_otp_code: string };

export async function getPublicAppConfig(): Promise<PublicAppConfig> {
  const value = await apiRequest<unknown>('/api/v1/public/app-config', {
    timeoutMs: 5_000,
  });
  if (!value || typeof value !== 'object') {
    throw new Error('Некорректная конфигурация приложения.');
  }
  const config = value as Record<string, unknown>;
  if (config.demo_mode === false) return { demo_mode: false };
  if (
    config.demo_mode === true &&
    typeof config.message === 'string' &&
    typeof config.demo_otp_code === 'string' &&
    /^\d{6}$/u.test(config.demo_otp_code)
  ) {
    return {
      demo_mode: true,
      message: config.message,
      demo_otp_code: config.demo_otp_code,
    };
  }
  throw new Error('Некорректная конфигурация приложения.');
}
