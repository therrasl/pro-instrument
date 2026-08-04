import { Ionicons } from '@expo/vector-icons';
import DateTimePicker, {
  type DateTimePickerEvent,
} from '@react-native-community/datetimepicker';
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react';
import {
  AccessibilityInfo,
  Animated,
  Image,
  KeyboardAvoidingView,
  Modal,
  Platform,
  Pressable,
  SafeAreaView,
  ScrollView,
  StyleSheet,
  Text,
  View,
  type GestureResponderEvent,
} from 'react-native';
import { createRental, getRentalQuote } from '../api/rentals';
import type {
  DeliveryMethod,
  Rental,
  RentalInput,
  RentalQuote,
} from '../types/api';
import { formatMoney } from '../utils/format';
import {
  colors,
  controlHeights,
  iconSizes,
  radius,
  spacing,
  typography,
} from '../theme/tokens';
import { Button, ErrorNotice, Field } from './ui';
import { SkeletonBlock, SkeletonGroup } from './skeleton';

type DateField = 'start' | 'end';

interface RentalCheckoutProps {
  toolID: string;
  toolImageURL?: string;
  toolName: string;
  token: string;
  onCreated: (rental: Rental) => void;
}

function startOfToday(): Date {
  const now = new Date();
  return new Date(now.getFullYear(), now.getMonth(), now.getDate());
}

function addDays(value: Date, days: number): Date {
  const result = new Date(value);
  result.setDate(result.getDate() + days);
  return result;
}

function toDateOnly(value: Date): string {
  const year = value.getFullYear();
  const month = String(value.getMonth() + 1).padStart(2, '0');
  const day = String(value.getDate()).padStart(2, '0');
  return `${year}-${month}-${day}`;
}

function displayDate(value: Date): string {
  return new Intl.DateTimeFormat('ru-RU', {
    day: 'numeric',
    month: 'long',
    year: 'numeric',
  }).format(value);
}

function inputKey(input: RentalInput): string {
  return JSON.stringify(input);
}

export function RentalCheckout({
  toolID,
  toolImageURL = '',
  toolName,
  token,
  onCreated,
}: RentalCheckoutProps) {
  const today = useMemo(() => startOfToday(), []);
  const [startDate, setStartDate] = useState(today);
  const [endDate, setEndDate] = useState(() => addDays(today, 1));
  const [deliveryMethod, setDeliveryMethod] =
    useState<DeliveryMethod>('self_pickup');
  const [deliveryAddress, setDeliveryAddress] = useState('');
  const [dateField, setDateField] = useState<DateField | null>(null);
  const [quote, setQuote] = useState<RentalQuote | null>(null);
  const [quoteKey, setQuoteKey] = useState('');
  const [quoteLoadingKey, setQuoteLoadingKey] = useState('');
  const [confirmationVisible, setConfirmationVisible] = useState(false);
  const [submitLoading, setSubmitLoading] = useState(false);
  const [quoteError, setQuoteError] = useState('');
  const [submitError, setSubmitError] = useState('');
  const quoteSequence = useRef(0);
  const submitInFlight = useRef(false);

  const input = useMemo<RentalInput>(
    () => ({
      tool_id: toolID,
      start_date: toDateOnly(startDate),
      end_date: toDateOnly(endDate),
      delivery_method: deliveryMethod,
      delivery_address:
        deliveryMethod === 'courier' ? deliveryAddress.trim() : '',
    }),
    [deliveryAddress, deliveryMethod, endDate, startDate, toolID],
  );
  const currentInputKey = inputKey(input);
  const inputReady =
    input.end_date >= input.start_date &&
    (deliveryMethod === 'self_pickup' || input.delivery_address.length > 0);
  const currentQuote =
    quoteKey === currentInputKey && quoteLoadingKey !== currentInputKey
      ? quote
      : null;

  useEffect(() => {
    const sequence = quoteSequence.current + 1;
    quoteSequence.current = sequence;

    if (!inputReady) {
      return;
    }

    const timeout = setTimeout(() => {
      setQuoteLoadingKey(currentInputKey);
      setQuoteError('');
      void getRentalQuote(token, input)
        .then((result) => {
          if (quoteSequence.current !== sequence) return;
          setQuote(result);
          setQuoteKey(currentInputKey);
        })
        .catch((requestError: unknown) => {
          if (quoteSequence.current !== sequence) return;
          setQuoteError(
            requestError instanceof Error
              ? requestError.message
              : 'Не удалось рассчитать стоимость.',
          );
        })
        .finally(() => {
          if (quoteSequence.current === sequence) setQuoteLoadingKey('');
        });
    }, 450);

    return () => clearTimeout(timeout);
  }, [currentInputKey, input, inputReady, token]);

  const changeDate = (
    field: DateField,
    event: DateTimePickerEvent,
    selected?: Date,
  ) => {
    if (Platform.OS === 'android') setDateField(null);
    if (event.type === 'dismissed' || !selected) return;
    if (field === 'start') {
      setStartDate(selected);
      if (selected > endDate) setEndDate(selected);
    } else {
      setEndDate(selected);
    }
  };

  const openConfirmation = () => {
    if (!currentQuote || !inputReady) return;
    setDateField(null);
    setSubmitError('');
    setConfirmationVisible(true);
  };

  const closeConfirmation = () => {
    if (submitInFlight.current) return;
    setConfirmationVisible(false);
    setSubmitError('');
  };

  const submit = async () => {
    if (
      submitInFlight.current ||
      !currentQuote ||
      !inputReady
    ) {
      return;
    }
    submitInFlight.current = true;
    setSubmitLoading(true);
    setSubmitError('');
    try {
      const rental = await createRental(token, input);
      setConfirmationVisible(false);
      onCreated(rental);
    } catch (requestError) {
      setSubmitError(
        requestError instanceof Error
          ? requestError.message
          : 'Не удалось отправить заявку. Проверьте соединение и попробуйте ещё раз.',
      );
    } finally {
      submitInFlight.current = false;
      setSubmitLoading(false);
    }
  };

  const selectedDate = dateField === 'start' ? startDate : endDate;
  const minimumDate = dateField === 'end' ? startDate : today;

  return (
    <KeyboardAvoidingView behavior={Platform.OS === 'ios' ? 'padding' : undefined}>
      <View style={styles.checkout}>
        <View style={styles.heading}>
          <Text style={styles.title}>Оформление аренды</Text>
          <Text style={styles.intro}>
            Выберите период и способ получения. Стоимость обновится автоматически.
          </Text>
        </View>

        <View style={styles.dateRow}>
          <View style={styles.dateField}>
            <Field
              label="Начало"
              onPress={() => setDateField('start')}
              placeholder="Выберите дату"
              trailingIcon="calendar-outline"
              value={displayDate(startDate)}
            />
          </View>
          <View style={styles.dateField}>
            <Field
              label="Окончание"
              onPress={() => setDateField('end')}
              placeholder="Выберите дату"
              trailingIcon="calendar-outline"
              value={displayDate(endDate)}
            />
          </View>
        </View>

        {dateField ? (
          <View style={styles.picker}>
            <DateTimePicker
              display={Platform.OS === 'ios' ? 'spinner' : 'default'}
              minimumDate={minimumDate}
              mode="date"
              onChange={(event, selected) =>
                changeDate(dateField, event, selected)
              }
              value={selectedDate}
            />
            {Platform.OS === 'ios' ? (
              <Button
                label="Готово"
                onPress={() => setDateField(null)}
                variant="text"
              />
            ) : null}
          </View>
        ) : null}

        <View style={styles.methodGroup}>
          <Text style={styles.label}>Способ получения</Text>
          <View style={styles.segmented}>
            <DeliveryOption
              active={deliveryMethod === 'self_pickup'}
              icon="storefront-outline"
              label="Самовывоз"
              onPress={() => setDeliveryMethod('self_pickup')}
            />
            <DeliveryOption
              active={deliveryMethod === 'courier'}
              icon="car-outline"
              label="Курьер"
              onPress={() => setDeliveryMethod('courier')}
            />
          </View>
        </View>

        {deliveryMethod === 'courier' ? (
          <Field
            autoCapitalize="sentences"
            error={
              deliveryAddress.length > 0 && !deliveryAddress.trim()
                ? 'Введите адрес доставки'
                : undefined
            }
            label="Адрес доставки"
            maxLength={1000}
            onChangeText={setDeliveryAddress}
            placeholder="Город, улица, дом, квартира"
            value={deliveryAddress}
          />
        ) : null}

        <QuoteSummary
          loading={inputReady && quoteLoadingKey === currentInputKey}
          quote={currentQuote}
        />
        {inputReady && quoteError ? <ErrorNotice message={quoteError} /> : null}
        <Button
          disabled={!currentQuote || !inputReady}
          label="Оформить"
          onPress={openConfirmation}
        />
        <Text style={styles.disclaimer}>
          Заявка уйдёт менеджеру только после подтверждения на следующем шаге.
        </Text>
      </View>

      <ConfirmationSheet
        address={input.delivery_address}
        deliveryMethod={deliveryMethod}
        endDate={endDate}
        error={submitError}
        imageURL={toolImageURL}
        loading={submitLoading}
        onClose={closeConfirmation}
        onConfirm={() => void submit()}
        quote={currentQuote}
        startDate={startDate}
        toolName={toolName}
        visible={confirmationVisible}
      />
    </KeyboardAvoidingView>
  );
}

function DeliveryOption({
  active,
  icon,
  label,
  onPress,
}: {
  active: boolean;
  icon: 'storefront-outline' | 'car-outline';
  label: string;
  onPress: () => void;
}) {
  return (
    <Pressable
      accessibilityRole="radio"
      accessibilityState={{ checked: active }}
      onPress={onPress}
      style={({ pressed }) => [
        styles.segment,
        active && styles.segmentActive,
        pressed && styles.segmentPressed,
      ]}
    >
      <Ionicons
        color={active ? colors.primary : colors.muted}
        name={icon}
        size={iconSizes.md}
      />
      <Text style={[styles.segmentLabel, active && styles.segmentLabelActive]}>
        {label}
      </Text>
    </Pressable>
  );
}

function QuoteSummary({
  loading,
  quote,
}: {
  loading: boolean;
  quote: RentalQuote | null;
}) {
  if (loading) {
    return (
      <SkeletonGroup style={styles.quoteSkeleton}>
        {[0, 1, 2].map((item) => (
          <View key={item} style={styles.quoteSkeletonRow}>
            <SkeletonBlock height={15} width={`${52 + item * 7}%`} />
            <SkeletonBlock height={17} width={74} />
          </View>
        ))}
        <View style={styles.totalDivider} />
        <View style={styles.quoteSkeletonRow}>
          <SkeletonBlock height={20} width="28%" />
          <SkeletonBlock height={28} width={102} />
        </View>
      </SkeletonGroup>
    );
  }
  if (!quote) return null;

  return (
    <View accessibilityLiveRegion="polite" style={styles.quote}>
      <CostRow label={`Аренда, ${quote.rental_days} дн.`} value={quote.rental_price} />
      <CostRow label="Возвратный залог" value={quote.deposit_amount} />
      <CostRow label="Доставка" value={quote.delivery_cost} />
      <View style={styles.totalDivider} />
      <CostRow emphasized label="Итого" value={quote.total_amount} />
    </View>
  );
}

function ConfirmationSheet({
  address,
  deliveryMethod,
  endDate,
  error,
  imageURL,
  loading,
  onClose,
  onConfirm,
  quote,
  startDate,
  toolName,
  visible,
}: {
  address: string;
  deliveryMethod: DeliveryMethod;
  endDate: Date;
  error: string;
  imageURL: string;
  loading: boolean;
  onClose: () => void;
  onConfirm: () => void;
  quote: RentalQuote | null;
  startDate: Date;
  toolName: string;
  visible: boolean;
}) {
  const [translateY] = useState(() => new Animated.Value(0));
  const reduceMotion = useRef(false);
  const touchStartY = useRef(0);
  const [imageFailed, setImageFailed] = useState(false);
  const [imageLoading, setImageLoading] = useState(Boolean(imageURL));

  useEffect(() => {
    void AccessibilityInfo.isReduceMotionEnabled().then((value) => {
      reduceMotion.current = value;
    });
  }, []);

  useEffect(() => {
    if (visible) {
      translateY.setValue(0);
    }
  }, [translateY, visible]);

  const dismissWithMotion = useCallback(() => {
    if (loading) return;
    Animated.timing(translateY, {
      duration: reduceMotion.current ? 1 : 180,
      toValue: 520,
      useNativeDriver: true,
    }).start(({ finished }) => {
      if (finished) onClose();
      translateY.setValue(0);
    });
  }, [loading, onClose, translateY]);

  const restoreSheet = () => {
    Animated.spring(translateY, {
      damping: 22,
      mass: 0.8,
      stiffness: 240,
      toValue: 0,
      useNativeDriver: true,
    }).start();
  };

  const handleTouchStart = (event: GestureResponderEvent) => {
    touchStartY.current = event.nativeEvent.pageY;
  };

  const handleTouchMove = (event: GestureResponderEvent) => {
    if (loading) return;
    const distance = event.nativeEvent.pageY - touchStartY.current;
    translateY.setValue(Math.max(0, distance));
  };

  const handleTouchEnd = (event: GestureResponderEvent) => {
    if (loading) return;
    const distance = event.nativeEvent.pageY - touchStartY.current;
    if (distance > 90) {
      dismissWithMotion();
      return;
    }
    restoreSheet();
  };

  if (!quote) return null;

  const courier = deliveryMethod === 'courier';

  return (
    <Modal
      allowSwipeDismissal={!loading}
      animationType="slide"
      onRequestClose={onClose}
      presentationStyle={Platform.OS === 'ios' ? 'pageSheet' : 'overFullScreen'}
      statusBarTranslucent
      transparent={Platform.OS !== 'ios'}
      visible={visible}
    >
      <View
        style={[
          styles.modalBackdrop,
          Platform.OS === 'ios' && styles.modalBackdropIOS,
        ]}
      >
        <Animated.View
          style={[
            styles.sheet,
            Platform.OS === 'ios' && styles.sheetIOS,
            { transform: [{ translateY }] },
          ]}
        >
          <SafeAreaView style={styles.sheetSafeArea}>
            <View
              accessibilityLabel="Потяните вниз, чтобы закрыть"
              onTouchEnd={handleTouchEnd}
              onTouchMove={handleTouchMove}
              onTouchStart={handleTouchStart}
              style={styles.sheetHandleArea}
            >
              <View style={styles.sheetHandle} />
            </View>

            <View style={styles.sheetHeader}>
              <View style={styles.sheetHeading}>
                <Text style={styles.sheetTitle}>Подтвердите заявку</Text>
                <Text style={styles.sheetSubtitle}>
                  Проверьте условия перед отправкой менеджеру
                </Text>
              </View>
              <Pressable
                accessibilityLabel="Закрыть подтверждение"
                accessibilityRole="button"
                disabled={loading}
                hitSlop={6}
                onPress={onClose}
                style={({ pressed }) => [
                  styles.closeButton,
                  pressed && styles.closeButtonPressed,
                  loading && styles.closeButtonDisabled,
                ]}
              >
                <Ionicons color={colors.ink} name="close" size={iconSizes.lg} />
              </Pressable>
            </View>

            <ScrollView
              contentContainerStyle={styles.sheetContent}
              keyboardShouldPersistTaps="handled"
              showsVerticalScrollIndicator={false}
            >
              <View style={styles.toolPreview}>
                {imageURL && !imageFailed ? (
                  <View style={styles.toolImage}>
                    <Image
                      accessibilityLabel={`${toolName}, фото`}
                      onError={() => {
                        setImageFailed(true);
                        setImageLoading(false);
                      }}
                      onLoadEnd={() => setImageLoading(false)}
                      resizeMode="cover"
                      source={{ uri: imageURL }}
                      style={styles.toolImageContent}
                    />
                    {imageLoading ? (
                      <SkeletonGroup style={styles.toolImageSkeleton}>
                        <SkeletonBlock height="100%" radiusValue={0} />
                      </SkeletonGroup>
                    ) : null}
                  </View>
                ) : (
                  <View style={styles.toolImageFallback}>
                    <Ionicons
                      color={colors.primary}
                      name="construct-outline"
                      size={iconSizes.xl}
                    />
                  </View>
                )}
                <View style={styles.toolCopy}>
                  <Text numberOfLines={3} style={styles.toolName}>
                    {toolName}
                  </Text>
                  <Text style={styles.toolDates}>
                    {displayDate(startDate)} — {displayDate(endDate)}
                  </Text>
                  <Text style={styles.toolDays}>
                    {quote.rental_days} {dayWord(quote.rental_days)}
                  </Text>
                </View>
              </View>

              <View style={styles.confirmationDetails}>
                <ConfirmationRow
                  icon={courier ? 'car-outline' : 'storefront-outline'}
                  label="Получение"
                  value={courier ? 'Курьерская доставка' : 'Самовывоз'}
                />
                {courier ? (
                  <ConfirmationRow
                    icon="location-outline"
                    label="Адрес"
                    value={address}
                  />
                ) : null}
              </View>

              <View style={styles.confirmationCosts}>
                <CostRow label="Аренда" value={quote.rental_price} />
                <CostRow label="Залог" value={quote.deposit_amount} />
                <CostRow label="Доставка" value={quote.delivery_cost} />
                <View style={styles.totalDivider} />
                <CostRow emphasized label="Итого" value={quote.total_amount} />
              </View>

              <View style={styles.managerNotice}>
                <Ionicons
                  color={colors.primary}
                  name="shield-checkmark-outline"
                  size={iconSizes.lg}
                />
                <Text style={styles.managerNoticeText}>
                  Сначала заявку проверит менеджер. Оплата станет доступна после
                  подтверждения.
                </Text>
              </View>

              {error ? <ErrorNotice message={error} /> : null}
            </ScrollView>

            <View style={styles.sheetActions}>
              <View style={styles.secondaryAction}>
                <Button
                  disabled={loading}
                  label="Изменить"
                  onPress={onClose}
                  variant="secondary"
                />
              </View>
              <View style={styles.primaryAction}>
                <Button
                  label="Подтвердить заявку"
                  loading={loading}
                  onPress={onConfirm}
                />
              </View>
            </View>
          </SafeAreaView>
        </Animated.View>
      </View>
    </Modal>
  );
}

function ConfirmationRow({
  icon,
  label,
  value,
}: {
  icon: 'car-outline' | 'storefront-outline' | 'location-outline';
  label: string;
  value: string;
}) {
  return (
    <View style={styles.confirmationRow}>
      <View style={styles.confirmationIcon}>
        <Ionicons color={colors.primary} name={icon} size={iconSizes.md} />
      </View>
      <View style={styles.confirmationRowCopy}>
        <Text style={styles.confirmationLabel}>{label}</Text>
        <Text style={styles.confirmationValue}>{value}</Text>
      </View>
    </View>
  );
}

function CostRow({
  emphasized = false,
  label,
  value,
}: {
  emphasized?: boolean;
  label: string;
  value: number;
}) {
  return (
    <View style={[styles.costRow, emphasized && styles.totalRow]}>
      <Text style={[styles.costLabel, emphasized && styles.totalLabel]}>
        {label}
      </Text>
      <Text style={[styles.costValue, emphasized && styles.totalValue]}>
        {formatMoney(value)}
      </Text>
    </View>
  );
}

function dayWord(days: number): string {
  const mod100 = days % 100;
  const mod10 = days % 10;
  if (mod100 >= 11 && mod100 <= 14) return 'дней';
  if (mod10 === 1) return 'день';
  if (mod10 >= 2 && mod10 <= 4) return 'дня';
  return 'дней';
}

const styles = StyleSheet.create({
  checkout: {
    gap: spacing.lg,
    borderRadius: radius.lg,
    padding: spacing.lg,
    backgroundColor: colors.surfaceSubtle,
  },
  heading: { gap: spacing.sm },
  title: { color: colors.ink, ...typography.section },
  intro: { color: colors.muted, ...typography.body },
  dateRow: {
    gap: spacing.lg,
  },
  dateField: { minWidth: 0 },
  picker: {
    borderRadius: radius.md,
    paddingHorizontal: spacing.sm,
    backgroundColor: colors.surfaceSubtle,
  },
  methodGroup: { gap: spacing.sm },
  label: { color: colors.ink, ...typography.label },
  segmented: {
    minHeight: controlHeights.large,
    borderRadius: radius.button,
    padding: spacing.xs,
    flexDirection: 'row',
    gap: spacing.xs,
    backgroundColor: colors.surfaceStrong,
  },
  segment: {
    flex: 1,
    minHeight: 48,
    borderRadius: radius.sm,
    alignItems: 'center',
    justifyContent: 'center',
    flexDirection: 'row',
    gap: spacing.sm,
  },
  segmentActive: { backgroundColor: colors.surface },
  segmentPressed: { opacity: 0.72, transform: [{ scale: 0.98 }] },
  segmentLabel: {
    color: colors.muted,
    fontSize: 14,
    lineHeight: 19,
    fontWeight: '700',
  },
  segmentLabelActive: { color: colors.primary },
  quote: {
    gap: spacing.md,
    paddingTop: spacing.xs,
  },
  quoteSkeleton: { gap: spacing.md, paddingTop: spacing.xs },
  quoteSkeletonRow: {
    minHeight: 26,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: spacing.md,
  },
  costRow: {
    minHeight: 26,
    flexDirection: 'row',
    alignItems: 'baseline',
    justifyContent: 'space-between',
    gap: spacing.md,
  },
  costLabel: { flex: 1, color: colors.muted, ...typography.body },
  costValue: {
    color: colors.ink,
    ...typography.label,
    fontVariant: ['tabular-nums'],
  },
  totalDivider: {
    height: StyleSheet.hairlineWidth,
    marginVertical: spacing.xs,
    backgroundColor: colors.outline,
  },
  totalRow: { minHeight: 34, alignItems: 'center' },
  totalLabel: {
    color: colors.ink,
    fontSize: 17,
    lineHeight: 22,
    fontWeight: '800',
  },
  totalValue: {
    color: colors.primary,
    fontSize: 24,
    lineHeight: 30,
    fontWeight: '800',
  },
  disclaimer: {
    color: colors.muted,
    fontSize: 12,
    lineHeight: 17,
    textAlign: 'center',
  },
  modalBackdrop: {
    flex: 1,
    justifyContent: 'flex-end',
    backgroundColor: colors.scrim,
  },
  modalBackdropIOS: {
    backgroundColor: colors.background,
  },
  sheet: {
    height: '92%',
    maxHeight: '94%',
    overflow: 'hidden',
    borderTopLeftRadius: radius.lg,
    borderTopRightRadius: radius.lg,
    backgroundColor: colors.background,
  },
  sheetIOS: {
    flex: 1,
    height: '100%',
    maxHeight: '100%',
    borderTopLeftRadius: 0,
    borderTopRightRadius: 0,
  },
  sheetSafeArea: { flex: 1 },
  sheetHandleArea: {
    minHeight: 28,
    alignItems: 'center',
    justifyContent: 'center',
  },
  sheetHandle: {
    width: 38,
    height: 5,
    borderRadius: radius.pill,
    backgroundColor: colors.outline,
  },
  sheetHeader: {
    paddingHorizontal: spacing.xl,
    paddingBottom: spacing.lg,
    flexDirection: 'row',
    alignItems: 'flex-start',
    gap: spacing.lg,
  },
  sheetHeading: { flex: 1, gap: spacing.xs },
  sheetTitle: {
    color: colors.ink,
    fontSize: 24,
    lineHeight: 30,
    fontWeight: '800',
    letterSpacing: -0.35,
  },
  sheetSubtitle: { color: colors.muted, ...typography.caption },
  closeButton: {
    width: 48,
    height: 48,
    borderRadius: radius.pill,
    alignItems: 'center',
    justifyContent: 'center',
    backgroundColor: colors.surfaceStrong,
  },
  closeButtonPressed: { opacity: 0.72, transform: [{ scale: 0.96 }] },
  closeButtonDisabled: { opacity: 0.44 },
  sheetContent: {
    paddingHorizontal: spacing.xl,
    paddingBottom: spacing.xl,
    gap: spacing.xl,
  },
  toolPreview: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.lg,
  },
  toolImage: {
    width: 88,
    height: 88,
    overflow: 'hidden',
    borderRadius: radius.md,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: colors.imageOutline,
    backgroundColor: colors.surfaceSubtle,
  },
  toolImageContent: { width: '100%', height: '100%' },
  toolImageSkeleton: {
    position: 'absolute',
    top: 0,
    right: 0,
    bottom: 0,
    left: 0,
  },
  toolImageFallback: {
    width: 88,
    height: 88,
    borderRadius: radius.md,
    alignItems: 'center',
    justifyContent: 'center',
    backgroundColor: colors.primarySoft,
  },
  toolCopy: { flex: 1, minWidth: 0, gap: spacing.xs },
  toolName: {
    color: colors.ink,
    fontSize: 17,
    lineHeight: 22,
    fontWeight: '800',
  },
  toolDates: { color: colors.muted, ...typography.caption },
  toolDays: { color: colors.primary, ...typography.label },
  confirmationDetails: {
    overflow: 'hidden',
    borderRadius: radius.md,
    backgroundColor: colors.surfaceSubtle,
  },
  confirmationRow: {
    minHeight: 64,
    paddingHorizontal: spacing.lg,
    paddingVertical: spacing.md,
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.md,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderBottomColor: colors.outline,
  },
  confirmationIcon: {
    width: 36,
    height: 36,
    borderRadius: radius.sm,
    alignItems: 'center',
    justifyContent: 'center',
    backgroundColor: colors.primarySoft,
  },
  confirmationRowCopy: { flex: 1, minWidth: 0, gap: spacing.xs },
  confirmationLabel: { color: colors.muted, ...typography.caption },
  confirmationValue: { color: colors.ink, ...typography.body, fontWeight: '600' },
  confirmationCosts: {
    gap: spacing.md,
  },
  managerNotice: {
    borderRadius: radius.md,
    padding: spacing.lg,
    flexDirection: 'row',
    alignItems: 'flex-start',
    gap: spacing.md,
    backgroundColor: colors.primarySoft,
  },
  managerNoticeText: { flex: 1, color: colors.primary, ...typography.body },
  sheetActions: {
    paddingHorizontal: spacing.xl,
    paddingTop: spacing.md,
    paddingBottom: spacing.sm,
    gap: spacing.md,
    backgroundColor: colors.surface,
  },
  secondaryAction: {},
  primaryAction: {},
});
