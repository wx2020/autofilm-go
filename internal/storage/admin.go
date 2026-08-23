package storage

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

type User struct {
	ID        int64     `json:"id"`
	Username  string    `json:"username"`
	Role      string    `json:"role"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
}

type Alert struct {
	ID           int64     `json:"id"`
	Level        string    `json:"level"`
	Source       string    `json:"source"`
	Message      string    `json:"message"`
	Acknowledged bool      `json:"acknowledged"`
	CreatedAt    time.Time `json:"created_at"`
}

type Backup struct {
	Version       int                                 `json:"version"`
	CreatedAt     time.Time                           `json:"created_at"`
	Settings      AppSettings                         `json:"settings"`
	ModuleConfigs map[string][]map[string]interface{} `json:"module_configs"`
}

func validRole(role string) bool {
	return role == "admin" || role == "operator" || role == "viewer"
}

func HashPassword(password string) (string, error) {
	if len(password) < 8 {
		return "", fmt.Errorf("密码至少需要 8 个字符")
	}
	salt := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return "", err
	}
	dk := pbkdf2SHA256([]byte(password), salt, 120000, 32)
	return fmt.Sprintf("pbkdf2-sha256$120000$%s$%s",
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(dk)), nil
}

func VerifyPassword(encoded, password string) bool {
	var iterations int
	parts := splitPasswordHash(encoded)
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	if _, err := fmt.Sscanf(parts[1], "%d", &iterations); err != nil || iterations < 1 {
		return false
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[2])
	want, err2 := base64.RawStdEncoding.DecodeString(parts[3])
	if err1 != nil || err2 != nil {
		return false
	}
	got := pbkdf2SHA256([]byte(password), salt, iterations, len(want))
	return subtle.ConstantTimeCompare(got, want) == 1
}

func splitPasswordHash(value string) []string {
	var parts []string
	start := 0
	for i, r := range value {
		if r == '$' {
			parts = append(parts, value[start:i])
			start = i + 1
		}
	}
	return append(parts, value[start:])
}

func pbkdf2SHA256(password, salt []byte, iterations, size int) []byte {
	result := make([]byte, 0, size)
	for block := uint32(1); len(result) < size; block++ {
		counter := []byte{byte(block >> 24), byte(block >> 16), byte(block >> 8), byte(block)}
		mac := hmac.New(sha256.New, password)
		mac.Write(salt)
		mac.Write(counter)
		u := mac.Sum(nil)
		t := append([]byte(nil), u...)
		for i := 1; i < iterations; i++ {
			mac = hmac.New(sha256.New, password)
			mac.Write(u)
			u = mac.Sum(nil)
			for j := range t {
				t[j] ^= u[j]
			}
		}
		result = append(result, t...)
	}
	return result[:size]
}

func (s *Store) UserCount() (int, error) {
	var count int
	return count, s.db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
}

func (s *Store) CreateUser(username, password, role string) (*User, error) {
	if username == "" || !validRole(role) {
		return nil, fmt.Errorf("用户名或角色无效")
	}
	hash, err := HashPassword(password)
	if err != nil {
		return nil, err
	}
	res, err := s.db.Exec("INSERT INTO users(username,password_hash,role) VALUES(?,?,?)", username, hash, role)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &User{ID: id, Username: username, Role: role, Enabled: true, CreatedAt: time.Now()}, nil
}

func (s *Store) ListUsers() ([]User, error) {
	rows, err := s.db.Query("SELECT id,username,role,enabled,created_at FROM users ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := []User{}
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.Role, &u.Enabled, &u.CreatedAt); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (s *Store) UpdateUser(id int64, role string, enabled bool, password string) error {
	if !validRole(role) {
		return fmt.Errorf("角色无效")
	}
	if password != "" {
		hash, err := HashPassword(password)
		if err != nil {
			return err
		}
		_, err = s.db.Exec("UPDATE users SET role=?,enabled=?,password_hash=?,updated_at=CURRENT_TIMESTAMP WHERE id=?", role, enabled, hash, id)
		return err
	}
	_, err := s.db.Exec("UPDATE users SET role=?,enabled=?,updated_at=CURRENT_TIMESTAMP WHERE id=?", role, enabled, id)
	return err
}

func (s *Store) DeleteUser(id int64) error {
	var admins int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM users WHERE role='admin' AND enabled=1").Scan(&admins); err != nil {
		return err
	}
	var role string
	if err := s.db.QueryRow("SELECT role FROM users WHERE id=?", id).Scan(&role); err != nil {
		return err
	}
	if role == "admin" && admins <= 1 {
		return fmt.Errorf("不能删除最后一个管理员")
	}
	_, err := s.db.Exec("DELETE FROM users WHERE id=?", id)
	return err
}

func (s *Store) Authenticate(username, password string) (*User, error) {
	var u User
	var hash string
	err := s.db.QueryRow("SELECT id,username,password_hash,role,enabled,created_at FROM users WHERE username=?", username).
		Scan(&u.ID, &u.Username, &hash, &u.Role, &u.Enabled, &u.CreatedAt)
	if err != nil || !u.Enabled || !VerifyPassword(hash, password) {
		return nil, fmt.Errorf("用户名或密码错误")
	}
	return &u, nil
}

func (s *Store) CreateSession(userID int64, ttl time.Duration) (string, error) {
	raw := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))
	_, err := s.db.Exec("INSERT INTO sessions(token_hash,user_id,expires_at) VALUES(?,?,?)",
		base64.RawStdEncoding.EncodeToString(hash[:]), userID, time.Now().Add(ttl))
	return token, err
}

func (s *Store) UserBySession(token string) (*User, error) {
	hash := sha256.Sum256([]byte(token))
	var u User
	err := s.db.QueryRow(`SELECT u.id,u.username,u.role,u.enabled,u.created_at
		FROM sessions s JOIN users u ON u.id=s.user_id
		WHERE s.token_hash=? AND s.expires_at>CURRENT_TIMESTAMP AND u.enabled=1`,
		base64.RawStdEncoding.EncodeToString(hash[:])).
		Scan(&u.ID, &u.Username, &u.Role, &u.Enabled, &u.CreatedAt)
	return &u, err
}

func (s *Store) DeleteSession(token string) error {
	hash := sha256.Sum256([]byte(token))
	_, err := s.db.Exec("DELETE FROM sessions WHERE token_hash=?", base64.RawStdEncoding.EncodeToString(hash[:]))
	return err
}

func (s *Store) CreateAlert(level, source, message string) error {
	_, err := s.db.Exec("INSERT INTO alerts(level,source,message) VALUES(?,?,?)", level, source, message)
	return err
}

func (s *Store) ListAlerts(limit int) ([]Alert, error) {
	if limit < 1 || limit > 1000 {
		limit = 100
	}
	rows, err := s.db.Query("SELECT id,level,source,message,acknowledged,created_at FROM alerts ORDER BY id DESC LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	alerts := []Alert{}
	for rows.Next() {
		var a Alert
		if err := rows.Scan(&a.ID, &a.Level, &a.Source, &a.Message, &a.Acknowledged, &a.CreatedAt); err != nil {
			return nil, err
		}
		alerts = append(alerts, a)
	}
	return alerts, rows.Err()
}

func (s *Store) AcknowledgeAlert(id int64) error {
	_, err := s.db.Exec("UPDATE alerts SET acknowledged=1 WHERE id=?", id)
	return err
}

func (s *Store) ExportBackup() (*Backup, error) {
	settings, err := s.GetAppSettings()
	if err != nil {
		return nil, err
	}
	b := &Backup{Version: 1, CreatedAt: time.Now(), Settings: settings, ModuleConfigs: map[string][]map[string]interface{}{}}
	for _, typ := range []string{"alist2strm", "ani2alist", "libraryposter", "alistsync"} {
		list, err := s.ListModuleConfigs(typ)
		if err != nil {
			return nil, err
		}
		b.ModuleConfigs[typ] = list
	}
	return b, nil
}

func (s *Store) ImportBackup(b *Backup) error {
	if b.Version != 1 {
		return fmt.Errorf("不支持的备份版本")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	settingsJSON, _ := json.Marshal(b.Settings)
	if _, err = tx.Exec(`INSERT INTO app_settings(key,value) VALUES('settings',?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=CURRENT_TIMESTAMP`, string(settingsJSON)); err != nil {
		return err
	}
	for typ, configs := range b.ModuleConfigs {
		if typ == "alissync" {
			typ = "alistsync"
		}
		if typ != "alist2strm" && typ != "ani2alist" && typ != "libraryposter" && typ != "alistsync" {
			continue
		}
		if _, err = tx.Exec("DELETE FROM module_configs WHERE module_type=?", typ); err != nil {
			return err
		}
		for _, cfg := range configs {
			id := fmt.Sprint(cfg["id"])
			data, _ := json.Marshal(cfg)
			payload, encErr := Encrypt(s.key, string(data))
			if encErr != nil {
				return encErr
			}
			if _, err = tx.Exec("INSERT INTO module_configs(module_type,cfg_id,payload) VALUES(?,?,?)", typ, id, payload); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
