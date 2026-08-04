import { describe, expect, it } from 'vitest';
import type { Client } from '../types/api';
import { normalizeClientProgress } from './auth';

describe('normalizeClientProgress', () => {
  it('restores onboarding flags from persisted backend fields', () => {
    const client: Client = {
      id: 'client-id',
      phone: '+79991234567',
      phone_verified: false,
      phone_verified_at: '2026-07-25T00:00:00Z',
      offer_accepted: false,
      offer_accepted_at: '2026-07-25T00:01:00Z',
      profile_completed: false,
      full_name: 'Иванов Иван',
      birth_date: '1990-04-12',
      email: null,
      status: 'profile_completed',
      verification_rejection_reason: null,
      created_at: '2026-07-25T00:00:00Z',
      updated_at: '2026-07-25T00:01:00Z',
    };

    expect(normalizeClientProgress(client)).toMatchObject({
      phone_verified: true,
      offer_accepted: true,
      profile_completed: true,
    });
  });
});
