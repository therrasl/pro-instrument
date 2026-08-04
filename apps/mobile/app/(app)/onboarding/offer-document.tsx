import { StyleSheet, Text, View } from 'react-native';
import { Body, ScrollPage, Title } from '../../../src/components/ui';
import { offerDate, offerSections } from '../../../src/content/legal';
import { colors, radius, spacing } from '../../../src/theme/tokens';

export default function OfferDocumentScreen() {
  return (
    <ScrollPage contentStyle={styles.content}>
      <View style={styles.heading}>
        <Title compact>Публичная оферта</Title>
        <Body muted>Редакция от {offerDate}</Body>
      </View>
      {offerSections.map((section) => (
        <View key={section.title} style={styles.section}>
          <Text style={styles.sectionTitle}>{section.title}</Text>
          <Text style={styles.sectionBody}>{section.body}</Text>
        </View>
      ))}
    </ScrollPage>
  );
}

const styles = StyleSheet.create({
  content: { gap: spacing.xxl2 },
  heading: {
    gap: spacing.sm,
    padding: spacing.lg,
    borderRadius: radius.lg,
    backgroundColor: colors.surfaceSubtle,
  },
  section: {
    gap: spacing.sm,
  },
  sectionTitle: {
    color: colors.ink,
    fontSize: 17,
    lineHeight: 23,
    fontWeight: '700',
  },
  sectionBody: { color: colors.muted, fontSize: 15, lineHeight: 23 },
});
