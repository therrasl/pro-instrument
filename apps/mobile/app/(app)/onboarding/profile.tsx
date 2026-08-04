import { useState } from 'react';
import { StyleSheet, View } from 'react-native';
import { useSession } from '../../../src/auth/session';
import { ProfileForm } from '../../../src/components/profile-form';
import { Body, ErrorNotice, ScrollPage, Title } from '../../../src/components/ui';
import { colors, spacing } from '../../../src/theme/tokens';
import type { ProfilePatch } from '../../../src/types/api';

export default function OnboardingProfileScreen() {
  const { client, updateProfile } = useSession();
  const [error, setError] = useState('');

  const save = async (profile: ProfilePatch) => {
    setError('');
    try {
      await updateProfile(profile);
      // AppLayout moves the completed profile to the catalog.
    } catch (requestError) {
      setError(requestError instanceof Error ? requestError.message : 'Не удалось сохранить анкету.');
      throw requestError;
    }
  };

  return (
    <ScrollPage contentStyle={styles.content}>
      <View style={styles.progress}>
        <View style={[styles.progressPart, styles.progressDone]} />
        <View style={[styles.progressPart, styles.progressDone]} />
      </View>
      <View style={styles.heading}>
        <Title compact>Расскажите о себе</Title>
        <Body muted>
          Данные понадобятся для оформления аренды. Телефон уже подтверждён.
        </Body>
      </View>
      {error ? <ErrorNotice message={error} /> : null}
      <ProfileForm
        initialBirthDate={client?.birth_date ?? ''}
        initialEmail={client?.email ?? ''}
        initialFullName={client?.full_name ?? ''}
        onSubmit={save}
        submitLabel="Сохранить и открыть каталог"
      />
    </ScrollPage>
  );
}

const styles = StyleSheet.create({
  content: { gap: spacing.xl },
  progress: { flexDirection: 'row', gap: spacing.xs },
  progressPart: {
    flex: 1,
    height: 4,
    borderRadius: 2,
    backgroundColor: colors.surfaceStrong,
  },
  progressDone: { backgroundColor: colors.primary },
  heading: { gap: spacing.sm },
});
