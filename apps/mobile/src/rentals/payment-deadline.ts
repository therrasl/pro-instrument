import type { Rental } from '../types/api';

type RentalPaymentDeadline = Pick<
  Rental,
  'expires_at' | 'payment_expires_at'
>;

export function getRemainingPaymentSeconds(
  rental: RentalPaymentDeadline,
  nowMs = Date.now(),
): number {
  const deadline = Date.parse(rental.payment_expires_at ?? rental.expires_at);
  if (!Number.isFinite(deadline)) return 0;
  return Math.max(0, Math.floor((deadline - nowMs) / 1_000));
}

export function formatPaymentCountdown(seconds: number): string {
  const minutes = Math.floor(seconds / 60);
  const remainder = seconds % 60;
  return `${String(minutes).padStart(2, '0')}:${String(remainder).padStart(2, '0')}`;
}
