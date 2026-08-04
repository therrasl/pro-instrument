import { Ionicons } from '@expo/vector-icons';
import { useRouter, type Href } from 'expo-router';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  AccessibilityInfo,
  ActivityIndicator,
  Animated,
  Easing,
  FlatList,
  Modal,
  Pressable,
  RefreshControl,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  View,
} from 'react-native';
import { getCategories, getTools } from '../../../../src/api/catalog';
import { useSession } from '../../../../src/auth/session';
import { ToolCard } from '../../../../src/components/tool-card';
import {
  CatalogGridSkeleton,
  SkeletonBlock,
  SkeletonGroup,
} from '../../../../src/components/skeleton';
import {
  Button,
  Checkbox,
  ErrorNotice,
  Page,
  StateView,
} from '../../../../src/components/ui';
import { colors, radius, spacing } from '../../../../src/theme/tokens';
import type { Category, Tool } from '../../../../src/types/api';

const TAB_BAR_CLEARANCE = spacing.hero * 2 + spacing.lg;

export default function CatalogScreen() {
  const router = useRouter();
  const { client } = useSession();
  const [categories, setCategories] = useState<Category[]>([]);
  const [tools, setTools] = useState<Tool[]>([]);
  const [categoryID, setCategoryID] = useState('');
  const [availableOnly, setAvailableOnly] = useState(false);
  const [search, setSearch] = useState('');
  const [searchFocused, setSearchFocused] = useState(false);
  const [filtersVisible, setFiltersVisible] = useState(false);
  const [loading, setLoading] = useState(true);
  const [initialLoaded, setInitialLoaded] = useState(false);
  const [categoriesLoading, setCategoriesLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [error, setError] = useState('');
  const toolsRequest = useRef(0);
  const refreshInFlight = useRef<Promise<void> | null>(null);
  const [listOpacity] = useState(() => new Animated.Value(1));
  const filterCount = Number(Boolean(categoryID)) + Number(availableOnly);
  const selectedCategory = useMemo(
    () => categories.find((category) => category.id === categoryID),
    [categories, categoryID],
  );
  const documentsPending = client?.status === 'pending_verification';
  const documentsVerified = client?.status === 'verified';
  const documentNotice =
    client?.status === 'documents_uploaded'
      ? 'Отправьте документы на проверку'
      : client?.status === 'verification_rejected'
        ? 'Документы отклонены — загрузите повторно'
        : 'Загрузите документы для оформления аренды';

  const loadCategories = useCallback(async () => {
    setCategories(await getCategories());
  }, []);

  const loadTools = useCallback(async () => {
    return getTools({
      categoryID: categoryID || undefined,
      search: search.trim() || undefined,
      available: availableOnly || undefined,
    });
  }, [availableOnly, categoryID, search]);

  useEffect(() => {
    const timeout = setTimeout(() => {
      void loadCategories().catch((requestError: unknown) => {
        setError(requestError instanceof Error ? requestError.message : 'Не удалось загрузить категории.');
      }).finally(() => setCategoriesLoading(false));
    }, 0);
    return () => clearTimeout(timeout);
  }, [loadCategories]);

  useEffect(() => {
    const timeout = setTimeout(() => {
      const request = ++toolsRequest.current;
      setLoading(true);
      setError('');
      void loadTools()
        .then((nextTools) => {
          if (request === toolsRequest.current) setTools(nextTools);
        })
        .catch((requestError: unknown) => {
          if (request === toolsRequest.current) {
            setError(requestError instanceof Error ? requestError.message : 'Не удалось загрузить каталог.');
          }
        })
        .finally(() => {
          if (request === toolsRequest.current) {
            setLoading(false);
            setInitialLoaded(true);
          }
        });
    }, search ? 300 : 0);
    return () => clearTimeout(timeout);
  }, [loadTools, search]);

  useEffect(() => {
    let active = true;
    void AccessibilityInfo.isReduceMotionEnabled().then((reduceMotion) => {
      if (!active) return;
      listOpacity.setValue(reduceMotion ? 1 : 0.62);
      Animated.timing(listOpacity, {
        duration: reduceMotion ? 1 : 180,
        easing: Easing.out(Easing.cubic),
        toValue: 1,
        useNativeDriver: true,
      }).start();
    });
    return () => {
      active = false;
      listOpacity.stopAnimation();
    };
  }, [listOpacity, tools]);

  const refresh = useCallback(() => {
    if (refreshInFlight.current) return refreshInFlight.current;

    setRefreshing(true);
    setError('');
    const request = ++toolsRequest.current;
    const task = Promise.all([loadCategories(), loadTools()])
      .then(([, nextTools]) => {
        if (request === toolsRequest.current) setTools(nextTools);
      })
      .catch((requestError: unknown) => {
        setError(
          requestError instanceof Error
            ? requestError.message
            : 'Не удалось обновить каталог.',
        );
      })
      .finally(() => {
        if (refreshInFlight.current === task) refreshInFlight.current = null;
        setRefreshing(false);
      });
    refreshInFlight.current = task;
    return task;
  }, [loadCategories, loadTools]);

  const clearFilters = () => {
    setCategoryID('');
    setAvailableOnly(false);
  };

  const header = (
    <View style={styles.header}>
      {client && !documentsVerified ? (
        <Pressable
          accessibilityHint="Открывает раздел документов"
          accessibilityRole="button"
          onPress={() => router.push('/(app)/documents' as Href)}
          style={({ pressed }) => [
            styles.documentNotice,
            documentsPending && styles.documentNoticePending,
            pressed && styles.documentNoticePressed,
          ]}
        >
          <Ionicons
            color={documentsPending ? colors.warning : colors.error}
            name={documentsPending ? 'time-outline' : 'alert-circle-outline'}
            size={18}
          />
          <Text
            numberOfLines={2}
            style={[
              styles.documentNoticeText,
              documentsPending && styles.documentNoticeTextPending,
            ]}
          >
            {documentsPending ? 'Документы на проверке' : documentNotice}
          </Text>
          <Ionicons
            color={documentsPending ? colors.warning : colors.error}
            name="chevron-forward"
            size={17}
          />
        </Pressable>
      ) : null}
      <View style={styles.discovery}>
        <View style={styles.discoveryCopy}>
          <Text style={styles.catalogTitle}>Каталог</Text>
          <Text style={styles.catalogSubtitle}>Выберите инструмент и даты аренды</Text>
        </View>
        <View style={[styles.search, searchFocused && styles.searchFocused]}>
          <Ionicons color={colors.muted} name="search-outline" size={20} />
          <TextInput
            accessibilityLabel="Поиск по каталогу"
            onBlur={() => setSearchFocused(false)}
            onChangeText={setSearch}
            onFocus={() => setSearchFocused(true)}
            placeholder="Что нужно найти?"
            placeholderTextColor={colors.muted}
            returnKeyType="search"
            selectionColor={colors.primary}
            style={styles.searchInput}
            value={search}
          />
          {search ? (
            <Pressable
              accessibilityLabel="Очистить поиск"
              accessibilityRole="button"
              hitSlop={8}
              onPress={() => setSearch('')}
              style={({ pressed }) => [styles.searchAction, pressed && styles.iconPressed]}
            >
              <Ionicons color={colors.muted} name="close" size={20} />
            </Pressable>
          ) : null}
          <Pressable
            accessibilityLabel={`Фильтры${filterCount ? `, выбрано ${filterCount}` : ''}`}
            accessibilityRole="button"
            hitSlop={8}
            onPress={() => setFiltersVisible(true)}
            style={({ pressed }) => [styles.filterButton, pressed && styles.iconPressed]}
          >
            <Ionicons color={colors.primary} name="options-outline" size={22} />
            {filterCount ? <Text style={styles.filterCount}>{filterCount}</Text> : null}
          </Pressable>
        </View>
        {filterCount ? (
          <View style={styles.activeFilters}>
            <View style={styles.activeFiltersCopy}>
              <Ionicons color={colors.primary} name="funnel-outline" size={16} />
              <Text numberOfLines={1} style={styles.activeFiltersText}>
                {[selectedCategory?.name, availableOnly ? 'Только доступные' : '']
                  .filter(Boolean)
                  .join(' · ')}
              </Text>
            </View>
            <Pressable
              accessibilityLabel="Сбросить фильтры"
              onPress={clearFilters}
              style={styles.clearFilters}
            >
              <Text style={styles.clearFiltersText}>Сбросить</Text>
            </Pressable>
          </View>
        ) : null}
      </View>
      {error && tools.length > 0 ? <ErrorNotice message={error} /> : null}
      <View style={styles.resultsHeader}>
        <Text style={styles.resultsTitle}>Инструменты</Text>
        <Text style={styles.resultsCount}>{loading ? 'Обновляем…' : `${tools.length} позиций`}</Text>
      </View>
    </View>
  );

  return (
    <Page>
      {!initialLoaded && loading ? (
        <View style={styles.loadingContent}>
          {header}
          <CatalogGridSkeleton />
        </View>
      ) : (
        <Animated.View style={[styles.flex, { opacity: listOpacity }]}>
          <FlatList
          columnWrapperStyle={styles.column}
          contentContainerStyle={styles.list}
          data={tools}
          ItemSeparatorComponent={() => <View style={styles.separator} />}
          keyExtractor={(tool) => tool.id}
          ListEmptyComponent={
            loading ? (
              <View style={styles.inlineLoading}>
                <ActivityIndicator color={colors.primary} size="small" />
                <Text style={styles.inlineLoadingText}>Обновляем каталог…</Text>
              </View>
            ) : error ? (
              <StateView
                action={<Button label="Повторить" onPress={() => void refresh()} />}
                compact
                icon="cloud-offline-outline"
                message={error}
                title="Каталог не загрузился"
              />
            ) : (
              <StateView
                compact
                icon="search-outline"
                message="Измените запрос или параметры фильтра."
                title="Ничего не найдено"
              />
            )
          }
          ListHeaderComponent={header}
          numColumns={2}
          refreshControl={
            <RefreshControl
              onRefresh={() => void refresh()}
              refreshing={refreshing}
              tintColor={colors.primary}
            />
          }
          renderItem={({ item }) => (
            <ToolCard
              onPress={() =>
                router.push({
                  pathname: '/(app)/tools/[id]',
                  params: { id: item.id },
                } as Href)
              }
              tool={item}
            />
          )}
          />
        </Animated.View>
      )}

      <Modal
        animationType="slide"
        onRequestClose={() => setFiltersVisible(false)}
        transparent
        visible={filtersVisible}
      >
        <Pressable
          accessibilityLabel="Закрыть фильтры"
          onPress={() => setFiltersVisible(false)}
          style={styles.scrim}
        />
        <View style={styles.sheet}>
          <View style={styles.sheetHandle} />
          <View style={styles.sheetHeader}>
            <Text style={styles.sheetTitle}>Фильтры</Text>
            {filterCount ? (
              <Button label="Сбросить" onPress={clearFilters} variant="text" />
            ) : null}
          </View>
          <ScrollView
            contentContainerStyle={styles.sheetContent}
            showsVerticalScrollIndicator={false}
          >
            <Text style={styles.filterSectionTitle}>Категория</Text>
            <FilterOption
              active={!categoryID}
              label="Все категории"
              onPress={() => setCategoryID('')}
            />
            {categoriesLoading ? (
              <SkeletonGroup style={styles.categorySkeleton}>
                {[0, 1, 2].map((item) => (
                  <View key={item} style={styles.categorySkeletonRow}>
                    <SkeletonBlock height={16} width={`${72 - item * 9}%`} />
                    <SkeletonBlock height={22} radiusValue={radius.pill} width={22} />
                  </View>
                ))}
              </SkeletonGroup>
            ) : (
              categories.map((category) => (
                <FilterOption
                  active={category.id === categoryID}
                  key={category.id}
                  label={category.name}
                  onPress={() => setCategoryID(category.id)}
                />
              ))
            )}
            <View style={styles.availability}>
              <Checkbox
                checked={availableOnly}
                label="Показывать только доступные"
                onPress={() => setAvailableOnly((value) => !value)}
              />
            </View>
          </ScrollView>
          <Button label="Показать результаты" onPress={() => setFiltersVisible(false)} />
        </View>
      </Modal>
    </Page>
  );
}

function FilterOption({
  active,
  label,
  onPress,
}: {
  active: boolean;
  label: string;
  onPress: () => void;
}) {
  return (
    <Pressable
      accessibilityRole="radio"
      accessibilityState={{ checked: active }}
      onPress={onPress}
      style={({ pressed }) => [styles.filterOption, pressed && styles.rowPressed]}
    >
      <Text style={[styles.filterOptionLabel, active && styles.filterOptionLabelActive]}>
        {label}
      </Text>
      <View style={[styles.radio, active && styles.radioActive]}>
        {active ? <View style={styles.radioDot} /> : null}
      </View>
    </Pressable>
  );
}

const styles = StyleSheet.create({
  flex: { flex: 1 },
  list: {
    paddingHorizontal: spacing.xl,
    paddingTop: spacing.xl,
    paddingBottom: TAB_BAR_CLEARANCE,
  },
  header: { gap: spacing.lg, marginBottom: spacing.lg },
  documentNotice: {
    minHeight: 44,
    borderRadius: radius.md,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.sm,
    backgroundColor: colors.errorSoft,
  },
  documentNoticePending: { backgroundColor: colors.warningSoft },
  documentNoticePressed: { opacity: 0.68 },
  documentNoticeText: {
    flex: 1,
    color: colors.error,
    fontSize: 13,
    lineHeight: 18,
    fontWeight: '700',
  },
  documentNoticeTextPending: { color: colors.warning },
  discovery: {
    gap: spacing.md,
    padding: spacing.lg,
    borderRadius: radius.lg,
    backgroundColor: colors.surfaceSubtle,
  },
  discoveryCopy: { gap: spacing.xs },
  catalogTitle: {
    color: colors.ink,
    fontSize: 27,
    lineHeight: 33,
    fontWeight: '800',
    letterSpacing: -0.5,
  },
  catalogSubtitle: { color: colors.muted, fontSize: 15, lineHeight: 21 },
  search: {
    minHeight: 52,
    borderWidth: 1,
    borderColor: colors.outline,
    borderRadius: radius.button,
    paddingLeft: spacing.md,
    paddingRight: spacing.xs,
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.sm,
    backgroundColor: colors.surface,
  },
  searchFocused: {
    borderColor: colors.primary,
  },
  searchInput: { flex: 1, color: colors.ink, fontSize: 16, lineHeight: 21 },
  searchAction: { width: 36, height: 44, alignItems: 'center', justifyContent: 'center' },
  filterButton: {
    minWidth: 44,
    height: 44,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: spacing.xs,
  },
  filterCount: {
    color: colors.primary,
    fontSize: 11,
    lineHeight: 14,
    fontWeight: '800',
    fontVariant: ['tabular-nums'],
  },
  iconPressed: { opacity: 0.55 },
  activeFilters: {
    minHeight: 44,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: spacing.sm,
    paddingLeft: spacing.md,
    paddingRight: spacing.xs,
    borderRadius: radius.md,
    backgroundColor: colors.primarySoft,
  },
  activeFiltersCopy: { flex: 1, flexDirection: 'row', alignItems: 'center', gap: spacing.sm },
  activeFiltersText: { flex: 1, color: colors.primary, fontSize: 13, lineHeight: 18 },
  clearFilters: { minHeight: 44, justifyContent: 'center', paddingHorizontal: spacing.sm },
  clearFiltersText: { color: colors.primary, fontSize: 13, lineHeight: 18, fontWeight: '700' },
  resultsHeader: {
    minHeight: 28,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: spacing.md,
  },
  resultsTitle: { color: colors.ink, fontSize: 19, lineHeight: 24, fontWeight: '800' },
  resultsCount: {
    color: colors.muted,
    fontSize: 13,
    lineHeight: 18,
    fontVariant: ['tabular-nums'],
  },
  inlineLoading: {
    minHeight: 120,
    alignItems: 'center',
    justifyContent: 'center',
    gap: spacing.md,
  },
  inlineLoadingText: { color: colors.muted, fontSize: 14, lineHeight: 20 },
  column: { gap: spacing.md },
  separator: { height: spacing.md },
  loadingContent: {
    paddingHorizontal: spacing.xl,
    paddingTop: spacing.xl,
    paddingBottom: TAB_BAR_CLEARANCE,
    gap: spacing.md,
  },
  categorySkeleton: { gap: spacing.xs },
  categorySkeletonRow: {
    minHeight: 48,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: spacing.lg,
  },
  scrim: {
    position: 'absolute',
    top: 0,
    right: 0,
    bottom: 0,
    left: 0,
    backgroundColor: colors.scrim,
  },
  sheet: {
    position: 'absolute',
    right: 0,
    bottom: 0,
    left: 0,
    maxHeight: '82%',
    paddingHorizontal: spacing.xl,
    paddingTop: spacing.sm,
    paddingBottom: spacing.xl,
    borderTopLeftRadius: radius.lg,
    borderTopRightRadius: radius.lg,
    backgroundColor: colors.surface,
  },
  sheetHandle: {
    width: 40,
    height: 4,
    alignSelf: 'center',
    borderRadius: 2,
    backgroundColor: colors.surfaceStrong,
  },
  sheetHeader: {
    minHeight: 60,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
  },
  sheetTitle: { color: colors.ink, fontSize: 22, lineHeight: 28, fontWeight: '800' },
  sheetContent: { paddingBottom: spacing.lg },
  filterSectionTitle: {
    color: colors.muted,
    fontSize: 13,
    lineHeight: 18,
    fontWeight: '700',
    marginBottom: spacing.xs,
  },
  filterOption: {
    minHeight: 48,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: spacing.lg,
  },
  rowPressed: { opacity: 0.6 },
  filterOptionLabel: { flex: 1, color: colors.ink, fontSize: 16, lineHeight: 22 },
  filterOptionLabelActive: { fontWeight: '700' },
  radio: {
    width: 22,
    height: 22,
    borderRadius: 11,
    borderWidth: 1.5,
    borderColor: colors.outline,
    alignItems: 'center',
    justifyContent: 'center',
  },
  radioActive: { borderColor: colors.primary },
  radioDot: { width: 12, height: 12, borderRadius: 6, backgroundColor: colors.primary },
  availability: {
    marginTop: spacing.md,
    paddingTop: spacing.md,
    borderTopWidth: StyleSheet.hairlineWidth,
    borderTopColor: colors.outline,
  },
});
