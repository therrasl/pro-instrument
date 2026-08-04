import {
  AccessibilityInfo,
  Animated,
  Easing,
  StyleSheet,
  View,
  type DimensionValue,
  type StyleProp,
  type ViewStyle,
} from 'react-native';
import {
  createContext,
  useContext,
  useEffect,
  useState,
  type PropsWithChildren,
} from 'react';
import { colors, radius, spacing } from '../theme/tokens';

const SkeletonOpacityContext = createContext<Animated.Value | null>(null);

export function SkeletonGroup({
  children,
  style,
}: PropsWithChildren<{ style?: StyleProp<ViewStyle> }>) {
  const [opacity] = useState(() => new Animated.Value(0.62));

  useEffect(() => {
    let active = true;
    let animation: Animated.CompositeAnimation | null = null;

    void AccessibilityInfo.isReduceMotionEnabled().then((reduceMotion) => {
      if (!active) return;
      if (reduceMotion) {
        opacity.setValue(0.76);
        return;
      }

      animation = Animated.loop(
        Animated.sequence([
          Animated.timing(opacity, {
            duration: 720,
            easing: Easing.inOut(Easing.cubic),
            toValue: 0.94,
            useNativeDriver: true,
          }),
          Animated.timing(opacity, {
            duration: 720,
            easing: Easing.inOut(Easing.cubic),
            toValue: 0.62,
            useNativeDriver: true,
          }),
        ]),
      );
      animation.start();
    });

    return () => {
      active = false;
      animation?.stop();
      opacity.stopAnimation();
    };
  }, [opacity]);

  return (
    <SkeletonOpacityContext.Provider value={opacity}>
      <View
        accessibilityLabel="Загрузка"
        accessibilityRole="progressbar"
        style={style}
      >
        {children}
      </View>
    </SkeletonOpacityContext.Provider>
  );
}

export function SkeletonBlock({
  height,
  radiusValue = radius.sm,
  style,
  width = '100%',
}: {
  height?: DimensionValue;
  radiusValue?: number;
  style?: StyleProp<ViewStyle>;
  width?: DimensionValue;
}) {
  const opacity = useContext(SkeletonOpacityContext);

  return (
    <Animated.View
      style={[
        styles.block,
        {
          borderRadius: radiusValue,
          height,
          opacity: opacity ?? 0.76,
          width,
        },
        style,
      ]}
    />
  );
}

export function CatalogGridSkeleton() {
  return (
    <SkeletonGroup style={styles.grid}>
      {[0, 1, 2, 3].map((item) => (
        <View key={item} style={styles.catalogCard}>
          <SkeletonBlock radiusValue={0} style={styles.catalogImage} />
          <View style={styles.catalogBody}>
            <SkeletonBlock height={18} width="84%" />
            <SkeletonBlock height={12} width="62%" />
            <SkeletonBlock height={22} width="48%" />
          </View>
        </View>
      ))}
    </SkeletonGroup>
  );
}

export function SessionSkeleton() {
  return (
    <SkeletonGroup style={styles.session}>
      <View style={styles.sessionMark}>
        <SkeletonBlock height={56} radiusValue={radius.lg} width={56} />
      </View>
      <SkeletonBlock height={26} width="64%" />
      <SkeletonBlock height={16} width="82%" />
      <SkeletonBlock height={16} width="56%" />
    </SkeletonGroup>
  );
}

export function ToolDetailSkeleton() {
  return (
    <SkeletonGroup style={styles.screen}>
      <SkeletonBlock height={264} radiusValue={radius.lg} />
      <View style={styles.stack}>
        <SkeletonBlock height={24} width="34%" />
        <SkeletonBlock height={32} width="88%" />
        <SkeletonBlock height={16} width="100%" />
        <SkeletonBlock height={16} width="72%" />
      </View>
      <SkeletonBlock height={88} radiusValue={radius.lg} />
      <View style={styles.stack}>
        <SkeletonBlock height={24} width="46%" />
        <SkeletonBlock height={164} radiusValue={radius.md} />
      </View>
      <SkeletonBlock height={52} radiusValue={radius.button} />
    </SkeletonGroup>
  );
}

export function DocumentsSkeleton() {
  return (
    <SkeletonGroup style={styles.screen}>
      <SkeletonBlock height={88} radiusValue={radius.lg} />
      <View style={styles.sectionHeading}>
        <SkeletonBlock height={22} width="54%" />
        <SkeletonBlock height={18} width={42} />
      </View>
      {[0, 1, 2].map((item) => (
        <View key={item} style={styles.documentCard}>
          <SkeletonBlock height={20} width={item === 1 ? '72%' : '58%'} />
          <SkeletonBlock height={44} radiusValue={radius.button} />
        </View>
      ))}
      <SkeletonBlock height={68} radiusValue={radius.md} />
    </SkeletonGroup>
  );
}

export function RentalsSkeleton() {
  return (
    <SkeletonGroup style={styles.screen}>
      <View style={styles.stack}>
        <SkeletonBlock height={34} width="58%" />
        <SkeletonBlock height={16} width="82%" />
      </View>
      <View style={styles.sectionHeading}>
        <SkeletonBlock height={22} width="34%" />
        <SkeletonBlock height={24} width={28} />
      </View>
      {[0, 1, 2].map((item) => (
        <View key={item} style={styles.rentalCard}>
          <SkeletonBlock radiusValue={0} style={styles.rentalImage} />
          <View style={styles.rentalContent}>
            <SkeletonBlock height={12} width="54%" />
            <SkeletonBlock height={22} width={item === 1 ? '94%' : '78%'} />
            <View style={styles.rentalAmount}>
              <SkeletonBlock height={16} width="44%" />
              <SkeletonBlock height={18} width="38%" />
            </View>
            <SkeletonBlock height={14} width="64%" />
            <SkeletonBlock height={14} width="42%" />
          </View>
        </View>
      ))}
    </SkeletonGroup>
  );
}

export function RentalDetailSkeleton() {
  return (
    <SkeletonGroup style={styles.screen}>
      <View style={styles.stack}>
        <SkeletonBlock height={18} width="42%" />
        <SkeletonBlock height={34} width="76%" />
        <SkeletonBlock height={16} width="100%" />
        <SkeletonBlock height={16} width="68%" />
      </View>
      <View style={styles.stack}>
        <SkeletonBlock height={24} width="44%" />
        <View style={styles.timelineCard}>
          {[0, 1, 2, 3, 4].map((item) => (
            <View key={item} style={styles.timelineRow}>
              <SkeletonBlock height={20} radiusValue={radius.pill} width={20} />
              <SkeletonBlock
                height={16}
                width={item === 3 ? '58%' : item === 1 ? '76%' : '68%'}
              />
            </View>
          ))}
        </View>
      </View>
      <SkeletonBlock height={96} radiusValue={radius.lg} />
      <SkeletonBlock height={178} radiusValue={radius.lg} />
      <SkeletonBlock height={188} radiusValue={radius.lg} />
      <SkeletonBlock height={52} radiusValue={radius.button} />
    </SkeletonGroup>
  );
}

export function ProfileSkeleton() {
  return (
    <SkeletonGroup style={styles.screen}>
      <View style={styles.profileHeader}>
        <SkeletonBlock height={34} width="40%" />
        <SkeletonBlock height={44} width={104} />
      </View>
      <View style={styles.profileIdentity}>
        <SkeletonBlock height={56} radiusValue={radius.lg} width={56} />
        <View style={styles.profileCopy}>
          <SkeletonBlock height={22} width="76%" />
          <SkeletonBlock height={16} width="54%" />
        </View>
      </View>
      <SkeletonBlock height={198} radiusValue={radius.lg} />
      <SkeletonBlock height={48} width="54%" />
    </SkeletonGroup>
  );
}

const styles = StyleSheet.create({
  block: { backgroundColor: colors.surfaceStrong },
  screen: {
    flex: 1,
    paddingHorizontal: spacing.xl,
    paddingTop: spacing.xl,
    paddingBottom: spacing.hero,
    gap: spacing.lg,
  },
  stack: { gap: spacing.sm },
  grid: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: spacing.md,
  },
  catalogCard: {
    flexGrow: 1,
    flexBasis: '46%',
    overflow: 'hidden',
    borderRadius: radius.lg,
    backgroundColor: colors.surface,
  },
  catalogBody: { padding: spacing.md, gap: spacing.sm },
  catalogImage: { width: '100%', aspectRatio: 1.08 },
  session: {
    flex: 1,
    alignItems: 'center',
    justifyContent: 'center',
    paddingHorizontal: spacing.xl,
    gap: spacing.md,
  },
  sessionMark: { marginBottom: spacing.xs },
  sectionHeading: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: spacing.md,
  },
  documentCard: {
    borderRadius: radius.lg,
    padding: spacing.lg,
    gap: spacing.md,
    backgroundColor: colors.surfaceSubtle,
  },
  rentalCard: {
    minHeight: 144,
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.md,
    overflow: 'hidden',
    borderRadius: radius.lg,
    padding: spacing.sm,
    backgroundColor: colors.surface,
  },
  rentalImage: {
    width: 120,
    aspectRatio: 4 / 3,
    alignSelf: 'center',
    flexShrink: 0,
    borderRadius: radius.md,
  },
  rentalContent: {
    minWidth: 0,
    flex: 1,
    gap: spacing.xs,
  },
  rentalAmount: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: spacing.md,
  },
  timelineCard: {
    borderRadius: radius.lg,
    paddingHorizontal: spacing.lg,
    paddingVertical: spacing.sm,
    gap: spacing.xs,
    backgroundColor: colors.surfaceSubtle,
  },
  timelineRow: {
    minHeight: 48,
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.md,
  },
  profileHeader: {
    minHeight: 44,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: spacing.md,
  },
  profileIdentity: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.lg,
  },
  profileCopy: { flex: 1, gap: spacing.sm },
});
