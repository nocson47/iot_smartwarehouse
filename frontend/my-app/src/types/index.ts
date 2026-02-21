export interface Reading {
  topic: string;
  payload?: string;
  device?: string;
  temp?: number;
  hum?: number;
  fire?: boolean;
  motion?: boolean;
  relay?: boolean;
  door?: boolean;
  alertType?: string;
  alertMessage?: string;
  ts: string;
}

export interface User {
  user_id: number;
  email: string;
}

export interface LoginResponse {
  status: string;
  token: string;
  email: string;
}

export interface AuthState {
  token: string | null;
  email: string | null;
  isAuthenticated: boolean;
}
