import { requireOptionalNativeModule } from 'expo';
import { Platform } from 'react-native';

export async function copyText(value: string): Promise<boolean> {
  if (Platform.OS !== 'web' && !requireOptionalNativeModule('ExpoClipboard')) return false;
  try {
    const Clipboard = await import('expo-clipboard');
    await Clipboard.setStringAsync(value);
    return true;
  } catch {
    return false;
  }
}
