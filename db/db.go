package db

import (
	"database/sql"
	"log"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

var DB *sql.DB

func Init() {
	var err error
	DB, err = sql.Open("sqlite3", "./globalchat.db?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		log.Fatal("db open:", err)
	}
	if err = DB.Ping(); err != nil {
		log.Fatal("db ping:", err)
	}
	migrate()
	log.Println("Database ready")
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
			paystack_ref TEXT,
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
	}
	for _, s := range stmts {
		if _, err := DB.Exec(s); err != nil {
			log.Fatal("migrate:", err)
		}
	}
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
	ID          int
	UserID      int
	Plan        string
	Status      string
	PaystackRef string
	StartedAt   *time.Time
	ExpiresAt   *time.Time
}

func GetActiveMembership(userID int) (*Membership, error) {
	m := &Membership{}
	err := DB.QueryRow(
		`SELECT id, user_id, plan, status, COALESCE(paystack_ref,''), started_at, expires_at
		 FROM memberships WHERE user_id=? AND status='active' AND expires_at > datetime('now')
		 ORDER BY expires_at DESC LIMIT 1`, userID,
	).Scan(&m.ID, &m.UserID, &m.Plan, &m.Status, &m.PaystackRef, &m.StartedAt, &m.ExpiresAt)
	if err != nil {
		return nil, err
	}
	return m, nil
}

func CreatePendingMembership(userID int, plan, ref string) error {
	_, err := DB.Exec(
		`INSERT INTO memberships (user_id, plan, status, paystack_ref) VALUES (?,?,'pending',?)`,
		userID, plan, ref,
	)
	return err
}

func ActivateMembership(ref string) error {
	now := time.Now()
	expires := now.AddDate(0, 1, 0)
	_, err := DB.Exec(
		`UPDATE memberships SET status='active', started_at=?, expires_at=? WHERE paystack_ref=?`,
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
		return sql.ErrNoRows // reuse as "insufficient funds"
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
