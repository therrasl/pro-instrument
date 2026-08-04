import { describe, expect, it } from 'vitest';
import type { Client } from '../types/api';
import { nextRoute, onboardingStage } from './routing';

const client = (overrides: Partial<Client> = {}): Client => ({
  id: 'client-id',
  phone: '+79991234567',
  phone_verified: true,
  phone_verified_at: '2026-07-25T00:00:00Z',
  offer_accepted: false,
  offer_accepted_at: null,
  profile_completed: false,
  full_name: null,
  birth_date: null,
  email: null,
  status: 'phone_verified',
  verification_rejection_reason: null,
  created_at: '2026-07-25T00:00:00Z',
  updated_at: '2026-07-25T00:00:00Z',
  ...overrides,
});

describe('centralized onboarding routing', () => {
  it('routes a verified phone to the offer exactly once', () => {
    const value = client();
    expect(onboardingStage(value)).toBe('offer');
    expect(nextRoute('token', value)).toBe('/(app)/onboarding/offer');
  });

  it('routes accepted clients to profile, never back to the offer', () => {
    const value = client({
      offer_accepted: true,
      offer_accepted_at: '2026-07-25T00:01:00Z',
    });
    expect(onboardingStage(value)).toBe('profile');
    expect(nextRoute('token', value)).toBe('/(app)/onboarding/profile');
  });

  it('opens the catalog only after required profile fields are present', () => {
    const value = client({
      offer_accepted: true,
      offer_accepted_at: '2026-07-25T00:01:00Z',
      profile_completed: true,
      full_name: 'Иванов Иван',
      birth_date: '1990-04-12',
      email: null,
    });
    expect(onboardingStage(value)).toBe('catalog');
    expect(nextRoute('token', value)).toBe('/(app)/(tabs)/catalog');
  });

  it('always routes a missing token to sign in', () => {
    expect(nextRoute(null, client({ offer_accepted: true, profile_completed: true }))).toBe(
      '/(auth)/sign-in',
    );
  });

  it('never redirects an active token back into the auth layout', () => {
    expect(nextRoute('token', client({ phone_verified: false, phone_verified_at: null }))).toBe(
      '/(app)/onboarding/offer',
    );
  });

  it('uses persisted backend timestamps as a compatibility fallback', () => {
    const value = client({
      offer_accepted: false,
      offer_accepted_at: '2026-07-25T00:01:00Z',
      profile_completed: false,
      full_name: 'Иванов Иван',
      birth_date: '1990-04-12',
    });

    expect(nextRoute('token', value)).toBe('/(app)/(tabs)/catalog');
  });
});
