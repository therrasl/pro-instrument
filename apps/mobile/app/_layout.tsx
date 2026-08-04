import * as Notifications from 'expo-notifications';
import { Stack, useRouter, type Href } from 'expo-router';
import { useEffect, useRef } from 'react';
import { StatusBar } from 'react-native';
import { SessionProvider, useSession } from '../src/auth/session';
import { AppConfigProvider } from '../src/app-config/context';
import { SessionSkeleton } from '../src/components/skeleton';
import { Button, Page, StateView } from '../src/components/ui';
import { colors } from '../src/theme/tokens';
import { rentalIDFromNotification } from '../src/notifications/push';

export default function RootLayout() {
  return (
    <AppConfigProvider>
      <SessionProvider>
        <StatusBar barStyle="dark-content" backgroundColor={colors.surface} />
        <SessionStack />
      </SessionProvider>
    </AppConfigProvider>
  );
}

function SessionStack() {
  const { error, refreshClient, status, token } = useSession();

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
          title="Не удалось войти"
        />
      </Page>
    );
  }

  const authenticated = status === 'authenticated' && Boolean(token);

  return <AuthenticatedStack authenticated={authenticated} />;
}

function AuthenticatedStack({ authenticated }: { authenticated: boolean }) {
  useNotificationNavigation(authenticated);

  return (
    <Stack
      screenOptions={{
        contentStyle: { backgroundColor: colors.background },
        headerShown: false,
      }}
    >
      <Stack.Screen name="index" />
      <Stack.Protected guard={!authenticated}>
        <Stack.Screen name="(auth)" />
      </Stack.Protected>
      <Stack.Protected guard={authenticated}>
        <Stack.Screen name="(app)" />
      </Stack.Protected>
    </Stack>
  );
}

function useNotificationNavigation(authenticated: boolean): void {
  const router = useRouter();
  const lastHandledID = useRef('');

  useEffect(() => {
    if (!authenticated) return;

    const openRental = (response: Notifications.NotificationResponse) => {
      const responseID = response.notification.request.identifier;
      if (lastHandledID.current === responseID) return;
      const rentalID = rentalIDFromNotification(response);
      if (!rentalID) return;
      lastHandledID.current = responseID;
      router.push(`/(app)/rentals/${encodeURIComponent(rentalID)}` as Href);
    };

    void Notifications.getLastNotificationResponseAsync().then((response) => {
      if (response) openRental(response);
    });
    const subscription = Notifications.addNotificationResponseReceivedListener(openRental);
    return () => subscription.remove();
  }, [authenticated, router]);
}
