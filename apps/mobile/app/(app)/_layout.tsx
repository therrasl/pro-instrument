import { Stack } from 'expo-router';
import { onboardingStage } from '../../src/auth/routing';
import { useSession } from '../../src/auth/session';
import { colors } from '../../src/theme/tokens';

export default function AppLayout() {
  const { client } = useSession();
  const stage = onboardingStage(client);

  return (
    <Stack
      screenOptions={{
        headerBackTitle: 'Назад',
        headerShadowVisible: false,
        contentStyle: { backgroundColor: colors.background },
        headerStyle: { backgroundColor: colors.surface },
        headerTintColor: colors.ink,
        headerTitleStyle: { fontSize: 17, fontWeight: '700' },
      }}
    >
      <Stack.Protected guard={stage === 'catalog'}>
        <Stack.Screen name="(tabs)" options={{ headerShown: false }} />
        <Stack.Screen name="tools/[id]" options={{ title: 'Инструмент' }} />
        <Stack.Screen name="rentals/[id]" options={{ title: 'Заказ' }} />
        <Stack.Screen name="payment-return" options={{ title: 'Проверка оплаты' }} />
        <Stack.Screen name="rental-unavailable" options={{ title: 'Аренда' }} />
        <Stack.Screen name="documents" options={{ title: 'Документы' }} />
      </Stack.Protected>
      <Stack.Protected guard={stage === 'offer'}>
        <Stack.Screen name="onboarding/offer" options={{ headerShown: false }} />
        <Stack.Screen name="onboarding/offer-document" options={{ title: 'Оферта' }} />
      </Stack.Protected>
      <Stack.Protected guard={stage === 'profile'}>
        <Stack.Screen name="onboarding/profile" options={{ headerShown: false }} />
      </Stack.Protected>
    </Stack>
  );
}
