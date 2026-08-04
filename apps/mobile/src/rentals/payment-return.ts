import * as SecureStore from 'expo-secure-store';

const PENDING_RENTAL_KEY = 'pro-instrument.pending-payment-rental';

export interface PendingPaymentAttempt {
  rentalID: string;
  startedAt: number;
}

export async function rememberPendingPaymentRental(
  rentalID: string,
): Promise<number> {
  const startedAt = Date.now();
  await SecureStore.setItemAsync(
    PENDING_RENTAL_KEY,
    JSON.stringify({ rentalID, startedAt } satisfies PendingPaymentAttempt),
  );
  return startedAt;
}

export async function getPendingPaymentAttempt(): Promise<
  PendingPaymentAttempt | null
> {
  const storedValue = await SecureStore.getItemAsync(PENDING_RENTAL_KEY);
  if (!storedValue) return null;

  try {
    const parsed = JSON.parse(storedValue) as Partial<PendingPaymentAttempt>;
    if (
      typeof parsed.rentalID === 'string' &&
      parsed.rentalID.length > 0 &&
      typeof parsed.startedAt === 'number' &&
      Number.isFinite(parsed.startedAt)
    ) {
      return { rentalID: parsed.rentalID, startedAt: parsed.startedAt };
    }
  } catch {
    // Старые версии приложения сохраняли только rental ID.
    return { rentalID: storedValue, startedAt: Date.now() };
  }

  return null;
}

export function clearPendingPaymentRental(): Promise<void> {
  return SecureStore.deleteItemAsync(PENDING_RENTAL_KEY);
}
