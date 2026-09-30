import { useEffect, useState, type ReactNode } from 'react';
import { Keyboard, StyleSheet, Text, View } from 'react-native';
import { getOrganization, putOrganization } from '../../src/api/organization';
import { useSession } from '../../src/auth/session';
import { Button, ErrorNotice, Field, ScrollPage, Title } from '../../src/components/ui';
import { colors, radius, spacing } from '../../src/theme/tokens';
import type { OrganizationInput } from '../../src/types/api';

const empty: OrganizationInput = {
  company_name:'', inn:'', kpp:null, ogrn:'', legal_address:'', actual_address:null,
  settlement_account:null, bik:null, correspondent_account:null, bank_name:null,
  email:'', phone:'', contact_full_name:'', contact_position:null,
};

function toInput(organization: Awaited<ReturnType<typeof getOrganization>>): OrganizationInput {
  return {
    company_name: organization.company_name, inn: organization.inn, kpp: organization.kpp,
    ogrn: organization.ogrn, legal_address: organization.legal_address,
    actual_address: organization.actual_address, settlement_account: organization.settlement_account,
    bik: organization.bik, correspondent_account: organization.correspondent_account,
    bank_name: organization.bank_name, email: organization.email, phone: organization.phone,
    contact_full_name: organization.contact_full_name, contact_position: organization.contact_position,
  };
}

export default function OrganizationScreen() {
  const { token, client, refreshClient } = useSession();
  const [form, setForm] = useState<OrganizationInput>(empty);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const [saved, setSaved] = useState(false);

  useEffect(() => {
    if (!token) return;
    void getOrganization(token).then((organization) => setForm(toInput(organization)))
      .catch(() => setForm({ ...empty, company_name:client?.company_name ?? '', inn:client?.inn ?? '', kpp:client?.kpp ?? null, ogrn:client?.ogrn ?? '', legal_address:client?.legal_address ?? '', email:client?.email ?? '', phone:client?.phone ?? '', contact_full_name:client?.company_contact ?? client?.full_name ?? '' }))
      .finally(() => setLoading(false));
  }, [client, token]);

  const change = (field: keyof OrganizationInput, value: string) => setForm((current) => ({ ...current, [field]: value.trim().length === 0 && field !== 'company_name' && field !== 'inn' && field !== 'ogrn' && field !== 'legal_address' && field !== 'email' && field !== 'phone' && field !== 'contact_full_name' ? null : value }));
  const save = async () => {
    if (!token || saving) return;
    Keyboard.dismiss(); setSaving(true); setError(''); setSaved(false);
    try { const result=await putOrganization(token, form); setForm(toInput(result)); await refreshClient(); setSaved(true); }
    catch (cause) { setError(cause instanceof Error ? cause.message : 'Не удалось сохранить реквизиты.'); }
    finally { setSaving(false); }
  };

  return (
    <ScrollPage contentStyle={styles.content}>
      <View><Title compact>Реквизиты организации</Title><Text style={styles.intro}>Данные сохраняются в профиле. При оформлении заказа создаётся неизменяемый snapshot для документов.</Text></View>
      {error ? <ErrorNotice message={error} /> : null}
      {saved ? <Text style={styles.saved}>Реквизиты сохранены</Text> : null}
      <FormSection title="Организация">
        <Field label="Название организации *" value={form.company_name} onChangeText={(v)=>change('company_name',v)} />
        <Field keyboardType="number-pad" label="ИНН *" maxLength={12} value={form.inn} onChangeText={(v)=>change('inn',v.replace(/\D/gu,''))} />
        <Field keyboardType="number-pad" label="КПП" maxLength={9} value={form.kpp ?? ''} onChangeText={(v)=>change('kpp',v.replace(/\D/gu,''))} />
        <Field keyboardType="number-pad" label="ОГРН / ОГРНИП *" maxLength={15} value={form.ogrn} onChangeText={(v)=>change('ogrn',v.replace(/\D/gu,''))} />
        <Field label="Юридический адрес *" multiline value={form.legal_address} onChangeText={(v)=>change('legal_address',v)} />
        <Field label="Фактический адрес" multiline value={form.actual_address ?? ''} onChangeText={(v)=>change('actual_address',v)} />
      </FormSection>
      <FormSection title="Контакты">
        <Field keyboardType="email-address" label="Email *" value={form.email} onChangeText={(v)=>change('email',v)} />
        <Field keyboardType="phone-pad" label="Телефон *" value={form.phone} onChangeText={(v)=>change('phone',v)} />
        <Field label="ФИО контактного лица *" value={form.contact_full_name} onChangeText={(v)=>change('contact_full_name',v)} />
        <Field label="Должность" value={form.contact_position ?? ''} onChangeText={(v)=>change('contact_position',v)} />
      </FormSection>
      <FormSection title="Банковские реквизиты (необязательно)">
        <Text style={styles.hint}>Заполняйте блок целиком, только если реквизиты нужны для текущего сценария.</Text>
        <Field label="Банк" value={form.bank_name ?? ''} onChangeText={(v)=>change('bank_name',v)} />
        <Field keyboardType="number-pad" label="Расчётный счёт" maxLength={20} value={form.settlement_account ?? ''} onChangeText={(v)=>change('settlement_account',v.replace(/\D/gu,''))} />
        <Field keyboardType="number-pad" label="БИК" maxLength={9} value={form.bik ?? ''} onChangeText={(v)=>change('bik',v.replace(/\D/gu,''))} />
        <Field keyboardType="number-pad" label="Корреспондентский счёт" maxLength={20} value={form.correspondent_account ?? ''} onChangeText={(v)=>change('correspondent_account',v.replace(/\D/gu,''))} />
      </FormSection>
      <Button disabled={loading} label={loading ? 'Загрузка…' : 'Сохранить реквизиты'} loading={saving} onPress={() => void save()} />
    </ScrollPage>
  );
}

function FormSection({title,children}:{title:string;children:ReactNode}) { return <View style={styles.section}><Text style={styles.sectionTitle}>{title}</Text>{children}</View>; }
const styles=StyleSheet.create({content:{gap:spacing.lg,paddingBottom:spacing.xl},intro:{color:colors.muted,fontSize:15,lineHeight:21,marginTop:spacing.xs},section:{backgroundColor:colors.surface,borderRadius:radius.lg,gap:spacing.md,padding:spacing.md},sectionTitle:{color:colors.ink,fontSize:17,fontWeight:'700'},hint:{color:colors.muted,fontSize:13,lineHeight:18},saved:{backgroundColor:colors.successSoft,color:colors.success,borderRadius:radius.md,padding:spacing.md,fontWeight:'600'}});
