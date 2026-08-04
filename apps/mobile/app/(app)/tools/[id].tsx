import { Ionicons } from '@expo/vector-icons';
import { useLocalSearchParams, useRouter, type Href } from 'expo-router';
import { useCallback, useEffect, useRef, useState } from 'react';
import {
  Image,
  ScrollView,
  StyleSheet,
  Text,
  useWindowDimensions,
  View,
} from 'react-native';
import { getRentalEligibility } from '../../../src/api/auth';
import { getTool } from '../../../src/api/catalog';
import { resolveAPIAssetURL } from '../../../src/api/client';
import { useSession } from '../../../src/auth/session';
import {
  Body,
  Button,
  ErrorNotice,
  Page,
  ScrollPage,
  StateView,
  StatusPill,
  Title,
} from '../../../src/components/ui';
import { RentalCheckout } from '../../../src/components/rental-checkout';
import {
  SkeletonBlock,
  SkeletonGroup,
  ToolDetailSkeleton,
} from '../../../src/components/skeleton';
import { colors, radius, spacing } from '../../../src/theme/tokens';
import type { StructuredValue, Tool } from '../../../src/types/api';
import {
  displayValue,
  formatMoney,
  formatSpecificationLabel,
  formatSpecificationValue,
} from '../../../src/utils/format';

export default function ToolDetailScreen() {
  const router = useRouter();
  const { token } = useSession();
  const { id } = useLocalSearchParams<{ id: string }>();
  const [tool, setTool] = useState<Tool | null>(null);
  const [loading, setLoading] = useState(true);
  const [rentLoading, setRentLoading] = useState(false);
  const [checkoutOpen, setCheckoutOpen] = useState(false);
  const [error, setError] = useState('');
  const rentalCheckInFlight = useRef(false);

  const load = useCallback(async () => {
    if (!id) return;
    setLoading(true);
    setError('');
    try {
      setTool(await getTool(id));
    } catch (requestError) {
      setError(
        requestError instanceof Error
          ? requestError.message
          : 'Не удалось загрузить инструмент.',
      );
    } finally {
      setLoading(false);
    }
  }, [id]);

  useEffect(() => {
    const timeout = setTimeout(() => void load(), 0);
    return () => clearTimeout(timeout);
  }, [load]);

  if (loading) {
    return (
      <Page>
        <ToolDetailSkeleton />
      </Page>
    );
  }

  if (!tool) {
    return (
      <Page>
        <StateView
          action={<Button label="Повторить" onPress={() => void load()} />}
          icon="cloud-offline-outline"
          message={error || 'Инструмент не найден.'}
          title="Не удалось загрузить инструмент"
        />
      </Page>
    );
  }

  const available = tool.available_units > 0;
  const startRental = async () => {
    if (!token || rentalCheckInFlight.current) return;
    rentalCheckInFlight.current = true;
    setRentLoading(true);
    setError('');
    try {
      const eligibility = await getRentalEligibility(token);
      if (eligibility.eligible) {
        setCheckoutOpen(true);
      } else {
        router.push({
          pathname: '/(app)/rental-unavailable',
          params: { verificationReason: eligibility.reason },
        } as Href);
      }
    } catch (requestError) {
      setError(
        requestError instanceof Error
          ? requestError.message
          : 'Не удалось проверить возможность аренды.',
      );
    } finally {
      rentalCheckInFlight.current = false;
      setRentLoading(false);
    }
  };

  return (
    <ScrollPage contentStyle={styles.content}>
      <ToolGallery
        imageURLs={(tool.image_urls ?? []).map(resolveAPIAssetURL)}
        name={tool.name}
      />

      <View style={styles.heading}>
        <StatusPill
          label={available ? `Доступно: ${tool.available_units}` : 'Нет в наличии'}
          tone={available ? 'success' : 'warning'}
        />
        <Title>{tool.name}</Title>
        {tool.description || tool.short_description ? (
          <Body muted>{tool.description || tool.short_description}</Body>
        ) : null}
      </View>

      <View style={styles.commercial}>
        <View style={styles.priceBlock}>
          <Text style={styles.price}>{formatMoney(tool.daily_price)}</Text>
          <Text style={styles.caption}>за сутки</Text>
        </View>
        <View style={styles.priceRight}>
          <Text style={styles.deposit}>{formatMoney(tool.deposit_amount)}</Text>
          <Text style={styles.caption}>возвратный залог</Text>
        </View>
      </View>

      <DetailSection title="Характеристики" value={tool.specifications} />
      <EquipmentSection value={tool.equipment} />
      {error ? <ErrorNotice message={error} /> : null}
      {checkoutOpen && token ? (
        <RentalCheckout
          onCreated={(rental) =>
            router.replace(`/(app)/rentals/${rental.id}` as Href)
          }
          token={token}
          toolID={tool.id}
          toolImageURL={resolveAPIAssetURL(tool.image_urls?.[0] ?? '')}
          toolName={tool.name}
        />
      ) : (
        <Button
          disabled={!available}
          label={available ? 'Оформить аренду' : 'Сейчас недоступно'}
          loading={rentLoading}
          onPress={() => void startRental()}
        />
      )}
    </ScrollPage>
  );
}

function ToolGallery({ imageURLs, name }: { imageURLs: string[]; name: string }) {
  const { width } = useWindowDimensions();
  const imageWidth = Math.max(240, width - spacing.xl * 2);

  if (imageURLs.length === 0) {
    return (
      <View style={[styles.galleryPlaceholder, { width: imageWidth }]}>
        <View style={styles.galleryMark}>
          <Ionicons color={colors.primary} name="construct-outline" size={42} />
        </View>
        <Text style={styles.galleryPlaceholderTitle}>Фото скоро появится</Text>
        <Text style={styles.galleryPlaceholderText}>Про Инструмент</Text>
      </View>
    );
  }

  return (
    <ScrollView
      contentContainerStyle={styles.gallery}
      decelerationRate="fast"
      horizontal
      pagingEnabled
      showsHorizontalScrollIndicator={false}
    >
      {imageURLs.map((imageURL, index) => (
        <GalleryImage
          imageURL={imageURL}
          key={`${imageURL}-${index}`}
          name={name}
          width={imageWidth}
        />
      ))}
    </ScrollView>
  );
}

function GalleryImage({
  imageURL,
  name,
  width,
}: {
  imageURL: string;
  name: string;
  width: number;
}) {
  const [failed, setFailed] = useState(false);
  const [imageLoading, setImageLoading] = useState(true);
  if (failed) {
    return (
      <View style={[styles.galleryPlaceholder, { width }]}>
        <Ionicons color={colors.primary} name="image-outline" size={42} />
        <Text style={styles.galleryPlaceholderTitle}>Фото недоступно</Text>
      </View>
    );
  }
  return (
    <View style={[styles.galleryImage, { width }]}>
      <Image
        accessibilityLabel={`${name}, фото`}
        onError={() => {
          setFailed(true);
          setImageLoading(false);
        }}
        onLoadEnd={() => setImageLoading(false)}
        resizeMode="cover"
        source={{ uri: imageURL }}
        style={styles.galleryImageContent}
      />
      {imageLoading ? (
        <SkeletonGroup style={styles.galleryImageSkeleton}>
          <SkeletonBlock height="100%" radiusValue={0} />
        </SkeletonGroup>
      ) : null}
    </View>
  );
}

function DetailSection({ title, value }: { title: string; value: StructuredValue }) {
  const entries =
    value && typeof value === 'object' && !Array.isArray(value)
      ? Object.entries(value)
      : [['', value] as [string, StructuredValue]];

  return (
    <View style={styles.section}>
      <Text style={styles.sectionTitle}>{title}</Text>
      <View style={styles.detailGrid}>
        {entries.map(([key, nested], index) => (
          <View key={`${key}-${index}`} style={styles.detailItem}>
            {key ? (
              <Text style={styles.rowLabel}>{formatSpecificationLabel(key)}</Text>
            ) : null}
            <Text style={styles.rowValue}>
              {key ? formatSpecificationValue(key, nested) : displayValue(nested)}
            </Text>
          </View>
        ))}
      </View>
    </View>
  );
}

function EquipmentSection({ value }: { value: StructuredValue }) {
  const items = Array.isArray(value)
    ? value
    : value && typeof value === 'object'
      ? Object.entries(value).map(([key, nested]) => `${key}: ${displayValue(nested)}`)
      : [value];

  return (
    <View style={styles.section}>
      <Text style={styles.sectionTitle}>Комплектация</Text>
      <View style={styles.equipmentList}>
        {items.map((item, index) => (
          <View key={index} style={styles.equipmentRow}>
            <Ionicons color={colors.success} name="checkmark-circle" size={20} />
            <Text style={styles.equipmentText}>{displayValue(item)}</Text>
          </View>
        ))}
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  content: { gap: spacing.xl },
  gallery: { gap: spacing.sm },
  galleryImage: {
    aspectRatio: 1.22,
    overflow: 'hidden',
    borderRadius: radius.lg,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: colors.imageOutline,
    backgroundColor: colors.surfaceSubtle,
  },
  galleryImageContent: { width: '100%', height: '100%' },
  galleryImageSkeleton: {
    position: 'absolute',
    top: 0,
    right: 0,
    bottom: 0,
    left: 0,
  },
  galleryPlaceholder: {
    aspectRatio: 1.22,
    borderRadius: radius.lg,
    alignItems: 'center',
    justifyContent: 'center',
    gap: spacing.sm,
    backgroundColor: colors.surfaceSubtle,
  },
  galleryMark: {
    width: 72,
    height: 72,
    borderRadius: radius.lg,
    alignItems: 'center',
    justifyContent: 'center',
    backgroundColor: colors.primarySoft,
  },
  galleryPlaceholderTitle: { color: colors.ink, fontSize: 15, lineHeight: 20, fontWeight: '700' },
  galleryPlaceholderText: { color: colors.muted, fontSize: 12, lineHeight: 17 },
  heading: { gap: spacing.md },
  commercial: {
    minHeight: 88,
    borderRadius: radius.lg,
    padding: spacing.lg,
    flexDirection: 'row',
    flexWrap: 'wrap',
    justifyContent: 'space-between',
    alignItems: 'center',
    gap: spacing.md,
    backgroundColor: colors.primary,
  },
  priceBlock: { flexGrow: 1, minWidth: 120 },
  priceRight: { flexGrow: 1, minWidth: 120, alignItems: 'flex-end' },
  price: {
    color: colors.white,
    fontSize: 26,
    lineHeight: 32,
    fontWeight: '800',
    fontVariant: ['tabular-nums'],
  },
  deposit: {
    color: colors.white,
    fontSize: 17,
    lineHeight: 22,
    fontWeight: '700',
    fontVariant: ['tabular-nums'],
  },
  caption: { color: colors.primarySoft, fontSize: 12, lineHeight: 16, marginTop: spacing.xs },
  section: { gap: spacing.sm },
  sectionTitle: { color: colors.ink, fontSize: 19, lineHeight: 24, fontWeight: '800' },
  detailGrid: {
    overflow: 'hidden',
    borderRadius: radius.md,
    backgroundColor: colors.surfaceSubtle,
  },
  detailItem: {
    minHeight: 54,
    paddingHorizontal: spacing.lg,
    paddingVertical: spacing.md,
    gap: spacing.xs,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderBottomColor: colors.outline,
  },
  rowLabel: { color: colors.muted, fontSize: 12, lineHeight: 16, fontWeight: '600' },
  rowValue: { color: colors.ink, fontSize: 15, lineHeight: 21, fontWeight: '600' },
  equipmentList: {
    borderRadius: radius.md,
    paddingHorizontal: spacing.lg,
    backgroundColor: colors.surfaceSubtle,
  },
  equipmentRow: {
    minHeight: 48,
    paddingVertical: spacing.sm,
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.md,
  },
  equipmentText: { flex: 1, color: colors.ink, fontSize: 15, lineHeight: 21 },
});
