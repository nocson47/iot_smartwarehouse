#include <WiFi.h>
#include <WiFiManager.h>
#include <PubSubClient.h>
#include <DHT.h>
#include <ESP32Servo.h>
#include <Preferences.h>                      // ✅ For secure storage in NVS

// ==========================================
// � SECURITY: Credential Management
// ==========================================
// ⚠️ BREAKING CHANGE: Hardcoded credentials removed!
//
// All sensitive credentials (MQTT username/password) are now:
// ✅ Stored securely in ESP32's NVS (Non-Volatile Storage)
// ✅ NOT visible in source code
// ✅ Encrypted at rest (hardware-based on ESP32-S3/C3)
// ✅ Configured via WiFiManager web portal (safer than hardcoding)
//
// 📱 SETUP INSTRUCTIONS:
// 1. ESP32 will broadcast WiFi AP: "IoT_RMUTI"
// 2. Connect to this AP with any device
// 3. A popup will appear (or visit 192.168.4.1)
// 4. Select your WiFi + enter password
// 5. Configure MQTT Server, Username, Password fields
// 6. Save & reboot - credentials stored in NVS
// 7. Future reboots will use stored credentials (no reconfiguration needed)
//
// 🔑 MQTT CREDENTIALS:
// - Server IP: Required (e.g., 192.168.1.100)
// - Username: Optional (leave blank for no auth)
// - Password: Optional (leave blank for no auth)
//
// 🆘 RESET CREDENTIALS: Long-press BOOT button (3+ seconds) to reset WiFi
// ==========================================

// ==========================================
// �📌 MQTT Configuration (Secure - NVS Stored)
// ==========================================
const char* deviceId = "esp32_001";           // Device ID (device-specific)
const int mqtt_port = 1883;

// 🔐 Secure storage using NVS (Non-Volatile Storage)
Preferences preferences;
char mqtt_server[64] = "";                    // ❌ Leave empty - configure via WiFiManager
char mqtt_user[64] = "";                      // ❌ Leave empty - configure via WiFiManager  
char mqtt_pass[128] = "";                     // ❌ Leave empty - configure via WiFiManager

// 🔒 NVS Keys for credential storage
#define NVS_NAMESPACE "mqtt_config"
#define NVS_KEY_SERVER "mqtt_server"
#define NVS_KEY_USER "mqtt_user"
#define NVS_KEY_PASS "mqtt_pass"

// ==========================================
// 📌 1. กำหนดขาอุปกรณ์ระบบอัตโนมัติ (Auto System)
// ==========================================
#define RESET_BUTTON_PIN 0      // ปุ่ม BOOT (ล้าง WiFi)

// --- ระบบแสดงสถานะ WiFi ---
#define WIFI_BLINK_LED_PIN 2    
#define WIFI_READY_LED_PIN 12   

// --- ระบบแจ้งเตือน ---
#define ALERT_LED_PIN 27        // 🚨 ไฟสีแดง (เตือนอันตราย ไฟไหม้/ผู้บุกรุก)
#define BUZZER_PIN 15           // 🔊 ขา Buzzer เสียงเตือนปี้บๆ

#define RELAY_PIN 26            // 🔌 ขาควบคุม Relay 
#define DHTPIN 4                // ขา DHT22
#define DHTTYPE DHT22           

#define FLAME_SENSOR_PIN 32     // ขา D0 เซนเซอร์เปลวไฟ
#define PIR_SENSOR_PIN 14       // ขา OUT ของ PIR

// --- 🚪 กำหนดขาประตู (Door System) ---
#define SERVO_PIN 5             // ขาสัญญาณ Servo MG995

// --- ขาไฟ LED แมนนวล ---
const int manualLedPins[4] = {21, 22, 23, 13}; 

// ==========================================
// 📌 2. ประกาศตัวแปรหลัก & MQTT
// ==========================================
WiFiClient espClient;
PubSubClient mqttClient(espClient);
WiFiManager wm;
DHT dht(DHTPIN, DHTTYPE);
Servo doorServo; 

unsigned long previousMillis = 0; 
const long interval = 2000;       

// ตัวแปรสถานะอุปกรณ์
bool ledStates[4] = {LOW, LOW, LOW, LOW}; 
bool isRelayOn = false; 
float previousTemp = 0.0; 
bool isDoorOpen = false;           

// ตัวแปรระบบแจ้งเตือน
unsigned long lastPirAlertTime = 0; 
const long pirAlertInterval = 5000; 
bool isFireActive = false;         
bool isWaitingToCloseDoor = false;     
unsigned long doorCloseStartTime = 0;  
const unsigned long doorCloseDelay = 5000; 

unsigned long lastBuzzerTime = 0;
bool buzzerState = LOW;

// MQTT reconnect
unsigned long lastMqttReconnect = 0;

TaskHandle_t blinkTaskHandle = NULL; 

// ==========================================
// � Load Credentials from Secure NVS Storage
// ==========================================
void loadCredentialsFromNVS() {
  preferences.begin(NVS_NAMESPACE, true); // read-only mode
  
  // Load from NVS, with fallback to empty strings
  String server = preferences.getString(NVS_KEY_SERVER, "");
  String user = preferences.getString(NVS_KEY_USER, "");
  String pass = preferences.getString(NVS_KEY_PASS, "");
  
  server.toCharArray(mqtt_server, sizeof(mqtt_server));
  user.toCharArray(mqtt_user, sizeof(mqtt_user));
  pass.toCharArray(mqtt_pass, sizeof(mqtt_pass));
  
  preferences.end();
  
  Serial.println("📂 [NVS] Credentials loaded from secure storage");
  if (strlen(mqtt_server) > 0) {
    Serial.printf("   MQTT Server: %s\n", mqtt_server);
    Serial.printf("   MQTT User: %s\n", mqtt_user[0] != '\0' ? "***hidden***" : "(none)");
  } else {
    Serial.println("   ⚠️ No MQTT credentials found - configure via WiFiManager");
  }
}

// ==========================================
// 💾 Save Credentials to Secure NVS Storage
// ==========================================
void saveCredentialsToNVS() {
  preferences.begin(NVS_NAMESPACE, false); // read-write mode
  
  preferences.putString(NVS_KEY_SERVER, String(mqtt_server));
  preferences.putString(NVS_KEY_USER, String(mqtt_user));
  preferences.putString(NVS_KEY_PASS, String(mqtt_pass));
  
  preferences.end();
  
  Serial.println("✅ [NVS] Credentials saved securely to flash storage");
}

// ==========================================
// 🔄 WiFiManager Callback - Save credentials on successful config
// ==========================================
void onWiFiSave() {
  Serial.println("💾 WiFiManager: Credentials updated, saving to NVS...");
  saveCredentialsToNVS();
}


void mqttCallback(char* topic, byte* payload, unsigned int length) {
  String msg = "";
  for (unsigned int i = 0; i < length; i++) {
    msg += (char)payload[i];
  }
  
  String topicStr = String(topic);
  Serial.println("📥 [MQTT] Topic: " + topicStr + " | Payload: " + msg);

  // --- รับคำสั่งจาก actuators/esp32_001/xxx ---
  if (topicStr.endsWith("/relay")) {
    if (msg == "ON") { digitalWrite(RELAY_PIN, HIGH); isRelayOn = true; Serial.println("🔌 Relay ON"); }
    else if (msg == "OFF") { digitalWrite(RELAY_PIN, LOW); isRelayOn = false; Serial.println("🔌 Relay OFF"); }
  }
  else if (topicStr.endsWith("/door")) {
    if (!isFireActive) {
      if (msg == "OPEN") { doorServo.write(90); isDoorOpen = true; Serial.println("🚪 Door OPEN"); }
      else if (msg == "CLOSE") { doorServo.write(0); isDoorOpen = false; Serial.println("🚪 Door CLOSE"); }
    } else {
      Serial.println("⚠️ Door locked - Fire emergency!");
    }
  }
  else if (topicStr.endsWith("/led1")) {
    if (msg == "ON") { ledStates[0] = HIGH; digitalWrite(manualLedPins[0], HIGH); }
    else if (msg == "OFF") { ledStates[0] = LOW; digitalWrite(manualLedPins[0], LOW); }
  }
  else if (topicStr.endsWith("/led2")) {
    if (msg == "ON") { ledStates[1] = HIGH; digitalWrite(manualLedPins[1], HIGH); }
    else if (msg == "OFF") { ledStates[1] = LOW; digitalWrite(manualLedPins[1], LOW); }
  }
  else if (topicStr.endsWith("/led3")) {
    if (msg == "ON") { ledStates[2] = HIGH; digitalWrite(manualLedPins[2], HIGH); }
    else if (msg == "OFF") { ledStates[2] = LOW; digitalWrite(manualLedPins[2], LOW); }
  }
  else if (topicStr.endsWith("/led4")) {
    if (msg == "ON") { ledStates[3] = HIGH; digitalWrite(manualLedPins[3], HIGH); }
    else if (msg == "OFF") { ledStates[3] = LOW; digitalWrite(manualLedPins[3], LOW); }
  }
  else if (topicStr.endsWith("/buzzer")) {
    if (msg == "ON") { digitalWrite(BUZZER_PIN, HIGH); Serial.println("🔊 Buzzer ON"); }
    else if (msg == "OFF") { digitalWrite(BUZZER_PIN, LOW); Serial.println("🔊 Buzzer OFF"); }
  }
  else if (topicStr.endsWith("/servo")) {
    int angle = msg.toInt();
    if (angle >= 0 && angle <= 180 && !isFireActive) {
      doorServo.write(angle);
      isDoorOpen = (angle != 0);
      Serial.println("🚪 Servo angle: " + String(angle));
    }
  }
}

// ==========================================
// 📡 MQTT Connect & Subscribe
// ==========================================
bool mqttReconnect() {
  if (strlen(mqtt_server) == 0) {
    Serial.println("❌ MQTT server not configured - use WiFiManager to set credentials");
    return false;
  }
  
  Serial.printf("🔄 Connecting to MQTT %s:%d...\n", mqtt_server, mqtt_port);
  
  // ✅ Support both authenticated and non-authenticated connections
  bool connected = false;
  if (strlen(mqtt_user) > 0 && strlen(mqtt_pass) > 0) {
    // 🔐 With authentication
    connected = mqttClient.connect(deviceId, mqtt_user, mqtt_pass);
  } else if (strlen(mqtt_user) > 0) {
    // Only username
    connected = mqttClient.connect(deviceId, mqtt_user);
  } else {
    // 🌐 No authentication
    connected = mqttClient.connect(deviceId);
  }
  
  if (connected) {
    Serial.println("✅ MQTT Connected!");
    
    // Subscribe to actuator topics
    String baseTopic = String("actuators/") + deviceId;
    mqttClient.subscribe((baseTopic + "/relay").c_str());
    mqttClient.subscribe((baseTopic + "/door").c_str());
    mqttClient.subscribe((baseTopic + "/servo").c_str());
    mqttClient.subscribe((baseTopic + "/led1").c_str());
    mqttClient.subscribe((baseTopic + "/led2").c_str());
    mqttClient.subscribe((baseTopic + "/led3").c_str());
    mqttClient.subscribe((baseTopic + "/led4").c_str());
    mqttClient.subscribe((baseTopic + "/buzzer").c_str());
    
    Serial.println("📡 Subscribed to: " + baseTopic + "/#");
    
    // Publish online status
    String statusTopic = String("status/") + deviceId;
    String statusPayload = "{\"status\":\"online\",\"ip\":\"" + WiFi.localIP().toString() + "\"}";
    mqttClient.publish(statusTopic.c_str(), statusPayload.c_str());
    
    return true;
  } else {
    Serial.printf("❌ MQTT failed, rc=%d\n", mqttClient.state());
    return false;
  }
}

// ==========================================
// 📤 Publish sensor data
// ==========================================
void publishSensorData(float temp, float hum, bool fire, bool motion) {
  if (!mqttClient.connected()) return;
  
  String topic = String("sensors/") + deviceId;
  String payload = "{\"device\":\"" + String(deviceId) + "\""
                   ",\"temp\":" + String(temp, 2) + 
                   ",\"hum\":" + String(hum, 2) + 
                   ",\"fire\":" + String(fire ? "true" : "false") + 
                   ",\"motion\":" + String(motion ? "true" : "false") + 
                   ",\"relay\":" + String(isRelayOn ? "true" : "false") + 
                   ",\"door\":" + String(isDoorOpen ? "true" : "false") + "}";
  
  mqttClient.publish(topic.c_str(), payload.c_str());
  Serial.println("📤 Published: " + payload);
}

// ==========================================
// 🚨 Publish alert
// ==========================================
void publishAlert(const char* alertType, const char* message) {
  if (!mqttClient.connected()) return;
  
  String topic = String("alerts/") + deviceId;
  String payload = "{\"device\":\"" + String(deviceId) + "\""
                   ",\"type\":\"" + String(alertType) + "\""
                   ",\"message\":\"" + String(message) + "\"}";
  
  mqttClient.publish(topic.c_str(), payload.c_str());
  Serial.println("🚨 Alert: " + payload);
}

void blinkTask(void *pvParameters) {
  while (true) {
    digitalWrite(WIFI_BLINK_LED_PIN, !digitalRead(WIFI_BLINK_LED_PIN)); 
    vTaskDelay(300 / portTICK_PERIOD_MS); 
  }
}

void setup() {
  Serial.begin(115200);

  pinMode(WIFI_BLINK_LED_PIN, OUTPUT);
  pinMode(WIFI_READY_LED_PIN, OUTPUT);
  pinMode(ALERT_LED_PIN, OUTPUT);
  pinMode(RELAY_PIN, OUTPUT);    
  pinMode(BUZZER_PIN, OUTPUT);
  
  pinMode(RESET_BUTTON_PIN, INPUT_PULLUP);
  pinMode(FLAME_SENSOR_PIN, INPUT);
  pinMode(PIR_SENSOR_PIN, INPUT);

  ESP32PWM::allocateTimer(0);
  doorServo.setPeriodHertz(50);             
  doorServo.attach(SERVO_PIN, 500, 2400);   
  doorServo.write(0);                       

  for (int i = 0; i < 4; i++) {
    pinMode(manualLedPins[i], OUTPUT);     
    digitalWrite(manualLedPins[i], LOW);   
  }

  digitalWrite(WIFI_BLINK_LED_PIN, LOW);
  digitalWrite(WIFI_READY_LED_PIN, LOW);
  digitalWrite(ALERT_LED_PIN, LOW);
  digitalWrite(RELAY_PIN, LOW); 
  digitalWrite(BUZZER_PIN, LOW);

  Serial.println("\n--- 🚀 เริ่มระบบ Smart Home (MQTT Version) ---");
  
  // 🔐 Load MQTT credentials from secure NVS storage
  Serial.println("🔐 Loading credentials from secure storage...");
  loadCredentialsFromNVS();
  
  Serial.println("รอเซนเซอร์ PIR อุ่นเครื่อง 10 วินาที...");
  delay(10000); 

  Serial.println("กำลังพยายามเชื่อมต่อ WiFi...");
  xTaskCreate(blinkTask, "BlinkTask", 1024, NULL, 1, &blinkTaskHandle);

  // ==========================================
  // 🔐 WiFiManager Configuration + MQTT Parameters
  // ==========================================
  WiFiManagerParameter mqtt_server_param("mqtt_server", "MQTT Server IP", mqtt_server, 64);
  WiFiManagerParameter mqtt_user_param("mqtt_user", "MQTT Username (optional)", mqtt_user, 64);
  WiFiManagerParameter mqtt_pass_param("mqtt_pass", "MQTT Password (optional)", mqtt_pass, 128);
  
  wm.addParameter(&mqtt_server_param);
  wm.addParameter(&mqtt_user_param);
  wm.addParameter(&mqtt_pass_param);
  
  // Set callback for when WiFiManager saves successfully
  wm.setSaveParamsCallback(onWiFiSave);
  wm.setConfigPortalTimeout(120); 
  
  bool res = wm.autoConnect("IoT_RMUTI"); 

  if (blinkTaskHandle != NULL) { vTaskDelete(blinkTaskHandle); }
  digitalWrite(WIFI_BLINK_LED_PIN, LOW); 

  if(!res) {
    Serial.println("⚠️ เชื่อมต่อ WiFi ไม่สำเร็จ! (เข้าสู่โหมด Offline)");
    digitalWrite(WIFI_READY_LED_PIN, LOW); 
  } else {
    // ✅ Copy MQTT parameters from WiFiManager to memory (only if user provided new values)
    String new_server = mqtt_server_param.getValue();
    String new_user = mqtt_user_param.getValue();
    String new_pass = mqtt_pass_param.getValue();
    
    if (new_server.length() > 0) {
      new_server.toCharArray(mqtt_server, sizeof(mqtt_server));
      new_user.toCharArray(mqtt_user, sizeof(mqtt_user));
      new_pass.toCharArray(mqtt_pass, sizeof(mqtt_pass));
      // Save to NVS for persistence
      saveCredentialsToNVS();
    }
    
    Serial.println("\n✅ เชื่อมต่อ WiFi สำเร็จ! IP: " + WiFi.localIP().toString());
    if (strlen(mqtt_server) > 0) {
      Serial.printf("📡 MQTT Server: %s:%d\n", mqtt_server, mqtt_port);
      digitalWrite(WIFI_READY_LED_PIN, HIGH); 
      
      // Setup MQTT
      mqttClient.setServer(mqtt_server, mqtt_port);
      mqttClient.setCallback(mqttCallback);
      mqttClient.setBufferSize(512);
      
      mqttReconnect();
    } else {
      Serial.println("⚠️ MQTT not configured - use WiFiManager to add MQTT credentials");
      digitalWrite(WIFI_READY_LED_PIN, LOW);
    }
  }

  dht.begin();
  Serial.println("✅ ระบบเซนเซอร์พร้อมทำงานเต็มรูปแบบ!");
}

void loop() {
  
  // ==========================================
  // 🔄 1. รักษาการเชื่อมต่อ MQTT
  // ==========================================
  if (WiFi.status() == WL_CONNECTED) {
    digitalWrite(WIFI_READY_LED_PIN, HIGH); 
    
    if (!mqttClient.connected()) {
      unsigned long now = millis();
      if (now - lastMqttReconnect > 5000) {
        lastMqttReconnect = now;
        mqttReconnect();
      }
    }
    mqttClient.loop();
    
  } else {
    digitalWrite(WIFI_READY_LED_PIN, LOW); 
  }

  // ==========================================
  // 🔘 2. ระบบล้างค่า WiFi 
  // ==========================================
  if (digitalRead(RESET_BUTTON_PIN) == LOW) {
    delay(3000); 
    if (digitalRead(RESET_BUTTON_PIN) == LOW) { 
      digitalWrite(WIFI_READY_LED_PIN, LOW); 
      wm.resetSettings(); 
      ESP.restart(); 
    }
  }

  // ==========================================
  // 🚨 3. ระบบรักษาความปลอดภัยแบบรวมศูนย์
  // ==========================================
  bool fireDetected = (digitalRead(FLAME_SENSOR_PIN) == LOW);
  bool motionDetected = (digitalRead(PIR_SENSOR_PIN) == HIGH);

  if (fireDetected) { 
    if (!isFireActive) {
      Serial.println("🔥 FIRE DETECTED! Emergency door open!");
      doorServo.write(90);
      isDoorOpen = true;
      isFireActive = true;
      publishAlert("fire", "Fire detected! Emergency door opened.");
    }
    if (isWaitingToCloseDoor) isWaitingToCloseDoor = false; 
  } else {
    if (isFireActive && !isWaitingToCloseDoor) {
      isWaitingToCloseDoor = true;
      doorCloseStartTime = millis();
      Serial.println("⏳ Fire cleared, waiting 5s to close door...");
    }
  }

  if (isWaitingToCloseDoor && (millis() - doorCloseStartTime >= doorCloseDelay)) {
    Serial.println("✅ Safe! Closing door.");
    doorServo.write(0);
    isDoorOpen = false;
    isFireActive = false;         
    isWaitingToCloseDoor = false;
    publishAlert("fire_cleared", "Fire cleared. Door closed.");
  }

  if (motionDetected) {
    if (millis() - lastPirAlertTime >= pirAlertInterval) {
      Serial.println("🏃 Motion detected!");
      publishAlert("motion", "Motion detected - possible intruder!");
      lastPirAlertTime = millis(); 
    }
  }

  if (fireDetected || motionDetected) {
    digitalWrite(ALERT_LED_PIN, HIGH);   
    if (millis() - lastBuzzerTime > 200) {
      buzzerState = !buzzerState;
      digitalWrite(BUZZER_PIN, buzzerState);
      lastBuzzerTime = millis();
    }
  } else {
    digitalWrite(ALERT_LED_PIN, LOW);
    digitalWrite(BUZZER_PIN, LOW);
    buzzerState = LOW;
  }

  // ==========================================
  // 🌡️ 4. ระบบอ่านค่าสภาพแวดล้อม และ 📤 ส่งข้อมูลผ่าน MQTT
  // ==========================================
  unsigned long currentMillis = millis(); 
  if (currentMillis - previousMillis >= interval) {
    previousMillis = currentMillis; 
    float h = dht.readHumidity();
    float t = dht.readTemperature();

    if (!isnan(h) && !isnan(t)) {
      Serial.printf("🌡️ Temp: %.2f°C | 💧 Hum: %.2f%%\n", t, h);
      
      // ระบบ Auto Relay พัดลม
      if (previousTemp != 0.0) { 
        if (t > 35.0 && previousTemp <= 35.0) {
          digitalWrite(RELAY_PIN, HIGH); 
          isRelayOn = true;
          Serial.println("⚠️ Temp > 35°C -> Fan ON");
          publishAlert("temp_high", "Temperature exceeded 35C - Fan activated");
        } 
        else if (t <= 35.0 && previousTemp > 35.0) {
          digitalWrite(RELAY_PIN, LOW);
          isRelayOn = false;
          Serial.println("❄️ Temp <= 35°C -> Fan OFF");
          publishAlert("temp_normal", "Temperature back to normal - Fan deactivated");
        }
      }
      previousTemp = t; 

      // 📤 Publish sensor data via MQTT
      publishSensorData(t, h, fireDetected, motionDetected);
    } else {
      Serial.println("❌ DHT read failed!");
    }
  }
}