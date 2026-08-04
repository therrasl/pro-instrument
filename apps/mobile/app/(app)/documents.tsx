import { Ionicons } from '@expo/vector-icons';
import * as ImagePicker from 'expo-image-picker';
import { useCallback, useEffect, useRef, useState } from 'react';
import {
  Alert,
  Modal,
  Pressable,
  RefreshControl,
  ScrollView,
  StyleSheet,
  Text,
  View,
} from 'react-native';
import {
  deleteDocument,
  getDocuments,
  submitDocuments,
  uploadDocument,
  type UploadAsset,
} from '../../src/api/documents';
import { useSession } from '../../src/auth/session';
import { DocumentsSkeleton } from '../../src/components/skeleton';
import {
  Button,
  ErrorNotice,
  Page,
  StateView,
  StatusPill,
} from '../../src/components/ui';
import { colors, radius, spacing } from '../../src/theme/tokens';
import type { ClientDocument, DocumentType } from '../../src/types/api';
import { clientStatusLabel, documentTypeLabel } from '../../src/utils/format';

const requiredTypes: DocumentType[] = [
  'passport_main',
  'passport_registration',
  'selfie_with_passport',
];

const documentTutorial: Record<
  DocumentType,
  { title: string; description: string; tips: string[] }
> = {
  passport_main: {
    title: 'Как сфотографировать главную страницу',
    description:
      'Откройте паспорт на развороте с фотографией и положите его на ровную поверхность.',
    tips: [
      'Весь разворот и его края должны полностью помещаться в кадре.',
      'Уберите блики, тени и посторонние предметы.',
      'Проверьте, что фотография и весь текст хорошо читаются.',
      'Не закрывайте данные пальцами и не используйте вспышку в упор.',
    ],
  },
  passport_registration: {
    title: 'Как сфотографировать регистрацию',
    description:
      'Откройте разворот с актуальным адресом регистрации и расправьте страницы.',
    tips: [
      'Сфотографируйте разворот целиком, не обрезая края.',
      'Штамп, дата и адрес регистрации должны быть в фокусе.',
      'Снимайте строго сверху, чтобы строки не искажались.',
      'Проверьте снимок на блики и смазывание перед загрузкой.',
    ],
  },
  selfie_with_passport: {
    title: 'Как сделать селфи с паспортом',
    description:
      'Держите открытый паспорт рядом с лицом на уровне камеры.',
    tips: [
      'Лицо и разворот с фотографией должны одновременно попадать в кадр.',
      'Не закрывайте лицо или данные паспорта пальцами.',
      'Смотрите в камеру и снимайте при ровном освещении.',
      'Не используйте фильтры, ретушь или сильное размытие фона.',
    ],
  },
};

interface TutorialSelection {
  documentType: DocumentType;
  replaceDocumentID?: string;
}

export default function DocumentsScreen() {
  const { client, token, refreshClient } = useSession();
  const [documents, setDocuments] = useState<ClientDocument[]>([]);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [uploadingType, setUploadingType] = useState<DocumentType | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [documentErrors, setDocumentErrors] = useState<
    Partial<Record<DocumentType, string>>
  >({});
  const [tutorialSelection, setTutorialSelection] =
    useState<TutorialSelection | null>(null);
  const [error, setError] = useState('');
  const uploadInFlight = useRef<DocumentType | null>(null);

  const load = useCallback(async () => {
    if (!token) return;
    setDocuments(await getDocuments(token));
  }, [token]);

  const loadInitial = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      await Promise.all([load(), refreshClient()]);
    } catch (requestError) {
      setError(
        requestError instanceof Error
          ? requestError.message
          : 'Не удалось загрузить документы.',
      );
    } finally {
      setLoading(false);
    }
  }, [load, refreshClient]);

  useEffect(() => {
    const timeout = setTimeout(() => {
      void loadInitial();
    }, 0);
    return () => clearTimeout(timeout);
  }, [loadInitial]);

  const refresh = async () => {
    setRefreshing(true);
    setError('');
    try {
      await Promise.all([load(), refreshClient()]);
    } catch (requestError) {
      setError(
        requestError instanceof Error
          ? requestError.message
          : 'Не удалось обновить документы.',
      );
    } finally {
      setRefreshing(false);
    }
  };

  const pickAsset = async (source: 'camera' | 'library'): Promise<UploadAsset | null> => {
    const permission =
      source === 'camera'
        ? await ImagePicker.requestCameraPermissionsAsync()
        : await ImagePicker.requestMediaLibraryPermissionsAsync();
    if (!permission.granted) {
      setError(source === 'camera' ? 'Нет доступа к камере.' : 'Нет доступа к галерее.');
      return null;
    }
    const result =
      source === 'camera'
        ? await ImagePicker.launchCameraAsync({ mediaTypes: ['images'], quality: 0.85 })
        : await ImagePicker.launchImageLibraryAsync({ mediaTypes: ['images'], quality: 0.85 });
    return result.canceled ? null : result.assets[0];
  };

  const selectSource = (documentType: DocumentType, replaceDocumentID?: string) => {
    if (uploadInFlight.current) return;
    Alert.alert(
      replaceDocumentID ? 'Загрузить документ заново' : 'Добавить документ',
      documentTypeLabel[documentType],
      [
        {
          text: 'Камера',
          onPress: () => void chooseAndUpload(documentType, 'camera', replaceDocumentID),
        },
        {
          text: 'Галерея',
          onPress: () => void chooseAndUpload(documentType, 'library', replaceDocumentID),
        },
        { text: 'Отмена', style: 'cancel' },
      ],
    );
  };

  const continueFromTutorial = () => {
    if (!tutorialSelection) return;
    const { documentType, replaceDocumentID } = tutorialSelection;
    setTutorialSelection(null);
    setTimeout(() => selectSource(documentType, replaceDocumentID), 250);
  };

  const chooseAndUpload = async (
    documentType: DocumentType,
    source: 'camera' | 'library',
    replaceDocumentID?: string,
  ) => {
    if (uploadInFlight.current) return;
    setError('');
    setDocumentErrors((current) => ({ ...current, [documentType]: undefined }));
    try {
      const asset = await pickAsset(source);
      if (!asset || !token) return;
      uploadInFlight.current = documentType;
      setUploadingType(documentType);

      if (replaceDocumentID) await deleteDocument(token, replaceDocumentID);
      await uploadDocument(token, documentType, asset);
      await Promise.all([load(), refreshClient()]);
    } catch (cause) {
      const message =
        cause instanceof Error ? cause.message : 'Не удалось загрузить документ.';
      setDocumentErrors((current) => ({ ...current, [documentType]: message }));
      await load().catch(() => undefined);
    } finally {
      uploadInFlight.current = null;
      setUploadingType(null);
    }
  };

  const submitForReview = async () => {
    if (!token || submitting || uploadingType) return;
    setSubmitting(true);
    setError('');
    try {
      await submitDocuments(token);
      await refreshClient();
    } catch (requestError) {
      setError(
        requestError instanceof Error
          ? requestError.message
          : 'Не удалось отправить документы на проверку.',
      );
    } finally {
      setSubmitting(false);
    }
  };

  if (loading) {
    return (
      <Page>
        <DocumentsSkeleton />
      </Page>
    );
  }

  if (error && documents.length === 0) {
    return (
      <Page>
        <StateView
          action={<Button label="Повторить" onPress={() => void loadInitial()} />}
          icon="cloud-offline-outline"
          message={error}
          title="Не удалось загрузить документы"
        />
      </Page>
    );
  }

  const rejected = client?.status === 'verification_rejected';
  const pending = client?.status === 'pending_verification';
  const verified = client?.status === 'verified';
  const allDocumentsUploaded = requiredTypes.every((documentType) =>
    documents.some((document) => document.document_type === documentType),
  );
  const statusMessage = verified
    ? 'Личность подтверждена. Можно оформлять аренду.'
    : rejected
      ? 'Замените отклонённые изображения и отправьте их повторно.'
      : pending
        ? 'Документы на проверке.'
        : allDocumentsUploaded
          ? 'Все изображения загружены. Проверьте их и отправьте на проверку.'
        : 'Загрузите три изображения — это займёт несколько минут.';

  return (
    <Page>
      <ScrollView
        contentContainerStyle={styles.content}
        refreshControl={
          <RefreshControl
            onRefresh={() => void refresh()}
            refreshing={refreshing}
            tintColor={colors.primary}
          />
        }
      >
        <View
          style={[
            styles.overallStatus,
            verified && styles.overallStatusSuccess,
            rejected && styles.overallStatusError,
            pending && styles.overallStatusWarning,
          ]}
        >
          <View
            style={[
              styles.overallStatusIcon,
              verified && styles.overallStatusIconSuccess,
              rejected && styles.overallStatusIconError,
              pending && styles.overallStatusIconWarning,
            ]}
          >
            <Ionicons
              color={
                verified
                  ? colors.success
                  : rejected
                    ? colors.error
                    : pending
                      ? colors.warning
                      : colors.primary
              }
              name={verified ? 'shield-checkmark' : rejected ? 'alert-circle' : 'shield-outline'}
              size={24}
            />
          </View>
          <View style={styles.overallStatusCopy}>
            <Text style={styles.overallStatusTitle}>
              {client ? clientStatusLabel[client.status] : 'Статус недоступен'}
            </Text>
            <Text style={styles.overallStatusMessage}>{statusMessage}</Text>
          </View>
        </View>

        {rejected ? (
          <ErrorNotice
            message={
              client.verification_rejection_reason
                ? `Причина отклонения: ${client.verification_rejection_reason}`
                : 'Загрузите исправленные изображения.'
            }
          />
        ) : null}
        {error ? <ErrorNotice message={error} /> : null}

        <View style={styles.sectionHeader}>
          <Text style={styles.sectionTitle}>Нужные документы</Text>
          <Text style={styles.sectionProgress}>
            {documents.length} из {requiredTypes.length}
          </Text>
        </View>

        <View style={styles.documents}>
          {requiredTypes.map((documentType) => {
            const document = documents.find((item) => item.document_type === documentType);
            const uploading = uploadingType === documentType;
            const documentError = documentErrors[documentType];
            const label = uploading
              ? 'Загружается'
              : rejected && document
                ? 'Отклонён'
                  : verified && document
                  ? 'Подтверждён'
                  : pending && document
                    ? 'На проверке'
                  : document
                    ? 'Загружен'
                    : 'Не загружен';
            const tone = rejected && document ? 'error' : verified && document ? 'success' : document ? 'warning' : 'neutral';

            return (
              <View key={documentType} style={styles.documentCard}>
                <View style={styles.documentMain}>
                  <Text style={styles.documentTitle}>{documentTypeLabel[documentType]}</Text>
                  <StatusPill label={label} tone={tone} />
                </View>

                {documentError ? (
                  <Text accessibilityLiveRegion="polite" style={styles.documentError}>
                    {documentError}
                  </Text>
                ) : null}
                {!verified && !pending && document ? (
                  <View style={styles.documentActions}>
                    <View style={styles.documentAction}>
                      <Button
                        disabled
                        icon="checkmark-circle-outline"
                        label="Загружено"
                        loading={uploading}
                        onPress={() => undefined}
                        variant="secondary"
                      />
                    </View>
                    <View style={styles.documentAction}>
                      <Button
                        disabled={Boolean(uploadingType)}
                        label="Перезагрузить"
                        onPress={() =>
                          setTutorialSelection({
                            documentType,
                            replaceDocumentID: document.id,
                          })
                        }
                        variant="secondary"
                      />
                    </View>
                  </View>
                ) : !verified && !pending ? (
                  <Button
                    disabled={Boolean(uploadingType && !uploading)}
                    label="Загрузить"
                    loading={uploading}
                    onPress={() =>
                      setTutorialSelection({
                        documentType,
                      })
                    }
                    variant="secondary"
                  />
                ) : null}
              </View>
            );
          })}
        </View>

        {allDocumentsUploaded && !pending && !verified ? (
          <View style={styles.submitReview}>
            <Text style={styles.submitReviewText}>
              Убедитесь, что выбраны правильные и чёткие изображения. После отправки
              заменить их можно будет только после завершения проверки.
            </Text>
            <Button
              disabled={Boolean(uploadingType)}
              label={rejected ? 'Отправить повторно на проверку' : 'Отправить на проверку'}
              loading={submitting}
              onPress={() => void submitForReview()}
            />
          </View>
        ) : null}

        <View style={styles.security}>
          <View style={styles.securityIcon}>
            <Ionicons color={colors.primary} name="lock-closed-outline" size={20} />
          </View>
          <Text style={styles.securityText}>
            Файлы передаются по защищённому соединению и доступны только для проверки личности.
          </Text>
        </View>
      </ScrollView>

      <Modal
        animationType="fade"
        onRequestClose={() => setTutorialSelection(null)}
        transparent
        visible={Boolean(tutorialSelection)}
      >
        <View style={styles.tutorialModal}>
          <Pressable
            accessibilityLabel="Закрыть инструкцию"
            onPress={() => setTutorialSelection(null)}
            style={styles.tutorialScrim}
          />
          {tutorialSelection ? (
            <View
              accessibilityViewIsModal
              style={styles.tutorialCard}
            >
              <ScrollView
                contentContainerStyle={styles.tutorialContent}
                showsVerticalScrollIndicator={false}
              >
                <View style={styles.tutorialHeading}>
                  <Text style={styles.tutorialTitle}>
                    {documentTutorial[tutorialSelection.documentType].title}
                  </Text>
                  <Text style={styles.tutorialDescription}>
                    {documentTutorial[tutorialSelection.documentType].description}
                  </Text>
                </View>

                <View style={styles.tutorialTips}>
                  {documentTutorial[tutorialSelection.documentType].tips.map(
                    (tip, index, tips) => (
                      <View
                        key={tip}
                        style={[
                          styles.tutorialTip,
                          index < tips.length - 1 && styles.tutorialTipBorder,
                        ]}
                      >
                        <Text style={styles.tutorialTipMarker}>•</Text>
                        <Text style={styles.tutorialTipText}>{tip}</Text>
                      </View>
                    ),
                  )}
                </View>

                <View style={styles.tutorialNotice}>
                  <Text style={styles.tutorialNoticeText}>
                    Перед загрузкой увеличьте снимок и убедитесь, что он чёткий.
                  </Text>
                </View>
              </ScrollView>

              <View style={styles.tutorialActions}>
                <Button label="Продолжить" onPress={continueFromTutorial} />
                <Button
                  label="Отмена"
                  onPress={() => setTutorialSelection(null)}
                  variant="text"
                />
              </View>
            </View>
          ) : null}
        </View>
      </Modal>
    </Page>
  );
}

const styles = StyleSheet.create({
  content: {
    paddingHorizontal: spacing.xl,
    paddingTop: spacing.xl,
    paddingBottom: spacing.hero,
    gap: spacing.lg,
  },
  overallStatus: {
    minHeight: 88,
    borderRadius: radius.lg,
    padding: spacing.lg,
    flexDirection: 'row',
    alignItems: 'flex-start',
    gap: spacing.md,
    backgroundColor: colors.surfaceSubtle,
  },
  overallStatusSuccess: { backgroundColor: colors.successSoft },
  overallStatusWarning: { backgroundColor: colors.warningSoft },
  overallStatusError: { backgroundColor: colors.errorSoft },
  overallStatusIcon: {
    width: 44,
    height: 44,
    borderRadius: radius.md,
    alignItems: 'center',
    justifyContent: 'center',
    backgroundColor: colors.surface,
  },
  overallStatusIconSuccess: { backgroundColor: colors.surface },
  overallStatusIconWarning: { backgroundColor: colors.surface },
  overallStatusIconError: { backgroundColor: colors.surface },
  overallStatusCopy: { flex: 1, gap: spacing.xs },
  overallStatusTitle: {
    color: colors.ink,
    fontSize: 16,
    lineHeight: 21,
    fontWeight: '800',
  },
  overallStatusMessage: { color: colors.ink, fontSize: 14, lineHeight: 20 },
  sectionHeader: {
    minHeight: 28,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: spacing.md,
  },
  sectionTitle: { color: colors.ink, fontSize: 18, lineHeight: 23, fontWeight: '800' },
  sectionProgress: {
    color: colors.muted,
    fontSize: 14,
    lineHeight: 20,
    fontWeight: '600',
    fontVariant: ['tabular-nums'],
  },
  documents: { gap: spacing.md },
  documentCard: {
    borderRadius: radius.lg,
    padding: spacing.lg,
    gap: spacing.md,
    backgroundColor: colors.surfaceSubtle,
  },
  documentMain: {
    minHeight: 28,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: spacing.md,
  },
  documentTitle: {
    flex: 1,
    color: colors.ink,
    fontSize: 16,
    lineHeight: 21,
    fontWeight: '800',
  },
  documentError: {
    color: colors.error,
    fontSize: 14,
    lineHeight: 20,
    fontWeight: '600',
  },
  documentActions: {
    gap: spacing.sm,
  },
  documentAction: { minWidth: 0 },
  submitReview: {
    borderRadius: radius.lg,
    padding: spacing.lg,
    gap: spacing.md,
    backgroundColor: colors.primarySoft,
  },
  submitReviewText: {
    color: colors.muted,
    fontSize: 14,
    lineHeight: 20,
  },
  security: {
    borderRadius: radius.md,
    padding: spacing.lg,
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.md,
    backgroundColor: colors.surfaceSubtle,
  },
  securityIcon: {
    width: 36,
    height: 36,
    borderRadius: radius.sm,
    alignItems: 'center',
    justifyContent: 'center',
    backgroundColor: colors.primarySoft,
  },
  securityText: { flex: 1, color: colors.ink, fontSize: 14, lineHeight: 20 },
  tutorialModal: {
    flex: 1,
    alignItems: 'center',
    justifyContent: 'center',
    padding: spacing.lg,
  },
  tutorialScrim: {
    position: 'absolute',
    top: 0,
    right: 0,
    bottom: 0,
    left: 0,
    backgroundColor: colors.scrim,
  },
  tutorialCard: {
    width: '100%',
    maxWidth: 420,
    maxHeight: '86%',
    overflow: 'hidden',
    borderRadius: radius.lg,
    backgroundColor: colors.surface,
  },
  tutorialContent: {
    padding: spacing.xl,
    gap: spacing.xl,
  },
  tutorialHeading: { gap: spacing.md },
  tutorialTitle: {
    color: colors.ink,
    fontSize: 22,
    lineHeight: 28,
    fontWeight: '800',
  },
  tutorialDescription: {
    color: colors.muted,
    fontSize: 15,
    lineHeight: 21,
  },
  tutorialTips: {
    overflow: 'hidden',
    borderRadius: radius.md,
    backgroundColor: colors.surfaceSubtle,
  },
  tutorialTip: {
    minHeight: 58,
    paddingHorizontal: spacing.lg,
    paddingVertical: spacing.md,
    flexDirection: 'row',
    alignItems: 'flex-start',
    gap: spacing.sm,
  },
  tutorialTipBorder: {
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderBottomColor: colors.outline,
  },
  tutorialTipMarker: {
    width: 16,
    color: colors.primary,
    fontSize: 20,
    lineHeight: 21,
    fontWeight: '800',
  },
  tutorialTipText: {
    flex: 1,
    color: colors.ink,
    fontSize: 15,
    lineHeight: 21,
  },
  tutorialNotice: {
    borderRadius: radius.md,
    padding: spacing.md,
    backgroundColor: colors.primarySoft,
  },
  tutorialNoticeText: {
    color: colors.primary,
    fontSize: 14,
    lineHeight: 20,
    fontWeight: '600',
  },
  tutorialActions: {
    paddingHorizontal: spacing.xl,
    paddingTop: spacing.lg,
    paddingBottom: spacing.xl,
    gap: spacing.xs,
    borderTopWidth: StyleSheet.hairlineWidth,
    borderTopColor: colors.outline,
  },
});
