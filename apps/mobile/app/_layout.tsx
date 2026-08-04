import { Stack } from 'expo-router';
import { StatusBar } from 'react-native';
import { SessionProvider, useSession } from '../src/auth/session';
import { SessionSkeleton } from '../src/components/skeleton';
import { Button, Page, StateView } from '../src/components/ui';
import { colors } from '../src/theme/tokens';

export default function RootLayout() {
  return (
    <SessionProvider>
      <StatusBar barStyle="dark-content" backgroundColor={colors.surface} />
      <SessionStack />
    </SessionProvider>
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
