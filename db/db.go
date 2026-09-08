package db

import (
	"database/sql"
	"errors"
	"log"
	"math/rand"
	"os"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

var DB *sql.DB

func Init() {
	dbPath := os.Getenv("DATABASE_PATH")
	if dbPath == "" {
		dbPath = "./globalchat.db"
	}

	var err error
	DB, err = sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		log.Fatal("db open:", err)
	}
	if err = DB.Ping(); err != nil {
		log.Fatal("db ping:", err)
	}
	migrate()
	log.Println("Database ready at:", dbPath)
}

func migrate() {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			email TEXT NOT NULL UNIQUE,
			phone TEXT,
			password_hash TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			expires_at DATETIME NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS memberships (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			plan TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'inactive',
			payment_ref TEXT,
			started_at DATETIME,
			expires_at DATETIME
		)`,
		`CREATE TABLE IF NOT EXISTS wallets (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
			balance_kes INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS transactions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			type TEXT NOT NULL,
			amount_kes INTEGER NOT NULL,
			description TEXT,
			status TEXT NOT NULL DEFAULT 'completed',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS user_daily_work (
			user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
			files_completed INTEGER NOT NULL DEFAULT 0,
			last_worked_date TEXT NOT NULL
		)`,
	}
	for _, s := range stmts {
		if _, err := DB.Exec(s); err != nil {
			log.Fatal("migrate:", err)
		}
	}

	// Dynamic alter column patch to support existing database schemas
	_, _ = DB.Exec(`ALTER TABLE memberships ADD COLUMN payment_ref TEXT;`)
}

// User helpers

type User struct {
	ID           int
	Name         string
	Email        string
	Phone        string
	PasswordHash string
	CreatedAt    time.Time
}

func CreateUser(name, email, phone, hash string) (int64, error) {
	res, err := DB.Exec(
		`INSERT INTO users (name, email, phone, password_hash) VALUES (?,?,?,?)`,
		name, email, phone, hash,
	)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	// create wallet
	DB.Exec(`INSERT INTO wallets (user_id, balance_kes) VALUES (?,0)`, id)
	return id, nil
}

func GetUserByEmail(email string) (*User, error) {
	u := &User{}
	err := DB.QueryRow(
		`SELECT id, name, email, phone, password_hash, created_at FROM users WHERE email=?`, email,
	).Scan(&u.ID, &u.Name, &u.Email, &u.Phone, &u.PasswordHash, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func GetUserByID(id int) (*User, error) {
	u := &User{}
	err := DB.QueryRow(
		`SELECT id, name, email, phone, password_hash, created_at FROM users WHERE id=?`, id,
	).Scan(&u.ID, &u.Name, &u.Email, &u.Phone, &u.PasswordHash, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return u, nil
}

// Session helpers

func CreateSession(sessionID string, userID int, expires time.Time) error {
	_, err := DB.Exec(
		`INSERT INTO sessions (id, user_id, expires_at) VALUES (?,?,?)`,
		sessionID, userID, expires,
	)
	return err
}

func GetSessionUser(sessionID string) (*User, error) {
	var userID int
	var expires time.Time
	err := DB.QueryRow(
		`SELECT user_id, expires_at FROM sessions WHERE id=?`, sessionID,
	).Scan(&userID, &expires)
	if err != nil {
		return nil, err
	}
	if time.Now().After(expires) {
		DB.Exec(`DELETE FROM sessions WHERE id=?`, sessionID)
		return nil, sql.ErrNoRows
	}
	return GetUserByID(userID)
}

func DeleteSession(sessionID string) {
	DB.Exec(`DELETE FROM sessions WHERE id=?`, sessionID)
}

// Membership helpers

type Membership struct {
	ID         int
	UserID     int
	Plan       string
	Status     string
	PaymentRef string
	StartedAt  *time.Time
	ExpiresAt  *time.Time
}

func GetActiveMembership(userID int) (*Membership, error) {
	m := &Membership{}
	err := DB.QueryRow(
		`SELECT id, user_id, plan, status, COALESCE(payment_ref, ''), started_at, expires_at
		 FROM memberships WHERE user_id=? AND status='active' AND expires_at > datetime('now')
		 ORDER BY expires_at DESC LIMIT 1`, userID,
	).Scan(&m.ID, &m.UserID, &m.Plan, &m.Status, &m.PaymentRef, &m.StartedAt, &m.ExpiresAt)
	if err != nil {
		return nil, err
	}
	return m, nil
}

func CreatePendingMembership(userID int, plan, ref string) error {
	_, err := DB.Exec(
		`INSERT INTO memberships (user_id, plan, status, payment_ref) VALUES (?,?,'pending',?)`,
		userID, plan, ref,
	)
	return err
}

func ActivateMembership(ref string) error {
	now := time.Now()
	expires := now.AddDate(0, 1, 0)
	_, err := DB.Exec(
		`UPDATE memberships SET status='active', started_at=?, expires_at=? WHERE payment_ref=?`,
		now, expires, ref,
	)
	return err
}

// Wallet helpers

func GetBalance(userID int) (int, error) {
	var bal int
	err := DB.QueryRow(`SELECT balance_kes FROM wallets WHERE user_id=?`, userID).Scan(&bal)
	return bal, err
}

func CreditWallet(userID, amount int, desc string) error {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`UPDATE wallets SET balance_kes = balance_kes + ? WHERE user_id=?`, amount, userID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(
		`INSERT INTO transactions (user_id, type, amount_kes, description, status) VALUES (?,'credit',?,?,'completed')`,
		userID, amount, desc,
	)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func DebitWallet(userID, amount int, desc string) error {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var bal int
	if err = tx.QueryRow(`SELECT balance_kes FROM wallets WHERE user_id=?`, userID).Scan(&bal); err != nil {
		return err
	}
	if bal < amount {
		return sql.ErrNoRows // insufficient funds
	}
	_, err = tx.Exec(`UPDATE wallets SET balance_kes = balance_kes - ? WHERE user_id=?`, amount, userID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(
		`INSERT INTO transactions (user_id, type, amount_kes, description, status) VALUES (?,'debit',?,?,'pending')`,
		userID, amount, desc,
	)
	if err != nil {
		return err
	}
	return tx.Commit()
}

type Transaction struct {
	ID          int
	Type        string
	AmountKES   int
	Description string
	Status      string
	CreatedAt   time.Time
}

func GetTransactions(userID int) ([]Transaction, error) {
	rows, err := DB.Query(
		`SELECT id, type, amount_kes, description, status, created_at
		 FROM transactions WHERE user_id=? ORDER BY created_at DESC LIMIT 50`, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var txs []Transaction
	for rows.Next() {
		var t Transaction
		rows.Scan(&t.ID, &t.Type, &t.AmountKES, &t.Description, &t.Status, &t.CreatedAt)
		txs = append(txs, t)
	}
	return txs, nil
}

// Work & Daily Limit Helpers

var ErrDailyLimitReached = errors.New("daily_limit_reached")

// GetUserWorkStats retrieves or resets daily work counts
func GetUserWorkStats(userID int) (int, error) {
	today := time.Now().Format("2006-01-02")
	var completed int
	var lastWorked string

	err := DB.QueryRow(`SELECT files_completed, last_worked_date FROM user_daily_work WHERE user_id=?`, userID).
		Scan(&completed, &lastWorked)

	if err == sql.ErrNoRows {
		// First time working
		DB.Exec(`INSERT INTO user_daily_work (user_id, files_completed, last_worked_date) VALUES (?, 0, ?)`, userID, today)
		return 0, nil
	} else if err != nil {
		return 0, err
	}

	// Reset count if it's a new calendar day
	if lastWorked != today {
		completed = 0
		DB.Exec(`UPDATE user_daily_work SET files_completed = 0, last_worked_date = ? WHERE user_id=?`, today, userID)
	}

	return completed, nil
}

// ProcessFileCompletion handles file reward calculation (KSh 700 - KSh 1000) and limit check
func ProcessFileCompletion(userID int) (int, int, int, error) {
	completed, err := GetUserWorkStats(userID)
	if err != nil {
		return 0, 0, 0, err
	}

	// Check if user is subscribed
	m, _ := GetActiveMembership(userID)
	isSubscribed := m != nil

	// Limit to 10 files if not subscribed
	if completed >= 10 && !isSubscribed {
		bal, _ := GetBalance(userID)
		return 0, completed, bal, ErrDailyLimitReached
	}

	// Thread-safe random generator between KSh 700 and KSh 1000
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	rewardKES := r.Intn(301) + 700

	// Credit user's wallet balance
	desc := "File Processing Earnings"
	if err := CreditWallet(userID, rewardKES, desc); err != nil {
		return 0, 0, 0, err
	}

	// Update completed file count
	today := time.Now().Format("2006-01-02")
	newCompleted := completed + 1
	_, err = DB.Exec(
		`UPDATE user_daily_work SET files_completed=?, last_worked_date=? WHERE user_id=?`,
		newCompleted, today, userID,
	)
	if err != nil {
		return 0, 0, 0, err
	}

	newBalance, _ := GetBalance(userID)
	return rewardKES, newCompleted, newBalance, nil
}

// Admin Payment & Transaction Types

type AdminTransaction struct {
	ID          int       `json:"id"`
	UserID      int       `json:"user_id"`
	UserName    string    `json:"user_name"`
	UserEmail   string    `json:"user_email"`
	UserPhone   string    `json:"user_phone"`
	Type        string    `json:"type"`
	AmountKES   int       `json:"amount_kes"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
}

// GetAllTransactions returns all payment/payout records for admin view
func GetAllTransactions() ([]AdminTransaction, error) {
	rows, err := DB.Query(`
		SELECT t.id, t.user_id, COALESCE(u.name, ''), COALESCE(u.email, ''), COALESCE(u.phone, ''), 
		       t.type, t.amount_kes, COALESCE(t.description, ''), t.status, t.created_at
		FROM transactions t
		LEFT JOIN users u ON t.user_id = u.id
		ORDER BY t.created_at DESC LIMIT 200
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var txs []AdminTransaction
	for rows.Next() {
		var t AdminTransaction
		if err := rows.Scan(&t.ID, &t.UserID, &t.UserName, &t.UserEmail, &t.UserPhone, &t.Type, &t.AmountKES, &t.Description, &t.Status, &t.CreatedAt); err != nil {
			return nil, err
		}
		txs = append(txs, t)
	}
	return txs, nil
}

// AdminMembership represents a membership record for the admin dashboard.
type AdminMembership struct {
	ID         int        `json:"id"`
	UserID     int        `json:"user_id"`
	UserName   string     `json:"user_name"`
	UserEmail  string     `json:"user_email"`
	UserPhone  string     `json:"user_phone"`
	Plan       string     `json:"plan"`
	Status     string     `json:"status"`
	PaymentRef string     `json:"payment_ref"`
	StartedAt  *time.Time `json:"started_at"`
	ExpiresAt  *time.Time `json:"expires_at"`
}

// GetAllMemberships returns membership/payment records for the admin dashboard.
func GetAllMemberships() ([]AdminMembership, error) {
	rows, err := DB.Query(`
		SELECT
			m.id,
			m.user_id,
			COALESCE(u.name, ''),
			COALESCE(u.email, ''),
			COALESCE(u.phone, ''),
			m.plan,
			m.status,
			COALESCE(m.payment_ref, ''),
			m.started_at,
			m.expires_at
		FROM memberships m
		LEFT JOIN users u ON m.user_id = u.id
		ORDER BY
			CASE
				WHEN m.status = 'active' THEN 1
				WHEN m.status = 'pending' THEN 2
				ELSE 3
			END,
			m.id DESC
		LIMIT 200
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]AdminMembership, 0)

	for rows.Next() {
		var m AdminMembership

		if err := rows.Scan(
			&m.ID,
			&m.UserID,
			&m.UserName,
			&m.UserEmail,
			&m.UserPhone,
			&m.Plan,
			&m.Status,
			&m.PaymentRef,
			&m.StartedAt,
			&m.ExpiresAt,
		); err != nil {
			return nil, err
		}

		list = append(list, m)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return list, nil
}
