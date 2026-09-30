import Constants from 'expo-constants';
import { requireOptionalNativeModule } from 'expo';
import { Platform } from 'react-native';
import type { NotificationResponse } from 'expo-notifications';

export const RENTAL_NOTIFICATION_CHANNEL = 'rental-updates';

type NotificationsModule = typeof import('expo-notifications');

let notificationsModule: Promise<NotificationsModule> | null = null;
let handlerConfigured = false;

function loadNotifications(): Promise<NotificationsModule | null> {
  if (
    Platform.OS !== 'web' &&
    !requireOptionalNativeModule('ExpoPushTokenManager')
  ) {
    return Promise.resolve(null);
  }
  notificationsModule ??= import('expo-notifications');
  return notificationsModule;
}

async function configureNotificationHandler(): Promise<NotificationsModule | null> {
  const Notifications = await loadNotifications();
  if (!Notifications) return null;
  if (!handlerConfigured) {
    Notifications.setNotificationHandler({
      handleNotification: async () => ({
        shouldPlaySound: true,
        shouldSetBadge: false,
        shouldShowBanner: true,
        shouldShowList: true,
      }),
    });
    handlerConfigured = true;
  }
  return Notifications;
}

async function ensureAndroidChannel(Notifications: NotificationsModule): Promise<void> {
  if (Platform.OS !== 'android') return;
  await Notifications.setNotificationChannelAsync(RENTAL_NOTIFICATION_CHANNEL, {
    name: 'Статусы аренды',
    description: 'Изменения статуса заказов и аренды',
    importance: Notifications.AndroidImportance.MAX,
    sound: 'default',
    vibrationPattern: [0, 250, 250, 250],
    lightColor: '#FF7A00',
  });
}

function easProjectID(): string | null {
  const configuredProjectID = Constants.expoConfig?.extra?.eas?.projectId;
  if (typeof configuredProjectID === 'string' && configuredProjectID.trim()) {
    return configuredProjectID;
  }
  const buildProjectID = Constants.easConfig?.projectId;
  return typeof buildProjectID === 'string' && buildProjectID.trim()
    ? buildProjectID
    : null;
}

export async function getAuthorizedExpoPushToken(): Promise<string | null> {
  if (
    Platform.OS !== 'web' &&
    !requireOptionalNativeModule('ExpoDevice')
  ) {
    return null;
  }

  let Device: typeof import('expo-device');
  let Notifications: NotificationsModule | null;
  try {
    [Device, Notifications] = await Promise.all([
      import('expo-device'),
      configureNotificationHandler(),
    ]);
  } catch {
    // The installed development client predates the native push modules.
    return null;
  }
  if (!Notifications) return null;
  if (!Device.isDevice) return null;

  await ensureAndroidChannel(Notifications);
  const current = await Notifications.getPermissionsAsync();
  let status = current.status;
  if (status !== 'granted') {
    const requested = await Notifications.requestPermissionsAsync();
    status = requested.status;
  }
  if (status !== 'granted') return null;

  const projectId = easProjectID();
  if (!projectId) {
    console.warn('Push notifications are unavailable: EAS projectId is missing.');
    return null;
  }

  return (await Notifications.getExpoPushTokenAsync({ projectId })).data;
}

export function rentalIDFromNotification(
  response: NotificationResponse,
): string | null {
  const rentalID = response.notification.request.content.data?.rental_id;
  return typeof rentalID === 'string' && rentalID.trim() ? rentalID : null;
}

export async function subscribeToNotificationResponses(
  listener: (response: NotificationResponse) => void,
): Promise<() => void> {
  try {
    const Notifications = await configureNotificationHandler();
    if (!Notifications) return () => undefined;
    const initialResponse = await Notifications.getLastNotificationResponseAsync();
    if (initialResponse) listener(initialResponse);
    const subscription = Notifications.addNotificationResponseReceivedListener(listener);
    return () => subscription.remove();
  } catch {
    // Authentication must keep working in an older development client.
    return () => undefined;
  }
}
