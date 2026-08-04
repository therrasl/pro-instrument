import * as SecureStore from 'expo-secure-store';
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type PropsWithChildren,
} from 'react';
import {
  acceptConsents as acceptClientConsents,
  getMe,
  patchMe as patchProfile,
  requestCode as requestOTP,
  verifyCode as verifyOTP,
} from '../api/auth';
import { ApiError } from '../api/client';
import type { Client, ProfilePatch } from '../types/api';

const TOKEN_KEY = 'pro-instrument.session-token';

type SessionStatus = 'loading' | 'authenticated' | 'unauthenticated' | 'error';

interface SessionValue {
  status: SessionStatus;
  token: string | null;
  client: Client | null;
  error: string;
  requestCode: (phone: string) => Promise<void>;
  verifyCode: (
    phone: string,
    code: string,
    onVerified?: () => void | Promise<void>,
  ) => Promise<void>;
  refreshClient: () => Promise<void>;
  updateProfile: (profile: ProfilePatch) => Promise<void>;
  acceptConsents: () => Promise<void>;
  signOut: () => Promise<void>;
}

const SessionContext = createContext<SessionValue | undefined>(undefined);

export function SessionProvider({ children }: PropsWithChildren) {
  const [status, setStatus] = useState<SessionStatus>('loading');
  const [token, setToken] = useState<string | null>(null);
  const [client, setClient] = useState<Client | null>(null);
  const [error, setError] = useState('');

  const clearSession = useCallback(async () => {
    await SecureStore.deleteItemAsync(TOKEN_KEY);
    setToken(null);
    setClient(null);
    setError('');
    setStatus('unauthenticated');
  }, []);

  useEffect(() => {
    let active = true;
    void (async () => {
      const storedToken = await SecureStore.getItemAsync(TOKEN_KEY);
      if (!storedToken) {
        if (active) setStatus('unauthenticated');
        return;
      }
      try {
        const currentClient = await getMe(storedToken);
        if (!active) return;
        setToken(storedToken);
        setClient(currentClient);
        setStatus('authenticated');
      } catch (requestError) {
        if (!active) return;
        if (requestError instanceof ApiError && requestError.status === 401) {
          await clearSession();
          return;
        }
        setToken(storedToken);
        setError(
          requestError instanceof Error
            ? requestError.message
            : 'Не удалось войти. Попробуйте ещё раз.',
        );
        setStatus('error');
      }
    })();
    return () => {
      active = false;
    };
  }, [clearSession]);

  const requestCode = useCallback(async (phone: string) => {
    await requestOTP(phone);
  }, []);

  const verifyCode = useCallback(
    async (
      phone: string,
      code: string,
      onVerified?: () => void | Promise<void>,
    ) => {
      const session = await verifyOTP(phone, code);
      await SecureStore.setItemAsync(TOKEN_KEY, session.access_token);
      try {
        const currentClient = await getMe(session.access_token);
        await onVerified?.();
        setToken(session.access_token);
        setClient(currentClient);
        setError('');
        setStatus('authenticated');
      } catch (error) {
        await SecureStore.deleteItemAsync(TOKEN_KEY);
        throw error;
      }
    },
    [],
  );

  const refreshClient = useCallback(async () => {
    if (!token) return;
    try {
      setClient(await getMe(token));
      setError('');
      setStatus('authenticated');
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) await clearSession();
      else {
        setError(error instanceof Error ? error.message : 'Не удалось обновить профиль.');
        setStatus('error');
      }
      throw error;
    }
  }, [clearSession, token]);

  const updateProfile = useCallback(
    async (profile: ProfilePatch) => {
      if (!token) throw new ApiError(401, 'Войдите в аккаунт ещё раз.');
      const updatedClient = await patchProfile(token, profile);
      setClient(updatedClient);
      setError('');
    },
    [token],
  );

  const acceptConsents = useCallback(async () => {
    if (!token) throw new ApiError(401, 'Войдите в аккаунт ещё раз.');
    const acceptance = await acceptClientConsents(token);
    setClient((currentClient) =>
      currentClient
        ? {
            ...currentClient,
            offer_accepted: true,
            offer_accepted_at: acceptance.accepted_at,
          }
        : currentClient,
    );
    setError('');
    try {
      const refreshedClient = await getMe(token);
      setClient({
        ...refreshedClient,
        offer_accepted: true,
        offer_accepted_at:
          refreshedClient.offer_accepted_at ?? acceptance.accepted_at,
      });
    } catch (requestError) {
      if (requestError instanceof ApiError && requestError.status === 401) {
        await clearSession();
        throw requestError;
      }
      // POST already confirmed persistence. Keep the confirmed state and
      // let the next regular refresh reconcile the remaining client fields.
    }
  }, [clearSession, token]);

  const value = useMemo<SessionValue>(
    () => ({
      status,
      token,
      client,
      error,
      requestCode,
      verifyCode,
      refreshClient,
      updateProfile,
      acceptConsents,
      signOut: clearSession,
    }),
    [
      status,
      token,
      client,
      error,
      requestCode,
      verifyCode,
      refreshClient,
      updateProfile,
      acceptConsents,
      clearSession,
    ],
  );

  return <SessionContext.Provider value={value}>{children}</SessionContext.Provider>;
}

export function useSession(): SessionValue {
  const context = useContext(SessionContext);
  if (!context) throw new Error('useSession must be used within SessionProvider');
  return context;
}
