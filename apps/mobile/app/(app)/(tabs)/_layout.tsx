import { Ionicons } from '@expo/vector-icons';
import { Tabs } from 'expo-router';
import { colors, controlHeights, spacing } from '../../../src/theme/tokens';

export default function TabsLayout() {
  return (
    <Tabs
      screenOptions={{
        headerShown: false,
        tabBarActiveTintColor: colors.primary,
        tabBarInactiveTintColor: colors.muted,
        tabBarHideOnKeyboard: true,
        tabBarStyle: {
          borderTopWidth: 0,
          backgroundColor: colors.surface,
          minHeight: 64,
          paddingTop: spacing.sm,
          paddingBottom: spacing.sm,
          shadowColor: colors.ink,
          shadowOffset: { width: 0, height: -3 },
          shadowOpacity: 0.06,
          shadowRadius: 8,
          elevation: 8,
        },
        tabBarItemStyle: { minHeight: controlHeights.default },
        tabBarLabelStyle: { fontSize: 12, lineHeight: 16, fontWeight: '700' },
      }}
    >
      <Tabs.Screen
        name="catalog"
        options={{
          title: 'Каталог',
          tabBarIcon: ({ color, focused, size }) => (
            <Ionicons color={color} name={focused ? 'grid' : 'grid-outline'} size={size} />
          ),
        }}
      />
      <Tabs.Screen
        name="rentals"
        options={{
          title: 'Мои аренды',
          tabBarIcon: ({ color, focused, size }) => (
            <Ionicons color={color} name={focused ? 'receipt' : 'receipt-outline'} size={size} />
          ),
        }}
      />
      <Tabs.Screen
        name="profile"
        options={{
          title: 'Профиль',
          tabBarIcon: ({ color, focused, size }) => (
            <Ionicons color={color} name={focused ? 'person' : 'person-outline'} size={size} />
          ),
        }}
      />
    </Tabs>
  );
}
