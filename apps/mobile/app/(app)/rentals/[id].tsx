import { Ionicons } from '@expo/vector-icons';
import {
  useFocusEffect,
  useLocalSearchParams,
  useRouter,
} from 'expo-router';
import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from 'react';
import {
  ActivityIndicator,
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
  createExtension,
  createRentalPayment,
  getExtensionQuote,
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
import type { ExtensionQuote, Rental, Tool } from '../../../src/types/api';
import { formatMoney } from '../../../src/utils/format';
import { copyText } from '../../../src/utils/clipboard';

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

function addDaysToDateString(dateStr: string, days: number): string {
  const [year, month, day] = dateStr.split('-').map(Number);
  const date = new Date(Date.UTC(year, month - 1, day));
  date.setUTCDate(date.getUTCDate() + days);
  return date.toISOString().slice(0, 10);
}

export default function RentalDetailScreen() {
	const router = useRouter();
  const { id, paymentStartedAt } = useLocalSearchParams<{
    id: string;
    paymentStartedAt?: string;
  }>();
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
  const [supportCopied, setSupportCopied] = useState(false);
  const [focused, setFocused] = useState(false);
  const [error, setError] = useState('');
  const [extensionModalVisible, setExtensionModalVisible] = useState(false);
  const [extensionDays, setExtensionDays] = useState(1);
  const [extensionQuote, setExtensionQuote] = useState<ExtensionQuote | null>(null);
  const [quotingExtension, setQuotingExtension] = useState(false);
  const [extending, setExtending] = useState(false);
  const [extensionError, setExtensionError] = useState('');
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

  const copyOrderNumber = async () => {
    if (!rental) return;
    const copied = await copyText(rental.order_number ?? rental.id.slice(0, 8).toUpperCase());
    setSupportCopied(copied);
  };

  const loadExtensionQuote = useCallback(async (days: number, currentRental = rental) => {
    if (!token || !currentRental) return;
    const newDate = addDaysToDateString(currentRental.end_date, days);
    setQuotingExtension(true);
    setExtensionError('');
    try {
      const quote = await getExtensionQuote(token, currentRental.id, newDate);
      setExtensionQuote(quote);
    } catch (cause) {
      setExtensionQuote(null);
      setExtensionError(cause instanceof Error ? cause.message : 'Не удалось рассчитать продление.');
    } finally {
      setQuotingExtension(false);
    }
  }, [rental, token]);

  const openExtensionModal = () => {
    setExtensionDays(1);
    setExtensionError('');
    setExtensionModalVisible(true);
    void loadExtensionQuote(1);
  };

  const handleSelectDays = (days: number) => {
    setExtensionDays(days);
    void loadExtensionQuote(days);
  };

  const handleConfirmExtension = async () => {
    if (!token || !rental || !extensionQuote || !extensionQuote.available || extending) return;
    setExtending(true);
    setExtensionError('');
    try {
      const extension = await createExtension(token, rental.id, extensionQuote.new_end_date);
      setExtensionModalVisible(false);
      if (extension.confirmation_url && isSafeConfirmationURL(extension.confirmation_url)) {
        await rememberPendingPaymentRental(rental.id);
        await Linking.openURL(extension.confirmation_url);
        paymentPollStartedAt.current = Date.now();
        setPaymentStarted(true);
        void load('silent');
      } else {
        await load('refresh');
      }
    } catch (cause) {
      setExtensionError(cause instanceof Error ? cause.message : 'Не удалось оформить продление.');
    } finally {
      setExtending(false);
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
  const canExtend = [
    'rented',
    'ready',
    'handed_to_courier',
    'awaiting_return',
  ].includes(rental.status);
  const canInspect = [
    'ready',
    'handed_to_courier',
    'rented',
    'awaiting_return',
    'inspection',
    'completed',
  ].includes(rental.status);
  const paymentDeadlinePassed =
    rental.status === 'awaiting_payment' && remainingPaymentSeconds === 0;
  const showTransactionArea =
    (paymentStarted && rental.status === 'awaiting_payment') ||
    rental.status === 'awaiting_payment' ||
    Boolean(error) ||
    cancellable ||
    canExtend;

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
            Заказ №{rental.order_number ?? rental.id.slice(0, 8).toUpperCase()}
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

        {(() => {
          const now = new Date();
          const [endYear, endMonth, endDay] = rental.end_date.split('-').map(Number);
          const deadlineDate = new Date(endYear, endMonth - 1, endDay, 19, 0, 0);
          const isOverdue =
            (rental.status === 'rented' || rental.status === 'awaiting_return') &&
            now.getTime() > deadlineDate.getTime();
          const overdueDays = isOverdue
            ? Math.max(1, Math.ceil((now.getTime() - deadlineDate.getTime()) / (1000 * 60 * 60 * 24)))
            : 0;
          const dailyRate =
            rental.rental_days > 0 ? Math.round(rental.rental_price / rental.rental_days) : 0;
          const overdueDebt = isOverdue ? overdueDays * dailyRate : 0;

          return (
            <>
              <Section title="Сроки аренды и получение">
                <View style={styles.sectionSurface}>
                  <DetailRow
                    icon="calendar-outline"
                    label="Период аренды"
                    value={`${formatDate(rental.start_date)} — ${formatDate(rental.end_date)} (${rental.rental_days} дн.)`}
                  />
                  <DetailRow
                    icon="time-outline"
                    label="Дата и время выдачи"
                    value={
                      rental.status === 'pending_manager' || rental.status === 'awaiting_payment'
                        ? `${formatDate(rental.start_date)} с 09:00`
                        : `${formatDate(rental.start_date)} с 09:00 (выдано)`
                    }
                  />
                  <DetailRow
                    icon="alarm-outline"
                    label="Оплачено до (плановый возврат)"
                    value={`${formatDate(rental.end_date)} до 19:00`}
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
                  ) : !courier && rental.pickup_address ? (
                    <DetailRow icon="location-outline" label="Пункт выдачи" value={rental.pickup_address} />
                  ) : null}
                </View>
              </Section>

              {isOverdue ? (
                <View style={styles.overdueAlert}>
                  <View style={styles.overdueHeader}>
                    <Ionicons color={colors.error} name="warning" size={22} />
                    <Text style={styles.overdueTitle}>Просрочка возврата: {overdueDays} дн.</Text>
                  </View>
                  <Text style={styles.overdueBody}>
                    Срок оплаченной аренды истёк {formatDate(rental.end_date)} в 19:00.
                  </Text>
                  <View style={styles.overdueDebtRow}>
                    <Text style={styles.overdueDebtLabel}>Сумма к доплате:</Text>
                    <Text style={styles.overdueDebtValue}>{formatMoney(overdueDebt)}</Text>
                  </View>
                  <Text style={styles.overdueHint}>
                    Доплата ({formatMoney(dailyRate)}/сут.) может быть удержана из суммы обеспечительного платежа или внесена при возврате.
                  </Text>
                </View>
              ) : null}

              <Section title="Стоимость">
                <View style={styles.costSurface}>
                  <MoneyRow
                    label={`Аренда, ${rental.rental_days} дн.`}
                    value={rental.rental_price}
                  />
                  <MoneyRow label="Обеспечительный платеж" value={rental.deposit_amount} />
                  <MoneyRow label="Доставка" value={rental.delivery_cost} />
                  <View style={styles.divider} />
                  <MoneyRow emphasized label="Итого" value={rental.total_amount} />
                </View>
              </Section>
            </>
          );
        })()}

        <View style={styles.actionArea}>
          <View style={styles.utilityGroup}>
            <View style={styles.quickActions}>
              <CompactAction
                icon="document-text-outline"
                label="Документы"
                onPress={() => router.push(`/(app)/rentals/${rental.id}/documents` as never)}
              />
              <CompactAction
                icon="camera-outline"
                label="Фото"
                onPress={() => router.push(`/(app)/rentals/${rental.id}/photos` as never)}
              />
              <CompactAction
                icon="chatbubble-ellipses-outline"
                label="Поддержка"
                onPress={() => setSupportVisible((value) => !value)}
              />
            </View>

            {supportVisible ? (
              <View style={styles.supportNotice}>
                <InlineNotice
                  icon="chatbubble-ellipses-outline"
                  message={`Сообщите менеджеру номер заказа №${rental.order_number ?? rental.id.slice(0, 8).toUpperCase()}.`}
                />
                <Button
                  label={supportCopied ? 'Номер скопирован' : 'Скопировать номер'}
                  onPress={() => void copyOrderNumber()}
                  variant="secondary"
                />
              </View>
            ) : null}

            {canInspect ? (
              <Pressable
                style={styles.inspectionCard}
                onPress={() => router.push(`/(app)/rentals/${rental.id}/photos` as never)}
              >
                <View style={styles.inspectionIconBox}>
                  <Ionicons name="camera-outline" size={20} color={colors.primary} />
                </View>
                <View style={styles.inspectionTextBox}>
                  <Text style={styles.inspectionTitle}>Фотофиксация инструмента</Text>
                  <Text style={styles.inspectionSubtitle}>
                    5 ракурсов для проверки сохранности оборудования
                  </Text>
                </View>
                <Ionicons name="chevron-forward" size={18} color={colors.muted} />
              </Pressable>
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

                {canExtend ? (
                  <Button
                    icon="calendar-outline"
                    label="Продлить аренду"
                    onPress={() => openExtensionModal()}
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

      <Modal
        animationType="fade"
        onRequestClose={() => !extending && setExtensionModalVisible(false)}
        transparent
        visible={extensionModalVisible}
      >
        <View style={styles.modalBackdrop}>
          <View accessibilityViewIsModal style={styles.extensionModal}>
            <View style={styles.extensionModalHeader}>
              <View style={{ flex: 1 }}>
                <Text accessibilityRole="header" style={styles.modalTitle}>
                  Продление аренды
                </Text>
                <Text style={styles.extensionModalSubtitle}>
                  Текущий срок: до {formatDate(rental.end_date)}
                </Text>
              </View>
              <Pressable
                disabled={extending}
                hitSlop={8}
                onPress={() => setExtensionModalVisible(false)}
              >
                <Ionicons name="close" size={24} color={colors.ink} />
              </Pressable>
            </View>

            <Text style={styles.extensionSectionTitle}>Срок продления:</Text>
            <View style={styles.daysSelector}>
              {[1, 2, 3, 5, 7].map((days) => (
                <Pressable
                  key={days}
                  style={[
                    styles.dayChip,
                    extensionDays === days && styles.dayChipActive,
                  ]}
                  onPress={() => handleSelectDays(days)}
                >
                  <Text
                    style={[
                      styles.dayChipText,
                      extensionDays === days && styles.dayChipTextActive,
                    ]}
                  >
                    +{days} {days === 1 ? 'день' : days < 5 ? 'дня' : 'дней'}
                  </Text>
                </Pressable>
              ))}
            </View>

            {quotingExtension ? (
              <View style={styles.extensionLoadingBox}>
                <ActivityIndicator size="small" color={colors.primary} />
                <Text style={styles.extensionLoadingText}>
                  Проверяем доступность инструмента...
                </Text>
              </View>
            ) : extensionQuote ? (
              <View style={styles.extensionQuoteBox}>
                <View style={styles.quoteRow}>
                  <Text style={styles.quoteLabel}>Новая дата возврата:</Text>
                  <Text style={styles.quoteValue}>
                    {formatDate(extensionQuote.new_end_date)}
                  </Text>
                </View>
                <View style={styles.quoteRow}>
                  <Text style={styles.quoteLabel}>Дополнительно дней:</Text>
                  <Text style={styles.quoteValue}>
                    +{extensionQuote.additional_days} дн.
                  </Text>
                </View>
                <View style={styles.quoteRow}>
                  <Text style={styles.quoteLabel}>Тариф в сутки:</Text>
                  <Text style={styles.quoteValue}>
                    {formatMoney(extensionQuote.daily_price)}
                  </Text>
                </View>
                <View style={styles.divider} />
                <View style={styles.quoteRow}>
                  <Text
                    style={[
                      styles.quoteLabel,
                      { fontWeight: '700', color: colors.ink },
                    ]}
                  >
                    К доплате:
                  </Text>
                  <Text
                    style={[
                      styles.quoteValue,
                      { fontWeight: '800', color: colors.primary, fontSize: 17 },
                    ]}
                  >
                    {formatMoney(extensionQuote.amount)}
                  </Text>
                </View>

                {!extensionQuote.available ? (
                  <View style={styles.extensionUnavailableNotice}>
                    <Ionicons
                      name="alert-circle-outline"
                      size={18}
                      color={colors.error}
                    />
                    <Text style={styles.extensionUnavailableText}>
                      Инструмент забронирован следующим клиентом на выбранный период.
                      Пожалуйста, выберите меньший срок.
                    </Text>
                  </View>
                ) : null}
              </View>
            ) : null}

            {extensionError ? (
              <Text style={styles.extensionErrorText}>{extensionError}</Text>
            ) : null}

            <View style={styles.modalActions}>
              <Button
                disabled={extending}
                label="Отмена"
                onPress={() => setExtensionModalVisible(false)}
                variant="secondary"
              />
              <Button
                disabled={
                  !extensionQuote ||
                  !extensionQuote.available ||
                  quotingExtension
                }
                label={
                  extensionQuote && extensionQuote.available
                    ? `Оплатить ${formatMoney(extensionQuote.amount)}`
                    : 'Продлить'
                }
                loading={extending}
                onPress={() => void handleConfirmExtension()}
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
  icon: 'calendar-outline' | 'time-outline' | 'alarm-outline' | 'car-outline' | 'storefront-outline' | 'location-outline';
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
  icon: 'document-text-outline' | 'chatbubble-ellipses-outline' | 'camera-outline';
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
      <Text adjustsFontSizeToFit minimumFontScale={0.85} numberOfLines={1} style={styles.compactActionLabel}>
        {label}
      </Text>
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
  supportNotice: { gap: spacing.sm },
  orderDocuments: { gap: spacing.sm },
  orderDocumentsTitle: { color: colors.ink, ...typography.section },
  orderDocumentsEmpty: { color: colors.muted, ...typography.body },
  documentLink: {
    minHeight: 56,
    borderRadius: radius.md,
    paddingHorizontal: spacing.md,
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.sm,
    backgroundColor: colors.surfaceSubtle,
  },
  documentLinkText: { flex: 1, minWidth: 0, color: colors.ink, ...typography.label },
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
  overdueAlert: {
    borderRadius: radius.lg,
    padding: spacing.lg,
    gap: spacing.sm,
    backgroundColor: colors.errorSoft,
    borderWidth: 1,
    borderColor: colors.error,
  },
  overdueHeader: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.sm,
  },
  overdueTitle: {
    color: colors.error,
    fontSize: 16,
    lineHeight: 21,
    fontWeight: '800',
  },
  overdueBody: {
    color: colors.ink,
    fontSize: 14,
    lineHeight: 19,
  },
  overdueDebtRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    paddingVertical: spacing.xs,
  },
  overdueDebtLabel: {
    color: colors.ink,
    fontSize: 15,
    lineHeight: 20,
    fontWeight: '700',
  },
  overdueDebtValue: {
    color: colors.error,
    fontSize: 18,
    lineHeight: 23,
    fontWeight: '800',
    fontVariant: ['tabular-nums'],
  },
  overdueHint: {
    color: colors.muted,
    fontSize: 12,
    lineHeight: 16,
  },
  inspectionCard: {
    flexDirection: 'row',
    alignItems: 'center',
    backgroundColor: colors.surfaceSubtle,
    padding: spacing.md,
    borderRadius: radius.md,
    borderWidth: 1,
    borderColor: colors.outline,
    gap: spacing.sm,
    marginTop: spacing.xs,
  },
  inspectionIconBox: {
    width: 36,
    height: 36,
    borderRadius: radius.sm,
    backgroundColor: colors.primarySoft,
    alignItems: 'center',
    justifyContent: 'center',
  },
  inspectionTextBox: {
    flex: 1,
    gap: 2,
  },
  inspectionTitle: {
    fontSize: 14,
    fontWeight: '700',
    color: colors.ink,
  },
  inspectionSubtitle: {
    fontSize: 12,
    color: colors.muted,
  },
  extensionModal: {
    borderRadius: radius.lg,
    padding: spacing.xl,
    gap: spacing.lg,
    backgroundColor: colors.surface,
  },
  extensionModalHeader: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'flex-start',
  },
  extensionModalSubtitle: {
    fontSize: 13,
    color: colors.muted,
    marginTop: 2,
  },
  extensionSectionTitle: {
    fontSize: 14,
    fontWeight: '700',
    color: colors.ink,
  },
  daysSelector: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: spacing.xs,
  },
  dayChip: {
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
    borderRadius: radius.pill,
    backgroundColor: colors.surfaceSubtle,
    borderWidth: 1,
    borderColor: colors.outline,
  },
  dayChipActive: {
    backgroundColor: colors.primary,
    borderColor: colors.primary,
  },
  dayChipText: {
    fontSize: 13,
    fontWeight: '600',
    color: colors.ink,
  },
  dayChipTextActive: {
    color: colors.white,
  },
  extensionLoadingBox: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    paddingVertical: spacing.md,
    gap: spacing.sm,
  },
  extensionLoadingText: {
    fontSize: 13,
    color: colors.muted,
  },
  extensionQuoteBox: {
    backgroundColor: colors.surfaceSubtle,
    padding: spacing.md,
    borderRadius: radius.md,
    gap: spacing.xs,
    borderWidth: 1,
    borderColor: colors.outline,
  },
  quoteRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    paddingVertical: 2,
  },
  quoteLabel: {
    fontSize: 13,
    color: colors.muted,
  },
  quoteValue: {
    fontSize: 14,
    fontWeight: '600',
    color: colors.ink,
  },
  extensionUnavailableNotice: {
    flexDirection: 'row',
    alignItems: 'flex-start',
    backgroundColor: colors.errorSoft,
    padding: spacing.sm,
    borderRadius: radius.sm,
    gap: spacing.xs,
    marginTop: spacing.xs,
  },
  extensionUnavailableText: {
    fontSize: 12,
    color: colors.error,
    flex: 1,
    lineHeight: 16,
  },
  extensionErrorText: {
    fontSize: 13,
    color: colors.error,
    textAlign: 'center',
  },
});
