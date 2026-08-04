import { apiRequest } from './client';

export type PushPlatform = 'android' | 'ios';

export function registerPushToken(
  sessionToken: string,
  token: string,
  platform: PushPlatform,
): Promise<void> {
  return apiRequest('/api/v1/me/push-tokens', {
    method: 'POST',
    token: sessionToken,
    body: { token, platform },
  });
}

export function deletePushToken(
  sessionToken: string,
  token: string,
): Promise<void> {
  return apiRequest('/api/v1/me/push-tokens', {
    method: 'DELETE',
    token: sessionToken,
    body: { token },
  });
}
