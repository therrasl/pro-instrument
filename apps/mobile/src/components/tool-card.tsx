import { Ionicons } from '@expo/vector-icons';
import { useEffect, useRef, useState } from 'react';
import {
  AccessibilityInfo,
  Animated,
  Easing,
  Image,
  Pressable,
  StyleSheet,
  Text,
  View,
} from 'react-native';
import { resolveAPIAssetURL } from '../api/client';
import { SkeletonBlock, SkeletonGroup } from './skeleton';
import type { Tool } from '../types/api';
import { formatMoney } from '../utils/format';
import { colors, radius, shadow, spacing } from '../theme/tokens';

export function ToolCard({ tool, onPress }: { tool: Tool; onPress: () => void }) {
  const available = tool.available_units > 0;
  const imageURL = resolveAPIAssetURL(tool.image_urls?.[0] ?? '');
  const [failedImageURL, setFailedImageURL] = useState('');
  const [imageLoading, setImageLoading] = useState(Boolean(imageURL));
  const imageFailed = failedImageURL === imageURL;
  const [entrance] = useState(() => new Animated.Value(0));
  const [pressScale] = useState(() => new Animated.Value(1));
  const reduceMotion = useRef(false);

  useEffect(() => {
    let active = true;
    void AccessibilityInfo.isReduceMotionEnabled().then((value) => {
      if (!active) return;
      reduceMotion.current = value;
      Animated.timing(entrance, {
        duration: value ? 1 : 220,
        easing: Easing.out(Easing.cubic),
        toValue: 1,
        useNativeDriver: true,
      }).start();
    });
    return () => {
      active = false;
      entrance.stopAnimation();
      pressScale.stopAnimation();
    };
  }, [entrance, pressScale]);

  const animatePress = (toValue: number) => {
    Animated.timing(pressScale, {
      duration: reduceMotion.current ? 1 : toValue < 1 ? 90 : 150,
      easing: Easing.out(Easing.cubic),
      toValue,
      useNativeDriver: true,
    }).start();
  };

  return (
    <Animated.View
      style={[
        styles.animatedCard,
        {
          opacity: entrance,
          transform: [
            {
              translateY: entrance.interpolate({
                inputRange: [0, 1],
                outputRange: [8, 0],
              }),
            },
            { scale: pressScale },
          ],
        },
      ]}
    >
      <Pressable
        accessibilityHint="Открывает карточку инструмента"
        accessibilityRole="button"
        onPress={onPress}
        onPressIn={() => animatePress(0.96)}
        onPressOut={() => animatePress(1)}
        style={styles.card}
      >
        <View style={styles.preview}>
          {imageURL && !imageFailed ? (
            <Image
              accessibilityIgnoresInvertColors
              onError={() => {
                setFailedImageURL(imageURL);
                setImageLoading(false);
              }}
              onLoadEnd={() => setImageLoading(false)}
              resizeMode="cover"
              source={{ uri: imageURL }}
              style={styles.image}
            />
          ) : (
            <View style={styles.placeholder}>
              <View style={styles.placeholderMark}>
                <Ionicons color={colors.primary} name="construct-outline" size={28} />
              </View>
              <Text style={styles.placeholderText}>Про Инструмент</Text>
            </View>
          )}
          {imageURL && !imageFailed && imageLoading ? (
            <SkeletonGroup style={styles.imageSkeleton}>
              <SkeletonBlock height="100%" radiusValue={0} />
            </SkeletonGroup>
          ) : null}
          <View style={[styles.availabilityBadge, !available && styles.unavailableBadge]}>
            <View style={[styles.statusDot, !available && styles.statusDotUnavailable]} />
            <Text style={[styles.availabilityText, !available && styles.unavailableText]}>
              {available ? 'В наличии' : 'Недоступно'}
            </Text>
          </View>
        </View>

        <View style={styles.content}>
          <Text numberOfLines={2} style={styles.name}>
            {tool.name}
          </Text>
          {tool.short_description ? (
            <Text numberOfLines={2} style={styles.description}>
              {tool.short_description}
            </Text>
          ) : null}
          <View style={styles.priceRow}>
            <View style={styles.priceCopy}>
              <Text numberOfLines={1} style={styles.price}>
                {formatMoney(tool.daily_price)}
              </Text>
              <Text style={styles.caption}>/ сутки</Text>
            </View>
            <View style={styles.action}>
              <Ionicons color={colors.primary} name="chevron-forward" size={16} />
            </View>
          </View>
        </View>
      </Pressable>
    </Animated.View>
  );
}

const styles = StyleSheet.create({
  animatedCard: { flex: 1, minWidth: 0 },
  card: {
    flex: 1,
    minWidth: 0,
    overflow: 'hidden',
    borderRadius: radius.lg,
    backgroundColor: colors.surface,
    ...shadow,
  },
  preview: {
    width: '100%',
    aspectRatio: 1.08,
    overflow: 'hidden',
    backgroundColor: colors.surfaceSubtle,
  },
  imageSkeleton: {
    position: 'absolute',
    top: 0,
    right: 0,
    bottom: 0,
    left: 0,
  },
  image: {
    width: '100%',
    height: '100%',
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: colors.imageOutline,
  },
  placeholder: {
    flex: 1,
    alignItems: 'center',
    justifyContent: 'center',
    gap: spacing.sm,
    padding: spacing.md,
  },
  placeholderMark: {
    width: 52,
    height: 52,
    borderRadius: radius.md,
    alignItems: 'center',
    justifyContent: 'center',
    backgroundColor: colors.primarySoft,
  },
  placeholderText: {
    color: colors.primary,
    fontSize: 10,
    lineHeight: 14,
    fontWeight: '700',
    textAlign: 'center',
  },
  availabilityBadge: {
    position: 'absolute',
    left: spacing.sm,
    bottom: spacing.sm,
    minHeight: 26,
    borderRadius: radius.sm,
    paddingHorizontal: spacing.sm,
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.xs,
    backgroundColor: colors.successSoft,
  },
  unavailableBadge: { backgroundColor: colors.warningSoft },
  statusDot: { width: 6, height: 6, borderRadius: 3, backgroundColor: colors.success },
  statusDotUnavailable: { backgroundColor: colors.warning },
  availabilityText: { color: colors.success, fontSize: 10, lineHeight: 14, fontWeight: '800' },
  unavailableText: { color: colors.warning },
  content: { flex: 1, padding: spacing.md, gap: spacing.sm },
  name: { color: colors.ink, fontSize: 15, lineHeight: 20, fontWeight: '800' },
  description: { color: colors.muted, fontSize: 12, lineHeight: 17 },
  priceRow: {
    marginTop: 'auto',
    paddingTop: spacing.sm,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: spacing.xs,
  },
  priceCopy: { flexShrink: 1 },
  price: {
    flexShrink: 1,
    color: colors.ink,
    fontSize: 17,
    lineHeight: 22,
    fontWeight: '800',
    fontVariant: ['tabular-nums'],
  },
  caption: { color: colors.muted, fontSize: 11, lineHeight: 15 },
  action: {
    width: 32,
    height: 32,
    borderRadius: radius.sm,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    backgroundColor: colors.primarySoft,
  },
});
