import { apiRequest } from './client';
import type {
  AuthSession,
  Client,
  ConsentAcceptance,
  ProfilePatch,
  RentalEligibility,
} from '../types/api';
import { offerVersion, privacyVersion } from '../content/legal';

export function requestCode(phone: string): Promise<{ status: string }> {
  return apiRequest('/api/v1/auth/request-code', {
    method: 'POST',
    body: { phone },
  });
}

export function verifyCode(phone: string, code: string): Promise<AuthSession> {
  return apiRequest('/api/v1/auth/verify-code', {
    method: 'POST',
    body: { phone, code },
  });
}

export function normalizeClientProgress(client: Client): Client {
  return {
    ...client,
    phone_verified: Boolean(client.phone_verified || client.phone_verified_at),
    offer_accepted: Boolean(client.offer_accepted || client.offer_accepted_at),
    profile_completed: Boolean(
      client.profile_completed || (client.full_name?.trim() && client.birth_date),
    ),
  };
}

export async function getMe(token: string): Promise<Client> {
  return normalizeClientProgress(await apiRequest('/api/v1/me', { token }));
}

export function getRentalEligibility(token: string): Promise<RentalEligibility> {
  return apiRequest('/api/v1/me/rental-eligibility', { token });
}

export async function patchMe(token: string, profile: ProfilePatch): Promise<Client> {
  return normalizeClientProgress(
    await apiRequest('/api/v1/me', { method: 'PATCH', token, body: profile }),
  );
}

export function acceptConsents(token: string): Promise<ConsentAcceptance> {
  return apiRequest('/api/v1/me/consents', {
    method: 'POST',
    token,
    timeoutMs: 15_000,
    body: {
      offer_version: offerVersion,
      privacy_version: privacyVersion,
      offer_accepted: true,
      privacy_accepted: true,
      data_accuracy_confirmed: true,
      rental_rules_accepted: true,
    },
  });
}
