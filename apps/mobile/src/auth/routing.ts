import type { Href } from 'expo-router';
import type { Client } from '../types/api';

export type OnboardingStage = 'offer' | 'profile' | 'catalog';

export function onboardingStage(client: Client | null): OnboardingStage {
  if (!client || (!client.offer_accepted && !client.offer_accepted_at)) return 'offer';
  if (
    !client.profile_completed &&
    (!client.full_name?.trim() || !client.birth_date)
  ) {
    return 'profile';
  }
  return 'catalog';
}

export function nextRoute(token: string | null, client: Client | null): Href {
  if (!token) return '/(auth)/sign-in' as Href;
  const stage = onboardingStage(client);
  if (stage === 'offer') return '/(app)/onboarding/offer' as Href;
  if (stage === 'profile') return '/(app)/onboarding/profile' as Href;
  return '/(app)/(tabs)/catalog' as Href;
}
