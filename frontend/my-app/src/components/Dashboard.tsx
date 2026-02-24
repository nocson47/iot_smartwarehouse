'use client';

import { useEffect, useState, useCallback, useRef } from 'react';
import { Line } from 'react-chartjs-2';
import {
  Chart as ChartJS,
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Title,
  Tooltip,
  Legend,
  TimeScale,
} from 'chart.js';
import 'chartjs-adapter-date-fns';
import { useWebSocket } from '@/hooks/useWebSocket';
import { api } from '@/lib/api';
import { Reading } from '@/types';

ChartJS.register(
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Title,
  Tooltip,
  Legend,
  TimeScale
);

interface DashboardProps {
  email: string;
  onLogout: () => void;
}

interface ReadingData {
  ts: string;
  temp?: number;
  hum?: number;
  fire?: boolean;
  motion?: boolean;
  relay?: boolean;
  door?: boolean;
}

interface DeviceState {
  temp: number | null;
  hum: number | null;
  fire: boolean;
  motion: boolean;
  relay: boolean;
  door: boolean;
  led1: boolean;
  led2: boolean;
  led3: boolean;
  led4: boolean;
  lastUpdate: string;
}

interface MotionLog {
  id: number;
  device: string;
  event_type: string;
  message: string;
  ts: string;
}

export function Dashboard({ email, onLogout }: DashboardProps) {
  const { status, lastMessage, serverStatus, connect } = useWebSocket();
  const [devices, setDevices] = useState<string[]>([]);
  const [selectedDevice, setSelectedDevice] = useState<string>('');
  const [readingsByDevice, setReadingsByDevice] = useState<Record<string, ReadingData[]>>({});
  const [messages, setMessages] = useState<Reading[]>([]);
  const [customTopic, setCustomTopic] = useState('');
  const [customPayload, setCustomPayload] = useState('');
  const [motionLogs, setMotionLogs] = useState<MotionLog[]>([]);
  const [showAdvanced, setShowAdvanced] = useState(false);
  const hasConnectedRef = useRef(false);
  
  const [deviceState, setDeviceState] = useState<DeviceState>({
    temp: null,
    hum: null,
    fire: false,
    motion: false,
    relay: false,
    door: false,
    led1: false,
    led2: false,
    led3: false,
    led4: false,
    lastUpdate: '',
  });

  useEffect(() => {
    if (!hasConnectedRef.current) {
      hasConnectedRef.current = true;
      connect();
    }
  }, [connect]);

  useEffect(() => {
    const loadHistory = async () => {
      try {
        const readings = await api.getReadings(200);
        const deviceSet = new Set<string>();
        const byDevice: Record<string, ReadingData[]> = {};

        // API returns newest first, so reverse to get chronological order
        readings.reverse().forEach((r) => {
          const device = r.device || r.topic || 'unknown';
          if (!device.startsWith('esp32_')) return;
          deviceSet.add(device);
          if (!byDevice[device]) byDevice[device] = [];
          byDevice[device].push({
            ts: r.ts,
            temp: r.temp,
            hum: r.hum,
            fire: r.fire,
            motion: r.motion,
            relay: r.relay,
            door: r.door,
          });
        });

        setDevices(Array.from(deviceSet).sort());
        setReadingsByDevice(byDevice);
        if (deviceSet.size > 0 && !selectedDevice) {
          setSelectedDevice(Array.from(deviceSet)[0]);
        }
      } catch (err) {
        console.error('Failed to load history:', err);
      }
    };

    loadHistory();
  }, []);

  useEffect(() => {
    const loadLogs = async () => {
      try {
        const logs = await api.getMotionLogs(100);
        setMotionLogs(logs);
      } catch (err) {
        console.error('Failed to load motion logs:', err);
      }
    };
    loadLogs();
    const interval = setInterval(loadLogs, 30000);
    return () => clearInterval(interval);
  }, []);

  // Real-time chart refresh every 5 seconds to ensure data is always fresh
  useEffect(() => {
    const refreshInterval = setInterval(() => {
      setReadingsByDevice((prev) => ({ ...prev })); // Trigger re-render
    }, 5000);
    return () => clearInterval(refreshInterval);
  }, []);

  useEffect(() => {
    if (lastMessage) {
      const device = lastMessage.device || lastMessage.topic || 'unknown';
      if (!device.startsWith('esp32_')) return;
      
      setDevices((prev) => {
        if (!prev.includes(device)) {
          return [...prev, device].sort();
        }
        return prev;
      });

      setReadingsByDevice((prev) => {
        const deviceReadings = prev[device] || [];
        const updated = [
          { ts: lastMessage.ts, temp: lastMessage.temp, hum: lastMessage.hum, fire: lastMessage.fire, motion: lastMessage.motion, relay: lastMessage.relay, door: lastMessage.door },
          ...deviceReadings,
        ].slice(0, 200);
        return { ...prev, [device]: updated };
      });

      setMessages((prev) => [lastMessage, ...prev].slice(0, 50));

      if (device === selectedDevice || !selectedDevice) {
        setDeviceState((prev) => ({
          ...prev,
          temp: lastMessage.temp ?? prev.temp,
          hum: lastMessage.hum ?? prev.hum,
          fire: lastMessage.fire ?? prev.fire,
          motion: lastMessage.motion ?? prev.motion,
          relay: lastMessage.relay ?? prev.relay,
          door: lastMessage.door ?? prev.door,
          lastUpdate: lastMessage.ts,
        }));
      }

      if (lastMessage.motion || lastMessage.fire) {
        api.getMotionLogs(100).then(setMotionLogs).catch(console.error);
      }

      if (!selectedDevice) {
        setSelectedDevice(device);
      }
    }
  }, [lastMessage, selectedDevice]);

  const handlePublish = useCallback(async (topic: string, payload: string) => {
    try {
      await api.publish(topic, payload);
      const parts = topic.split('/');
      const actuator = parts[parts.length - 1];
      const isOn = payload === 'ON';
      
      if (actuator.startsWith('led')) {
        setDeviceState((prev) => ({ ...prev, [actuator]: isOn }));
      } else if (actuator === 'relay') {
        setDeviceState((prev) => ({ ...prev, relay: isOn }));
      } else if (actuator === 'door') {
        setDeviceState((prev) => ({ ...prev, door: payload === 'OPEN' }));
      }
    } catch (err) {
      alert('Publish failed: ' + (err instanceof Error ? err.message : 'Unknown error'));
    }
  }, []);

  const handleCustomPublish = (e: React.FormEvent) => {
    e.preventDefault();
    if (customTopic && customPayload) {
      handlePublish(customTopic, customPayload);
    }
  };

  const chartData = {
    labels: (readingsByDevice[selectedDevice] || []).slice(-50).map((r) => new Date(r.ts)),
    datasets: [
      {
        label: 'TEMP (°C)',
        data: (readingsByDevice[selectedDevice] || []).slice(-50).map((r) => r.temp),
        borderColor: '#dc2626',
        backgroundColor: 'rgba(220, 38, 38, 0.1)',
        fill: false,
        tension: 0.2,
        pointRadius: 2,
        borderWidth: 2,
        yAxisID: 'y',
      },
      {
        label: 'HUM (%)',
        data: (readingsByDevice[selectedDevice] || []).slice(-50).map((r) => r.hum),
        borderColor: '#3b82f6',
        backgroundColor: 'rgba(59, 130, 246, 0.1)',
        fill: false,
        tension: 0.2,
        pointRadius: 2,
        borderWidth: 2,
        yAxisID: 'y1',
      },
    ],
  };

  const chartOptions = {
    responsive: true,
    maintainAspectRatio: false,
    animation: false as const,
    interaction: { mode: 'index' as const, intersect: false },
    scales: {
      x: {
        type: 'time' as const,
        time: { tooltipFormat: 'HH:mm:ss' },
        title: { display: false },
        grid: { color: 'rgba(255,255,255,0.05)' },
        ticks: { color: '#6b7280', maxTicksLimit: 10 },
      },
      y: {
        type: 'linear' as const,
        display: true,
        position: 'left' as const,
        title: { display: true, text: '°C', color: '#dc2626' },
        suggestedMin: 20,
        suggestedMax: 40,
        grid: { color: 'rgba(255,255,255,0.05)' },
        ticks: { color: '#dc2626' },
      },
      y1: {
        type: 'linear' as const,
        display: true,
        position: 'right' as const,
        title: { display: true, text: '%', color: '#3b82f6' },
        suggestedMin: 30,
        suggestedMax: 100,
        grid: { drawOnChartArea: false },
        ticks: { color: '#3b82f6' },
      },
    },
    plugins: {
      legend: { position: 'top' as const, labels: { color: '#9ca3af', usePointStyle: true, pointStyle: 'line' } },
    },
  };

  const StatusIndicator = ({ active, label }: { active: boolean; label: string }) => (
    <div className={`flex items-center gap-2 px-3 py-1 rounded text-xs font-medium tracking-wide ${
      active ? 'bg-red-900/50 text-red-400 border border-red-700' : 'bg-slate-800 text-slate-400 border border-slate-700'
    }`}>
      <div className={`w-2 h-2 rounded-full ${active ? 'bg-red-500 animate-pulse' : 'bg-slate-600'}`} />
      {label}
    </div>
  );

  const ControlButton = ({ 
    active, 
    onClick, 
    children 
  }: { 
    active: boolean; 
    onClick: () => void; 
    children: React.ReactNode 
  }) => (
    <button
      onClick={onClick}
      className={`px-4 py-2 text-xs font-semibold tracking-wider uppercase transition-all ${
        active 
          ? 'bg-emerald-600 text-white' 
          : 'bg-slate-700 text-slate-300 hover:bg-slate-600'
      }`}
    >
      {children}
    </button>
  );

  return (
    <div className="min-h-screen bg-slate-900 text-slate-100">
      {/* Header */}
      <header className="bg-slate-800 border-b border-slate-700 px-6 py-4">
        <div className="max-w-7xl mx-auto flex justify-between items-center">
          <div>
            <h1 className="text-xl font-bold tracking-wider uppercase text-slate-100">
              Warehouse Control System
            </h1>
            <p className="text-xs text-slate-500 tracking-wide">IoT Monitoring & Automation</p>
          </div>
          <div className="flex items-center gap-4 flex-wrap justify-end">
            {/* Server Status */}
            <div className="flex items-center gap-2 px-3 py-1 rounded bg-slate-700 border border-slate-600">
              <div className={`w-2 h-2 rounded-full ${serverStatus?.server_online ? 'bg-emerald-500' : 'bg-red-500'}`} />
              <span className="text-xs text-slate-400 uppercase tracking-wider">Server</span>
            </div>
            
            {/* MQTT Status */}
            <div className="flex items-center gap-2 px-3 py-1 rounded bg-slate-700 border border-slate-600">
              <div className={`w-2 h-2 rounded-full ${serverStatus?.mqtt_connected ? 'bg-emerald-500' : 'bg-red-500'}`} />
              <span className="text-xs text-slate-400 uppercase tracking-wider">MQTT</span>
            </div>

            {/* WebSocket Status */}
            <div className="flex items-center gap-2 px-3 py-1 rounded bg-slate-700 border border-slate-600">
              <div className={`w-2 h-2 rounded-full ${
                status === 'connected' ? 'bg-emerald-500' : 
                status === 'connecting' ? 'bg-amber-500 animate-pulse' : 'bg-red-500'
              }`} />
              <span className="text-xs text-slate-400 uppercase tracking-wider">WS</span>
            </div>

            {/* Device Count */}
            {serverStatus?.device_statuses && (
              <div className="flex items-center gap-2 px-3 py-1 rounded bg-slate-700 border border-slate-600">
                <div className="w-2 h-2 rounded-full bg-blue-500" />
                <span className="text-xs text-slate-400 uppercase tracking-wider">
                  {serverStatus.device_statuses.length} device{serverStatus.device_statuses.length !== 1 ? 's' : ''}
                </span>
              </div>
            )}

            <span className="text-sm text-slate-400">{email}</span>
            <button
              onClick={onLogout}
              className="px-4 py-2 text-xs font-semibold tracking-wider uppercase bg-slate-700 text-slate-300 hover:bg-red-600 hover:text-white transition-colors"
            >
              Logout
            </button>
          </div>
        </div>
      </header>

      <main className="max-w-7xl mx-auto p-6 space-y-6">
        {/* Device Status Bar */}
        {serverStatus?.device_statuses && serverStatus.device_statuses.length > 0 && (
          <div className="bg-slate-800 border border-slate-700 p-4 rounded">
            <p className="text-xs text-slate-500 uppercase tracking-wider mb-3">Connected Devices</p>
            <div className="flex gap-2 flex-wrap">
              {serverStatus.device_statuses.map((dev) => (
                <div 
                  key={dev.device}
                  className={`px-3 py-2 rounded text-xs font-medium tracking-wide border ${
                    dev.online 
                      ? 'bg-emerald-900/30 border-emerald-700 text-emerald-400' 
                      : 'bg-slate-900/30 border-slate-700 text-slate-400'
                  }`}
                >
                  <span className="inline-block w-2 h-2 rounded-full mr-2" style={{
                    backgroundColor: dev.online ? '#10b981' : '#6b7280'
                  }} />
                  {dev.device}
                  {dev.temp !== undefined && ` (${dev.temp}°C)`}
                </div>
              ))}
            </div>
          </div>
        )}

        {/* Alert Banner */}
        {(deviceState.fire || deviceState.motion) && (
          <div className={`border-l-4 p-4 flex items-center gap-4 ${
            deviceState.fire 
              ? 'bg-red-900/30 border-red-500 text-red-400' 
              : 'bg-amber-900/30 border-amber-500 text-amber-400'
          }`}>
            <div className="w-3 h-3 rounded-full bg-current animate-pulse" />
            <div>
              <span className="font-bold uppercase tracking-wider">
                {deviceState.fire ? 'FIRE ALERT' : 'MOTION DETECTED'}
              </span>
              <span className="text-sm ml-4 opacity-75">
                {deviceState.fire ? 'Emergency protocols activated' : 'Perimeter breach detected'}
              </span>
            </div>
          </div>
        )}

        {/* Device Selector + Status */}
        <div className="flex flex-wrap items-center gap-6 bg-slate-800 border border-slate-700 p-4">
          <div className="flex items-center gap-3">
            <span className="text-xs text-slate-500 uppercase tracking-wider">Unit:</span>
            <select
              value={selectedDevice}
              onChange={(e) => setSelectedDevice(e.target.value)}
              className="bg-slate-900 border border-slate-600 text-slate-100 px-4 py-2 text-sm focus:outline-none focus:border-emerald-500"
            >
              {devices.length === 0 && <option>Awaiting signal...</option>}
              {devices.map((d) => (
                <option key={d} value={d}>{d}</option>
              ))}
            </select>
          </div>
          
          <div className="flex items-center gap-4">
            <StatusIndicator active={deviceState.fire} label="FIRE" />
            <StatusIndicator active={deviceState.motion} label="MOTION" />
          </div>

          {deviceState.lastUpdate && (
            <span className="text-xs text-slate-500 ml-auto font-mono">
              {new Date(deviceState.lastUpdate).toLocaleTimeString()}
            </span>
          )}
        </div>

        {/* Main Grid */}
        <div className="grid grid-cols-1 lg:grid-cols-4 gap-6">
          {/* Sensor Values */}
          <div className="bg-slate-800 border border-slate-700 p-6">
            <h2 className="text-xs text-slate-500 uppercase tracking-wider mb-6">Environment</h2>
            <div className="space-y-6">
              <div>
                <div className="text-xs text-slate-500 mb-1">Temperature</div>
                <div className="flex items-baseline gap-2">
                  <span className={`text-4xl font-light tabular-nums ${
                    deviceState.temp !== null && deviceState.temp > 35 ? 'text-red-400' : 'text-slate-100'
                  }`}>
                    {deviceState.temp !== null ? deviceState.temp.toFixed(1) : '--'}
                  </span>
                  <span className="text-slate-500">°C</span>
                </div>
              </div>
              <div>
                <div className="text-xs text-slate-500 mb-1">Humidity</div>
                <div className="flex items-baseline gap-2">
                  <span className="text-4xl font-light tabular-nums text-slate-100">
                    {deviceState.hum !== null ? deviceState.hum.toFixed(1) : '--'}
                  </span>
                  <span className="text-slate-500">%</span>
                </div>
              </div>
            </div>
          </div>

          {/* Chart */}
          <div className="lg:col-span-3 bg-slate-800 border border-slate-700 p-6">
            <h2 className="text-xs text-slate-500 uppercase tracking-wider mb-4">Sensor Graph</h2>
            <div className="h-64">
              <Line data={chartData} options={chartOptions} />
            </div>
          </div>
        </div>

        {/* Controls Grid */}
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6 items-start">
          {/* Relay */}
          <div className="bg-slate-800 border border-slate-700 p-6">
            <div className="flex items-center justify-between mb-4">
              <h3 className="text-sm font-medium uppercase tracking-wider">Relay / Fan</h3>
              <div className={`w-2 h-2 rounded-full ${deviceState.relay ? 'bg-emerald-500' : 'bg-slate-600'}`} />
            </div>
            <div className="flex gap-2">
              <ControlButton active={deviceState.relay} onClick={() => handlePublish(`actuators/${selectedDevice}/relay`, 'ON')}>
                ON
              </ControlButton>
              <ControlButton active={!deviceState.relay} onClick={() => handlePublish(`actuators/${selectedDevice}/relay`, 'OFF')}>
                OFF
              </ControlButton>
            </div>
          </div>

          {/* Door */}
          <div className="bg-slate-800 border border-slate-700 p-6">
            <div className="flex items-center justify-between mb-4">
              <h3 className="text-sm font-medium uppercase tracking-wider">Door</h3>
              <div className={`w-2 h-2 rounded-full ${deviceState.door ? 'bg-emerald-500' : 'bg-slate-600'}`} />
            </div>
            <div className="flex gap-2">
              <ControlButton active={deviceState.door} onClick={() => handlePublish(`actuators/${selectedDevice}/door`, 'OPEN')}>
                OPEN
              </ControlButton>
              <ControlButton active={!deviceState.door} onClick={() => handlePublish(`actuators/${selectedDevice}/door`, 'CLOSE')}>
                CLOSE
              </ControlButton>
            </div>
          </div>

          {/* LEDs */}
          <div className="bg-slate-800 border border-slate-700 p-6">
            <h3 className="text-sm font-medium uppercase tracking-wider mb-4">LED Controls</h3>
            <div className="grid grid-cols-4 gap-4">
              {[1, 2, 3, 4].map((led) => {
                const ledKey = `led${led}` as keyof DeviceState;
                const isOn = deviceState[ledKey] as boolean;
                return (
                  <button
                    key={led}
                    onClick={() => handlePublish(`actuators/${selectedDevice}/led${led}`, isOn ? 'OFF' : 'ON')}
                    className={`aspect-square flex flex-col items-center justify-center transition-all border ${
                      isOn 
                        ? 'bg-amber-500/20 border-amber-500 text-amber-400' 
                        : 'bg-slate-900 border-slate-600 text-slate-500 hover:border-slate-500'
                    }`}
                  >
                    <div className={`w-4 h-4 rounded-full mb-2 ${isOn ? 'bg-amber-400' : 'bg-slate-600'}`} />
                    <span className="text-xs font-mono">L{led}</span>
                  </button>
                );
              })}
            </div>
          </div>
        </div>

        {/* Live Data Stream */}
        <div className="bg-slate-800 border border-slate-700 p-6">
          <h3 className="text-sm font-medium uppercase tracking-wider mb-4">Live Data Stream</h3>
          <div className="h-48 overflow-auto bg-slate-900 border border-slate-700 font-mono text-xs">
            {messages.length === 0 ? (
              <p className="text-slate-500 text-center py-8">Awaiting sensor data...</p>
            ) : (
              messages.map((msg, i) => (
                <div key={i} className="flex items-center gap-4 px-4 py-2 border-b border-slate-800 hover:bg-slate-800/50">
                  <span className="text-slate-500 w-20">
                    {new Date(msg.ts).toLocaleTimeString()}
                  </span>
                  <span className="text-emerald-400 w-24">{msg.device}</span>
                  <span className="text-red-400">T:{msg.temp?.toFixed(1) ?? '--'}</span>
                  <span className="text-blue-400">H:{msg.hum?.toFixed(1) ?? '--'}</span>
                  {msg.fire && <span className="text-red-500 font-bold">FIRE</span>}
                  {msg.motion && <span className="text-amber-500 font-bold">MOTION</span>}
                </div>
              ))
            )}
          </div>
        </div>

        {/* Event Log */}
        <div className="bg-slate-800 border border-slate-700 p-6">
          <div className="flex items-center justify-between mb-4">
            <h3 className="text-sm font-medium uppercase tracking-wider">Event Log</h3>
            <button
              onClick={async () => {
                try {
                  const logs = await api.getMotionLogs(100);
                  setMotionLogs(logs);
                } catch (err) {
                  console.error('Failed to load logs:', err);
                }
              }}
              className="px-3 py-1 text-xs uppercase tracking-wider bg-slate-700 text-slate-300 hover:bg-slate-600 transition-colors"
            >
              Refresh
            </button>
          </div>
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-slate-700 text-left text-xs text-slate-500 uppercase tracking-wider">
                  <th className="px-4 py-3">Time</th>
                  <th className="px-4 py-3">Unit</th>
                  <th className="px-4 py-3">Event</th>
                  <th className="px-4 py-3">Details</th>
                </tr>
              </thead>
              <tbody>
                {motionLogs.length === 0 ? (
                  <tr>
                    <td colSpan={4} className="px-4 py-8 text-center text-slate-500">
                      No events recorded
                    </td>
                  </tr>
                ) : (
                  motionLogs.map((log) => (
                    <tr key={log.id} className="border-b border-slate-700/50 hover:bg-slate-700/30">
                      <td className="px-4 py-3 font-mono text-xs text-slate-400">
                        {new Date(log.ts).toLocaleString()}
                      </td>
                      <td className="px-4 py-3 text-emerald-400">
                        {log.device}
                      </td>
                      <td className="px-4 py-3">
                        <span className={`px-2 py-1 text-xs font-semibold uppercase tracking-wider ${
                          log.event_type === 'fire' 
                            ? 'bg-red-900/50 text-red-400 border border-red-700' 
                            : 'bg-amber-900/50 text-amber-400 border border-amber-700'
                        }`}>
                          {log.event_type}
                        </span>
                      </td>
                      <td className="px-4 py-3 text-slate-400">
                        {log.message}
                      </td>
                    </tr>
                  ))
                )}
              </tbody>
            </table>
          </div>
        </div>

        {/* Advanced Section */}
        <div className="bg-slate-800 border border-slate-700">
          <button
            onClick={() => setShowAdvanced(!showAdvanced)}
            className="w-full px-6 py-4 text-left text-sm font-medium uppercase tracking-wider text-slate-400 hover:bg-slate-700/50 flex items-center justify-between"
          >
            Advanced: Custom MQTT
            <span className="text-xs">{showAdvanced ? '▲' : '▼'}</span>
          </button>
          {showAdvanced && (
            <form onSubmit={handleCustomPublish} className="p-6 border-t border-slate-700 flex gap-4 flex-wrap">
              <input
                type="text"
                value={customTopic}
                onChange={(e) => setCustomTopic(e.target.value)}
                placeholder="Topic"
                className="flex-1 min-w-64 px-4 py-2 bg-slate-900 border border-slate-600 text-slate-100 placeholder-slate-500 focus:outline-none focus:border-emerald-500"
              />
              <input
                type="text"
                value={customPayload}
                onChange={(e) => setCustomPayload(e.target.value)}
                placeholder="Payload"
                className="w-32 px-4 py-2 bg-slate-900 border border-slate-600 text-slate-100 placeholder-slate-500 focus:outline-none focus:border-emerald-500"
              />
              <button
                type="submit"
                className="px-6 py-2 bg-emerald-600 text-white text-xs font-semibold uppercase tracking-wider hover:bg-emerald-500 transition-colors"
              >
                Publish
              </button>
            </form>
          )}
        </div>
      </main>
    </div>
  );
}
