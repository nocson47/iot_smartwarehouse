package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/smtp"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// ==========================================
// 🔒 Rate Limiter
// ==========================================
type RateLimiter struct {
	mu       sync.RWMutex
	attempts map[string]*loginAttempt
}

type loginAttempt struct {
	count     int
	firstTry  time.Time
	blockedAt time.Time
}

const (
	maxLoginAttempts = 5                // จำนวนครั้งสูงสุดที่ลองได้
	attemptWindow    = 15 * time.Minute // ภายในเวลา 15 นาที
	blockDuration    = 30 * time.Minute // บล็อค 30 นาที
	sessionDuration  = 24 * time.Hour   // session หมดเวลาใน 24 ชั่วโมง
)

func NewRateLimiter() *RateLimiter {
	rl := &RateLimiter{
		attempts: make(map[string]*loginAttempt),
	}
	// Cleanup old entries every 10 minutes
	go func() {
		for {
			time.Sleep(10 * time.Minute)
			rl.cleanup()
		}
	}()
	return rl
}

func (rl *RateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	for key, attempt := range rl.attempts {
		// Remove if past block duration and attempt window
		if now.Sub(attempt.firstTry) > attemptWindow && now.Sub(attempt.blockedAt) > blockDuration {
			delete(rl.attempts, key)
		}
	}
}

func (rl *RateLimiter) IsBlocked(ip string) (bool, time.Duration) {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	attempt, exists := rl.attempts[ip]
	if !exists {
		return false, 0
	}

	if !attempt.blockedAt.IsZero() {
		remaining := blockDuration - time.Since(attempt.blockedAt)
		if remaining > 0 {
			return true, remaining
		}
	}
	return false, 0
}

func (rl *RateLimiter) RecordAttempt(ip string, success bool) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	attempt, exists := rl.attempts[ip]
	if !exists {
		attempt = &loginAttempt{firstTry: time.Now()}
		rl.attempts[ip] = attempt
	}

	// Reset if window expired
	if time.Since(attempt.firstTry) > attemptWindow {
		attempt.count = 0
		attempt.firstTry = time.Now()
		attempt.blockedAt = time.Time{}
	}

	if success {
		// Reset on successful login
		delete(rl.attempts, ip)
		return
	}

	attempt.count++
	if attempt.count >= maxLoginAttempts {
		attempt.blockedAt = time.Now()
		log.Printf("🚨 IP %s blocked due to too many failed login attempts", ip)
	}
}

func (rl *RateLimiter) GetAttemptCount(ip string) int {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	if attempt, exists := rl.attempts[ip]; exists {
		return attempt.count
	}
	return 0
}

func randToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (s *Server) sendVerificationEmail(to, token string) error {
	smtpHost := os.Getenv("SMTP_HOST")
	if smtpHost == "" {
		smtpHost = "localhost:1025"
	}
	from := os.Getenv("SMTP_FROM")
	if from == "" {
		from = "noreply@example.com"
	}
	base := os.Getenv("BASE_URL")
	if base == "" {
		base = "http://localhost:5000"
	}
	verifyURL := fmt.Sprintf("%s/api/verify?token=%s", base, token)

	body := fmt.Sprintf("To: %s\r\nSubject: Verify your account\r\n\r\nPlease verify your account by visiting: %s\r\n", to, verifyURL)

	// MailHog / local dev SMTP usually does not require auth
	return smtp.SendMail(smtpHost, nil, from, []string{to}, []byte(body))
}

func (s *Server) apiRegister(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if p.Email == "" || p.Password == "" {
		http.Error(w, "missing fields", http.StatusBadRequest)
		return
	}
	// hash password
	ph, err := bcrypt.GenerateFromPassword([]byte(p.Password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	token, err := randToken(16)
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}

	// insert user (use Postgres or sqlite placeholders depending on env)
	tx, err := s.db.Begin()
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()
	var insertQ string
	if os.Getenv("DATABASE_URL") != "" {
		insertQ = "INSERT INTO users (email, password_hash, verify_token, is_verified, created_at) VALUES ($1, $2, $3, $4, now())"
		_, err = tx.Exec(insertQ, p.Email, string(ph), token, false)
	} else {
		insertQ = "INSERT INTO users (email, password_hash, verify_token, is_verified, created_at) VALUES (?, ?, ?, ?, datetime('now'))"
		_, err = tx.Exec(insertQ, p.Email, string(ph), token, 0)
	}
	if err != nil {
		http.Error(w, "email exists or db error", http.StatusBadRequest)
		return
	}
	if err := tx.Commit(); err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}

	// send verification email
	if err := s.sendVerificationEmail(p.Email, token); err != nil {
		log.Println("send mail error:", err)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) apiVerify(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "missing token", http.StatusBadRequest)
		return
	}
	var res sql.Result
	var err error
	if os.Getenv("DATABASE_URL") != "" {
		res, err = s.db.Exec("UPDATE users SET is_verified = $1, verify_token = NULL WHERE verify_token = $2", true, token)
	} else {
		res, err = s.db.Exec("UPDATE users SET is_verified = ?, verify_token = NULL WHERE verify_token = ?", 1, token)
	}
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		http.Error(w, "invalid token", http.StatusBadRequest)
		return
	}
	w.Write([]byte("verified"))
}

// ==========================================
// 🔐 Login with Rate Limiting & Logging
// ==========================================
func (s *Server) getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header first (for reverse proxy)
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		ips := strings.Split(xff, ",")
		return strings.TrimSpace(ips[0])
	}
	// Check X-Real-IP
	xri := r.Header.Get("X-Real-IP")
	if xri != "" {
		return xri
	}
	// Fallback to RemoteAddr
	ip := r.RemoteAddr
	if idx := strings.LastIndex(ip, ":"); idx != -1 {
		ip = ip[:idx]
	}
	return ip
}

func (s *Server) logLoginAttempt(email, ip, userAgent string, success bool, reason string) {
	if os.Getenv("DATABASE_URL") != "" {
		s.db.Exec(`INSERT INTO login_logs (email, ip_address, user_agent, success, failure_reason, created_at) 
			VALUES ($1, $2, $3, $4, $5, now())`, email, ip, userAgent, success, reason)
	} else {
		s.db.Exec(`INSERT INTO login_logs (email, ip_address, user_agent, success, failure_reason, created_at) 
			VALUES (?, ?, ?, ?, ?, datetime('now'))`, email, ip, userAgent, success, reason)
	}
}

func (s *Server) apiLogin(w http.ResponseWriter, r *http.Request) {
	clientIP := s.getClientIP(r)
	userAgent := r.Header.Get("User-Agent")

	// 🚫 Check rate limit
	if blocked, remaining := s.rateLimiter.IsBlocked(clientIP); blocked {
		log.Printf("🚫 Blocked login attempt from IP %s (remaining: %v)", clientIP, remaining)
		s.logLoginAttempt("", clientIP, userAgent, false, "rate_limited")
		w.Header().Set("Retry-After", fmt.Sprintf("%d", int(remaining.Seconds())))
		http.Error(w, fmt.Sprintf("Too many login attempts. Try again in %d minutes.", int(remaining.Minutes())+1), http.StatusTooManyRequests)
		return
	}

	var p struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}

	var userID int
	var storedHash string
	var isVerified bool
	var err error
	if os.Getenv("DATABASE_URL") != "" {
		err = s.db.QueryRow("SELECT id, password_hash, is_verified FROM users WHERE email = $1", p.Email).Scan(&userID, &storedHash, &isVerified)
	} else {
		err = s.db.QueryRow("SELECT id, password_hash, is_verified FROM users WHERE email = ?", p.Email).Scan(&userID, &storedHash, &isVerified)
	}
	if err != nil {
		s.rateLimiter.RecordAttempt(clientIP, false)
		s.logLoginAttempt(p.Email, clientIP, userAgent, false, "user_not_found")
		log.Printf("❌ Failed login attempt for %s from %s (user not found)", p.Email, clientIP)
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	if !isVerified {
		s.rateLimiter.RecordAttempt(clientIP, false)
		s.logLoginAttempt(p.Email, clientIP, userAgent, false, "email_not_verified")
		log.Printf("❌ Failed login attempt for %s from %s (not verified)", p.Email, clientIP)
		http.Error(w, "email not verified", http.StatusForbidden)
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(p.Password)); err != nil {
		s.rateLimiter.RecordAttempt(clientIP, false)
		s.logLoginAttempt(p.Email, clientIP, userAgent, false, "wrong_password")
		attempts := s.rateLimiter.GetAttemptCount(clientIP)
		remaining := maxLoginAttempts - attempts
		log.Printf("❌ Failed login attempt for %s from %s (wrong password, %d attempts remaining)", p.Email, clientIP, remaining)
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	// ✅ Login successful - record and reset rate limiter
	s.rateLimiter.RecordAttempt(clientIP, true)
	s.logLoginAttempt(p.Email, clientIP, userAgent, true, "")
	log.Printf("✅ Successful login for %s from %s", p.Email, clientIP)

	// Create session token with expiry
	sessionToken, err := randToken(32)
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}

	expiresAt := time.Now().Add(sessionDuration)

	if os.Getenv("DATABASE_URL") != "" {
		_, err = s.db.Exec("INSERT INTO sessions (user_id, token, created_at, expires_at) VALUES ($1, $2, now(), $3)", userID, sessionToken, expiresAt)
	} else {
		_, err = s.db.Exec("INSERT INTO sessions (user_id, token, created_at, expires_at) VALUES (?, ?, datetime('now'), ?)", userID, sessionToken, expiresAt)
	}
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":     "ok",
		"token":      sessionToken,
		"email":      p.Email,
		"expires_at": expiresAt.Format(time.RFC3339),
	})
}

// getUserFromToken returns user_id from session token, or 0 if invalid or expired
func (s *Server) getUserFromToken(token string) int {
	if token == "" {
		return 0
	}
	var userID int
	var expiresAt time.Time
	var err error
	if os.Getenv("DATABASE_URL") != "" {
		err = s.db.QueryRow("SELECT user_id, expires_at FROM sessions WHERE token = $1", token).Scan(&userID, &expiresAt)
	} else {
		err = s.db.QueryRow("SELECT user_id, expires_at FROM sessions WHERE token = ?", token).Scan(&userID, &expiresAt)
	}
	if err != nil {
		return 0
	}
	// Check if session expired
	if time.Now().After(expiresAt) {
		// Delete expired session
		if os.Getenv("DATABASE_URL") != "" {
			s.db.Exec("DELETE FROM sessions WHERE token = $1", token)
		} else {
			s.db.Exec("DELETE FROM sessions WHERE token = ?", token)
		}
		log.Printf("⏰ Session expired for user %d", userID)
		return 0
	}
	return userID
}

func (s *Server) apiMe(w http.ResponseWriter, r *http.Request) {
	token := r.Header.Get("Authorization")
	if len(token) > 7 && token[:7] == "Bearer " {
		token = token[7:]
	}
	userID := s.getUserFromToken(token)
	if userID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var email string
	var err error
	if os.Getenv("DATABASE_URL") != "" {
		err = s.db.QueryRow("SELECT email FROM users WHERE id = $1", userID).Scan(&email)
	} else {
		err = s.db.QueryRow("SELECT email FROM users WHERE id = ?", userID).Scan(&email)
	}
	if err != nil {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"user_id": userID,
		"email":   email,
	})
}

func (s *Server) apiLogout(w http.ResponseWriter, r *http.Request) {
	token := r.Header.Get("Authorization")
	if len(token) > 7 && token[:7] == "Bearer " {
		token = token[7:]
	}
	if os.Getenv("DATABASE_URL") != "" {
		s.db.Exec("DELETE FROM sessions WHERE token = $1", token)
	} else {
		s.db.Exec("DELETE FROM sessions WHERE token = ?", token)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
