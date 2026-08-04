import { Ionicons } from '@expo/vector-icons';
import { useFocusEffect, useRouter, type Href } from 'expo-router';
import { useCallback, useState } from 'react';
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

  const active = rentals.filter(
    (rental) => !historicalRentalStatuses.has(rental.status),
  );
  const history = rentals.filter((rental) =>
    historicalRentalStatuses.has(rental.status),
  );

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
            Здесь видны текущие заказы и история аренды
          </Text>
        </View>
        {error ? <ErrorNotice message={error} /> : null}

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
        ) : (
          <>
            <RentalSection
              emptyMessage="Сейчас нет заявок в работе."
              rentals={active}
              title="Активные"
              toolSummaries={toolSummaries}
              onPress={(rentalID) =>
                router.push(`/(app)/rentals/${rentalID}` as Href)
              }
            />
            <RentalSection
              emptyMessage="Завершённые и отменённые заявки появятся здесь."
              rentals={history}
              title="История"
              toolSummaries={toolSummaries}
              onPress={(rentalID) =>
                router.push(`/(app)/rentals/${rentalID}` as Href)
              }
            />
          </>
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
            Заказ №{rental.id.slice(0, 8).toUpperCase()}
          </Text>
          <Ionicons color={colors.muted} name="chevron-forward" size={18} />
        </View>

        <Text numberOfLines={2} style={styles.toolName}>
          {tool?.name ?? 'Инструмент'}
        </Text>

        <View style={styles.statusRow}>
          <View
            style={[
              styles.statusDot,
              status.tone === 'success' && styles.statusDotSuccess,
              status.tone === 'warning' && styles.statusDotWarning,
              status.tone === 'error' && styles.statusDotError,
            ]}
          />
          <Text
            numberOfLines={1}
            style={[
              styles.statusText,
              status.tone === 'success' && styles.statusTextSuccess,
              status.tone === 'warning' && styles.statusTextWarning,
              status.tone === 'error' && styles.statusTextError,
            ]}
          >
            {status.label}
          </Text>
          <Text numberOfLines={1} style={styles.amount}>
            {formatMoney(rental.total_amount)}
          </Text>
        </View>

        <View style={styles.metaRow}>
          <Ionicons color={colors.muted} name="calendar-outline" size={14} />
          <Text numberOfLines={1} style={styles.metaText}>
            {shortDate(rental.start_date)} — {shortDate(rental.end_date)}
          </Text>
        </View>

        <View style={styles.metaRow}>
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
    gap: spacing.xxl2,
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
    minHeight: 144,
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.md,
    overflow: 'hidden',
    borderRadius: radius.lg,
    padding: spacing.sm,
    backgroundColor: colors.surface,
    ...shadow,
  },
  rentalPressed: { opacity: 0.78, transform: [{ scale: 0.99 }] },
  preview: {
    width: 120,
    aspectRatio: 4 / 3,
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
  statusRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.xs,
  },
  statusDot: {
    width: 7,
    height: 7,
    borderRadius: radius.pill,
    backgroundColor: colors.primary,
  },
  statusDotSuccess: { backgroundColor: colors.success },
  statusDotWarning: { backgroundColor: colors.warning },
  statusDotError: { backgroundColor: colors.error },
  statusText: {
    minWidth: 0,
    flex: 1,
    color: colors.primary,
    fontSize: 13,
    lineHeight: 18,
    fontWeight: '700',
  },
  statusTextSuccess: { color: colors.success },
  statusTextWarning: { color: colors.warning },
  statusTextError: { color: colors.error },
  metaRow: {
    minWidth: 0,
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.xs,
  },
  metaText: { minWidth: 0, flex: 1, color: colors.muted, ...typography.caption },
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
