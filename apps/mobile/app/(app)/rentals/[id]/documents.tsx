import { Ionicons } from '@expo/vector-icons';
import * as FileSystem from 'expo-file-system/legacy';
import { useLocalSearchParams } from 'expo-router';
import { useCallback, useEffect, useState } from 'react';
import { Linking, Platform, Pressable, Share, StyleSheet, Text, View } from 'react-native';
import { downloadRentalDocument, getRentalDocuments } from '../../../../src/api/rentals';
import { useSession } from '../../../../src/auth/session';
import { Button, Page, ScrollPage, StateView, Title } from '../../../../src/components/ui';
import { colors, radius, spacing } from '../../../../src/theme/tokens';
import type { OrderDocument } from '../../../../src/types/api';

const icons: Record<OrderDocument['document_type'], keyof typeof Ionicons.glyphMap> = {
  rental_contract: 'document-text-outline', invoice: 'receipt-outline', payment_receipt: 'card-outline',
  transfer_act: 'log-out-outline', return_act: 'return-down-back-outline', closing_document: 'checkmark-done-outline',
};

export default function RentalDocumentsScreen() {
  const { id: rawID } = useLocalSearchParams<{ id: string | string[] }>();
  const id = Array.isArray(rawID) ? rawID[0] : rawID;
  const { token } = useSession();
  const [documents, setDocuments] = useState<OrderDocument[]>([]);
  const [loading, setLoading] = useState(true);
  const [busyID, setBusyID] = useState('');
  const [error, setError] = useState('');

  const load = useCallback(async () => {
    if (!token || !id) return;
    setLoading(true); setError('');
    try { setDocuments(await getRentalDocuments(token, id)); }
    catch (cause) { setError(cause instanceof Error ? cause.message : 'Не удалось загрузить документы.'); }
    finally { setLoading(false); }
  }, [id, token]);

  useEffect(() => { const timer=setTimeout(() => void load(), 0); return () => clearTimeout(timer); }, [load]);

  const download = async (document: OrderDocument, share: boolean) => {
    if (!token || !id || busyID) return;
    setBusyID(document.id); setError('');
    try {
      const uri = await downloadRentalDocument(token, id, document);
      const systemURI = Platform.OS === 'android' ? await FileSystem.getContentUriAsync(uri) : uri;
      if (share) await Share.share({ title: document.title, message: `${document.title}\n${systemURI}`, url: systemURI });
      else await Linking.openURL(systemURI);
    } catch (cause) { setError(cause instanceof Error ? cause.message : 'Не удалось открыть документ.'); }
    finally { setBusyID(''); }
  };

  if (loading) return <Page><StateView icon="document-text-outline" title="Загружаем документы" message="Проверяем доступные документы заказа…" /></Page>;
  if (error && documents.length === 0) return <Page><StateView icon="cloud-offline-outline" title="Не удалось загрузить" message={error} action={<Button label="Повторить" onPress={() => void load()} />} /></Page>;

  return (
    <ScrollPage contentStyle={styles.content}>
      <View><Title compact>Документы заказа</Title><Text style={styles.intro}>Здесь отображаются только сформированные документы этой аренды.</Text></View>
      {error ? <Text style={styles.error}>{error}</Text> : null}
      {documents.length === 0 ? (
        <View style={styles.empty}><Ionicons color={colors.muted} name="document-outline" size={42} /><Text style={styles.emptyTitle}>Документов пока нет</Text><Text style={styles.emptyText}>Они появятся автоматически на соответствующем этапе заказа.</Text></View>
      ) : documents.map((document) => (
        <View key={document.id} style={styles.card}>
          <View style={styles.icon}><Ionicons color={colors.primary} name={icons[document.document_type]} size={24} /></View>
          <View style={styles.copy}><Text style={styles.name}>{document.title}</Text><Text style={styles.meta}>{new Intl.DateTimeFormat('ru-RU').format(new Date(document.created_at))} · PDF</Text></View>
          <View style={styles.actions}>
            <Pressable accessibilityLabel={`Открыть ${document.title}`} onPress={() => void download(document, false)} style={styles.action}><Ionicons color={colors.primary} name={busyID === document.id ? 'hourglass-outline' : 'open-outline'} size={22} /></Pressable>
            <Pressable accessibilityLabel={`Поделиться ${document.title}`} onPress={() => void download(document, true)} style={styles.action}><Ionicons color={colors.primary} name="share-outline" size={22} /></Pressable>
          </View>
        </View>
      ))}
    </ScrollPage>
  );
}

const styles = StyleSheet.create({
  content:{gap:spacing.lg,paddingBottom:spacing.xl}, intro:{color:colors.muted,fontSize:15,lineHeight:21,marginTop:spacing.xs},
  error:{backgroundColor:colors.errorSoft,color:colors.error,padding:spacing.md,borderRadius:radius.md},
  empty:{alignItems:'center',backgroundColor:colors.surface,borderRadius:radius.lg,gap:spacing.sm,padding:spacing.xl},emptyTitle:{color:colors.ink,fontSize:18,fontWeight:'700'},emptyText:{color:colors.muted,lineHeight:20,textAlign:'center'},
  card:{alignItems:'center',backgroundColor:colors.surface,borderRadius:radius.lg,flexDirection:'row',gap:spacing.md,padding:spacing.md},icon:{alignItems:'center',backgroundColor:colors.primarySoft,borderRadius:radius.md,height:46,justifyContent:'center',width:46},copy:{flex:1,gap:4},name:{color:colors.ink,fontSize:16,fontWeight:'700'},meta:{color:colors.muted,fontSize:13},actions:{flexDirection:'row',gap:spacing.xs},action:{alignItems:'center',height:42,justifyContent:'center',width:38},
});
