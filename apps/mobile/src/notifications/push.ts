import Constants from 'expo-constants';
import * as Device from 'expo-device';
import * as Notifications from 'expo-notifications';
import { Platform } from 'react-native';

export const RENTAL_NOTIFICATION_CHANNEL = 'rental-updates';

Notifications.setNotificationHandler({
  handleNotification: async () => ({
    shouldPlaySound: true,
    shouldSetBadge: false,
    shouldShowBanner: true,
    shouldShowList: true,
  }),
});

async function ensureAndroidChannel(): Promise<void> {
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
  if (!Device.isDevice) return null;

  await ensureAndroidChannel();
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
  response: Notifications.NotificationResponse,
): string | null {
  const rentalID = response.notification.request.content.data?.rental_id;
  return typeof rentalID === 'string' && rentalID.trim() ? rentalID : null;
}
