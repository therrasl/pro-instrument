import { useRouter, type Href } from 'expo-router';
import { useCallback, useEffect, useState } from 'react';
import {
  clearPendingPaymentRental,
  getPendingPaymentAttempt,
} from '../../src/rentals/payment-return';
import { SessionSkeleton } from '../../src/components/skeleton';
import { Button, Page, StateView } from '../../src/components/ui';

export default function PaymentReturnScreen() {
  const router = useRouter();
  const [missing, setMissing] = useState(false);
  const [error, setError] = useState('');

  const resolvePayment = useCallback(async () => {
    setMissing(false);
    setError('');
    try {
      const attempt = await getPendingPaymentAttempt();
      if (!attempt) {
        setMissing(true);
        return;
      }
      await clearPendingPaymentRental();
      if (attempt.paymentType === 'extension') {
        router.replace({
          pathname: '/(app)/(tabs)/rentals',
          params: {
            extendedRentalID: attempt.rentalID,
            successBanner: 'extension_paid',
          },
        } as Href);
      } else {
        router.replace({
          pathname: '/(app)/rentals/[id]',
          params: {
            id: attempt.rentalID,
            paymentStartedAt: String(attempt.startedAt),
          },
        });
      }
    } catch {
      setError('Не удалось проверить оплату.');
    }
  }, [router]);

  useEffect(() => {
    const timeout = setTimeout(() => {
      void resolvePayment();
    }, 0);
    return () => clearTimeout(timeout);
  }, [resolvePayment]);

  if (error) {
    return (
      <Page>
        <StateView
          action={<Button label="Повторить" onPress={() => void resolvePayment()} />}
          icon="cloud-offline-outline"
          message={error}
          title="Не удалось проверить оплату"
        />
      </Page>
    );
  }

  if (!missing) {
    return (
      <Page>
        <SessionSkeleton />
      </Page>
    );
  }

  return (
    <Page>
      <StateView
        action={
          <Button
            label="Открыть мои аренды"
            onPress={() => router.replace('/(app)/(tabs)/rentals' as Href)}
          />
        }
        icon="receipt-outline"
        message="Откройте нужный заказ и потяните экран вниз, чтобы обновить статус."
        title="Заказ не выбран"
      />
    </Page>
  );
}
