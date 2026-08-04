import { useRouter, type Href } from 'expo-router';
import { useRef, useState } from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { useSession } from '../../../src/auth/session';
import {
  Body,
  Button,
  Checkbox,
  ErrorNotice,
  ScrollPage,
  Title,
} from '../../../src/components/ui';
import { offerDate } from '../../../src/content/legal';
import { colors, radius, spacing } from '../../../src/theme/tokens';

export default function OfferScreen() {
  const router = useRouter();
  const { acceptConsents } = useSession();
  const [isOfferAccepted, setIsOfferAccepted] = useState(false);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState('');
  const submissionInFlight = useRef(false);

  const toggleAcceptance = () => {
    if (!submissionInFlight.current) {
      setIsOfferAccepted((current) => !current);
    }
  };

  const openOffer = () => {
    router.push('/(app)/onboarding/offer-document' as Href);
  };

  const accept = async () => {
    if (!isOfferAccepted || submissionInFlight.current) return;

    submissionInFlight.current = true;
    setIsSubmitting(true);
    setError('');

    try {
      await acceptConsents();
      // AppLayout performs the single transition after session state is updated.
    } catch (requestError) {
      setError(
        requestError instanceof Error
          ? requestError.message
          : 'Не удалось сохранить согласие.',
      );
    } finally {
      submissionInFlight.current = false;
      setIsSubmitting(false);
    }
  };

  const acceptanceLabel = (
    <Text style={styles.acceptanceText}>
      Я принимаю{' '}
      <Text
        accessibilityHint="Открывает полный текст оферты"
        accessibilityRole="link"
        onPress={openOffer}
        style={styles.offerLink}
      >
        условия оферты
      </Text>
    </Text>
  );

  return (
    <ScrollPage contentStyle={styles.content}>
      <View style={styles.progress}>
        <View style={[styles.progressPart, styles.progressDone]} />
        <View style={styles.progressPart} />
      </View>

      <View style={styles.heading}>
        <Title compact>Условия использования</Title>
        <Body muted>
          Ознакомьтесь с офертой и подтвердите согласие, чтобы перейти к анкете.
        </Body>
      </View>

      <View style={styles.documentMeta}>
        <Text style={styles.documentTitle}>Публичная оферта</Text>
        <Text style={styles.documentDate}>Редакция от {offerDate}</Text>
      </View>

      <View style={styles.actions}>
        {error ? <ErrorNotice message={error} /> : null}
        <Checkbox
          checked={isOfferAccepted}
          label={acceptanceLabel}
          onPress={toggleAcceptance}
        />

        <Button
          disabled={!isOfferAccepted || isSubmitting}
          label="Продолжить"
          loading={isSubmitting}
          onPress={() => void accept()}
        />
      </View>
    </ScrollPage>
  );
}

const styles = StyleSheet.create({
  content: {
    flexGrow: 1,
    gap: spacing.xl,
    paddingTop: spacing.xl,
    paddingBottom: spacing.xl,
  },
  progress: {
    flexDirection: 'row',
    gap: spacing.xs,
  },
  progressPart: {
    flex: 1,
    height: 4,
    borderRadius: 2,
    backgroundColor: colors.surfaceStrong,
  },
  progressDone: { backgroundColor: colors.primary },
  heading: {
    gap: spacing.sm,
  },
  documentMeta: {
    gap: spacing.xs,
    padding: spacing.lg,
    borderRadius: radius.lg,
    backgroundColor: colors.surfaceSubtle,
  },
  documentTitle: {
    color: colors.ink,
    fontSize: 16,
    lineHeight: 22,
    fontWeight: '700',
  },
  documentDate: { color: colors.muted, fontSize: 13, lineHeight: 18 },
  actions: {
    gap: spacing.md,
  },
  acceptanceText: { color: colors.ink, fontSize: 16, lineHeight: 23 },
  offerLink: {
    color: colors.primary,
    fontSize: 16,
    lineHeight: 23,
    fontWeight: '700',
    textDecorationLine: 'underline',
  },
});
