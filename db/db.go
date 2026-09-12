package db

import (
	"database/sql"
	"errors"
	"log"
	"math/rand"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

var (
	DB                   *sql.DB
	ErrDailyLimitReached = errors.New("daily_limit_reached")
)

// ============================================================
// DATABASE INIT
// ============================================================

func Init() {
	var err error

	// Keep the existing database so we don't lose your local bridge data.
	DB, err = sql.Open("sqlite3", "./globalchat.db")
	if err != nil {
		log.Fatal("db open:", err)
	}

	if err = DB.Ping(); err != nil {
		log.Fatal("db ping:", err)
	}

	migrate()

	log.Println("SQLite database connected and ready successfully")
}

// ============================================================
// MIGRATIONS
// ============================================================

func migrate() {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			supabase_auth_id TEXT UNIQUE,
			name TEXT NOT NULL,
			email TEXT NOT NULL UNIQUE,
			phone TEXT,
			password_hash TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,

		`CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL,
			expires_at DATETIME NOT NULL,
			FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
		)`,

		`CREATE TABLE IF NOT EXISTS memberships (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			plan TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'inactive',
			payment_ref TEXT,
			started_at DATETIME,
			expires_at DATETIME,
			FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
		)`,

		`CREATE TABLE IF NOT EXISTS wallets (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL UNIQUE,
			balance_kes INTEGER NOT NULL DEFAULT 0,
			FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
		)`,

		`CREATE TABLE IF NOT EXISTS transactions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			type TEXT NOT NULL,
			amount_kes INTEGER NOT NULL,
			description TEXT,
			status TEXT NOT NULL DEFAULT 'completed',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
		)`,

		`CREATE TABLE IF NOT EXISTS user_daily_work (
			user_id INTEGER PRIMARY KEY,
			files_completed INTEGER NOT NULL DEFAULT 0,
			last_worked_date TEXT NOT NULL,
			FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
		)`,
	}

	for _, statement := range stmts {
		if _, err := DB.Exec(statement); err != nil {
			log.Fatal("migrate:", err)
		}
	}

	// --------------------------------------------------------
	// Compatibility checks for older databases
	// --------------------------------------------------------

	addColumnIfMissing("users", "supabase_auth_id", "TEXT")
	addColumnIfMissing("memberships", "payment_ref", "TEXT")

	// Fast Supabase Auth ID lookup.
	_, _ = DB.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_users_supabase_auth_id
		ON users(supabase_auth_id)
		WHERE supabase_auth_id IS NOT NULL
	`)

	// Fast email lookup.
	_, _ = DB.Exec(`
		CREATE INDEX IF NOT EXISTS idx_users_email
		ON users(email)
	`)

	// Fast session lookup.
	_, _ = DB.Exec(`
		CREATE INDEX IF NOT EXISTS idx_sessions_user_id
		ON sessions(user_id)
	`)

	// Fast transaction lookup.
	_, _ = DB.Exec(`
		CREATE INDEX IF NOT EXISTS idx_transactions_user_id
		ON transactions(user_id)
	`)

	// Fast membership lookup.
	_, _ = DB.Exec(`
		CREATE INDEX IF NOT EXISTS idx_memberships_user_id
		ON memberships(user_id)
	`)
}

// addColumnIfMissing safely adds a column to an existing SQLite table.
func addColumnIfMissing(table, column, columnType string) {
	var count int

	err := DB.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`,
		table,
		column,
	).Scan(&count)

	if err != nil {
		log.Println("column check:", err)
		return
	}

	if count == 0 {
		query := "ALTER TABLE " + table + " ADD COLUMN " + column + " " + columnType

		if _, err := DB.Exec(query); err != nil {
			log.Println("add column:", err)
		}
	}
}

// ============================================================
// USER HELPERS
// ============================================================

type User struct {
	ID             int
	SupabaseAuthID string
	Name           string
	Email          string
	Phone          string
	PasswordHash   string
	CreatedAt      time.Time
}

func CreateUser(name, email, phone, hash string) (int64, error) {
	var id int64

	err := DB.QueryRow(
		`INSERT INTO users
			(name, email, phone, password_hash)
		 VALUES (?, ?, ?, ?)
		 RETURNING id`,
		name,
		email,
		phone,
		hash,
	).Scan(&id)

	if err != nil {
		return 0, err
	}

	_, err = DB.Exec(
		`INSERT INTO wallets (user_id, balance_kes)
		 VALUES (?, 0)
		 ON CONFLICT(user_id) DO NOTHING`,
		id,
	)

	if err != nil {
		return 0, err
	}

	return id, nil
}

func SetSupabaseAuthID(userID int, authID string) error {
	_, err := DB.Exec(
		`UPDATE users
		 SET supabase_auth_id = ?
		 WHERE id = ?`,
		authID,
		userID,
	)

	return err
}

func GetUserBySupabaseAuthID(authID string) (*User, error) {
	u := &User{}

	err := DB.QueryRow(
		`SELECT
			id,
			COALESCE(supabase_auth_id, ''),
			name,
			email,
			COALESCE(phone, ''),
			COALESCE(password_hash, ''),
			created_at
		 FROM users
		 WHERE supabase_auth_id = ?
		 LIMIT 1`,
		authID,
	).Scan(
		&u.ID,
		&u.SupabaseAuthID,
		&u.Name,
		&u.Email,
		&u.Phone,
		&u.PasswordHash,
		&u.CreatedAt,
	)

	if err != nil {
		return nil, err
	}

	return u, nil
}

func GetUserByEmail(email string) (*User, error) {
	u := &User{}

	err := DB.QueryRow(
		`SELECT
			id,
			COALESCE(supabase_auth_id, ''),
			name,
			email,
			COALESCE(phone, ''),
			COALESCE(password_hash, ''),
			created_at
		 FROM users
		 WHERE LOWER(email) = LOWER(?)
		 LIMIT 1`,
		email,
	).Scan(
		&u.ID,
		&u.SupabaseAuthID,
		&u.Name,
		&u.Email,
		&u.Phone,
		&u.PasswordHash,
		&u.CreatedAt,
	)

	if err != nil {
		return nil, err
	}

	return u, nil
}

func GetUserByID(id int) (*User, error) {
	u := &User{}

	err := DB.QueryRow(
		`SELECT
			id,
			COALESCE(supabase_auth_id, ''),
			name,
			email,
			COALESCE(phone, ''),
			COALESCE(password_hash, ''),
			created_at
		 FROM users
		 WHERE id = ?
		 LIMIT 1`,
		id,
	).Scan(
		&u.ID,
		&u.SupabaseAuthID,
		&u.Name,
		&u.Email,
		&u.Phone,
		&u.PasswordHash,
		&u.CreatedAt,
	)

	if err != nil {
		return nil, err
	}

	return u, nil
}

// ============================================================
// SESSION HELPERS
// ============================================================

func CreateSession(sessionID string, userID int, expires time.Time) error {
	_, err := DB.Exec(
		`INSERT INTO sessions
			(id, user_id, expires_at)
		 VALUES (?, ?, ?)`,
		sessionID,
		userID,
		expires,
	)

	return err
}

func GetSessionUser(sessionID string) (*User, error) {
	var userID int
	var expires time.Time

	err := DB.QueryRow(
		`SELECT user_id, expires_at
		 FROM sessions
		 WHERE id = ?
		 LIMIT 1`,
		sessionID,
	).Scan(&userID, &expires)

	if err != nil {
		return nil, err
	}

	if time.Now().After(expires) {
		_, _ = DB.Exec(
			`DELETE FROM sessions WHERE id = ?`,
			sessionID,
		)

		return nil, sql.ErrNoRows
	}

	return GetUserByID(userID)
}

func DeleteSession(sessionID string) {
	_, _ = DB.Exec(
		`DELETE FROM sessions WHERE id = ?`,
		sessionID,
	)
}

// ============================================================
// MEMBERSHIP HELPERS
// ============================================================

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
		`SELECT
			id,
			user_id,
			plan,
			status,
			COALESCE(payment_ref, ''),
			started_at,
			expires_at
		 FROM memberships
		 WHERE user_id = ?
		   AND status = 'active'
		   AND expires_at > CURRENT_TIMESTAMP
		 ORDER BY expires_at DESC
		 LIMIT 1`,
		userID,
	).Scan(
		&m.ID,
		&m.UserID,
		&m.Plan,
		&m.Status,
		&m.PaymentRef,
		&m.StartedAt,
		&m.ExpiresAt,
	)

	if err != nil {
		return nil, err
	}

	return m, nil
}

func CreatePendingMembership(userID int, plan, ref string) error {
	_, err := DB.Exec(
		`INSERT INTO memberships
			(user_id, plan, status, payment_ref)
		 VALUES (?, ?, 'pending', ?)`,
		userID,
		plan,
		ref,
	)

	return err
}

func ActivateMembership(ref string) error {
	now := time.Now()
	expires := now.AddDate(0, 1, 0)

	_, err := DB.Exec(
		`UPDATE memberships
		 SET status = 'active',
		     started_at = ?,
		     expires_at = ?
		 WHERE payment_ref = ?`,
		now,
		expires,
		ref,
	)

	return err
}

// ============================================================
// WALLET HELPERS
// ============================================================

func GetBalance(userID int) (int, error) {
	var balance int

	err := DB.QueryRow(
		`SELECT balance_kes
		 FROM wallets
		 WHERE user_id = ?
		 LIMIT 1`,
		userID,
	).Scan(&balance)

	return balance, err
}

func CreditWallet(userID, amount int, desc string) error {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}

	defer tx.Rollback()

	_, err = tx.Exec(
		`UPDATE wallets
		 SET balance_kes = balance_kes + ?
		 WHERE user_id = ?`,
		amount,
		userID,
	)

	if err != nil {
		return err
	}

	_, err = tx.Exec(
		`INSERT INTO transactions
			(user_id, type, amount_kes, description, status)
		 VALUES (?, 'credit', ?, ?, 'completed')`,
		userID,
		amount,
		desc,
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

	var balance int

	err = tx.QueryRow(
		`SELECT balance_kes
		 FROM wallets
		 WHERE user_id = ?
		 LIMIT 1`,
		userID,
	).Scan(&balance)

	if err != nil {
		return err
	}

	if balance < amount {
		return sql.ErrNoRows
	}

	_, err = tx.Exec(
		`UPDATE wallets
		 SET balance_kes = balance_kes - ?
		 WHERE user_id = ?`,
		amount,
		userID,
	)

	if err != nil {
		return err
	}

	_, err = tx.Exec(
		`INSERT INTO transactions
			(user_id, type, amount_kes, description, status)
		 VALUES (?, 'debit', ?, ?, 'pending')`,
		userID,
		amount,
		desc,
	)

	if err != nil {
		return err
	}

	return tx.Commit()
}

// ============================================================
// TRANSACTIONS & ADMIN HELPERS
// ============================================================

type Transaction struct {
	ID          int
	Type        string
	AmountKES   int
	Description string
	Status      string
	CreatedAt   time.Time
}

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

func GetAllTransactions() ([]AdminTransaction, error) {
	rows, err := DB.Query(`
		SELECT
			t.id,
			t.user_id,
			COALESCE(u.name, ''),
			COALESCE(u.email, ''),
			COALESCE(u.phone, ''),
			t.type,
			t.amount_kes,
			COALESCE(t.description, ''),
			t.status,
			t.created_at
		FROM transactions t
		LEFT JOIN users u ON t.user_id = u.id
		ORDER BY t.created_at DESC
		LIMIT 200
	`)

	if err != nil {
		return nil, err
	}

	defer rows.Close()

	list := make([]AdminTransaction, 0)

	for rows.Next() {
		var t AdminTransaction

		if err := rows.Scan(
			&t.ID,
			&t.UserID,
			&t.UserName,
			&t.UserEmail,
			&t.UserPhone,
			&t.Type,
			&t.AmountKES,
			&t.Description,
			&t.Status,
			&t.CreatedAt,
		); err != nil {
			return nil, err
		}

		list = append(list, t)
	}

	return list, rows.Err()
}

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

	return list, rows.Err()
}

// ============================================================
// DAILY WORK
// ============================================================

func GetUserWorkStats(userID int) (int, error) {
	today := time.Now().Format("2006-01-02")

	var completed int
	var lastWorked string

	err := DB.QueryRow(
		`SELECT files_completed, last_worked_date
		 FROM user_daily_work
		 WHERE user_id = ?`,
		userID,
	).Scan(
		&completed,
		&lastWorked,
	)

	if err == sql.ErrNoRows {
		_, err = DB.Exec(
			`INSERT INTO user_daily_work
				(user_id, files_completed, last_worked_date)
			 VALUES (?, 0, ?)`,
			userID,
			today,
		)

		return 0, err
	}

	if err != nil {
		return 0, err
	}

	if lastWorked != today {
		completed = 0

		_, _ = DB.Exec(
			`UPDATE user_daily_work
			 SET files_completed = 0,
			     last_worked_date = ?
			 WHERE user_id = ?`,
			today,
			userID,
		)
	}

	return completed, nil
}

func ProcessFileCompletion(userID int) (int, int, int, error) {
	completed, err := GetUserWorkStats(userID)

	if err != nil {
		return 0, 0, 0, err
	}

	membership, _ := GetActiveMembership(userID)

	if completed >= 10 && membership == nil {
		balance, _ := GetBalance(userID)

		return 0, completed, balance, ErrDailyLimitReached
	}

	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	rewardKES := r.Intn(301) + 700

	if err := CreditWallet(
		userID,
		rewardKES,
		"File Processing Earnings",
	); err != nil {
		return 0, 0, 0, err
	}

	today := time.Now().Format("2006-01-02")
	newCompleted := completed + 1

	_, err = DB.Exec(
		`UPDATE user_daily_work
		 SET files_completed = ?,
		     last_worked_date = ?
		 WHERE user_id = ?`,
		newCompleted,
		today,
		userID,
	)

	if err != nil {
		return 0, 0, 0, err
	}

	newBalance, _ := GetBalance(userID)

	return rewardKES, newCompleted, newBalance, nil
}
