import { Ionicons } from '@expo/vector-icons';
import { requireOptionalNativeModule } from 'expo';
import {
  createContext,
  useContext,
  useEffect,
  useRef,
  useState,
  type PropsWithChildren,
} from 'react';
import { Modal, Platform, Pressable, StyleSheet, Text, View } from 'react-native';
import { getPublicAppConfig, type PublicAppConfig } from '../api/app-config';
import { Button } from '../components/ui';
import { colors, radius, shadow, spacing, typography } from '../theme/tokens';

interface AppConfigValue {
  config: PublicAppConfig | null;
}

const AppConfigContext = createContext<AppConfigValue>({ config: null });

async function copyDemoCode(code: string): Promise<boolean> {
  if (
    Platform.OS !== 'web' &&
    !requireOptionalNativeModule('ExpoClipboard')
  ) {
    return false;
  }

  try {
    const Clipboard = await import('expo-clipboard');
    await Clipboard.setStringAsync(code);
    return true;
  } catch {
    return false;
  }
}

export function AppConfigProvider({ children }: PropsWithChildren) {
  const [config, setConfig] = useState<PublicAppConfig | null>(null);
  const [demoVisible, setDemoVisible] = useState(false);
  const [copied, setCopied] = useState(false);
  const presented = useRef(false);

  useEffect(() => {
    let active = true;
    void getPublicAppConfig()
      .then((nextConfig) => {
        if (!active) return;
        setConfig(nextConfig);
        if (nextConfig.demo_mode && !presented.current) {
          presented.current = true;
          setDemoVisible(true);
        }
      })
      .catch(() => {
        // Demo discovery is optional: ordinary authentication remains available.
      });
    return () => {
      active = false;
    };
  }, []);

  const demoConfig = config?.demo_mode ? config : null;

  return (
    <AppConfigContext.Provider value={{ config }}>
      {children}
      <Modal
        animationType="fade"
        onRequestClose={() => setDemoVisible(false)}
        transparent
        visible={Boolean(demoConfig && demoVisible)}
      >
        <View style={styles.backdrop}>
          {demoConfig ? (
            <View
              accessibilityLabel={`${demoConfig.message} ${demoConfig.demo_otp_code}`}
              accessibilityViewIsModal
              style={styles.modal}
            >
              <View style={styles.iconWrap}>
                <Ionicons color={colors.primary} name="flask-outline" size={26} />
              </View>
              <View style={styles.copyBlock}>
                <Text accessibilityRole="header" style={styles.title}>
                  Демонстрационная версия
                </Text>
                <Text style={styles.body}>{demoConfig.message}</Text>
              </View>
              <Pressable
                accessibilityHint="Копирует демо-код"
                accessibilityLabel={`Демо-код ${demoConfig.demo_otp_code}`}
                accessibilityRole="button"
                onPress={() => {
                  void copyDemoCode(demoConfig.demo_otp_code).then((didCopy) => {
                    if (didCopy) setCopied(true);
                  });
                }}
                style={({ pressed }) => [styles.codeRow, pressed && styles.codeRowPressed]}
              >
                <Text selectable style={styles.code}>
                  {demoConfig.demo_otp_code}
                </Text>
                <View style={styles.copyAction}>
                  <Ionicons
                    color={colors.primary}
                    name={copied ? 'checkmark' : 'copy-outline'}
                    size={18}
                  />
                  <Text accessibilityLiveRegion="polite" style={styles.copyLabel}>
                    {copied ? 'Скопировано' : 'Копировать'}
                  </Text>
                </View>
              </Pressable>
              <Button label="Продолжить" onPress={() => setDemoVisible(false)} />
            </View>
          ) : null}
        </View>
      </Modal>
    </AppConfigContext.Provider>
  );
}

export function useAppConfig(): AppConfigValue {
  return useContext(AppConfigContext);
}

const styles = StyleSheet.create({
  backdrop: {
    flex: 1,
    justifyContent: 'center',
    padding: spacing.xl,
    backgroundColor: colors.scrim,
  },
  modal: {
    width: '100%',
    maxWidth: 420,
    alignSelf: 'center',
    gap: spacing.xl,
    padding: spacing.xl,
    borderRadius: radius.lg,
    backgroundColor: colors.surface,
    ...shadow,
  },
  iconWrap: {
    width: 48,
    height: 48,
    alignItems: 'center',
    justifyContent: 'center',
    borderRadius: radius.md,
    backgroundColor: colors.primarySoft,
  },
  copyBlock: { gap: spacing.sm },
  title: { ...typography.section, color: colors.ink },
  body: { ...typography.body, color: colors.muted },
  codeRow: {
    minHeight: 64,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: spacing.md,
    paddingHorizontal: spacing.lg,
    borderRadius: radius.md,
    backgroundColor: colors.primarySoft,
  },
  codeRowPressed: { opacity: 0.72 },
  code: {
    color: colors.ink,
    fontSize: 25,
    lineHeight: 30,
    fontWeight: '800',
    letterSpacing: 2.5,
    fontVariant: ['tabular-nums'],
  },
  copyAction: { alignItems: 'center', gap: spacing.xs },
  copyLabel: { ...typography.caption, color: colors.primary, fontWeight: '700' },
});
