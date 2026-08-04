import { Ionicons } from '@expo/vector-icons';
import {
  useFocusEffect,
  useLocalSearchParams,
  useRouter,
  type Href,
} from 'expo-router';
import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from 'react';
import {
  AppState,
  Image,
  Linking,
  Modal,
  Pressable,
  RefreshControl,
  ScrollView,
  StyleSheet,
  Text,
  View,
} from 'react-native';
import { getTool } from '../../../src/api/catalog';
import { resolveAPIAssetURL } from '../../../src/api/client';
import {
  cancelRental,
  createRentalPayment,
  getRental,
} from '../../../src/api/rentals';
import { useSession } from '../../../src/auth/session';
import {
  RentalDetailSkeleton,
  SkeletonBlock,
  SkeletonGroup,
} from '../../../src/components/skeleton';
import {
  Button,
  ErrorNotice,
  Page,
  StateView,
} from '../../../src/components/ui';
import {
  clearPendingPaymentRental,
  rememberPendingPaymentRental,
} from '../../../src/rentals/payment-return';
import {
  formatPaymentCountdown,
  getRemainingPaymentSeconds,
} from '../../../src/rentals/payment-deadline';
import { isSafeConfirmationURL } from '../../../src/rentals/payment-url';
import {
  buildRentalTimeline,
  pollingRentalStatuses,
  rentalStatusPresentation,
} from '../../../src/rentals/status';
import {
  colors,
  iconSizes,
  radius,
  spacing,
  typography,
} from '../../../src/theme/tokens';
import type { Rental, Tool } from '../../../src/types/api';
import { formatMoney } from '../../../src/utils/format';

const STATUS_POLL_INTERVAL_MS = 12_000;
const MAX_STATUS_POLL_ATTEMPTS = 25;
const PAYMENT_POLL_INTERVAL_MS = 2_000;
const PAYMENT_POLL_DURATION_MS = 60_000;
const TIMELINE_DOT_Z_INDEX = 1;

function formatDate(value: string): string {
  const [year, month, day] = value.split('-').map(Number);
  return new Intl.DateTimeFormat('ru-RU', {
    day: 'numeric',
    month: 'long',
    year: 'numeric',
  }).format(new Date(year, month - 1, day));
}

export default function RentalDetailScreen() {
  const { id, paymentStartedAt } = useLocalSearchParams<{
    id: string;
    paymentStartedAt?: string;
  }>();
  const router = useRouter();
  const { token } = useSession();
  const [rental, setRental] = useState<Rental | null>(null);
  const [tool, setTool] = useState<Tool | null>(null);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [paying, setPaying] = useState(false);
  const [paymentStarted, setPaymentStarted] = useState(false);
  const [paymentUnavailable, setPaymentUnavailable] = useState(false);
  const [remainingPaymentSeconds, setRemainingPaymentSeconds] = useState<
    number | null
  >(null);
  const [cancelVisible, setCancelVisible] = useState(false);
  const [cancelling, setCancelling] = useState(false);
  const [supportVisible, setSupportVisible] = useState(false);
  const [focused, setFocused] = useState(false);
  const [error, setError] = useState('');
  const paymentInFlight = useRef(false);
  const latestRequest = useRef(0);
  const statusPollAttempts = useRef(0);
  const paymentPollStartedAt = useRef<number | null>(null);
  const loadedToolID = useRef('');
  const hasLoaded = useRef(false);
  const deadlineRefreshRequested = useRef(false);

  const load = useCallback(
    async (mode: 'initial' | 'refresh' | 'silent' = 'initial') => {
      if (!id || !token) return;
      const requestID = ++latestRequest.current;
      if (mode === 'initial') setLoading(true);
      if (mode === 'refresh') setRefreshing(true);
      if (mode !== 'silent') setError('');

      try {
        const nextRental = await getRental(token, id);
        if (requestID !== latestRequest.current) return;
        setRental(nextRental);
        if (nextRental.status !== 'awaiting_payment') {
          setPaymentStarted(false);
          paymentPollStartedAt.current = null;
          void clearPendingPaymentRental();
        }
        setPaymentUnavailable((current) => {
          if (nextRental.status !== 'awaiting_payment') return false;
          if (mode !== 'silent') return !nextRental.payment_available;
          return current || !nextRental.payment_available;
        });
        hasLoaded.current = true;

        if (loadedToolID.current !== nextRental.tool_id) {
          try {
            const nextTool = await getTool(nextRental.tool_id);
            if (requestID !== latestRequest.current) return;
            loadedToolID.current = nextTool.id;
            setTool(nextTool);
          } catch {
            // Статус и условия заказа остаются доступны без каталожного медиа.
          }
        }
      } catch (requestError) {
        if (requestID === latestRequest.current && mode !== 'silent') {
          setError(
            requestError instanceof Error
              ? requestError.message
              : 'Не удалось загрузить заказ.',
          );
        }
      } finally {
        if (requestID === latestRequest.current) {
          setLoading(false);
          setRefreshing(false);
        }
      }
    },
    [id, token],
  );

  useFocusEffect(
    useCallback(() => {
      setFocused(true);
      statusPollAttempts.current = 0;

      const now = Date.now();
      const parsedPaymentStartedAt = Number(paymentStartedAt);
      if (
        Number.isFinite(parsedPaymentStartedAt) &&
        parsedPaymentStartedAt > 0 &&
        parsedPaymentStartedAt <= now &&
        now - parsedPaymentStartedAt < PAYMENT_POLL_DURATION_MS
      ) {
        paymentPollStartedAt.current = parsedPaymentStartedAt;
        setPaymentStarted(true);
      }

      void load(hasLoaded.current ? 'silent' : 'initial');

      const subscription = AppState.addEventListener('change', (state) => {
        if (state !== 'active') return;
        statusPollAttempts.current = 0;
        void load('silent');
      });

      return () => {
        setFocused(false);
        subscription.remove();
      };
    }, [load, paymentStartedAt]),
  );

  useEffect(() => {
    if (
      !focused ||
      !rental ||
      paymentStarted ||
      rental.status === 'awaiting_payment' ||
      rental.status === 'paid' ||
      !pollingRentalStatuses.has(rental.status)
    ) {
      return;
    }

    const interval = setInterval(() => {
      if (
        AppState.currentState !== 'active' ||
        statusPollAttempts.current >= MAX_STATUS_POLL_ATTEMPTS
      ) {
        return;
      }
      statusPollAttempts.current += 1;
      void load('silent');
    }, STATUS_POLL_INTERVAL_MS);

    return () => clearInterval(interval);
  }, [focused, load, paymentStarted, rental]);

  useEffect(() => {
    if (
      !focused ||
      !paymentStarted ||
      !rental ||
      rental.status !== 'awaiting_payment'
    ) {
      return;
    }

    const startedAt = paymentPollStartedAt.current ?? Date.now();
    paymentPollStartedAt.current = startedAt;
    const remainingDuration = PAYMENT_POLL_DURATION_MS - (Date.now() - startedAt);

    if (remainingDuration <= 0) {
      setPaymentStarted(false);
      paymentPollStartedAt.current = null;
      void clearPendingPaymentRental();
      return;
    }

    const interval = setInterval(() => {
      if (AppState.currentState === 'active') void load('silent');
    }, PAYMENT_POLL_INTERVAL_MS);
    const timeout = setTimeout(() => {
      clearInterval(interval);
      setPaymentStarted(false);
      paymentPollStartedAt.current = null;
      void clearPendingPaymentRental();
    }, remainingDuration);

    return () => {
      clearInterval(interval);
      clearTimeout(timeout);
    };
  }, [focused, load, paymentStarted, rental]);

  useEffect(() => {
    if (!rental || rental.status !== 'awaiting_payment') {
      deadlineRefreshRequested.current = false;
      return;
    }

    const updateCountdown = () => {
      const seconds = getRemainingPaymentSeconds(rental);
      setRemainingPaymentSeconds(seconds);
      if (seconds === 0 && !deadlineRefreshRequested.current) {
        deadlineRefreshRequested.current = true;
        void load('silent');
      }
    };

    const initialUpdate = setTimeout(updateCountdown, 0);
    const interval = setInterval(updateCountdown, 1_000);
    const subscription = AppState.addEventListener('change', (state) => {
      if (state === 'active') updateCountdown();
    });

    return () => {
      clearTimeout(initialUpdate);
      clearInterval(interval);
      subscription.remove();
    };
  }, [load, rental]);

  const refresh = async () => {
    statusPollAttempts.current = 0;
    await load('refresh');
  };

  const pay = async () => {
    if (
      !token ||
      !rental ||
      rental.status !== 'awaiting_payment' ||
      !rental.payment_available ||
      paymentUnavailable ||
      remainingPaymentSeconds === 0 ||
      paymentInFlight.current
    ) {
      return;
    }

    paymentInFlight.current = true;
    setPaying(true);
    setError('');
    try {
      const payment = await createRentalPayment(token, rental.id);
      const confirmationURL = payment.confirmation_url?.trim() ?? '';
      if (!isSafeConfirmationURL(confirmationURL)) {
        throw new Error('Платёжный сервис не вернул безопасную ссылку для оплаты.');
      }
      const startedAt = await rememberPendingPaymentRental(rental.id);
      paymentPollStartedAt.current = startedAt;
      statusPollAttempts.current = 0;
      setPaymentStarted(true);
      await Linking.openURL(confirmationURL);
    } catch (requestError) {
      const message =
        requestError instanceof Error
          ? requestError.message
          : 'Не удалось перейти к оплате.';
      if (message.toLocaleLowerCase('ru-RU').includes('недоступ')) {
        setPaymentUnavailable(true);
      } else {
        setError(message);
      }
    } finally {
      paymentInFlight.current = false;
      setPaying(false);
    }
  };

  const cancel = async () => {
    if (
      !token ||
      !rental ||
      (rental.status !== 'pending_manager' &&
        rental.status !== 'awaiting_payment') ||
      cancelling
    ) {
      return;
    }

    setCancelling(true);
    setError('');
    try {
      const nextRental = await cancelRental(token, rental.id);
      setRental(nextRental);
      setPaymentStarted(false);
      setPaymentUnavailable(false);
      setCancelVisible(false);
      statusPollAttempts.current = 0;
    } catch (requestError) {
      setCancelVisible(false);
      setError(
        requestError instanceof Error
          ? requestError.message
          : 'Не удалось отменить заказ.',
      );
      await load('silent');
    } finally {
      setCancelling(false);
    }
  };

  if (loading) {
    return (
      <Page>
        <RentalDetailSkeleton />
      </Page>
    );
  }

  if (!rental) {
    return (
      <Page>
        <StateView
          action={<Button label="Повторить" onPress={() => void load('initial')} />}
          icon="cloud-offline-outline"
          message={error || 'Заказ не найден или недоступен.'}
          title="Не удалось открыть заказ"
        />
      </Page>
    );
  }

  const presentation = rentalStatusPresentation[rental.status];
  const courier = rental.delivery_method === 'courier';
  const timeline = buildRentalTimeline(rental.status, rental.delivery_method);
  const imageURL = resolveAPIAssetURL(tool?.image_urls?.[0] ?? '');
  const cancellable =
    rental.status === 'pending_manager' ||
    rental.status === 'awaiting_payment';
  const paymentDeadlinePassed =
    rental.status === 'awaiting_payment' && remainingPaymentSeconds === 0;
  const showTransactionArea =
    (paymentStarted && rental.status === 'awaiting_payment') ||
    rental.status === 'awaiting_payment' ||
    Boolean(error) ||
    cancellable;

  return (
    <Page>
      <ScrollView
        contentContainerStyle={styles.content}
        refreshControl={
          <RefreshControl
            colors={[colors.primary]}
            onRefresh={() => void refresh()}
            refreshing={refreshing}
            tintColor={colors.primary}
          />
        }
        showsVerticalScrollIndicator={false}
      >
        <View style={styles.orderHeader}>
          <Text style={styles.orderNumber}>
            Заказ №{rental.id.slice(0, 8).toUpperCase()}
          </Text>
          <Text
            accessibilityRole="header"
            style={[
              styles.statusTitle,
              presentation.tone === 'error' && styles.statusTitleError,
            ]}
          >
            {presentation.label}
          </Text>
          <View style={styles.nextAction}>
            <Ionicons
              color={colors.primary}
              name="information-circle-outline"
              size={iconSizes.lg}
            />
            <Text style={styles.nextActionText}>{presentation.nextAction}</Text>
          </View>
        </View>

        <Section title="Статус заказа">
          <View style={styles.timeline}>
            {timeline.map((stage, index) => (
              <TimelineItem
                first={index === 0}
                key={stage.key}
                label={stage.label}
                last={index === timeline.length - 1}
                state={stage.state}
              />
            ))}
          </View>
        </Section>

        <Section title="Инструмент">
          <ToolSummary imageURL={imageURL} name={tool?.name ?? 'Инструмент'} />
        </Section>

        <Section title="Аренда и получение">
          <View style={styles.sectionSurface}>
            <DetailRow
              icon="calendar-outline"
              label="Даты"
              value={`${formatDate(rental.start_date)} — ${formatDate(rental.end_date)}`}
            />
            <DetailRow
              icon={courier ? 'car-outline' : 'storefront-outline'}
              label="Способ получения"
              value={courier ? 'Курьерская доставка' : 'Самовывоз'}
            />
            {courier && rental.delivery_address ? (
              <DetailRow
                icon="location-outline"
                label="Адрес"
                value={rental.delivery_address}
              />
            ) : null}
          </View>
        </Section>

        <Section title="Стоимость">
          <View style={styles.costSurface}>
            <MoneyRow
              label={`Аренда, ${rental.rental_days} дн.`}
              value={rental.rental_price}
            />
            <MoneyRow label="Возвратный залог" value={rental.deposit_amount} />
            <MoneyRow label="Доставка" value={rental.delivery_cost} />
            <View style={styles.divider} />
            <MoneyRow emphasized label="Итого" value={rental.total_amount} />
          </View>
        </Section>

        <View style={styles.actionArea}>
          <View style={styles.utilityGroup}>
            <View style={styles.quickActions}>
              <CompactAction
                icon="document-text-outline"
                label="Документы"
                onPress={() => router.push('/(app)/documents' as Href)}
              />
              <CompactAction
                icon="chatbubble-ellipses-outline"
                label="Поддержка"
                onPress={() => setSupportVisible((value) => !value)}
              />
            </View>

            {supportVisible ? (
              <InlineNotice
                icon="chatbubble-ellipses-outline"
                message={`Сообщите менеджеру номер заказа ${rental.id
                  .slice(0, 8)
                  .toUpperCase()}, чтобы быстрее найти заявку.`}
              />
            ) : null}
          </View>

          {showTransactionArea ? (
            <View style={styles.transactionGroup}>
              {paymentStarted && rental.status === 'awaiting_payment' ? (
                <InlineNotice
                  icon="sync-outline"
                  message="Проверяем оплату…"
                />
              ) : null}

              {rental.status === 'awaiting_payment' &&
              remainingPaymentSeconds !== null &&
              !paymentDeadlinePassed &&
              !paymentStarted ? (
                <InlineNotice
                  icon="time-outline"
                  message={`Оплатите в течение ${formatPaymentCountdown(
                    remainingPaymentSeconds,
                  )}`}
                  tone="error"
                />
              ) : null}

              {paymentDeadlinePassed && !paymentStarted ? (
                <InlineNotice
                  icon="time-outline"
                  message="Время на оплату истекло"
                  tone="error"
                />
              ) : null}

              {rental.status === 'awaiting_payment' && paymentUnavailable ? (
                <InlineNotice
                  icon="card-outline"
                  message="Оплата временно недоступна. Попробуйте позже"
                />
              ) : null}

              {error ? <ErrorNotice message={error} /> : null}

              <View style={styles.orderActions}>
                {rental.status === 'awaiting_payment' &&
                rental.payment_available &&
                !paymentDeadlinePassed &&
                !paymentStarted &&
                !paymentUnavailable ? (
                  <Button
                    icon="card-outline"
                    label="Оплатить заказ"
                    loading={paying}
                    onPress={() => void pay()}
                  />
                ) : null}

                {cancellable ? (
                  <Button
                    disabled={cancelling}
                    label="Отменить заказ"
                    onPress={() => setCancelVisible(true)}
                    variant="dangerText"
                  />
                ) : null}
              </View>
            </View>
          ) : null}
        </View>
      </ScrollView>
      <Modal
        animationType="fade"
        onRequestClose={() => !cancelling && setCancelVisible(false)}
        transparent
        visible={cancelVisible}
      >
        <View style={styles.modalBackdrop}>
          <View accessibilityViewIsModal style={styles.modal}>
            <Text accessibilityRole="header" style={styles.modalTitle}>
              Отменить заказ?
            </Text>
            <View style={styles.modalActions}>
              <Button
                disabled={cancelling}
                label="Оставить"
                onPress={() => setCancelVisible(false)}
                variant="secondary"
              />
              <Button
                label="Отменить заказ"
                loading={cancelling}
                onPress={() => void cancel()}
                variant="danger"
              />
            </View>
          </View>
        </View>
      </Modal>
    </Page>
  );
}

function Section({
  children,
  title,
}: {
  children: ReactNode;
  title: string;
}) {
  return (
    <View style={styles.section}>
      <Text style={styles.sectionTitle}>{title}</Text>
      {children}
    </View>
  );
}

function TimelineItem({
  first,
  label,
  last,
  state,
}: {
  first: boolean;
  label: string;
  last: boolean;
  state: 'completed' | 'current' | 'upcoming';
}) {
  return (
    <View
      accessibilityLabel={`${label}: ${
        state === 'completed'
          ? 'этап пройден'
          : state === 'current'
            ? 'текущий этап'
            : 'предстоящий этап'
      }`}
      style={styles.timelineItem}
    >
      <View style={styles.timelineRail}>
        {!first ? (
          <View
            style={[
              styles.timelineLine,
              styles.timelineLineTop,
              state !== 'upcoming' && styles.timelineLineCompleted,
            ]}
          />
        ) : null}
        <View
          style={[
            styles.timelineDot,
            state === 'completed' && styles.timelineDotCompleted,
            state === 'current' && styles.timelineDotCurrent,
          ]}
        >
          {state === 'completed' ? (
            <Ionicons color={colors.white} name="checkmark" size={14} />
          ) : state === 'current' ? (
            <View style={styles.timelineDotCore} />
          ) : null}
        </View>
        {!last ? (
          <View
            style={[
              styles.timelineLine,
              styles.timelineLineBottom,
              state === 'completed' && styles.timelineLineCompleted,
            ]}
          />
        ) : null}
      </View>
      <Text
        style={[
          styles.timelineLabel,
          state === 'current' && styles.timelineLabelCurrent,
          state === 'upcoming' && styles.timelineLabelUpcoming,
        ]}
      >
        {label}
      </Text>
    </View>
  );
}

function ToolSummary({ imageURL, name }: { imageURL: string; name: string }) {
  const [failed, setFailed] = useState(false);
  const [imageLoading, setImageLoading] = useState(Boolean(imageURL));

  return (
    <View style={styles.toolSummary}>
      {imageURL && !failed ? (
        <View style={styles.toolImage}>
          <Image
            accessibilityLabel={`${name}, фото`}
            onError={() => {
              setFailed(true);
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
      <Text numberOfLines={3} style={styles.toolName}>
        {name}
      </Text>
    </View>
  );
}

function DetailRow({
  icon,
  label,
  value,
}: {
  icon: 'calendar-outline' | 'car-outline' | 'storefront-outline' | 'location-outline';
  label: string;
  value: string;
}) {
  return (
    <View style={styles.detailRow}>
      <View style={styles.detailIcon}>
        <Ionicons color={colors.primary} name={icon} size={iconSizes.md} />
      </View>
      <View style={styles.detailCopy}>
        <Text style={styles.rowLabel}>{label}</Text>
        <Text style={styles.rowValue}>{value}</Text>
      </View>
    </View>
  );
}

function MoneyRow({
  emphasized = false,
  label,
  value,
}: {
  emphasized?: boolean;
  label: string;
  value: number;
}) {
  return (
    <View style={[styles.moneyRow, emphasized && styles.totalRow]}>
      <Text style={[styles.moneyLabel, emphasized && styles.totalLabel]}>{label}</Text>
      <Text style={[styles.moneyValue, emphasized && styles.totalValue]}>
        {formatMoney(value)}
      </Text>
    </View>
  );
}

function CompactAction({
  icon,
  label,
  onPress,
}: {
  icon: 'document-text-outline' | 'chatbubble-ellipses-outline';
  label: string;
  onPress: () => void;
}) {
  return (
    <Pressable
      accessibilityRole="button"
      onPress={onPress}
      style={({ pressed }) => [
        styles.compactAction,
        pressed && styles.compactActionPressed,
      ]}
    >
      <Ionicons color={colors.primary} name={icon} size={iconSizes.lg} />
      <Text style={styles.compactActionLabel}>{label}</Text>
      <Ionicons color={colors.muted} name="chevron-forward" size={iconSizes.md} />
    </Pressable>
  );
}

function InlineNotice({
  icon,
  message,
  tone = 'default',
}: {
  icon:
    | 'card-outline'
    | 'chatbubble-ellipses-outline'
    | 'sync-outline'
    | 'time-outline';
  message: string;
  tone?: 'default' | 'error';
}) {
  return (
    <View
      accessibilityLiveRegion="polite"
      style={[
        styles.inlineNotice,
        tone === 'error' && styles.inlineNoticeError,
      ]}
    >
      <Ionicons
        color={tone === 'error' ? colors.error : colors.primary}
        name={icon}
        size={iconSizes.lg}
      />
      <Text
        style={[
          styles.inlineNoticeText,
          tone === 'error' && styles.inlineNoticeTextError,
        ]}
      >
        {message}
      </Text>
    </View>
  );
}

const styles = StyleSheet.create({
  content: {
    paddingHorizontal: spacing.xl,
    paddingTop: spacing.md,
    paddingBottom: spacing.hero,
    gap: spacing.xl,
  },
  orderHeader: { gap: spacing.md },
  orderNumber: {
    color: colors.muted,
    ...typography.label,
    fontVariant: ['tabular-nums'],
  },
  statusTitle: {
    maxWidth: 340,
    color: colors.ink,
    fontSize: 30,
    lineHeight: 36,
    fontWeight: '800',
    letterSpacing: -0.6,
  },
  statusTitleError: { color: colors.error },
  nextAction: {
    flexDirection: 'row',
    alignItems: 'flex-start',
    gap: spacing.md,
  },
  nextActionText: { flex: 1, color: colors.ink, ...typography.body },
  section: { gap: spacing.sm },
  sectionTitle: { color: colors.ink, ...typography.section },
  timeline: {
    borderRadius: radius.lg,
    paddingHorizontal: spacing.lg,
    paddingVertical: spacing.sm,
    backgroundColor: colors.surfaceSubtle,
  },
  timelineItem: {
    minHeight: 52,
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.md,
  },
  timelineRail: {
    alignSelf: 'stretch',
    width: 28,
    alignItems: 'center',
    justifyContent: 'center',
  },
  timelineLine: {
    position: 'absolute',
    width: 2,
    backgroundColor: colors.outline,
  },
  timelineLineTop: { top: 0, bottom: '50%' },
  timelineLineBottom: { top: '50%', bottom: 0 },
  timelineLineCompleted: { backgroundColor: colors.success },
  timelineDot: {
    zIndex: TIMELINE_DOT_Z_INDEX,
    width: 20,
    height: 20,
    borderRadius: radius.pill,
    borderWidth: 2,
    borderColor: colors.outline,
    alignItems: 'center',
    justifyContent: 'center',
    backgroundColor: colors.surface,
  },
  timelineDotCompleted: {
    borderColor: colors.success,
    backgroundColor: colors.success,
  },
  timelineDotCurrent: {
    width: 24,
    height: 24,
    borderColor: colors.primary,
    backgroundColor: colors.surfaceSubtle,
  },
  timelineDotCore: {
    width: 8,
    height: 8,
    borderRadius: radius.pill,
    backgroundColor: colors.primary,
  },
  timelineLabel: {
    flex: 1,
    paddingVertical: spacing.md,
    color: colors.ink,
    ...typography.body,
    fontWeight: '600',
  },
  timelineLabelCurrent: {
    color: colors.primary,
    fontWeight: '800',
  },
  timelineLabelUpcoming: { color: colors.muted, fontWeight: '400' },
  toolSummary: {
    minHeight: 96,
    borderRadius: radius.lg,
    padding: spacing.md,
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.lg,
    backgroundColor: colors.primarySoft,
  },
  toolImage: {
    width: 72,
    height: 72,
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
    width: 72,
    height: 72,
    borderRadius: radius.md,
    alignItems: 'center',
    justifyContent: 'center',
    backgroundColor: colors.primarySoft,
  },
  toolName: {
    flex: 1,
    color: colors.ink,
    fontSize: 17,
    lineHeight: 22,
    fontWeight: '800',
  },
  sectionSurface: {
    overflow: 'hidden',
    borderRadius: radius.lg,
    paddingHorizontal: spacing.lg,
    backgroundColor: colors.surfaceSubtle,
  },
  detailRow: {
    minHeight: 68,
    paddingVertical: spacing.md,
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.md,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderBottomColor: colors.outline,
  },
  detailIcon: {
    width: 36,
    height: 36,
    borderRadius: radius.sm,
    alignItems: 'center',
    justifyContent: 'center',
    backgroundColor: colors.primarySoft,
  },
  detailCopy: { flex: 1, minWidth: 0, gap: spacing.xs },
  rowLabel: { color: colors.muted, ...typography.caption },
  rowValue: { color: colors.ink, ...typography.body, fontWeight: '600' },
  costSurface: {
    borderRadius: radius.lg,
    paddingHorizontal: spacing.lg,
    paddingVertical: spacing.md,
    backgroundColor: colors.surface,
  },
  moneyRow: {
    minHeight: 42,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: spacing.md,
  },
  moneyLabel: { flex: 1, color: colors.muted, ...typography.body },
  moneyValue: {
    color: colors.ink,
    ...typography.label,
    fontVariant: ['tabular-nums'],
  },
  divider: {
    height: StyleSheet.hairlineWidth,
    marginVertical: spacing.sm,
    backgroundColor: colors.outline,
  },
  totalRow: { minHeight: 50 },
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
  actionArea: { gap: spacing.lg },
  utilityGroup: { gap: spacing.md },
  transactionGroup: { gap: spacing.md },
  orderActions: { gap: spacing.sm },
  quickActions: { flexDirection: 'row', gap: spacing.md },
  compactAction: {
    flex: 1,
    minHeight: 60,
    borderRadius: radius.md,
    paddingHorizontal: spacing.md,
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.sm,
    backgroundColor: colors.surfaceSubtle,
  },
  compactActionPressed: { opacity: 0.72, transform: [{ scale: 0.96 }] },
  compactActionLabel: {
    flex: 1,
    color: colors.ink,
    fontSize: 14,
    lineHeight: 19,
    fontWeight: '700',
  },
  inlineNotice: {
    borderRadius: radius.md,
    padding: spacing.lg,
    flexDirection: 'row',
    alignItems: 'flex-start',
    gap: spacing.md,
    backgroundColor: colors.primarySoft,
  },
  inlineNoticeText: { flex: 1, color: colors.primary, ...typography.body },
  inlineNoticeError: { backgroundColor: colors.errorSoft },
  inlineNoticeTextError: {
    color: colors.error,
    fontWeight: '700',
    fontVariant: ['tabular-nums'],
  },
  modalBackdrop: {
    flex: 1,
    justifyContent: 'flex-end',
    padding: spacing.xl,
    backgroundColor: colors.scrim,
  },
  modal: {
    borderRadius: radius.lg,
    padding: spacing.xl,
    gap: spacing.xl,
    backgroundColor: colors.surface,
  },
  modalTitle: {
    color: colors.ink,
    fontSize: 21,
    lineHeight: 27,
    fontWeight: '800',
  },
  modalActions: { gap: spacing.md },
});
