'use client';

import { useState, useEffect, useCallback } from 'react';
import { api } from '@/lib/api';

interface AuthState {
  token: string | null;
  email: string | null;
  isLoading: boolean;
}

export function useAuth() {
  const [auth, setAuth] = useState<AuthState>({
    token: null,
    email: null,
    isLoading: true,
  });

  useEffect(() => {
    const token = localStorage.getItem('authToken');
    const email = localStorage.getItem('userEmail');
    
    if (token) {
      // Verify token
      api.me()
        .then((data) => {
          setAuth({ token, email: data.email, isLoading: false });
          localStorage.setItem('userEmail', data.email);
        })
        .catch(() => {
          localStorage.removeItem('authToken');
          localStorage.removeItem('userEmail');
          setAuth({ token: null, email: null, isLoading: false });
        });
    } else {
      setAuth({ token: null, email: null, isLoading: false });
    }
  }, []);

  const login = useCallback(async (email: string, password: string) => {
    const data = await api.login(email, password);
    localStorage.setItem('authToken', data.token);
    localStorage.setItem('userEmail', data.email);
    setAuth({ token: data.token, email: data.email, isLoading: false });
    return data;
  }, []);

  const register = useCallback(async (email: string, password: string) => {
    return api.register(email, password);
  }, []);

  const logout = useCallback(async () => {
    try {
      await api.logout();
    } catch {
      // Ignore logout errors
    }
    localStorage.removeItem('authToken');
    localStorage.removeItem('userEmail');
    setAuth({ token: null, email: null, isLoading: false });
  }, []);

  return {
    ...auth,
    isAuthenticated: !!auth.token,
    login,
    register,
    logout,
  };
}
