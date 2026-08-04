import { useEffect, useRef, useState } from 'react';
import {
  AccessibilityInfo,
  Animated,
  Easing,
  Pressable,
  StyleSheet,
  TextInput,
  useWindowDimensions,
  View,
} from 'react-native';
import { colors, radius, spacing } from '../theme/tokens';
import { normalizeOTP } from '../utils/auth';

const CODE_LENGTH = 6;
const CELL_GAP = spacing.sm;
const MAX_CELL_SIZE = 54;
const MIN_CELL_SIZE = 40;

export type OTPVisualState = 'idle' | 'verifying' | 'success' | 'error';

export function OTPInput({
  value,
  onChange,
  disabled = false,
  state = 'idle',
}: {
  value: string;
  onChange: (value: string) => void;
  disabled?: boolean;
  state?: OTPVisualState;
}) {
  const inputRef = useRef<TextInput>(null);
  const { width } = useWindowDimensions();
  const [successProgress] = useState(() =>
    Array.from({ length: CODE_LENGTH }, () => new Animated.Value(0)),
  );
  const availableWidth = width - spacing.xl * 2 - CELL_GAP * (CODE_LENGTH - 1);
  const cellSize = Math.max(
    MIN_CELL_SIZE,
    Math.min(MAX_CELL_SIZE, Math.floor(availableWidth / CODE_LENGTH)),
  );

  useEffect(() => {
    const timeout = setTimeout(() => inputRef.current?.focus(), 180);
    return () => clearTimeout(timeout);
  }, []);

  useEffect(() => {
    if (state !== 'success') {
      successProgress.forEach((progress) => progress.setValue(0));
      return;
    }

    let active = true;
    let animation: Animated.CompositeAnimation | undefined;
    void AccessibilityInfo.isReduceMotionEnabled().then((reduceMotion) => {
      if (!active) return;
      if (reduceMotion) {
        successProgress.forEach((progress) => progress.setValue(1));
        return;
      }
      animation = Animated.stagger(
        65,
        successProgress.map((progress) =>
          Animated.timing(progress, {
            duration: 240,
            easing: Easing.out(Easing.cubic),
            toValue: 1,
            useNativeDriver: false,
          }),
        ),
      );
      animation.start();
    });

    return () => {
      active = false;
      animation?.stop();
    };
  }, [state, successProgress]);

  const update = (nextValue: string) => {
    onChange(normalizeOTP(nextValue));
  };

  return (
    <Pressable
      accessibilityLabel="Шестизначный код из SMS"
      accessibilityRole="none"
      accessibilityValue={{ text: `Введено ${value.length} из ${CODE_LENGTH} цифр` }}
      onPress={() => inputRef.current?.focus()}
      style={styles.wrapper}
    >
      <View importantForAccessibility="no-hide-descendants" style={styles.cells}>
        {successProgress.map((progress, index) => {
          const active = index === value.length && value.length < CODE_LENGTH;
          const filled = Boolean(value[index]);
          const successBackground = progress.interpolate({
            inputRange: [0, 1],
            outputRange: [colors.primarySoft, colors.success],
          });
          const successBorder = progress.interpolate({
            inputRange: [0, 1],
            outputRange: [colors.primary, colors.success],
          });
          const digitColor = progress.interpolate({
            inputRange: [0, 1],
            outputRange: [colors.ink, colors.white],
          });
          const scale = progress.interpolate({
            inputRange: [0, 0.65, 1],
            outputRange: [1, 1.06, 1],
          });

          return (
            <Animated.View
              key={index}
              style={[
                styles.cell,
                { height: cellSize, width: cellSize },
                active && styles.cellActive,
                filled && styles.cellFilled,
                state === 'verifying' && filled && styles.cellVerifying,
                state === 'error' && styles.cellError,
                state === 'success' && {
                  backgroundColor: successBackground,
                  borderColor: successBorder,
                  transform: [{ scale }],
                },
              ]}
            >
              <Animated.Text
                style={[
                  styles.digit,
                  state === 'error' && styles.digitError,
                  state === 'success' && { color: digitColor },
                ]}
              >
                {value[index] ?? ''}
              </Animated.Text>
            </Animated.View>
          );
        })}
      </View>
      <TextInput
        ref={inputRef}
        accessibilityLabel="Код из SMS"
        autoComplete="sms-otp"
        caretHidden
        contextMenuHidden={false}
        editable={!disabled && state !== 'success'}
        keyboardType="number-pad"
        maxLength={CODE_LENGTH}
        onChangeText={update}
        selectionColor="transparent"
        style={styles.hiddenInput}
        textContentType="oneTimeCode"
        value={value}
      />
    </Pressable>
  );
}

const styles = StyleSheet.create({
  wrapper: {
    alignSelf: 'stretch',
    minHeight: MAX_CELL_SIZE,
    justifyContent: 'center',
  },
  cells: {
    width: '100%',
    flexDirection: 'row',
    justifyContent: 'center',
    gap: CELL_GAP,
  },
  cell: {
    flexGrow: 0,
    flexShrink: 0,
    overflow: 'hidden',
    borderRadius: radius.md,
    borderWidth: 1,
    borderColor: colors.outline,
    alignItems: 'center',
    justifyContent: 'center',
    backgroundColor: colors.surface,
  },
  cellActive: {
    borderColor: colors.primary,
    borderWidth: 2,
    backgroundColor: colors.surface,
  },
  cellFilled: {
    borderColor: colors.outline,
    backgroundColor: colors.surface,
  },
  cellVerifying: {
    borderColor: colors.primary,
    backgroundColor: colors.primarySoft,
    opacity: 0.76,
  },
  cellError: {
    borderColor: colors.error,
    backgroundColor: colors.errorSoft,
  },
  digit: {
    color: colors.ink,
    fontSize: 25,
    lineHeight: 31,
    fontWeight: '800',
    fontVariant: ['tabular-nums'],
    textAlign: 'center',
  },
  digitError: { color: colors.error },
  hiddenInput: {
    position: 'absolute',
    top: 0,
    right: 0,
    bottom: 0,
    left: 0,
    opacity: 0.01,
    color: 'transparent',
  },
});
