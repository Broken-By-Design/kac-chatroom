package main

import (
	"database/sql"
	"fmt"
	"os"
	"time"

	"github.com/go-sql-driver/mysql"
)

var db *sql.DB
var testMode bool

func connectDB() *sql.DB {
	host := os.Getenv("DB_HOST")
	port := os.Getenv("DB_PORT")
	if port == "" {
		port = "3306"
	}
	user := os.Getenv("DB_USER")
	pass := os.Getenv("DB_PASSWORD")
	name := os.Getenv("DB_NAME")

	if host == "" {
		fmt.Println("⚠️ No database configuration found, running without database support.")
		return nil
	}

	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true&charset=utf8mb4", user, pass, host, port, name)

	const maxAttempts = 3
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		fmt.Printf("Attempting to connect to the database (attempt %d/%d)...\n", attempt, maxAttempts)
		conn, err := sql.Open("mysql", dsn)
		if err == nil {
			conn.SetMaxOpenConns(10)
			conn.SetMaxIdleConns(5)
			if err := conn.Ping(); err == nil {
				fmt.Println("✅ Successfully created database connection pool.")
				return conn
			}
			_ = conn.Close()
		}
		fmt.Printf("⚠️ Database connection failed: %v\n", err)
		if attempt < maxAttempts {
			fmt.Println("Retrying in 3 seconds...")
			time.Sleep(3 * time.Second)
		}
	}

	fmt.Println("⚠️ Database unreachable after 3 attempts. Starting without database support.")
	fmt.Println("Will periodically retry connecting in the background to enable ban persistence.")
	go reconnectDB(dsn)
	return nil
}

func reconnectDB(dsn string) {
	for {
		time.Sleep(1 * time.Minute)
		fmt.Println("Attempting to reconnect to the database...")
		conn, err := sql.Open("mysql", dsn)
		if err == nil {
			conn.SetMaxOpenConns(10)
			conn.SetMaxIdleConns(5)
			if err := conn.Ping(); err == nil {
				fmt.Println("✅ Database connection restored.")
				db = conn
				syncBanListFromDB()
				return
			}
			_ = conn.Close()
		}
		fmt.Printf("⚠️ Database reconnection failed: %v. Will retry again in 30 seconds.\n", err)
	}
}

func syncBanListFromDB() {
	if testMode || db == nil {
		return
	}
	fmt.Println("SYNCING BAN LIST: Reloading active bans from database into cache...")
	rows, err := db.Query("SELECT target_ip, target_username, expires_at FROM BanList WHERE is_active = TRUE")
	if err != nil {
		fmt.Printf("DATABASE ERROR during ban list sync: %v\n", err)
		return
	}
	defer rows.Close()

	temp := map[string]*time.Time{}
	now := time.Now().UTC()
	for rows.Next() {
		var ip, username string
		var expiresAt mysql.NullTime
		if err := rows.Scan(&ip, &username, &expiresAt); err != nil {
			continue
		}
		if expiresAt.Valid {
			t := expiresAt.Time.UTC()
			if t.Before(now) {
				continue
			}
			temp[fmt.Sprintf("%s@%s", username, ip)] = &t
		} else {
			temp[fmt.Sprintf("%s@%s", username, ip)] = nil
		}
	}
	state.setBannedIPs(temp)
	fmt.Printf("SYNC COMPLETE: Loaded %d active bans into cache.\n", len(temp))
}

func getBanExpiry(durationStr string) *time.Time {
	switch durationStr {
	case "1d":
		t := time.Now().UTC().Add(24 * time.Hour)
		return &t
	case "7d":
		t := time.Now().UTC().Add(7 * 24 * time.Hour)
		return &t
	default:
		return nil // permanent
	}
}

// insertBans upserts ban rows into the BanList table.
func insertBans(banData [][]any) error {
	if db == nil {
		return fmt.Errorf("database not configured")
	}
	stmt, err := db.Prepare(`INSERT INTO BanList (target_ip, target_username, expires_at, is_active)
		VALUES (?, ?, ?, TRUE)
		ON DUPLICATE KEY UPDATE expires_at = VALUES(expires_at), is_active = TRUE`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, row := range banData {
		if _, err := stmt.Exec(row...); err != nil {
			return err
		}
	}
	return nil
}
