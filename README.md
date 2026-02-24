# IoT Warehouse Smart Detection and Control System

ระบบ IoT สำหรับตรวจจับและควบคุมอุปกรณ์ในโกดัง/คลังสินค้า ผ่าน MQTT พร้อม Dashboard แบบ Real-time

## สารบัญ

- [Architecture](#architecture)
- [Features](#features)
- [Prerequisites](#prerequisites)
- [Quick Start](#quick-start)
- [Environment Variables](#environment-variables)
- [Database Access](#database-access-pgadmin)
- [ESP32 Setup](#esp32-setup)
- [MQTT Topics](#mqtt-topics)
- [Dashboard](#dashboard)
- [Security](#security) ⭐ NEW
- [Cloudflare Tunnel](#cloudflare-tunnel-optional)
- [Troubleshooting](#troubleshooting)

---

## Architecture

```
┌──────────────────────────────────────────────────────────────────────────┐
│                           Docker Compose Stack                           │
│                                                                          │
│  ┌───────────────┐   ┌───────────────┐   ┌───────────────┐              │
│  │   Frontend    │   │    Server     │   │   Postgres    │              │
│  │  (Next.js)    │   │     (Go)      │   │     (DB)      │              │
│  │  Port: 3000   │   │  Port: 5555   │   │  Port: 5432   │              │
│  └───────┬───────┘   └───────┬───────┘   └───────────────┘              │
│          │                   │                                           │
│          │  HTTP/WebSocket   │  MQTT                                     │
│          └───────────────────┼───────────────────────────────────────────┤
│                              │                                           │
│                        ┌─────┴─────┐        ┌───────────────┐           │
│                        │  MQTT     │        │   pgAdmin     │           │
│                        │  Broker   │        │  Port: 5050   │           │
│                        │ (HiveMQ)  │        └───────────────┘           │
│                        └─────┬─────┘                                     │
│                              │                                           │
└──────────────────────────────┼───────────────────────────────────────────┘
                               │
                     ┌─────────┴─────────┐
                     │      ESP32        │
                     │  ┌─────────────┐  │
                     │  │ DHT22       │  │  Temperature & Humidity
                     │  │ Flame Sensor│  │  Fire Detection
                     │  │ PIR Sensor  │  │  Motion Detection
                     │  │ Relay (Fan) │  │  Auto Fan Control
                     │  │ Servo (Door)│  │  Emergency Door
                     │  │ LEDs x4     │  │  Manual Control
                     │  │ Buzzer      │  │  Alert Sound
                     │  └─────────────┘  │
                     └───────────────────┘
```

---

## Features

- **Real-time Monitoring** - อุณหภูมิ, ความชื้น, ไฟไหม้, การเคลื่อนไหว
- **Auto Fan Control** - พัดลมทำงานอัตโนมัติเมื่ออุณหภูมิสูงเกิน 30°C
- **Fire Alert** - ตรวจจับเปลวไฟ เปิดประตูอัตโนมัติ + เสียงเตือน
- **Motion Detection** - ตรวจจับผู้บุกรุก + บันทึก Log
- **Remote Control** - ควบคุม LED, Relay, Door ผ่าน Dashboard
- **Event Logging** - บันทึกเหตุการณ์ Motion/Fire ลง Database

---

## Prerequisites

### สำหรับรันระบบ (Production)
- **Docker** & **Docker Compose** - [Download](https://docs.docker.com/get-docker/)

### สำหรับพัฒนา (Development)
- **Node.js 20+** & **pnpm** - สำหรับ Frontend
- **Go 1.24+** - สำหรับ Backend

### สำหรับ ESP32
- **Arduino IDE** หรือ **PlatformIO**
- **Libraries ที่ต้องติดตั้ง:**
  - WiFiManager
  - PubSubClient
  - DHT sensor library
  - ESP32Servo

---

## Quick Start

### 1. Clone Repository

```bash
git clone https://github.com/your-username/IoT_SmartDetectionandControl.git
cd IoT_SmartDetectionandControl
```

### 2. Setup Environment Variables

```bash
# Copy template
cp .env.example .env

# แก้ไขค่าใน .env ตามต้องการ
nano .env
```

### 3. Start Docker Containers

```bash
# Build และ Start ทุก container
docker compose up -d --build

# ดู logs
docker compose logs -f
```

### 4. เข้าใช้งาน

| Service | URL | Description |
|---------|-----|-------------|
| **Dashboard** | http://localhost:3000 | หน้าควบคุมหลัก |
| **API Server** | http://localhost:5555 | Go Backend |
| **pgAdmin** | http://localhost:5050 | Database Admin UI |
| **MailHog** | http://localhost:8025 | Email Testing UI |

### 5. สมัครสมาชิก

1. เปิด Dashboard ที่ http://localhost:3000
2. กด **Register** ใส่ Email + Password
3. เปิด **MailHog** ที่ http://localhost:8025 คลิก Verify Link
4. กลับมา Login ได้เลย

---

## Environment Variables

สร้างไฟล์ `.env` จาก template:

```bash
cp .env.example .env
```

แก้ไขค่าตามต้องการ:

```dotenv
# ========== Database ==========
POSTGRES_USER=iot_user          # ชื่อ user database
POSTGRES_PASSWORD=your_password # รหัสผ่าน (เปลี่ยนเป็นค่าที่ปลอดภัย)
POSTGRES_DB=iot_warehouse       # ชื่อ database

# ========== pgAdmin ==========
PGADMIN_EMAIL=admin@local.dev   # Email สำหรับ login pgAdmin
PGADMIN_PASSWORD=admin_pass     # รหัสผ่าน pgAdmin

# ========== Server ==========
BASE_URL=http://localhost:5555  # URL ของ Backend API
MQTT_BROKER=tcp://broker.hivemq.com:1883  # Public MQTT Broker

# ========== SMTP (MailHog for dev) ==========
SMTP_HOST=mailhog:1025          # SMTP server
SMTP_FROM=noreply@iot.local     # ชื่อผู้ส่ง email
```

> **หมายเหตุ:** ไม่ควร commit ไฟล์ `.env` ขึ้น Git (มี .gitignore อยู่แล้ว)

---

## Database Access (pgAdmin)

### เข้า pgAdmin

1. เปิด http://localhost:5050
2. Login ด้วย email/password จาก `.env`:
   - Email: ค่า `PGADMIN_EMAIL`
   - Password: ค่า `PGADMIN_PASSWORD`

### เพิ่ม Server Connection

1. คลิกขวาที่ **Servers** → **Register** → **Server**
2. Tab **General**:
   - Name: `IoT Warehouse`
3. Tab **Connection**:
   - Host: `postgres` (ชื่อ container)
   - Port: `5432`
   - Username: ค่า `POSTGRES_USER` จาก `.env`
   - Password: ค่า `POSTGRES_PASSWORD` จาก `.env`
4. กด **Save**

### Tables ในระบบ

| Table | Description |
|-------|-------------|
| `users` | ข้อมูล user (email, password hash, verified) |
| `readings` | ข้อมูล sensor ทั้งหมด (temp, hum, fire, motion) |
| `motion_logs` | Log เหตุการณ์ motion/fire |

### Query ตัวอย่าง

```sql
-- ดู readings ล่าสุด 10 รายการ
SELECT * FROM readings ORDER BY ts DESC LIMIT 10;

-- ดู motion logs
SELECT * FROM motion_logs ORDER BY ts DESC;

-- ดู users ทั้งหมด
SELECT id, email, verified, created_at FROM users;
```

---

## ESP32 Setup

### Wiring Diagram

```
ESP32 DevKit         Sensors & Actuators
===============      ========================================

GPIO 4   ────────────  DHT22 (Data)
GPIO 32  ────────────  Flame Sensor (D0)
GPIO 14  ────────────  PIR Sensor (OUT)

GPIO 26  ────────────  Relay Module (IN)
GPIO 5   ────────────  Servo Motor (Signal)
GPIO 15  ────────────  Buzzer (+)

GPIO 2   ────────────  WiFi Status LED (Blinking)
GPIO 12  ────────────  WiFi Ready LED (Solid)
GPIO 27  ────────────  Alert LED (Red)

GPIO 21  ────────────  Manual LED 1
GPIO 22  ────────────  Manual LED 2
GPIO 23  ────────────  Manual LED 3
GPIO 13  ────────────  Manual LED 4

GPIO 0   ────────────  Reset Button (ล้าง WiFi Settings)
```

### ติดตั้ง Libraries

ใน Arduino IDE ไปที่ **Sketch** → **Include Library** → **Manage Libraries**:

1. ค้นหาและติดตั้ง:
   - `WiFiManager` by tzapu
   - `PubSubClient` by Nick O'Leary
   - `DHT sensor library` by Adafruit
   - `ESP32Servo` by Kevin Harrington

### Upload Code

1. เปิดไฟล์ `esp32/esp32_mqtt_full.ino`
2. แก้ไข Device ID (ถ้าต้องการ):
   ```cpp
   const char* deviceId = "esp32_001";  // เปลี่ยนตาม device
   ```
3. เลือก Board: **ESP32 Dev Module**
4. กด **Upload**

### ตั้งค่า WiFi

1. หลัง Upload เสร็จ ESP32 จะสร้าง Access Point ชื่อ **ESP32_Setup**
2. ใช้มือถือเชื่อมต่อ WiFi **ESP32_Setup**
3. เปิด Browser ไปที่ http://192.168.4.1
4. เลือก WiFi และใส่ Password
5. ESP32 จะ restart และเชื่อมต่อ MQTT อัตโนมัติ

### Reset WiFi Settings

กดปุ่ม **BOOT** (GPIO 0) ค้าง 3 วินาที จะเข้าโหมด AP Setup ใหม่

---

## MQTT Topics

### Sensor Data (ESP32 → Server)

| Topic | Payload | Description |
|-------|---------|-------------|
| `sensors/esp32_001` | `{"temp":25.5,"hum":60,"fire":false,"motion":false,"relay":true,"door":false}` | ส่งทุก 2 วินาที |
| `alerts/esp32_001` | `{"fire":true}` หรือ `{"motion":true}` | เมื่อเกิดเหตุการณ์ |
| `status/esp32_001` | `online` / `offline` | สถานะ connection |

### Actuator Commands (Server → ESP32)

| Topic | Payload | Description |
|-------|---------|-------------|
| `actuators/esp32_001/relay` | `ON` / `OFF` | เปิด/ปิด Relay (พัดลม) |
| `actuators/esp32_001/door` | `OPEN` / `CLOSE` | เปิด/ปิด ประตู |
| `actuators/esp32_001/buzzer` | `ON` / `OFF` | เปิด/ปิด Buzzer |
| `actuators/esp32_001/led1` | `ON` / `OFF` | LED 1 |
| `actuators/esp32_001/led2` | `ON` / `OFF` | LED 2 |
| `actuators/esp32_001/led3` | `ON` / `OFF` | LED 3 |
| `actuators/esp32_001/led4` | `ON` / `OFF` | LED 4 |
| `actuators/esp32_001/servo` | `0` - `180` | มุม Servo |

---

## Dashboard

Dashboard ออกแบบแบบ **Military Minimal** - สีเข้ม, professional

### Features

- **Environment Panel** - แสดงอุณหภูมิ/ความชื้นแบบ real-time
- **Sensor Graph** - กราฟแสดงข้อมูลย้อนหลัง (Temperature & Humidity)
- **Control Panel** - ปุ่มควบคุม Relay, Door, Buzzer, LEDs
- **Live Data Stream** - แสดง raw data จาก sensor
- **Event Log** - บันทึกเหตุการณ์ Motion/Fire

### API Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/register` | สมัครสมาชิก |
| POST | `/api/login` | Login |
| GET | `/api/verify?token=xxx` | Verify email |
| GET | `/api/readings?limit=100` | ดึงข้อมูล sensor |
| POST | `/api/publish` | ส่งคำสั่ง MQTT |
| GET | `/api/logs?limit=100` | ดึง event logs |
| WS | `/ws` | WebSocket real-time |

---

## Security 🔐

### ที่ต้องใส่ใจ

#### 1. Environment Variables
- **ไม่ต้อง commit `.env` ขึ้น Git** - ใช้ `.env.example` เป็น template
- **เปลี่ยน default passwords** - ในไฟล์ `.env` ให้ใช้รหัสผ่านที่ปลอดภัย:
  ```bash
  # ❌ WRONG (Default)
  POSTGRES_PASSWORD=iot_pass
  MQTT_PASS=iot_secret_2026
  
  # ✅ RIGHT (Secure)
  POSTGRES_PASSWORD=$(openssl rand -base64 32)  # Generate secure password
  MQTT_PASS=$(openssl rand -base64 32)
  ```

#### 2. ESP32 Credentials
- **MQTT credentials** อยู่ใน `esp32/esp32_mqtt_full.ino` - ใช้ค่าจาก `.env` file แทน hardcode
- **WiFi credentials** ถูก configure ผ่าน WiFiManager UI - ไม่ได้ hardcode

#### 3. Database Security
- **Postgres password** ต้องเปลี่ยนจาก default `iot_pass` 
- **pgAdmin password** ต้องเปลี่ยนจาก default `admin`
- **Enable SSL/TLS** สำหรับ production databases

#### 4. API Security (Already Implemented)
- ✅ **JWT Token Authentication** - ทุก request ต้องมี Bearer token
- ✅ **Rate Limiting** - Limit 5 login attempts ต่อ 15 นาที
- ✅ **Email Verification** - User ต้อง verify email ก่อน login
- ✅ **Password Hashing** - ใช้ bcrypt hash passwords
- ✅ **Session Expiry** - Sessions หมดเวลาใน 24 ชม.

#### 5. MQTT Security
- **Authentication** - ต้องมี username/password (default: `iot_device`/`iot_secret_2026`)
- **Authorization** - กำหนด ACL per topic (recommended)
- **TLS/SSL** - ใช้เมื่อ production (port 8883 or 8884)

#### 6. Git & Version Control
- 🔒 **Secrets ถูก ignore** - `.env`, `*.key`, `*.pem`, `secrets/` ล้วนอยู่ใน `.gitignore`
- 🔒 **Sensitive files** - ไม่มี hardcoded tokens, API keys, passwords
- 📝 **Use `.env.example`** - แสดง template variables แต่ไม่มี actual values

#### 7. Production Checklist
```bash
# ✅ Before deploying to production:

# 1. Change all default passwords
nano .env                    # set strong passwords

# 2. Enable HTTPS/SSL
# - ใช้ Let's Encrypt สำหรับ certificates
# - Configure nginx/load balancer กับ SSL

# 3. Enable MQTT TLS
# - Generate SSL certificates
# - Configure mosquitto with TLS port

# 4. Database backups
# - Setup automated backups
# - Test restore procedures

# 5. Monitor & Logging
# - Setup log aggregation
# - Monitor performance metrics

# 6. Access Control
# - Limit access to admin ports (5050, 5432)
# - Use VPN/Firewall rules
```

---

## Cloudflare Tunnel (Optional)

สำหรับเข้าถึงจากภายนอก network โดยไม่ต้อง port forward:

### ติดตั้ง cloudflared

```bash
# macOS
brew install cloudflared

# Linux
curl -L https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-amd64 -o cloudflared
chmod +x cloudflared
sudo mv cloudflared /usr/local/bin/
```

### Quick Tunnel

```bash
cloudflared tunnel --url http://localhost:3000
```

จะได้ URL แบบ `https://xxxxx.trycloudflare.com` ส่งให้ทีมใช้งานได้เลย

---

## Troubleshooting

### Docker Issues

```bash
# ดู containers ที่รันอยู่
docker compose ps

# ดู logs ของ service
docker compose logs server
docker compose logs frontend

# Restart ทุก service
docker compose down && docker compose up -d --build

# ลบ volumes ทั้งหมด (ระวัง: ข้อมูล DB จะหาย)
docker compose down -v
```

### ESP32 Issues

| ปัญหา | วิธีแก้ |
|-------|--------|
| ไม่เชื่อม WiFi | กดปุ่ม BOOT ค้าง 3 วินาที ตั้งค่าใหม่ |
| DHT อ่านค่าไม่ได้ | เช็คสาย VCC/GND/Data, ใส่ pull-up 4.7K |
| Servo สั่น | ใช้ external power supply 5V 2A |
| MQTT disconnect | เช็ค WiFi signal, ลองเปลี่ยน broker |

### Database Issues

```bash
# เข้า postgres container
docker compose exec postgres psql -U your_db_user -d your_db_name

# ดู tables
\dt

# ดูข้อมูล
SELECT * FROM readings LIMIT 5;

# ออก
\q
```

### Frontend Issues

```bash
# Rebuild frontend
docker compose build frontend
docker compose up -d frontend

# ดู build logs
docker compose logs frontend

# Clear Next.js cache
rm -rf frontend/my-app/.next
```

### WebSocket Connection Issues

```bash
# Check WebSocket logs
docker logs -f iot_smartdetectionandcontrol-server-1

# Check MQTT connection
mosquitto_sub -h localhost -p 1883 -t "sensors/#" -u iot_device -P iot_secret_2026

# Test WebSocket manually
wscat -c ws://localhost:5000/ws
```

### Performance Issues

| ปัญหา | วิธีแก้ |
|-------|--------|
| High CPU | ลดจำนวน WebSocket clients หรือ polling frequency |
| High Memory | ลดจำนวน readings cache (แก้ใน Dashboard.tsx) |
| Slow API | Check database indexes หรือ optimize queries |
| Graph sluggish | ลดจำนวน data points ใน chart (ปัจจุบัน 50 points) |

---

## Development Tips

### Local Development Setup

```bash
# Frontend development
cd frontend/my-app
pnpm dev        # Runs on http://localhost:3000

# Backend development  
go run main.go auth.go

# Database management
docker compose up -d postgres pgadmin
# Access pgAdmin at http://localhost:5050
```

### Testing
```bash
# Test API endpoints
curl -X POST http://localhost:5000/api/register \
  -H "Content-Type: application/json" \
  -d '{"email":"test@test.com","password":"test123"}'

# Test MQTT publish
mosquitto_pub -h localhost -t "sensors/test" -m '{"temp":25.5,"hum":60}'

# Run Go tests
go test ./...
```

---

## Project Structure (Detailed)

```
IoT_SmartDetectionandControl/
│
├── 🔧 Configuration Files
│   ├── docker-compose.yml          # All services config (postgres, mosquitto, server, frontend, pgadmin)
│   ├── .env.example                # Environment template (COPY THIS TO .env for local dev)
│   ├── .gitignore                  # Git ignore patterns (secrets, logs, build output)
│   ├── Dockerfile                  # Go server Docker build
│   ├── Makefile                    # Build tasks
│   └── go.mod / go.sum             # Go dependencies
│
├── 📱 Backend (Go Server)
│   ├── main.go                     # HTTP handlers, WebSocket, MQTT subscribe
│   ├── auth.go                     # Auth logic, rate limiting, email verification
│   ├── db/migrations.sql           # Database schema migrations
│   └── static/index.html           # Fallback HTML
│
├── 🎨 Frontend (Next.js)
│   └── frontend/my-app/
│       ├── src/
│       │   ├── app/
│       │   │   ├── page.tsx        # Login/Register UI
│       │   │   ├── layout.tsx      # App layout
│       │   │   └── globals.css     # Global styles (Tailwind)
│       │   ├── components/
│       │   │   ├── Dashboard.tsx   # Main dashboard (sensors, charts, controls)
│       │   │   └── AuthForm.tsx    # Login/register form
│       │   ├── hooks/
│       │   │   ├── useWebSocket.ts # Real-time data via WS
│       │   │   ├── useAuth.ts      # Authentication state
│       │   │   └── ...
│       │   ├── lib/
│       │   │   └── api.ts          # API client functions
│       │   └── types/
│       │       └── index.ts        # TypeScript types
│       ├── Dockerfile              # Next.js production build
│       ├── nginx.conf              # Production nginx config
│       ├── tsconfig.json           # TypeScript config
│       ├── next.config.ts          # Next.js config
│       └── package.json            # Dependencies (react, chart.js, tailwindcss)
│
├── 🎛️ ESP32 Firmware
│   └── esp32/
│       └── esp32_mqtt_full.ino     # Full Arduino sketch
│                                    # - WiFiManager config
│                                    # - DHT22/Flame/PIR sensors
│                                    # - MQTT publish (sensors, alerts)
│                                    # - Actuator control (LEDs, relay, servo, buzzer)
│
├── 🐝 MQTT Broker Config
│   └── mosquitto/
│       ├── mosquitto.conf          # Broker configuration
│       └── passwd                  # MQTT user credentials
│
├── 🤖 Simulator
│   └── simulator/
│       └── sensor_simulator.py     # Python script to simulate sensor data
│                                    # (for testing without ESP32)
│
├── 📊 Database
│   └── db/
│       └── migrations.sql          # SQL schema (users, sessions, readings, motion_logs)
│
└── 📝 Documentation
    ├── README.md                   # This file
    └── docs/                       # (optional) Additional docs

## Key Files Explained

### Backend Files
- **main.go** - Server entry point, HTTP routes, WebSocket handler
- **auth.go** - User registration, login, JWT tokens, rate limiting
- Database schema is auto-created if missing

### Frontend Files
- **Dashboard.tsx** - Main UI component (chart, controls, logs)
- **useWebSocket.ts** - WebSocket connection + message handling
- **api.ts** - HTTP API client wrapper

### Configuration Files
- **.env** - Local environment (secrets, DO NOT COMMIT)
- **.env.example** - Template (safe to commit)
- **.gitignore** - Excludes .env, node_modules, build artifacts
- **docker-compose.yml** - Orchestrates all services

---

## Tech Stack (Detailed)

| Component | Technology | Version |
|-----------|-----------|---------|
| **Frontend-Build** | Next.js | 16 |
| **Frontend-Framework** | React | 19 |
| **Frontend-Styling** | Tailwind CSS | 4 |
| **Frontend-Charts** | Chart.js + date-fns | 4.5 + 3.0 |
| **Backend** | Go | 1.24 |
| **Backend-WebSocket** | gorilla/websocket | latest |
| **Backend-MQTT** | paho.mqtt | latest |
| **Database-SQL** | PostgreSQL | 15-alpine |
| **Database-Local** | SQLite | embedded |
| **MQTT-Broker** | Mosquitto / HiveMQ | 2.0 / Public |
| **DevOps** | Docker Compose | latest |
| **Microcontroller** | ESP32 DevKit | board:esp32 |

---

## License

MIT License - ใช้งานได้ฟรี ดัดแปลงได้ตามต้องการ

ถ้าใช้ในโครงการ ขอให้ cite author หรือให้ credit ด้วย เพื่อส่วมน.

---

## Contributing

1. **Fork** the repository
2. **Create branch**: `git checkout -b feature/amazing-feature`
3. **Commit changes**: `git commit -m 'Add amazing feature'`
4. **Push to branch**: `git push origin feature/amazing-feature`
5. **Open Pull Request**

### Contribution Guidelines
- Follow existing code style
- Add tests for new features
- Update README.md for major changes
- Keep commits atomic and descriptive

---

## Contact & Support

- 📧 Issues & Bugs: Open an GitHub Issue
- 💬 Questions: Start a Discussion
- 🐛 Security Issues: Do NOT open public issue - contact privately

---

**Happy IoT Building! 🚀**

ทำให้ติดตั้งได้ง่าย โดยใช้ Docker - ใช้ 3 คำสั่งเพียงแค่นี้:
\`\`\`bash
git clone <repo>
# แก้ไข .env ด้วย passwords ที่ปลอดภัย
docker compose up -d
\`\`\`

Done! Dashboard สามารถเข้าถึงได้ที่ http://localhost:3000 หรือ url ของคุณ
 
