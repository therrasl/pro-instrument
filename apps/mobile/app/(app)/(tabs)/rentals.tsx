import { Ionicons } from '@expo/vector-icons';
import { useFocusEffect, useRouter, type Href } from 'expo-router';
import { useCallback, useMemo, useState } from 'react';
import {
  ActivityIndicator,
  AppState,
  Image,
  Pressable,
  RefreshControl,
  ScrollView,
  StyleSheet,
  Text,
  View,
} from 'react-native';
import { getTool } from '../../../src/api/catalog';
import { resolveAPIAssetURL } from '../../../src/api/client';
import { getRentals } from '../../../src/api/rentals';
import { useSession } from '../../../src/auth/session';
import { RentalsSkeleton } from '../../../src/components/skeleton';
import {
  Button,
  ErrorNotice,
  Page,
  StateView,
  Title,
} from '../../../src/components/ui';
import {
  historicalRentalStatuses,
  rentalStatusPresentation,
} from '../../../src/rentals/status';
import {
  colors,
  radius,
  shadow,
  spacing,
  typography,
} from '../../../src/theme/tokens';
import type { Rental } from '../../../src/types/api';
import { formatMoney } from '../../../src/utils/format';

const TAB_BAR_CLEARANCE = spacing.hero * 2 + spacing.lg;

type FilterTab = 'all' | 'active' | 'history';

interface RentalToolSummary {
  imageURL: string;
  name: string;
}

function shortDate(value: string): string {
  const [year, month, day] = value.split('-').map(Number);
  return new Intl.DateTimeFormat('ru-RU', {
    day: 'numeric',
    month: 'short',
  }).format(new Date(year, month - 1, day));
}

function deliveryLabel(rental: Rental): string {
  return rental.delivery_method === 'courier' ? 'Доставка' : 'Самовывоз';
}

export default function RentalsScreen() {
  const router = useRouter();
  const { token } = useSession();
  const [rentals, setRentals] = useState<Rental[]>([]);
  const [toolSummaries, setToolSummaries] = useState<
    Record<string, RentalToolSummary>
  >({});
  const [filter, setFilter] = useState<FilterTab>('all');
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [error, setError] = useState('');
  const [initialLoaded, setInitialLoaded] = useState(false);

  const load = useCallback(
    async (refresh = false, silent = false) => {
      if (!token) return;
      if (refresh) setRefreshing(true);
      else if (!silent) setLoading(true);
      setError('');
      try {
        const nextRentals = await getRentals(token);
        const ids = [...new Set(nextRentals.map((rental) => rental.tool_id))];
        const tools = await Promise.all(
          ids.map(async (toolID) => {
            try {
              const tool = await getTool(toolID);
              return [
                toolID,
                {
                  imageURL: resolveAPIAssetURL(tool.image_urls?.[0] ?? ''),
                  name: tool.name,
                },
              ] as const;
            } catch {
              return [toolID, { imageURL: '', name: 'Инструмент' }] as const;
            }
          }),
        );
        setToolSummaries(Object.fromEntries(tools));
        setRentals(nextRentals);
        setInitialLoaded(true);
      } catch (requestError) {
        setError(
          requestError instanceof Error
            ? requestError.message
            : 'Не удалось загрузить аренды.',
        );
      } finally {
        setLoading(false);
        setRefreshing(false);
      }
    },
    [token],
  );

  useFocusEffect(
    useCallback(() => {
      void load();

      const subscription = AppState.addEventListener('change', (state) => {
        if (state === 'active') void load(false, true);
      });

      return () => subscription.remove();
    }, [load]),
  );

  const active = useMemo(
    () => rentals.filter((rental) => !historicalRentalStatuses.has(rental.status)),
    [rentals],
  );

  const history = useMemo(
    () => rentals.filter((rental) => historicalRentalStatuses.has(rental.status)),
    [rentals],
  );

  const stats = useMemo(() => {
    // Loyalty and statistics only credit successfully completed rentals
    const completedRentals = rentals.filter((r) => r.status === 'completed');
    const totalOrders = completedRentals.length;
    const totalDays = completedRentals.reduce((acc, r) => acc + (r.rental_days || 0), 0);
    const totalSpent = completedRentals.reduce((acc, r) => acc + (r.rental_price || 0), 0);
    const points = Math.floor(totalSpent / 10000); // 1 балл за каждые 100 ₽ успешной аренды

    let tier = 'Старт';
    let discount = '3%';
    let nextTier: string | null = 'Мастер (7%)';
    let progress = Math.min(1, totalSpent / 1500000);
    let needed = Math.max(0, 1500000 - totalSpent);

    if (totalSpent >= 5000000) {
      tier = 'Профи';
      discount = '10%';
      nextTier = null;
      progress = 1;
      needed = 0;
    } else if (totalSpent >= 1500000) {
      tier = 'Мастер';
      discount = '7%';
      nextTier = 'Профи (10%)';
      progress = Math.min(1, (totalSpent - 1500000) / 3500000);
      needed = Math.max(0, 5000000 - totalSpent);
    }

    return { totalOrders, totalDays, totalSpent, points, tier, discount, nextTier, progress, needed };
  }, [rentals]);

  if (loading && rentals.length === 0 && !initialLoaded) {
    return (
      <Page>
        <RentalsSkeleton />
      </Page>
    );
  }

  if (error && rentals.length === 0) {
    return (
      <Page>
        <View style={styles.header}>
          <Title>Мои аренды</Title>
        </View>
        <StateView
          action={<Button label="Повторить" onPress={() => void load()} />}
          icon="cloud-offline-outline"
          message={error}
          title="Не удалось загрузить аренды"
        />
      </Page>
    );
  }

  return (
    <Page>
      <ScrollView
        contentContainerStyle={styles.content}
        refreshControl={
          <RefreshControl
            colors={[colors.primary]}
            onRefresh={() => void load(true)}
            refreshing={refreshing}
            tintColor={colors.primary}
          />
        }
        showsVerticalScrollIndicator={false}
      >
        <View style={styles.headerInScroll}>
          <View style={styles.titleRow}>
            <Title>Мои аренды</Title>
            {loading && !refreshing ? (
              <ActivityIndicator color={colors.primary} size="small" />
            ) : null}
          </View>
          <Text style={styles.subtitle}>
            Здесь видны текущие заказы, статистика и история аренды
          </Text>
        </View>
        {error ? <ErrorNotice message={error} /> : null}

        {/* Loyalty & Stats Card */}
        {rentals.length > 0 ? (
          <View style={styles.loyaltyCard}>
            <View style={styles.loyaltyHeader}>
              <View style={styles.loyaltyBadge}>
                <Ionicons color={colors.primary} name="ribbon-outline" size={20} />
                <Text style={styles.loyaltyBadgeText}>Программа лояльности</Text>
              </View>
              <View style={styles.loyaltyTierBadge}>
                <Text style={styles.loyaltyTierText}>
                  {stats.points > 0 ? `${stats.points} б. · ` : ''}{stats.tier} · {stats.discount}
                </Text>
              </View>
            </View>

            <View style={styles.statsRow}>
              <View style={styles.statItem}>
                <Text style={styles.statValue}>{stats.totalOrders}</Text>
                <Text style={styles.statLabel}>Завершено</Text>
              </View>
              <View style={styles.statDivider} />
              <View style={styles.statItem}>
                <Text style={styles.statValue}>{stats.totalDays}</Text>
                <Text style={styles.statLabel}>Дней</Text>
              </View>
              <View style={styles.statDivider} />
              <View style={styles.statItem}>
                <Text style={styles.statValue}>{stats.points}</Text>
                <Text style={styles.statLabel}>Баллов</Text>
              </View>
              <View style={styles.statDivider} />
              <View style={styles.statItem}>
                <Text style={styles.statValue}>{formatMoney(stats.totalSpent)}</Text>
                <Text style={styles.statLabel}>Накоплено</Text>
              </View>
            </View>

            {stats.nextTier ? (
              <View style={styles.progressContainer}>
                <View style={styles.progressBar}>
                  <View
                    style={[
                      styles.progressFill,
                      { width: `${Math.round(stats.progress * 100)}%` },
                    ]}
                  />
                </View>
                <Text style={styles.progressText}>
                  До уровня «{stats.nextTier}»: {formatMoney(stats.needed)}
                </Text>
              </View>
            ) : (
              <Text style={styles.progressTextMax}>
                🎉 У вас максимальный уровень скидки постоянного клиента!
              </Text>
            )}

            <Text style={styles.loyaltyHint}>
              Баллы и уровень начисляются только при успешном завершении аренды
            </Text>
          </View>
        ) : null}

        {/* Filter Switcher */}
        {rentals.length > 0 ? (
          <View style={styles.segmentedFilter}>
            <Pressable
              accessibilityRole="tab"
              accessibilityState={{ selected: filter === 'all' }}
              onPress={() => setFilter('all')}
              style={[styles.segmentBtn, filter === 'all' && styles.segmentBtnActive]}
            >
              <Text style={[styles.segmentText, filter === 'all' && styles.segmentTextActive]}>
                Все ({rentals.length})
              </Text>
            </Pressable>
            <Pressable
              accessibilityRole="tab"
              accessibilityState={{ selected: filter === 'active' }}
              onPress={() => setFilter('active')}
              style={[styles.segmentBtn, filter === 'active' && styles.segmentBtnActive]}
            >
              <Text style={[styles.segmentText, filter === 'active' && styles.segmentTextActive]}>
                В аренде ({active.length})
              </Text>
            </Pressable>
            <Pressable
              accessibilityRole="tab"
              accessibilityState={{ selected: filter === 'history' }}
              onPress={() => setFilter('history')}
              style={[styles.segmentBtn, filter === 'history' && styles.segmentBtnActive]}
            >
              <Text style={[styles.segmentText, filter === 'history' && styles.segmentTextActive]}>
                Завершённые ({history.length})
              </Text>
            </Pressable>
          </View>
        ) : null}

        {rentals.length === 0 ? (
          <StateView
            action={
              <Button
                label="Открыть каталог"
                onPress={() => router.push('/(app)/(tabs)/catalog' as Href)}
              />
            }
            compact
            icon="receipt-outline"
            message="Выберите инструмент и оформите первую заявку."
            title="Здесь пока ничего нет"
          />
        ) : filter === 'all' ? (
          <>
            <RentalSection
              emptyMessage="Сейчас нет активных заказов в работе."
              rentals={active}
              title="В работе / Активные"
              toolSummaries={toolSummaries}
              onPress={(rentalID) =>
                router.push(`/(app)/rentals/${rentalID}` as Href)
              }
            />
            <RentalSection
              emptyMessage="Завершённые заявки появятся здесь."
              rentals={history}
              title="История"
              toolSummaries={toolSummaries}
              onPress={(rentalID) =>
                router.push(`/(app)/rentals/${rentalID}` as Href)
              }
            />
          </>
        ) : filter === 'active' ? (
          <RentalSection
            emptyMessage="Сейчас нет активных заказов в работе."
            rentals={active}
            title="В работе / Активные"
            toolSummaries={toolSummaries}
            onPress={(rentalID) =>
              router.push(`/(app)/rentals/${rentalID}` as Href)
            }
          />
        ) : (
          <RentalSection
            emptyMessage="Завершённые и отменённые заявки появятся здесь."
            rentals={history}
            title="Завершённые заказы"
            toolSummaries={toolSummaries}
            onPress={(rentalID) =>
              router.push(`/(app)/rentals/${rentalID}` as Href)
            }
          />
        )}
      </ScrollView>
    </Page>
  );
}

function RentalSection({
  emptyMessage,
  onPress,
  rentals,
  title,
  toolSummaries,
}: {
  emptyMessage: string;
  onPress: (rentalID: string) => void;
  rentals: Rental[];
  title: string;
  toolSummaries: Record<string, RentalToolSummary>;
}) {
  return (
    <View style={styles.section}>
      <View style={styles.sectionHeading}>
        <Text style={styles.sectionTitle}>{title}</Text>
        <Text style={styles.count}>{rentals.length}</Text>
      </View>
      {rentals.length === 0 ? (
        <Text style={styles.emptySection}>{emptyMessage}</Text>
      ) : (
        <View style={styles.list}>
          {rentals.map((rental) => {
            return (
              <RentalCard
                key={rental.id}
                onPress={() => onPress(rental.id)}
                rental={rental}
                tool={toolSummaries[rental.tool_id]}
              />
            );
          })}
        </View>
      )}
    </View>
  );
}

function RentalCard({
  onPress,
  rental,
  tool,
}: {
  onPress: () => void;
  rental: Rental;
  tool?: RentalToolSummary;
}) {
  const status = rentalStatusPresentation[rental.status];
  const [failedImageURL, setFailedImageURL] = useState('');
  const imageURL = tool?.imageURL ?? '';
  const imageFailed = Boolean(imageURL) && failedImageURL === imageURL;

  return (
    <Pressable
      accessibilityHint="Открывает детали заказа"
      accessibilityLabel={`${tool?.name ?? 'Инструмент'}, ${status.label}, ${shortDate(
        rental.start_date,
      )} — ${shortDate(rental.end_date)}, ${deliveryLabel(rental)}, ${formatMoney(
        rental.total_amount,
      )}`}
      accessibilityRole="button"
      onPress={onPress}
      style={({ pressed }) => [
        styles.rental,
        pressed && styles.rentalPressed,
      ]}
    >
      <View style={styles.preview}>
        <View style={styles.imagePlaceholder}>
          <Ionicons
            color={colors.primary}
            name="construct-outline"
            size={28}
          />
        </View>
        {imageURL && !imageFailed ? (
          <Image
            accessibilityIgnoresInvertColors
            fadeDuration={0}
            onError={() => setFailedImageURL(imageURL)}
            resizeMode="contain"
            source={{ uri: imageURL }}
            style={styles.image}
          />
        ) : null}
      </View>

      <View style={styles.rentalContent}>
        <View style={styles.orderRow}>
          <Text numberOfLines={1} style={styles.orderNumber}>
            Заказ №{rental.order_number ?? rental.id.slice(0, 8).toUpperCase()}
          </Text>
          <Ionicons color={colors.muted} name="chevron-forward" size={18} />
        </View>

        <Text numberOfLines={2} style={styles.toolName}>
          {tool?.name ?? 'Инструмент'}
        </Text>

        {/* Unclipped Status Badge */}
        <View
          style={[
            styles.statusBadge,
            status.tone === 'success' && styles.statusBadgeSuccess,
            status.tone === 'warning' && styles.statusBadgeWarning,
            status.tone === 'error' && styles.statusBadgeError,
          ]}
        >
          <View
            style={[
              styles.statusDot,
              status.tone === 'success' && styles.statusDotSuccess,
              status.tone === 'warning' && styles.statusDotWarning,
              status.tone === 'error' && styles.statusDotError,
            ]}
          />
          <Text
            style={[
              styles.statusBadgeText,
              status.tone === 'success' && styles.statusBadgeTextSuccess,
              status.tone === 'warning' && styles.statusBadgeTextWarning,
              status.tone === 'error' && styles.statusBadgeTextError,
            ]}
          >
            {status.label}
          </Text>
        </View>

        <View style={styles.metaRow}>
          <Ionicons color={colors.muted} name="calendar-outline" size={14} />
          <Text numberOfLines={1} style={styles.metaText}>
            {shortDate(rental.start_date)} — {shortDate(rental.end_date)} ({rental.rental_days} дн.)
          </Text>
        </View>

        <View style={styles.bottomRow}>
          <View style={styles.metaRowCompact}>
            <Ionicons
              color={colors.muted}
              name={
                rental.delivery_method === 'courier'
                  ? 'car-outline'
                  : 'storefront-outline'
              }
              size={14}
            />
            <Text numberOfLines={1} style={styles.metaText}>
              {deliveryLabel(rental)}
            </Text>
          </View>
          <Text style={styles.amount}>
            {formatMoney(rental.total_amount)}
          </Text>
        </View>
      </View>
    </Pressable>
  );
}

const styles = StyleSheet.create({
  header: { paddingHorizontal: spacing.xl, paddingTop: spacing.xl },
  content: {
    paddingHorizontal: spacing.xl,
    paddingTop: spacing.xl,
    paddingBottom: TAB_BAR_CLEARANCE,
    gap: spacing.xl,
  },
  headerInScroll: { gap: spacing.sm },
  titleRow: {
    minHeight: 40,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: spacing.md,
  },
  subtitle: { color: colors.muted, ...typography.body },
  loyaltyCard: {
    borderRadius: radius.lg,
    padding: spacing.lg,
    gap: spacing.md,
    backgroundColor: colors.surface,
    borderWidth: 1,
    borderColor: colors.outline,
    ...shadow,
  },
  loyaltyHeader: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
  },
  loyaltyBadge: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.xs,
  },
  loyaltyBadgeText: {
    color: colors.ink,
    fontSize: 14,
    lineHeight: 18,
    fontWeight: '700',
  },
  loyaltyTierBadge: {
    borderRadius: radius.pill,
    paddingHorizontal: spacing.sm,
    paddingVertical: 2,
    backgroundColor: colors.primarySoft,
  },
  loyaltyTierText: {
    color: colors.primary,
    fontSize: 13,
    lineHeight: 17,
    fontWeight: '800',
  },
  statsRow: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    paddingVertical: spacing.xs,
  },
  statItem: {
    flex: 1,
    alignItems: 'center',
    gap: 2,
  },
  statValue: {
    color: colors.ink,
    fontSize: 16,
    lineHeight: 21,
    fontWeight: '800',
    fontVariant: ['tabular-nums'],
  },
  statLabel: {
    color: colors.muted,
    fontSize: 11,
    lineHeight: 15,
  },
  statDivider: {
    width: 1,
    height: 24,
    backgroundColor: colors.outline,
  },
  progressContainer: {
    gap: 4,
  },
  progressBar: {
    height: 6,
    borderRadius: radius.pill,
    backgroundColor: colors.surfaceSubtle,
    overflow: 'hidden',
  },
  progressFill: {
    height: '100%',
    borderRadius: radius.pill,
    backgroundColor: colors.primary,
  },
  progressText: {
    color: colors.muted,
    fontSize: 11,
    lineHeight: 15,
  },
  progressTextMax: {
    color: colors.success,
    fontSize: 12,
    lineHeight: 16,
    fontWeight: '600',
  },
  loyaltyHint: {
    color: colors.muted,
    fontSize: 11,
    lineHeight: 15,
    textAlign: 'center',
  },
  segmentedFilter: {
    flexDirection: 'row',
    borderRadius: radius.button,
    padding: spacing.xs,
    backgroundColor: colors.surfaceStrong,
    gap: spacing.xs,
  },
  segmentBtn: {
    flex: 1,
    minHeight: 38,
    borderRadius: radius.sm,
    alignItems: 'center',
    justifyContent: 'center',
  },
  segmentBtnActive: {
    backgroundColor: colors.surface,
  },
  segmentText: {
    color: colors.muted,
    fontSize: 13,
    lineHeight: 17,
    fontWeight: '600',
  },
  segmentTextActive: {
    color: colors.primary,
    fontWeight: '800',
  },
  section: { gap: spacing.md },
  sectionHeading: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.sm,
  },
  sectionTitle: { color: colors.ink, ...typography.section },
  count: {
    minWidth: 26,
    height: 26,
    borderRadius: radius.pill,
    paddingHorizontal: spacing.sm,
    color: colors.primary,
    backgroundColor: colors.primarySoft,
    fontSize: 13,
    lineHeight: 26,
    fontWeight: '800',
    textAlign: 'center',
    fontVariant: ['tabular-nums'],
  },
  emptySection: {
    borderRadius: radius.md,
    padding: spacing.lg,
    color: colors.muted,
    backgroundColor: colors.surfaceSubtle,
    ...typography.body,
  },
  list: { gap: spacing.md },
  rental: {
    minHeight: 148,
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.md,
    overflow: 'hidden',
    borderRadius: radius.lg,
    padding: spacing.md,
    backgroundColor: colors.surface,
    ...shadow,
  },
  rentalPressed: { opacity: 0.78, transform: [{ scale: 0.99 }] },
  preview: {
    width: 108,
    aspectRatio: 1,
    alignSelf: 'center',
    flexShrink: 0,
    overflow: 'hidden',
    borderRadius: radius.md,
    backgroundColor: colors.surfaceSubtle,
  },
  image: {
    position: 'absolute',
    top: 0,
    right: 0,
    bottom: 0,
    left: 0,
  },
  imagePlaceholder: {
    flex: 1,
    alignItems: 'center',
    justifyContent: 'center',
    backgroundColor: colors.surfaceSubtle,
  },
  rentalContent: {
    minWidth: 0,
    flex: 1,
    gap: spacing.xs,
  },
  orderRow: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: spacing.xs,
  },
  orderNumber: {
    minWidth: 0,
    flex: 1,
    color: colors.muted,
    fontSize: 11,
    lineHeight: 15,
  },
  toolName: {
    color: colors.ink,
    fontSize: 15,
    lineHeight: 19,
    fontWeight: '800',
  },
  statusBadge: {
    alignSelf: 'flex-start',
    flexDirection: 'row',
    alignItems: 'center',
    gap: 6,
    paddingHorizontal: spacing.sm,
    paddingVertical: 3,
    borderRadius: radius.pill,
    backgroundColor: colors.primarySoft,
    marginVertical: 2,
  },
  statusBadgeSuccess: { backgroundColor: colors.successSoft },
  statusBadgeWarning: { backgroundColor: colors.warningSoft },
  statusBadgeError: { backgroundColor: colors.errorSoft },
  statusDot: {
    width: 6,
    height: 6,
    borderRadius: radius.pill,
    backgroundColor: colors.primary,
  },
  statusDotSuccess: { backgroundColor: colors.success },
  statusDotWarning: { backgroundColor: colors.warning },
  statusDotError: { backgroundColor: colors.error },
  statusBadgeText: {
    color: colors.primary,
    fontSize: 12,
    lineHeight: 16,
    fontWeight: '700',
  },
  statusBadgeTextSuccess: { color: colors.success },
  statusBadgeTextWarning: { color: colors.warning },
  statusBadgeTextError: { color: colors.error },
  metaRow: {
    minWidth: 0,
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.xs,
  },
  metaRowCompact: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 4,
  },
  metaText: { color: colors.muted, ...typography.caption },
  bottomRow: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    marginTop: 2,
  },
  amount: {
    flexShrink: 0,
    color: colors.ink,
    fontSize: 16,
    lineHeight: 21,
    fontWeight: '800',
    fontVariant: ['tabular-nums'],
    textAlign: 'right',
  },
});
