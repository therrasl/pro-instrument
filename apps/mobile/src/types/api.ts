export type ClientStatus =
  | 'registered'
  | 'phone_verified'
  | 'profile_completed'
  | 'documents_uploaded'
  | 'pending_verification'
  | 'verified'
  | 'verification_rejected'
  | 'blocked';

export interface Client {
  id: string;
  phone: string;
  phone_verified: boolean;
  phone_verified_at: string | null;
  offer_accepted: boolean;
  offer_accepted_at: string | null;
  profile_completed: boolean;
  full_name: string | null;
  birth_date: string | null;
  email: string | null;
  client_type?: 'individual' | 'legal_entity';
  company_name?: string | null;
  inn?: string | null;
  kpp?: string | null;
  ogrn?: string | null;
  legal_address?: string | null;
  company_contact?: string | null;
  status: ClientStatus;
  verification_rejection_reason: string | null;
  created_at: string;
  updated_at: string;
}

export interface AuthSession {
  access_token: string;
  token_type: string;
  expires_in: number;
}

export interface Category {
  id: string;
  name: string;
  slug: string;
  created_at: string;
  updated_at: string;
}

export type StructuredValue =
  | null
  | boolean
  | number
  | string
  | StructuredValue[]
  | { [key: string]: StructuredValue };

export interface Tool {
  id: string;
  category_id: string;
  name: string;
  slug: string;
  short_description: string;
  description: string;
  image_urls: string[];
  specifications: StructuredValue;
  equipment: StructuredValue;
  daily_price: number;
  deposit_amount: number;
  is_active: boolean;
  available_units: number;
  created_at: string;
  updated_at: string;
}

export type DocumentType =
  | 'passport_main'
  | 'passport_registration'
  | 'selfie_with_passport';

export interface ClientDocument {
  id: string;
  document_type: DocumentType;
  mime_type: string;
  size_bytes: number;
  created_at: string;
  updated_at: string;
}

export interface ProfilePatch {
  full_name: string;
  birth_date?: string;
  email?: string;
  client_type?: 'individual' | 'legal_entity';
  company_name?: string;
  inn?: string;
  kpp?: string;
  ogrn?: string;
  legal_address?: string;
  company_contact?: string;
}

export interface Organization {
  client_id: string;
  company_name: string;
  inn: string;
  kpp: string | null;
  ogrn: string;
  legal_address: string;
  actual_address: string | null;
  settlement_account: string | null;
  bik: string | null;
  correspondent_account: string | null;
  bank_name: string | null;
  email: string;
  phone: string;
  contact_full_name: string;
  contact_position: string | null;
  created_at: string;
  updated_at: string;
}

export type OrganizationInput = Omit<Organization, 'client_id' | 'created_at' | 'updated_at'>;

export interface ConsentAcceptance {
  id: string;
  offer_version: string;
  privacy_version: string;
  offer_accepted: boolean;
  privacy_accepted: boolean;
  data_accuracy_confirmed: boolean;
  rental_rules_accepted: boolean;
  accepted_at: string;
}

export interface RentalEligibility {
  eligible: boolean;
  reason: '' | 'documents_required' | 'documents_pending' | 'documents_rejected';
}

export type DeliveryMethod = 'self_pickup' | 'courier';

export type RentalStatus =
  | 'pending_manager'
  | 'awaiting_payment'
  | 'paid'
  | 'preparing'
  | 'ready'
  | 'handed_to_courier'
  | 'rented'
  | 'awaiting_return'
  | 'inspection'
  | 'completed'
  | 'rejected'
  | 'cancelled'
  | 'payment_expired';

export interface RentalInput {
  tool_id: string;
  start_date: string;
  end_date: string;
  delivery_method: DeliveryMethod;
  delivery_address: string;
}

export interface RentalQuote {
  tool_id: string;
  start_date: string;
  end_date: string;
  rental_days: number;
  daily_price: number;
  rental_price: number;
  deposit_amount: number;
  delivery_cost: number;
  total_amount: number;
  delivery_method: DeliveryMethod;
  pickup_address?: string;
}

export interface Rental {
  id: string;
  order_number?: string;
  client_id: string;
  tool_id: string;
  tool_unit_id: string;
  start_date: string;
  end_date: string;
  rental_days: number;
  rental_price: number;
  deposit_amount: number;
  delivery_cost: number;
  total_amount: number;
  delivery_method: DeliveryMethod;
  delivery_address: string | null;
  pickup_address?: string;
  status: RentalStatus;
  payment_available: boolean;
  expires_at: string;
  payment_expires_at: string | null;
  bitrix_deal_id: string | null;
  created_at: string;
  updated_at: string;
}

export interface OrderDocument {
  id: string;
  order_number: string;
  document_type:
    | 'rental_contract'
    | 'invoice'
    | 'payment_receipt'
    | 'transfer_act'
    | 'return_act'
    | 'closing_document';
  title: string;
  download_url: string;
  mime_type: string | null;
  created_at: string;
}

export interface Payment {
  id: string;
  rental_request_id: string;
  provider_payment_id: string | null;
  rental_amount: number;
  deposit_amount: number;
  delivery_amount: number;
  total_amount: number;
  currency: string;
  status: string;
  confirmation_url: string | null;
  created_at: string;
  updated_at: string;
  succeeded_at?: string;
  cancelled_at?: string;
}

export interface ExtensionQuote {
  rental_id: string;
  order_number: string;
  tool_id: string;
  current_end_date: string;
  new_end_date: string;
  additional_days: number;
  daily_price: number;
  amount: number;
  available: boolean;
}

export interface RentalExtension {
  id: string;
  rental_request_id: string;
  previous_end_date: string;
  new_end_date: string;
  additional_days: number;
  daily_price: number;
  amount: number;
  status: 'pending_payment' | 'paid' | 'cancelled';
  payment_id: string | null;
  confirmation_url?: string;
  created_at: string;
  updated_at: string;
}

export type PhotoPhase = 'handover' | 'return';
export type PhotoType = 'body' | 'equipment' | 'battery' | 'serial_number' | 'cleanliness';

export interface InspectionPhoto {
  id: string;
  rental_request_id: string;
  phase: PhotoPhase;
  photo_type: PhotoType;
  storage_key: string;
  file_name: string;
  mime_type: string;
  file_size: number;
  comment: string;
  download_url?: string;
  created_at: string;
  updated_at: string;
}
