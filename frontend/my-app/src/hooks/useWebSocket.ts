'use client';

import { useState, useEffect, useRef, useCallback } from 'react';
import { Reading } from '@/types';

type ConnectionStatus = 'connecting' | 'connected' | 'disconnected';

export interface DeviceStatus {
  device: string;
  online: boolean;
  last_update?: string;
  temp?: number;
  hum?: number;
}

export interface StatusMessage {
  type: string;
  server_online?: boolean;
  mqtt_connected?: boolean;
  device_statuses?: DeviceStatus[];
  timestamp?: string;
}

export function useWebSocket() {
  const [status, setStatus] = useState<ConnectionStatus>('disconnected');
  const [lastMessage, setLastMessage] = useState<Reading | null>(null);
  const [serverStatus, setServerStatus] = useState<StatusMessage | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const reconnectTimerRef = useRef<NodeJS.Timeout | null>(null);

  const connect = useCallback(() => {
    if (wsRef.current?.readyState === WebSocket.CONNECTING || 
        wsRef.current?.readyState === WebSocket.OPEN) {
      return;
    }

    const wsUrl = process.env.NEXT_PUBLIC_WS_URL || 
      `${window.location.protocol === 'https:' ? 'wss' : 'ws'}://${window.location.host}/ws`;
    
    setStatus('connecting');
    const ws = new WebSocket(wsUrl);
    wsRef.current = ws;

    ws.onopen = () => {
      setStatus('connected');
      if (reconnectTimerRef.current) {
        clearTimeout(reconnectTimerRef.current);
        reconnectTimerRef.current = null;
      }
    };

    ws.onclose = () => {
      setStatus('disconnected');
      wsRef.current = null;
      // Auto-reconnect after 3 seconds
      if (!reconnectTimerRef.current) {
        reconnectTimerRef.current = setTimeout(() => {
          reconnectTimerRef.current = null;
          connect();
        }, 3000);
      }
    };

    ws.onerror = (err) => {
      console.error('WebSocket error:', err);
    };

    ws.onmessage = (ev) => {
      try {
        const data = JSON.parse(ev.data);
        
        // Handle status messages
        if (data.type === 'status') {
          setServerStatus(data);
        } else {
          // Handle sensor reading messages
          setLastMessage({
            topic: data.topic || data.Topic,
            device: data.device || data.Device,
            temp: data.temp ?? data.Temp,
            hum: data.hum ?? data.Hum,
            fire: data.fire ?? data.Fire,
            motion: data.motion ?? data.Motion,
            relay: data.relay ?? data.Relay,
            door: data.door ?? data.Door,
            ts: data.ts || data.Ts,
          });
        }
      } catch (e) {
        console.error('Failed to parse WebSocket message:', e);
      }
    };
  }, []);

  const disconnect = useCallback(() => {
    if (reconnectTimerRef.current) {
      clearTimeout(reconnectTimerRef.current);
      reconnectTimerRef.current = null;
    }
    if (wsRef.current) {
      wsRef.current.close();
      wsRef.current = null;
    }
  }, []);

  useEffect(() => {
    return () => {
      disconnect();
    };
  }, [disconnect]);

  return {
    status,
    lastMessage,
    serverStatus,
    connect,
    disconnect,
  };
}
