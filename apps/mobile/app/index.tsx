import { Redirect } from 'expo-router';
import { nextRoute } from '../src/auth/routing';
import { useSession } from '../src/auth/session';
import { SessionSkeleton } from '../src/components/skeleton';
import { Button, Page, StateView } from '../src/components/ui';

export default function IndexScreen() {
  const { client, error, refreshClient, status, token } = useSession();
  if (status === 'loading') {
    return (
      <Page>
        <SessionSkeleton />
      </Page>
    );
  }
  if (status === 'error') {
    return (
      <Page>
        <StateView
          action={<Button label="Повторить" onPress={() => void refreshClient()} />}
          icon="cloud-offline-outline"
          message={error}
          title="Не удалось открыть приложение"
        />
      </Page>
    );
  }
  return <Redirect href={nextRoute(token, client)} />;
}
