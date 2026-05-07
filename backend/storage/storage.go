package storage

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"

	_ "modernc.org/sqlite"
)

type Storage struct {
	db *sql.DB
}

type TokenData struct {
	Id           int
	Token        string
	TelegramID   string
	Label        string
	CountRequest int
	IsActive     bool
	CreatedAt    string
}

func NewStorage(dbPath string) (*Storage, error) {
	db, err := sql.Open("sqlite", dbPath)

	if err != nil {
		return nil, fmt.Errorf("Ошибка при инициализации базы данных: %v", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("Ошибка подключения к базе данных: %v", err)
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS tokens (
						id INTEGER PRIMARY KEY AUTOINCREMENT,
						token TEXT NOT NULL UNIQUE,
						telegram_id TEXT DEFAULT '',
						label TEXT DEFAULT '',
						count_request INTEGER DEFAULT 0,
						is_active BOOLEAN DEFAULT TRUE,
						created_at DATETIME DEFAULT CURRENT_TIMESTAMP
					);`)
	if err != nil {
		return nil, fmt.Errorf("Ошибка при создании базы данных: %v", err)
	}

	storage := Storage{db: db}
	return &storage, nil
}

func (s *Storage) Close() error {
	return s.db.Close()
}

func (s *Storage) AddToken(token, telegramID, label string) error {
	stmt := `INSERT INTO tokens (token, telegram_id, label) VALUES (?, ?, ?)`

	_, err := s.db.Exec(stmt, token, telegramID, label)
	if err != nil {
		return fmt.Errorf("Ошибка добавления токена в базу данных: %v", err)
	}

	return nil
}

func (s *Storage) RevokeToken(token string) error {
	stmt := `UPDATE tokens SET is_active = 0 WHERE token = ?`

	_, err := s.db.Exec(stmt, token)
	if err != nil {
		return fmt.Errorf("Ошибка деактивации токена: %v", err)
	}

	return nil
}

func (s *Storage) ActivateToken(token string) error {
	stmt := `UPDATE tokens SET is_active = 1 WHERE token = ?`

	_, err := s.db.Exec(stmt, token)
	if err != nil {
		return fmt.Errorf("Ошибка активации токена: %v", err)
	}

	return nil
}

func (s *Storage) DeleteToken(token string) error {
	stmt := `DELETE FROM tokens WHERE token = ?`

	_, err := s.db.Exec(stmt, token)
	if err != nil {
		return fmt.Errorf("Ошибка удаления токена: %v", err)
	}

	return nil
}

func (s *Storage) ValidateToken(token string) (bool, error) {
	var isActive bool
	stmt := "SELECT is_active FROM tokens WHERE token = ?"

	err := s.db.QueryRow(stmt, token).Scan(&isActive)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, fmt.Errorf("Токен не найден: %v", err)
		}

		return false, fmt.Errorf("Ошибка при поиске токена: %v", err)
	}

	return isActive, nil
}

func (s *Storage) IncrementTokenUsage(token string) error {
	stmt := "UPDATE tokens SET count_request = count_request + 1 WHERE token = ?"

	_, err := s.db.Exec(stmt, token)
	if err != nil {
		return fmt.Errorf("Ошибка при увеличении счетчика запросов пользователя: %v", err)
	}

	return nil
}

func GenerateToken() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("Ошибка генерации токена: %v", err)
	}

	return hex.EncodeToString(bytes), nil
}

func (s *Storage) GetAllTokens() ([]TokenData, error) {
	rows, err := s.db.Query("SELECT id, token, telegram_id, label, count_request, is_active, created_at FROM tokens ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tokens []TokenData
	for rows.Next() {
		var t TokenData
		if err := rows.Scan(&t.Id, &t.Token, &t.TelegramID, &t.Label, &t.CountRequest, &t.IsActive, &t.CreatedAt); err != nil {
			return nil, err
		}

		tokens = append(tokens, t)
	}
	return tokens, nil
}

func (s *Storage) GetTokenById(id int) (*TokenData, error) {
	var t TokenData
	stmt := "SELECT id, token, telegram_id, label, count_request, is_active, created_at FROM tokens WHERE id = ?"
	err := s.db.QueryRow(stmt, id).Scan(&t.Id, &t.Token, &t.TelegramID, &t.Label, &t.CountRequest, &t.IsActive, &t.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}