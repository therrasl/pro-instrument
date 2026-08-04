import { Ionicons } from '@expo/vector-icons';
import {
  useEffect,
  useRef,
  useState,
  type ComponentProps,
  type PropsWithChildren,
  type ReactNode,
} from 'react';
import {
  AccessibilityInfo,
  ActivityIndicator,
  Animated,
  Easing,
  Pressable,
  SafeAreaView,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  View,
  type KeyboardTypeOptions,
  type StyleProp,
  type TextInputProps,
  type ViewStyle,
} from 'react-native';
import {
  colors,
  controlHeights,
  iconSizes,
  radius,
  spacing,
  typography,
} from '../theme/tokens';

type IconName = ComponentProps<typeof Ionicons>['name'];

export function Page({
  children,
  style,
}: PropsWithChildren<{ style?: StyleProp<ViewStyle> }>) {
  const [opacity] = useState(() => new Animated.Value(0));

  useEffect(() => {
    let active = true;
    void AccessibilityInfo.isReduceMotionEnabled().then((reduceMotion) => {
      if (!active) return;
      Animated.timing(opacity, {
        duration: reduceMotion ? 1 : 200,
        easing: Easing.out(Easing.cubic),
        toValue: 1,
        useNativeDriver: true,
      }).start();
    });
    return () => {
      active = false;
      opacity.stopAnimation();
    };
  }, [opacity]);

  return (
    <SafeAreaView style={[styles.page, style]}>
      <Animated.View style={[styles.pageContent, { opacity }]}>{children}</Animated.View>
    </SafeAreaView>
  );
}

export function ScrollPage({
  children,
  contentStyle,
}: PropsWithChildren<{ contentStyle?: StyleProp<ViewStyle> }>) {
  return (
    <Page>
      <ScrollView
        contentContainerStyle={[styles.scrollContent, contentStyle]}
        keyboardShouldPersistTaps="handled"
        showsVerticalScrollIndicator={false}
      >
        {children}
      </ScrollView>
    </Page>
  );
}

export function Title({
  children,
  compact = false,
}: PropsWithChildren<{ compact?: boolean }>) {
  return <Text style={[styles.title, compact && styles.titleCompact]}>{children}</Text>;
}

export function Body({
  children,
  muted = false,
}: PropsWithChildren<{ muted?: boolean }>) {
  return <Text style={[styles.body, muted && styles.muted]}>{children}</Text>;
}

interface ButtonProps {
  label: string;
  onPress: () => void;
  loading?: boolean;
  disabled?: boolean;
  variant?: 'primary' | 'secondary' | 'danger' | 'text' | 'dangerText';
  icon?: IconName;
}

export function Button({
  label,
  onPress,
  loading = false,
  disabled = false,
  variant = 'primary',
  icon,
}: ButtonProps) {
  const inactive = loading || disabled;
  const [scale] = useState(() => new Animated.Value(1));
  const reduceMotion = useRef(false);

  useEffect(() => {
    void AccessibilityInfo.isReduceMotionEnabled().then((value) => {
      reduceMotion.current = value;
    });
  }, []);

  const animateScale = (toValue: number) => {
    Animated.timing(scale, {
      duration: reduceMotion.current ? 1 : toValue < 1 ? 90 : 150,
      easing: Easing.out(Easing.cubic),
      toValue,
      useNativeDriver: true,
    }).start();
  };

  return (
    <Animated.View style={{ transform: [{ scale }] }}>
      <Pressable
        accessibilityRole="button"
        accessibilityState={{ disabled: inactive, busy: loading }}
        disabled={inactive}
        onPress={onPress}
        onPressIn={() => !inactive && animateScale(0.96)}
        onPressOut={() => animateScale(1)}
        style={({ pressed }) => [
          styles.button,
          variant === 'secondary' && styles.buttonSecondary,
          variant === 'danger' && styles.buttonDanger,
          variant === 'text' && styles.buttonText,
          variant === 'dangerText' && styles.buttonText,
          inactive && styles.buttonDisabled,
          pressed && !inactive && variant === 'primary' && styles.buttonPrimaryPressed,
        ]}
      >
        {loading ? (
          <ActivityIndicator
            color={
              variant === 'dangerText'
                ? colors.error
                : variant === 'secondary' || variant === 'text'
                  ? colors.primary
                  : colors.white
            }
          />
        ) : (
          <>
            {icon ? (
              <Ionicons
                color={
                  variant === 'dangerText'
                    ? colors.error
                    : variant === 'secondary' || variant === 'text'
                      ? colors.primary
                      : colors.white
                }
                name={icon}
                size={iconSizes.md}
              />
            ) : null}
            <Text
              style={[
                styles.buttonLabel,
                (variant === 'secondary' || variant === 'text') && styles.buttonLabelSecondary,
                variant === 'dangerText' && styles.buttonLabelDanger,
              ]}
            >
              {label}
            </Text>
          </>
        )}
      </Pressable>
    </Animated.View>
  );
}

interface FieldProps extends Pick<TextInputProps, 'autoCapitalize' | 'maxLength' | 'secureTextEntry'> {
  label: string;
  value: string;
  onChangeText?: (value: string) => void;
  placeholder?: string;
  keyboardType?: KeyboardTypeOptions;
  error?: string;
  onBlur?: TextInputProps['onBlur'];
  onPress?: () => void;
  trailingIcon?: IconName;
}

export function Field({
  label,
  error,
  onBlur,
  onPress,
  trailingIcon,
  ...props
}: FieldProps) {
  const [focused, setFocused] = useState(false);

  return (
    <View style={styles.fieldGroup}>
      <Text style={styles.fieldLabel}>{label}</Text>
      {onPress ? (
        <Pressable
          accessibilityLabel={label}
          accessibilityRole="button"
          onPress={onPress}
          style={({ pressed }) => [
            styles.field,
            styles.fieldPressable,
            error && styles.fieldError,
            pressed && styles.fieldPressed,
          ]}
        >
          <Text style={[styles.fieldValue, !props.value && styles.fieldPlaceholder]}>
            {props.value || props.placeholder}
          </Text>
          {trailingIcon ? (
            <Ionicons color={colors.primary} name={trailingIcon} size={20} />
          ) : null}
        </Pressable>
      ) : (
        <TextInput
          {...props}
          accessibilityLabel={label}
          onBlur={(event) => {
            setFocused(false);
            onBlur?.(event);
          }}
          onFocus={() => setFocused(true)}
          placeholderTextColor={colors.muted}
          selectionColor={colors.primary}
          style={[styles.field, focused && styles.fieldFocused, error && styles.fieldError]}
        />
      )}
      {error ? (
        <View style={styles.fieldErrorRow}>
          <Ionicons color={colors.error} name="alert-circle" size={16} />
          <Text style={styles.errorText}>{error}</Text>
        </View>
      ) : null}
    </View>
  );
}

export function Card({
  children,
  style,
}: PropsWithChildren<{ style?: StyleProp<ViewStyle> }>) {
  return <View style={[styles.card, style]}>{children}</View>;
}

export function Checkbox({
  checked,
  label,
  onPress,
}: {
  checked: boolean;
  label: ReactNode;
  onPress: () => void;
}) {
  return (
    <View style={styles.checkboxRow}>
      <Pressable
        accessibilityLabel={typeof label === 'string' ? label : 'Переключить согласие'}
        accessibilityRole="checkbox"
        accessibilityState={{ checked }}
        hitSlop={10}
        onPress={onPress}
        style={({ pressed }) => [
          styles.checkboxControl,
          pressed && styles.checkboxPressed,
        ]}
      >
        <View style={[styles.checkbox, checked && styles.checkboxChecked]}>
          {checked ? <Ionicons color={colors.white} name="checkmark" size={18} /> : null}
        </View>
      </Pressable>
      {typeof label === 'string' ? (
        <Pressable
          accessibilityRole="checkbox"
          accessibilityState={{ checked }}
          onPress={onPress}
          style={({ pressed }) => [
            styles.checkboxLabelControl,
            pressed && styles.checkboxPressed,
          ]}
        >
          <Text style={styles.checkboxLabel}>{label}</Text>
        </Pressable>
      ) : (
        <Pressable
          accessibilityRole="checkbox"
          accessibilityState={{ checked }}
          onPress={onPress}
          style={({ pressed }) => [
            styles.checkboxLabelControl,
            pressed && styles.checkboxPressed,
          ]}
        >
          {label}
        </Pressable>
      )}
    </View>
  );
}

export function StateView({
  icon,
  title,
  message,
  action,
  compact = false,
  loading = false,
}: {
  icon: IconName;
  title: string;
  message: string;
  action?: ReactNode;
  compact?: boolean;
  loading?: boolean;
}) {
  return (
    <View style={[styles.state, compact && styles.stateCompact]}>
      <View style={styles.stateIcon}>
        {loading ? (
          <ActivityIndicator color={colors.primary} />
        ) : (
          <Ionicons color={colors.primary} name={icon} size={25} />
        )}
      </View>
      <Text style={styles.stateTitle}>{title}</Text>
      <Text style={styles.stateMessage}>{message}</Text>
      {action ? <View style={styles.stateAction}>{action}</View> : null}
    </View>
  );
}

export function ErrorNotice({ message }: { message: string }) {
  return (
    <View accessibilityLiveRegion="polite" style={styles.notice}>
      <Ionicons color={colors.error} name="alert-circle-outline" size={20} />
      <Text style={styles.noticeText}>{message}</Text>
    </View>
  );
}

export function StatusPill({
  label,
  tone = 'neutral',
}: {
  label: string;
  tone?: 'neutral' | 'success' | 'warning' | 'error';
}) {
  return (
    <View
      style={[
        styles.pill,
        tone === 'success' && styles.pillSuccess,
        tone === 'warning' && styles.pillWarning,
        tone === 'error' && styles.pillError,
      ]}
    >
      <Text
        style={[
          styles.pillText,
          tone === 'success' && styles.pillTextSuccess,
          tone === 'warning' && styles.pillTextWarning,
          tone === 'error' && styles.pillTextError,
        ]}
      >
        {label}
      </Text>
    </View>
  );
}

const styles = StyleSheet.create({
  page: { flex: 1, backgroundColor: colors.background },
  pageContent: { flex: 1 },
  scrollContent: {
    paddingHorizontal: spacing.xl,
    paddingTop: spacing.xl,
    paddingBottom: spacing.hero,
    gap: spacing.lg,
  },
  title: {
    color: colors.ink,
    ...typography.title,
  },
  titleCompact: { fontSize: 27, lineHeight: 33, letterSpacing: -0.5 },
  body: { color: colors.ink, ...typography.body },
  muted: { color: colors.muted },
  button: {
    minHeight: controlHeights.default,
    borderRadius: radius.button,
    paddingHorizontal: spacing.lg,
    alignItems: 'center',
    justifyContent: 'center',
    flexDirection: 'row',
    gap: spacing.sm,
    backgroundColor: colors.primary,
  },
  buttonSecondary: {
    backgroundColor: colors.primarySoft,
  },
  buttonText: {
    alignSelf: 'flex-start',
    minHeight: controlHeights.compact,
    paddingHorizontal: spacing.sm,
    backgroundColor: 'transparent',
  },
  buttonPrimaryPressed: { backgroundColor: colors.primaryPressed },
  buttonDanger: { backgroundColor: colors.error },
  buttonDisabled: { opacity: 0.44 },
  buttonLabel: { color: colors.white, fontSize: 16, lineHeight: 21, fontWeight: '700' },
  buttonLabelSecondary: { color: colors.primary },
  buttonLabelDanger: { color: colors.error },
  fieldGroup: { gap: spacing.sm },
  fieldLabel: { color: colors.ink, ...typography.label },
  field: {
    minHeight: controlHeights.default,
    borderWidth: 1,
    borderColor: colors.outline,
    borderRadius: radius.button,
    paddingHorizontal: spacing.lg,
    color: colors.ink,
    backgroundColor: colors.surface,
    fontSize: 16,
  },
  fieldFocused: { borderColor: colors.primary },
  fieldPressable: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: spacing.md,
  },
  fieldPressed: { backgroundColor: colors.primarySoft, transform: [{ scale: 0.99 }] },
  fieldValue: { flex: 1, color: colors.ink, fontSize: 16, lineHeight: 21 },
  fieldPlaceholder: { color: colors.muted },
  fieldError: { borderColor: colors.error },
  fieldErrorRow: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm },
  errorText: { color: colors.error, flex: 1, fontSize: 13, lineHeight: 18 },
  card: {
    backgroundColor: colors.surfaceSubtle,
    borderRadius: radius.lg,
    padding: spacing.lg,
  },
  checkboxRow: {
    minHeight: 48,
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.sm,
  },
  checkboxPressed: { opacity: 0.72 },
  checkboxControl: {
    width: 44,
    height: 48,
    alignItems: 'center',
    justifyContent: 'center',
  },
  checkbox: {
    width: 24,
    height: 24,
    borderRadius: radius.sm,
    borderWidth: 1.5,
    borderColor: colors.outline,
    alignItems: 'center',
    justifyContent: 'center',
    backgroundColor: colors.surface,
  },
  checkboxChecked: { borderColor: colors.primary, backgroundColor: colors.primary },
  checkboxLabelControl: {
    flex: 1,
    minHeight: 48,
    justifyContent: 'center',
  },
  checkboxLabel: { color: colors.ink, ...typography.body },
  state: {
    flex: 1,
    alignItems: 'center',
    justifyContent: 'center',
    paddingHorizontal: spacing.xl,
    paddingVertical: spacing.hero,
  },
  stateCompact: { paddingVertical: spacing.xxl },
  stateIcon: {
    width: 56,
    height: 56,
    borderRadius: radius.lg,
    alignItems: 'center',
    justifyContent: 'center',
    backgroundColor: colors.primarySoft,
    marginBottom: spacing.lg,
  },
  stateTitle: {
    color: colors.ink,
    fontSize: 21,
    lineHeight: 27,
    fontWeight: '800',
    textAlign: 'center',
  },
  stateMessage: {
    color: colors.muted,
    fontSize: 16,
    lineHeight: 23,
    textAlign: 'center',
    marginTop: spacing.md,
    maxWidth: 320,
  },
  stateAction: { width: '100%', marginTop: spacing.xl },
  notice: {
    borderRadius: radius.button,
    padding: spacing.lg,
    flexDirection: 'row',
    alignItems: 'flex-start',
    gap: spacing.md,
    backgroundColor: colors.errorSoft,
  },
  noticeText: { color: colors.error, flex: 1, fontSize: 14, lineHeight: 21, fontWeight: '600' },
  pill: {
    alignSelf: 'flex-start',
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
    borderRadius: radius.pill,
    backgroundColor: colors.surfaceSubtle,
  },
  pillSuccess: { backgroundColor: colors.successSoft },
  pillWarning: { backgroundColor: colors.warningSoft },
  pillError: { backgroundColor: colors.errorSoft },
  pillText: { color: colors.muted, fontSize: 12, lineHeight: 16, fontWeight: '700' },
  pillTextSuccess: { color: colors.success },
  pillTextWarning: { color: colors.warning },
  pillTextError: { color: colors.error },
});
