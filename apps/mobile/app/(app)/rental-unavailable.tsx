import { useLocalSearchParams, useRouter, type Href } from 'expo-router';
import { useSession } from '../../src/auth/session';
import { Button, Page, StateView } from '../../src/components/ui';

export default function RentalUnavailableScreen() {
  const router = useRouter();
  const { verificationReason } = useLocalSearchParams<{ verificationReason?: string }>();
  const { client } = useSession();

  if (verificationReason === 'eligible' || (!verificationReason && client?.status === 'verified')) {
    return (
      <Page>
        <StateView
          action={<Button label="Вернуться к инструменту" onPress={() => router.back()} />}
          icon="hammer-outline"
          message="Вернитесь к инструменту и продолжите оформление."
          title="Можно оформлять аренду"
        />
      </Page>
    );
  }

  if (
    verificationReason === 'documents_pending' ||
    client?.status === 'pending_verification'
  ) {
    return (
      <Page>
        <StateView
          action={<Button label="Проверить статус" onPress={() => router.push('/(app)/documents' as Href)} />}
          icon="time-outline"
          message="Мы уже получили документы. После проверки аренда станет доступна автоматически."
          title="Документы на проверке"
        />
      </Page>
    );
  }

  if (verificationReason === 'documents_rejected' || client?.status === 'verification_rejected') {
    const reason = client?.verification_rejection_reason
      ? ` Причина: ${client.verification_rejection_reason}`
      : '';
    return (
      <Page>
        <StateView
          action={<Button label="Загрузить заново" onPress={() => router.push('/(app)/documents' as Href)} />}
          icon="alert-circle-outline"
          message={`Документы нужно исправить и отправить повторно.${reason}`}
          title="Документы отклонены"
        />
      </Page>
    );
  }

  return (
    <Page>
      <StateView
        action={<Button label="Загрузить документы" onPress={() => router.push('/(app)/documents' as Href)} />}
        icon="document-text-outline"
        message="Каталог доступен без проверки, но для получения инструмента нужно подтвердить личность."
        title="Подтвердите документы"
      />
    </Page>
  );
}
