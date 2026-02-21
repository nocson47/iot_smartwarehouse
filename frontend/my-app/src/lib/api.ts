const API_BASE = process.env.NEXT_PUBLIC_API_URL || '';

export async function apiRequest<T>(
  endpoint: string,
  options: RequestInit = {}
): Promise<T> {
  const token = typeof window !== 'undefined' ? localStorage.getItem('authToken') : null;
  
  const headers: HeadersInit = {
    'Content-Type': 'application/json',
    ...(token ? { Authorization: `Bearer ${token}` } : {}),
    ...options.headers,
  };

  const res = await fetch(`${API_BASE}${endpoint}`, {
    ...options,
    headers,
  });

  if (!res.ok) {
    const text = await res.text();
    throw new Error(text || `HTTP ${res.status}`);
  }

  const contentType = res.headers.get('content-type');
  if (contentType?.includes('application/json')) {
    return res.json();
  }
  return res.text() as unknown as T;
}

export const api = {
  login: (email: string, password: string) =>
    apiRequest<{ status: string; token: string; email: string }>('/api/login', {
      method: 'POST',
      body: JSON.stringify({ email, password }),
    }),

  register: (email: string, password: string) =>
    apiRequest<{ status: string }>('/api/register', {
      method: 'POST',
      body: JSON.stringify({ email, password }),
    }),

  logout: () =>
    apiRequest<{ status: string }>('/api/logout', { method: 'POST' }),

  me: () =>
    apiRequest<{ user_id: number; email: string }>('/api/me'),

  getReadings: (limit = 500) =>
    apiRequest<Array<{
      topic: string;
      payload?: string;
      device?: string;
      temp?: number;
      hum?: number;
      fire?: boolean;
      motion?: boolean;
      relay?: boolean;
      door?: boolean;
      ts: string;
    }>>(`/api/readings?limit=${limit}`),

  publish: (topic: string, payload: string) =>
    apiRequest<{ status: string; topic: string }>('/api/publish', {
      method: 'POST',
      body: JSON.stringify({ topic, payload }),
    }),

  getMotionLogs: (limit = 100) =>
    apiRequest<Array<{
      id: number;
      device: string;
      event_type: string;
      message: string;
      ts: string;
    }>>(`/api/logs?limit=${limit}`),
};
