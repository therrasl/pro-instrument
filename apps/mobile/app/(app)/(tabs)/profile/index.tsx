import { Ionicons } from '@expo/vector-icons';
import { useRouter, type Href } from 'expo-router';
import { useState } from 'react';
import { Pressable, StyleSheet, Text, View } from 'react-native';
import { useSession } from '../../../../src/auth/session';
import {
  formatBirthDateLabel,
  ProfileForm,
} from '../../../../src/components/profile-form';
import { ProfileSkeleton } from '../../../../src/components/skeleton';
import {
  ErrorNotice,
  Page,
  ScrollPage,
  Title,
} from '../../../../src/components/ui';
import { colors, radius, spacing } from '../../../../src/theme/tokens';
import type { ProfilePatch } from '../../../../src/types/api';
import { clientStatusLabel } from '../../../../src/utils/format';

export default function ProfileScreen() {
  const router = useRouter();
  const { client, status, updateProfile, signOut } = useSession();
  const [editing, setEditing] = useState(false);
  const [error, setError] = useState('');
  const [saved, setSaved] = useState(false);

  const save = async (profile: ProfilePatch) => {
    setError('');
    setSaved(false);
    try {
      await updateProfile(profile);
      setEditing(false);
      setSaved(true);
    } catch (requestError) {
      setError(requestError instanceof Error ? requestError.message : 'Не удалось сохранить профиль.');
      throw requestError;
    }
  };

  const documentStatusColor =
    client?.status === 'verified'
      ? colors.success
      : client?.status === 'verification_rejected'
        ? colors.error
      : client?.status === 'pending_verification'
        ? colors.warning
        : client?.status === 'documents_uploaded'
          ? colors.primary
        : colors.muted;

  if (status === 'loading' || !client) {
    return (
      <Page>
        <ProfileSkeleton />
      </Page>
    );
  }

  return (
    <ScrollPage contentStyle={styles.content}>
      {editing ? (
        <View style={styles.header}>
          <Title compact>Личные данные</Title>
          <Pressable
            accessibilityLabel="Отменить редактирование"
            accessibilityRole="button"
            onPress={() => {
              setEditing(false);
              setSaved(false);
              setError('');
            }}
            style={({ pressed }) => [styles.headerAction, pressed && styles.pressed]}
          >
            <Text style={styles.headerActionLabel}>Отмена</Text>
          </Pressable>
        </View>
      ) : (
        <View style={styles.profileHero}>
          <View style={styles.heroHeader}>
            <Text style={styles.heroTitle}>Профиль</Text>
            <Pressable
              accessibilityLabel="Редактировать профиль"
              accessibilityRole="button"
              onPress={() => {
                setEditing(true);
                setSaved(false);
                setError('');
              }}
              style={({ pressed }) => [styles.heroAction, pressed && styles.pressed]}
            >
              <Ionicons color={colors.primary} name="create-outline" size={19} />
              <Text style={styles.heroActionLabel}>Изменить</Text>
            </Pressable>
          </View>
          <View style={styles.identity}>
            <View style={styles.avatar}>
              <Text style={styles.avatarText}>
                {(client?.full_name?.trim().charAt(0) || 'П').toUpperCase()}
              </Text>
            </View>
            <View style={styles.identityText}>
              <Text numberOfLines={2} style={styles.name}>
                {client?.full_name || 'Имя не указано'}
              </Text>
              <Text style={styles.phone}>{client?.phone ?? '—'}</Text>
            </View>
          </View>
        </View>
      )}

      {error ? <ErrorNotice message={error} /> : null}
      {saved ? (
        <View accessibilityLiveRegion="polite" style={styles.savedNotice}>
          <Ionicons color={colors.success} name="checkmark-circle" size={20} />
          <Text style={styles.savedNoticeText}>Изменения сохранены</Text>
        </View>
      ) : null}

      {editing ? (
        <View style={styles.editor}>
          <Text style={styles.editorHint}>
            Проверьте данные — они используются при оформлении аренды.
          </Text>
          <ProfileForm
            initialBirthDate={client?.birth_date ?? ''}
            initialEmail={client?.email ?? ''}
            initialFullName={client?.full_name ?? ''}
            onSubmit={save}
            submitLabel="Сохранить"
          />
        </View>
      ) : (
        <>
          <View style={styles.settingsGroup}>
            <ProfileRow icon="mail-outline" label="Email" value={client?.email || 'Не указан'} />
            <View style={styles.divider} />
            <ProfileRow
              icon="calendar-clear-outline"
              label="Дата рождения"
              value={formatBirthDateLabel(client?.birth_date ?? '') || 'Не указана'}
            />
            <View style={styles.divider} />
            <Pressable
              accessibilityHint="Открывает загрузку документов"
              accessibilityRole="button"
              onPress={() => router.push('/(app)/documents' as Href)}
              style={({ pressed }) => [styles.documentRow, pressed && styles.rowPressed]}
            >
              <View style={styles.rowIcon}>
                <Ionicons color={colors.primary} name="shield-checkmark-outline" size={20} />
              </View>
              <View style={styles.rowText}>
                <Text style={styles.rowValue}>Документы</Text>
                <Text style={[styles.documentStatus, { color: documentStatusColor }]}>
                  {client ? clientStatusLabel[client.status] : 'Неизвестно'}
                </Text>
                {client?.status === 'verification_rejected' &&
                client.verification_rejection_reason ? (
                  <View style={styles.rejection}>
                    <Ionicons color={colors.error} name="alert-circle-outline" size={17} />
                    <Text style={styles.rejectionReason}>
                      {client.verification_rejection_reason}
                    </Text>
                  </View>
                ) : null}
              </View>
              <Ionicons color={colors.muted} name="chevron-forward" size={20} />
            </Pressable>
          </View>

          <Pressable
            accessibilityRole="button"
            onPress={() => void signOut()}
            style={({ pressed }) => [styles.signOut, pressed && styles.pressed]}
          >
            <Ionicons color={colors.error} name="log-out-outline" size={21} />
            <Text style={styles.signOutLabel}>Выйти из аккаунта</Text>
          </Pressable>
        </>
      )}
    </ScrollPage>
  );
}

function ProfileRow({
  icon,
  label,
  value,
}: {
  icon: 'mail-outline' | 'calendar-clear-outline';
  label: string;
  value: string;
}) {
  return (
    <View style={styles.row}>
      <View style={styles.rowIcon}>
        <Ionicons color={colors.primary} name={icon} size={20} />
      </View>
      <View style={styles.rowText}>
        <Text style={styles.rowLabel}>{label}</Text>
        <Text numberOfLines={2} style={styles.rowValue}>
          {value}
        </Text>
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  content: { gap: spacing.lg, paddingBottom: spacing.xxl2 },
  header: {
    minHeight: 44,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: spacing.md,
  },
  headerAction: {
    minHeight: 44,
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.xs,
    paddingHorizontal: spacing.md,
    borderRadius: radius.button,
    backgroundColor: colors.primarySoft,
  },
  headerActionLabel: { color: colors.primary, fontSize: 14, lineHeight: 19, fontWeight: '700' },
  pressed: { opacity: 0.64, transform: [{ scale: 0.96 }] },
  profileHero: {
    gap: spacing.md,
    padding: spacing.xl,
    borderRadius: radius.lg,
    backgroundColor: colors.surfaceSubtle,
  },
  heroHeader: {
    minHeight: 44,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: spacing.md,
  },
  heroTitle: {
    flex: 1,
    color: colors.ink,
    fontSize: 27,
    lineHeight: 33,
    fontWeight: '800',
    letterSpacing: -0.5,
  },
  heroAction: {
    minHeight: 44,
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.xs,
    paddingHorizontal: spacing.md,
    borderRadius: radius.button,
    backgroundColor: colors.surface,
  },
  heroActionLabel: { color: colors.primary, fontSize: 14, lineHeight: 19, fontWeight: '700' },
  savedNotice: {
    minHeight: 44,
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.sm,
    paddingHorizontal: spacing.md,
    borderRadius: radius.button,
    backgroundColor: colors.successSoft,
  },
  savedNoticeText: {
    color: colors.success,
    fontSize: 14,
    lineHeight: 20,
    fontWeight: '700',
  },
  editor: { gap: spacing.lg },
  editorHint: { color: colors.muted, fontSize: 15, lineHeight: 22 },
  identity: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.lg,
  },
  avatar: {
    width: 56,
    height: 56,
    borderRadius: radius.lg,
    alignItems: 'center',
    justifyContent: 'center',
    backgroundColor: colors.primary,
  },
  avatarText: { color: colors.white, fontSize: 22, lineHeight: 27, fontWeight: '800' },
  identityText: { flex: 1, minWidth: 0, gap: spacing.xs },
  name: { color: colors.ink, fontSize: 20, lineHeight: 25, fontWeight: '700' },
  phone: {
    color: colors.muted,
    fontSize: 15,
    lineHeight: 21,
    fontVariant: ['tabular-nums'],
  },
  settingsGroup: {
    paddingHorizontal: spacing.lg,
    borderRadius: radius.lg,
    backgroundColor: colors.surfaceSubtle,
  },
  row: {
    minHeight: 64,
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.md,
    paddingVertical: spacing.sm,
  },
  rowIcon: {
    width: 40,
    height: 40,
    alignItems: 'center',
    justifyContent: 'center',
    borderRadius: radius.md,
    backgroundColor: colors.primarySoft,
  },
  rowText: { flex: 1, minWidth: 0, gap: spacing.xs },
  rowLabel: { color: colors.muted, fontSize: 13, lineHeight: 18 },
  rowValue: {
    color: colors.ink,
    fontSize: 16,
    lineHeight: 22,
    fontWeight: '600',
  },
  divider: {
    height: StyleSheet.hairlineWidth,
    marginLeft: spacing.hero + spacing.xs,
    backgroundColor: colors.outline,
  },
  documentRow: {
    minHeight: 64,
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.md,
    paddingVertical: spacing.sm,
  },
  rowPressed: { opacity: 0.64 },
  documentStatus: { fontSize: 13, lineHeight: 18, fontWeight: '600' },
  rejection: {
    flexDirection: 'row',
    alignItems: 'flex-start',
    gap: spacing.sm,
  },
  rejectionReason: {
    flex: 1,
    color: colors.error,
    fontSize: 14,
    lineHeight: 20,
  },
  signOut: {
    minHeight: 48,
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.sm,
    alignSelf: 'center',
    paddingHorizontal: spacing.md,
  },
  signOutLabel: { color: colors.error, fontSize: 16, lineHeight: 22, fontWeight: '700' },
});
