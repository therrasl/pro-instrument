import { Ionicons } from '@expo/vector-icons';
import { useCallback, useEffect, useRef, useState } from 'react';
import {
  KeyboardAvoidingView,
  Platform,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  View,
} from 'react-native';
import { useSession } from '../../src/auth/session';
import {
  OTPInput,
  type OTPVisualState,
} from '../../src/components/otp-input';
import {
  Button,
  ErrorNotice,
  Page,
} from '../../src/components/ui';
import {
  colors,
  controlHeights,
  radius,
  spacing,
} from '../../src/theme/tokens';
import { formatPhone, getPhoneError } from '../../src/utils/auth';

const RESEND_SECONDS = 60;
const SUCCESS_ANIMATION_MS = 760;

export default function SignInScreen() {
  const { requestCode, verifyCode } = useSession();
  const [phone, setPhone] = useState('');
  const [phoneError, setPhoneError] = useState('');
  const [code, setCode] = useState('');
  const [step, setStep] = useState<'phone' | 'code'>('phone');
  const [secondsLeft, setSecondsLeft] = useState(0);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [otpState, setOTPState] = useState<OTPVisualState>('idle');
  const [phoneFocused, setPhoneFocused] = useState(false);
  const lastSubmittedCode = useRef('');
  const requestInFlight = useRef(false);
  const verificationInFlight = useRef(false);

  useEffect(() => {
    if (secondsLeft <= 0) return;
    const timer = setInterval(() => {
      setSecondsLeft((current) => Math.max(0, current - 1));
    }, 1000);
    return () => clearInterval(timer);
  }, [secondsLeft]);

  const sendCode = async () => {
    if (requestInFlight.current) return;
    const validationError = getPhoneError(phone);
    setPhoneError(validationError);
    if (validationError) return;

    requestInFlight.current = true;
    setLoading(true);
    setError('');
    try {
      await requestCode(phone);
      setStep('code');
      setCode('');
      setOTPState('idle');
      lastSubmittedCode.current = '';
      setSecondsLeft(RESEND_SECONDS);
    } catch (requestError) {
      setError(requestError instanceof Error ? requestError.message : 'Не удалось отправить код.');
    } finally {
      requestInFlight.current = false;
      setLoading(false);
    }
  };

  const confirmCode = useCallback(async () => {
    if (
      code.length !== 6 ||
      verificationInFlight.current ||
      lastSubmittedCode.current === code
    ) {
      return;
    }
    verificationInFlight.current = true;
    lastSubmittedCode.current = code;
    setLoading(true);
    setOTPState('verifying');
    setError('');
    try {
      await verifyCode(phone, code, async () => {
        setOTPState('success');
        await new Promise((resolve) => setTimeout(resolve, SUCCESS_ANIMATION_MS));
      });
    } catch (requestError) {
      setOTPState('error');
      setError(requestError instanceof Error ? requestError.message : 'Не удалось проверить код.');
    } finally {
      verificationInFlight.current = false;
      setLoading(false);
    }
  }, [code, phone, verifyCode]);

  useEffect(() => {
    if (step !== 'code' || code.length !== 6) return;
    const timeout = setTimeout(() => void confirmCode(), 120);
    return () => clearTimeout(timeout);
  }, [code, confirmCode, step]);

  const updatePhone = (value: string) => {
    const formatted = formatPhone(value);
    setPhone(formatted);
    if (phoneError) setPhoneError(getPhoneError(formatted));
  };

  const updateCode = (value: string) => {
    lastSubmittedCode.current = '';
    setCode(value);
    setOTPState('idle');
    if (error) setError('');
  };

  const editPhone = () => {
    setStep('phone');
    setCode('');
    setOTPState('idle');
    setError('');
    setPhoneError('');
  };

  if (step === 'code') {
    return (
      <Page>
        <KeyboardAvoidingView
          behavior={Platform.OS === 'ios' ? 'padding' : undefined}
          style={styles.flex}
        >
          <ScrollView
            contentContainerStyle={styles.codeContent}
            keyboardShouldPersistTaps="handled"
            showsVerticalScrollIndicator={false}
          >
            <Pressable
              accessibilityLabel="Вернуться к номеру телефона"
              accessibilityRole="button"
              disabled={loading}
              hitSlop={8}
              onPress={editPhone}
              style={({ pressed }) => [
                styles.backButton,
                pressed && styles.controlPressed,
              ]}
            >
              <Ionicons color={colors.ink} name="chevron-back" size={23} />
            </Pressable>

            <View style={styles.codeMain}>
              <View style={styles.codeHeading}>
                <Text style={styles.title}>Введите код</Text>
                <Text style={styles.subtitle}>Отправили шесть цифр на {phone}</Text>
                <Pressable
                  accessibilityRole="button"
                  disabled={loading}
                  hitSlop={8}
                  onPress={editPhone}
                  style={({ pressed }) => [
                    styles.changePhone,
                    pressed && styles.controlPressed,
                  ]}
                >
                  <Text style={styles.changePhoneText}>Изменить номер</Text>
                </Pressable>
              </View>

              {error ? <ErrorNotice message={error} /> : null}

              <OTPInput
                disabled={loading}
                onChange={updateCode}
                state={otpState}
                value={code}
              />

              <View style={styles.resend}>
                {loading ? (
                  <Text accessibilityLiveRegion="polite" style={styles.timer}>
                    Проверяем код…
                  </Text>
                ) : secondsLeft > 0 ? (
                  <Text accessibilityLiveRegion="polite" style={styles.timer}>
                    {`Отправить код повторно через 0:${String(secondsLeft).padStart(2, '0')}`}
                  </Text>
                ) : (
                  <Button
                    label="Отправить код ещё раз"
                    onPress={() => void sendCode()}
                    variant="text"
                  />
                )}
              </View>
            </View>
          </ScrollView>
        </KeyboardAvoidingView>
      </Page>
    );
  }

  return (
    <Page>
      <KeyboardAvoidingView
        behavior={Platform.OS === 'ios' ? 'padding' : undefined}
        style={styles.flex}
      >
        <ScrollView
          contentContainerStyle={styles.phoneContent}
          keyboardShouldPersistTaps="handled"
          showsVerticalScrollIndicator={false}
        >
          <View style={styles.brand}>
            <View style={styles.brandMark}>
              <Ionicons color={colors.white} name="construct" size={23} />
            </View>
            <View style={styles.brandCopy}>
              <Text style={styles.brandName}>Про Инструмент</Text>
            </View>
          </View>

          <View style={styles.phoneMain}>
            <View style={styles.heading}>
              <Text style={styles.title}>Войдите, чтобы начать</Text>
              <Text style={styles.subtitle}>
                Введите номер телефона — пришлём одноразовый код в SMS.
              </Text>
            </View>

            {error ? <ErrorNotice message={error} /> : null}

            <View style={styles.form}>
              <View
                style={[
                  styles.phoneField,
                  phoneFocused && styles.phoneFieldFocused,
                  phoneError && styles.phoneFieldError,
                ]}
              >
                <Ionicons color={colors.muted} name="call-outline" size={20} />
                <TextInput
                  accessibilityLabel="Номер телефона"
                  autoComplete="tel"
                  editable={!loading}
                  keyboardType="phone-pad"
                  maxLength={18}
                  onBlur={() => {
                    setPhoneFocused(false);
                    setPhoneError(getPhoneError(phone));
                  }}
                  onChangeText={updatePhone}
                  onFocus={() => setPhoneFocused(true)}
                  placeholder="+7 (999) 000-00-00"
                  placeholderTextColor={colors.muted}
                  returnKeyType="done"
                  selectionColor={colors.primary}
                  style={styles.phoneInput}
                  textContentType="telephoneNumber"
                  value={phone}
                />
              </View>
              {phoneError ? (
                <Text accessibilityLiveRegion="polite" style={styles.phoneError}>
                  {phoneError}
                </Text>
              ) : null}
              <Button
                disabled={Boolean(getPhoneError(phone))}
                label="Продолжить"
                loading={loading}
                onPress={() => void sendCode()}
              />
            </View>

            <Text style={styles.smsHint}>
              Код нужен только для безопасного входа. Мы не будем звонить или
              отправлять рекламу.
            </Text>
          </View>
        </ScrollView>
      </KeyboardAvoidingView>
    </Page>
  );
}

const styles = StyleSheet.create({
  flex: { flex: 1 },
  phoneContent: {
    flexGrow: 1,
    minHeight: '100%',
    paddingHorizontal: spacing.xl,
    paddingTop: spacing.hero,
    paddingBottom: spacing.hero,
    justifyContent: 'space-between',
    gap: spacing.xxl,
  },
  brand: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.md,
  },
  brandMark: {
    width: 46,
    height: 46,
    borderRadius: radius.md,
    alignItems: 'center',
    justifyContent: 'center',
    backgroundColor: colors.primary,
  },
  brandCopy: { flex: 1, gap: spacing.xs },
  brandName: {
    color: colors.ink,
    fontSize: 19,
    lineHeight: 24,
    fontWeight: '800',
    letterSpacing: -0.3,
  },
  phoneMain: {
    gap: spacing.lg,
    paddingBottom: spacing.xxl2,
  },
  heading: { gap: spacing.sm },
  title: {
    maxWidth: 320,
    color: colors.ink,
    fontSize: 34,
    lineHeight: 39,
    fontWeight: '800',
    letterSpacing: -0.9,
  },
  subtitle: {
    maxWidth: 330,
    color: colors.muted,
    fontSize: 16,
    lineHeight: 23,
  },
  form: { gap: spacing.md },
  phoneField: {
    minHeight: controlHeights.large,
    borderWidth: 1,
    borderColor: colors.outline,
    borderRadius: radius.button,
    paddingHorizontal: spacing.lg,
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.md,
    backgroundColor: colors.surface,
  },
  phoneFieldFocused: {
    borderColor: colors.primary,
  },
  phoneFieldError: { borderColor: colors.error },
  phoneInput: {
    flex: 1,
    minHeight: controlHeights.large - 2,
    paddingVertical: 0,
    color: colors.ink,
    fontSize: 17,
    lineHeight: 22,
    fontVariant: ['tabular-nums'],
  },
  phoneError: {
    color: colors.error,
    fontSize: 13,
    lineHeight: 18,
  },
  smsHint: {
    color: colors.muted,
    fontSize: 13,
    lineHeight: 19,
  },
  codeContent: {
    flexGrow: 1,
    paddingHorizontal: spacing.xl,
    paddingTop: spacing.md,
    paddingBottom: spacing.xxl,
    gap: spacing.xxl,
  },
  backButton: {
    width: 48,
    height: 48,
    borderRadius: radius.md,
    borderWidth: 1,
    borderColor: colors.outline,
    alignItems: 'center',
    justifyContent: 'center',
    backgroundColor: colors.surface,
  },
  controlPressed: { opacity: 0.58 },
  codeMain: {
    gap: spacing.xxl,
  },
  codeHeading: { gap: spacing.sm },
  changePhone: {
    alignSelf: 'flex-start',
    minHeight: 32,
    justifyContent: 'center',
  },
  changePhoneText: {
    color: colors.primary,
    fontSize: 15,
    lineHeight: 20,
    fontWeight: '700',
  },
  resend: {
    minHeight: controlHeights.compact,
    justifyContent: 'center',
  },
  timer: {
    color: colors.muted,
    fontSize: 14,
    lineHeight: 20,
    fontVariant: ['tabular-nums'],
  },
});
