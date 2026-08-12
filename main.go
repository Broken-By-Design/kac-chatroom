package main

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
	_ "time/tzdata"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/adaptor"
	"github.com/joho/godotenv"
	socketio "github.com/zishang520/socket.io/servers/socket/v3"
	"golang.org/x/text/encoding/charmap"
)

const shareKey = "LofCen6W"

var (
	secretKey      string
	chatSecretKey  string
	adminSecretKey string
	geminiAPIKey   string
)

func main() {
	_ = godotenv.Load()
	_ = godotenv.Load("../.env")

	secretKey = os.Getenv("SECRET_KEY")
	chatSecretKey = os.Getenv("CHAT_SECRET_KEY")
	adminSecretKey = os.Getenv("ADMIN_SECRET_KEY")
	geminiAPIKey = os.Getenv("GEMINI_API_KEY")
	if secretKey == "" {
		secretKey = "dev-secret-key-change-me"
	}

	initState()
	loadAIPersonality()
	initTemplates()

	db = connectDB()
	aiClient = newGenAIClient(geminiAPIKey)

	selectTodayLog()
	initializeAIHistoryFromLog()
	syncBanListFromDB()

	opts := socketio.DefaultServerOptions()
	opts.SetPingTimeout(30 * time.Second)
	opts.SetPingInterval(30 * time.Second)
	opts.SetServeClient(false)
	io := setupSocketIO(opts)
	engineHandler := io.ServeHandler(opts)

	go runPeriodicBanSync()
	go checkExpiredMutes()
	go dailyChatlogRotation()

	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	setupRoutes(app)
	httpHandler := adaptor.FiberApp(app)

	mux := http.NewServeMux()
	mux.Handle("/socket.io/", engineHandler)
	mux.Handle("/", httpHandler)

	fmt.Println("Chatroom server listening on 0.0.0.0:5000")
	if err := http.ListenAndServe("0.0.0.0:5000", mux); err != nil {
		panic(err)
	}
}

func getRealIPOfCtx(c *fiber.Ctx) string {
	if v := c.Get("Cf-Connecting-Ip"); v != "" {
		return v
	}
	if v := c.Get("X-Forwarded-For"); v != "" {
		return strings.TrimSpace(strings.Split(v, ",")[0])
	}
	return c.IP()
}

func adminRequired(c *fiber.Ctx) error {
	if !getSession(c).IsAdmin {
		if strings.HasPrefix(c.Path(), "/get-users") || strings.HasPrefix(c.Path(), "/admin/") {
			return c.Status(401).JSON(fiber.Map{"message": "Authentication required"})
		}
		return c.Redirect("/admin-login")
	}
	return c.Next()
}

func setupRoutes(app *fiber.App) {
	app.Use("/static", func(c *fiber.Ctx) error {
		c.Set("Cache-Control", "no-cache")
		return c.Next()
	})
	app.Static("/static", "./static")

	app.Use(func(c *fiber.Ctx) error {
		// before-request: check_if_kicked
		sess := getSession(c)
		if sess.Nickname != "" && state.isKicked(sess.Nickname) {
			state.clearKicked(sess.Nickname)
			sess.LoggedIn = false
			sess.AcceptanceToken = ""
			setSession(c, sess)
			return c.Redirect("/student-portal")
		}

		// before-request: check_ban_status
		path := c.Path()
		if path != "/banned" &&
			!strings.HasPrefix(path, "/static/") &&
			!strings.HasPrefix(path, "/admin") &&
			!strings.HasPrefix(path, "/get-users") {
			ip := getRealIPOfCtx(c)
			nickname := getSession(c).Nickname
			if exp, banned := state.getBanned(nickname, ip); banned {
				expiryStr := "Permanent"
				if exp != nil && time.Now().UTC().Before(*exp) {
					chicago, _ := time.LoadLocation("America/Chicago")
					expiryStr = exp.In(chicago).Format("2006-01-02 15:04:05 MST")
					return render(c, "BANNED.html", map[string]any{"expiry": expiryStr})
				}
				if exp == nil {
					return render(c, "BANNED.html", map[string]any{"expiry": expiryStr})
				}
			}
		}

		err := c.Next()

		// after-request: clear_old_insecure_cookies
		for _, cookieName := range []string{"acceptance_cookie", "nickname", "admin_acceptance_cookie"} {
			if c.Cookies(cookieName) != "" {
				c.Cookie(&fiber.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1})
				fmt.Printf("Instructed browser to delete old cookie: %s\n", cookieName)
			}
		}
		return err
	})

	app.Get("/", func(c *fiber.Ctx) error {
		return render(c, "decoy.html", nil)
	})

	app.Get("/ping", func(c *fiber.Ctx) error {
		return c.SendString("Pong!")
	})

	app.Get("/tests", func(c *fiber.Ctx) error {
		return render(c, "tests/tests.html", nil)
	})

	app.Get("/tutors", func(c *fiber.Ctx) error {
		sess := getSession(c)
		if !sess.LoggedIn || sess.AcceptanceToken != chatSecretKey {
			return c.Redirect("/student-portal")
		}
		nickname := sess.Nickname
		if nickname == "" {
			nickname = "Guest"
		}
		return render(c, "video_chat.html", map[string]any{"nickname": nickname})
	})

	app.Get("/student-portal", func(c *fiber.Ctx) error {
		sess := getSession(c)
		if sess.LoggedIn && sess.AcceptanceToken == chatSecretKey && sess.Nickname != "" {
			return render(c, "chatroom.html", map[string]any{"nickname": sess.Nickname})
		}
		return render(c, "login.html", nil)
	})

	app.Post("/login", func(c *fiber.Ctx) error {
		sess := getSession(c)
		if c.FormValue("password") == chatSecretKey {
			sess.AcceptanceToken = c.FormValue("password")
			sess.LoggedIn = true
			setSession(c, sess)
			if sess.Nickname != "" {
				return c.Redirect("/student-portal")
			}
			return render(c, "nickname.html", nil)
		}
		return render(c, "login.html", map[string]any{"error": "Incorrect password"})
	})

	app.Post("/set-nickname", func(c *fiber.Ctx) error {
		sess := getSession(c)
		if sess.LoggedIn && sess.AcceptanceToken == chatSecretKey {
			nickname := c.FormValue("nickname")
			if state.isConnected(nickname) {
				return render(c, "nickname.html", map[string]any{"error": "User with that name already in chat"})
			}
			sess.Nickname = nickname
			setSession(c, sess)
			sio.Sockets().Emit("user_connected", nickname)
			return c.Redirect("/student-portal")
		}
		return render(c, "login.html", nil)
	})

	app.Get("/get_chatlogs", func(c *fiber.Ctx) error {
		sess := getSession(c)
		if !sess.LoggedIn || sess.AcceptanceToken != chatSecretKey {
			return c.Status(401).SendString("Unauthorized")
		}
		logs := readChatlogs()
		public := make([]ChatlogEntry, 0, len(logs))
		for _, log := range logs {
			if log.Type != "dm" {
				public = append(public, log)
			}
		}
		return c.JSON(public)
	})

	app.Get("/get_dm_logs", func(c *fiber.Ctx) error {
		sess := getSession(c)
		if !sess.LoggedIn || sess.AcceptanceToken != chatSecretKey {
			return c.Status(401).SendString("Unauthorized")
		}
		currentUser := sess.Nickname
		otherUser := c.Query("with")
		if currentUser == "" || otherUser == "" {
			return c.Status(400).JSON(fiber.Map{"error": "Missing required parameters"})
		}
		dmLogs := []ChatlogEntry{}
		for _, log := range readChatlogs() {
			if log.Type == "dm" {
				if (log.Nickname == currentUser && log.Recipient == otherUser) ||
					(log.Nickname == otherUser && log.Recipient == currentUser) {
					dmLogs = append(dmLogs, log)
				}
			}
		}
		return c.JSON(dmLogs)
	})

	app.Get("/get_connected_users", func(c *fiber.Ctx) error {
		sess := getSession(c)
		if !sess.LoggedIn || sess.AcceptanceToken != chatSecretKey {
			return c.Status(401).SendString("Unauthorized")
		}
		return c.JSON(state.getOnlineUsers())
	})

	app.Get("/get_image/*", func(c *fiber.Ctx) error {
		id := c.Params("*")
		ext := filepath.Ext(id)
		base := strings.TrimSuffix(id, ext)
		basePath := filepath.Join("./chatlogs/images/", base)
		for _, e := range []string{".jpg", ".jpeg", ".png", ".gif", ".webp", ".tiff", ".bmp", ".psd", ".raw", ".svg", ".heif", ".jp2", ".jpx", ".jpm", ".j2k", ".mj2"} {
			full := basePath + e
			if _, err := os.Stat(full); err == nil {
				return c.SendFile(full)
			}
		}
		return c.Status(404).SendString("File not found")
	})

	app.Get("/game-gamble-d6eca0", func(c *fiber.Ctx) error {
		return render(c, "gamble.html", nil)
	})

	app.Get("/get_stream/:fid", func(c *fiber.Ctx) error {
		fid := c.Params("fid")
		resp, err := http.Get(fmt.Sprintf("https://feb.superstudies.site/api/febbox/links?shareKey=%s&fid=%s", shareKey, fid))
		if err != nil {
			return c.JSON(fiber.Map{"url": ""})
		}
		defer resp.Body.Close()
		var data []struct {
			URL string `json:"url"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&data)
		url := ""
		if len(data) > 0 {
			url = data[0].URL
		}
		return c.JSON(fiber.Map{"url": url})
	})

	app.Get("/subtitles/:fid/*", func(c *fiber.Ctx) error {
		fid := c.Params("fid")
		filename := c.Params("*")
		subsDir := filepath.Clean(filepath.Join("subtitles", fid))
		full := filepath.Clean(filepath.Join(subsDir, filename))
		if !strings.HasPrefix(full, subsDir+string(filepath.Separator)) {
			return c.Status(403).SendString("Forbidden")
		}
		if _, err := os.Stat(full); err != nil {
			return c.Status(404).SendString("Not found")
		}
		return c.SendFile(full)
	})

	app.Get("/get_subtitles/:fid", func(c *fiber.Ctx) error {
		fid := c.Params("fid")
		subs, status, errMsg := fetchSubtitles(fid)
		if status != 200 {
			return c.Status(status).JSON(fiber.Map{"error": errMsg})
		}
		return c.JSON(fiber.Map{"subs": subs})
	})

	app.Get("/movie/:fid", func(c *fiber.Ctx) error {
		fid := c.Params("fid")
		resp, err := http.Get(fmt.Sprintf("https://feb.superstudies.site/api/febbox/links?shareKey=%s&fid=%s", shareKey, fid))
		if err != nil {
			return c.Status(500).SendString("Error loading movie")
		}
		defer resp.Body.Close()
		var data []struct {
			URL string `json:"url"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&data)
		url := ""
		if len(data) > 0 {
			url = data[0].URL
		}
		return render(c, "movie.html", map[string]any{"fid": fid, "movie_url": url})
	})

	app.Get("/movies", func(c *fiber.Ctx) error {
		resp, err := http.Get(fmt.Sprintf("https://feb.superstudies.site/api/febbox/files?shareKey=%s", shareKey))
		if err != nil {
			return c.Status(500).SendString("Error loading movies")
		}
		defer resp.Body.Close()
		var data []struct {
			FileName string `json:"file_name"`
			Thumb    string `json:"thumb"`
			FID      string `json:"fid"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			return c.Status(500).SendString("Error loading movies")
		}
		movieList := []map[string]string{}
		for _, item := range data {
			if item.FileName != "" {
				cleanName := strings.Split(item.FileName, ")")[0] + ")"
				movieList = append(movieList, map[string]string{"name": cleanName, "thumb": item.Thumb, "file_id": item.FID})
			}
		}
		sort.Slice(movieList, func(i, j int) bool {
			return strings.ToLower(strings.ReplaceAll(movieList[i]["name"], " ", "")) <
				strings.ToLower(strings.ReplaceAll(movieList[j]["name"], " ", ""))
		})
		return render(c, "movie_list.html", map[string]any{"movies": movieList})
	})

	app.Get("/crash", func(c *fiber.Ctx) error {
		return render(c, "pain.html", nil)
	})

	app.Get("/admin", func(c *fiber.Ctx) error {
		if getSession(c).IsAdmin {
			return render(c, "admin/admin.html", nil)
		}
		return render(c, "admin/admin-login.html", nil)
	})

	app.Post("/admin-login", func(c *fiber.Ctx) error {
		if c.FormValue("password") == adminSecretKey {
			sess := getSession(c)
			sess.IsAdmin = true
			setSession(c, sess)
			return c.Redirect("/admin")
		}
		return render(c, "admin/admin-login.html", map[string]any{"error": "Incorrect password"})
	})

	app.Get("/get-users", adminRequired, func(c *fiber.Ctx) error {
		fmt.Println(getRealIPOfCtx(c))
		return c.JSON(state.usersWithIPCopy())
	})

	app.Post("/admin/kick", adminRequired, func(c *fiber.Ctx) error {
		var body struct {
			Users []string `json:"users"`
		}
		if err := c.BodyParser(&body); err != nil || len(body.Users) == 0 {
			return c.Status(400).JSON(fiber.Map{"message": "Invalid request. 'users' key is missing."})
		}
		fmt.Printf("--- KICK ACTION: Received request to log out %s ---\n", strings.Join(body.Users, ", "))
		kickedCount := 0
		for _, user := range body.Users {
			sids := state.getSIDs(user)
			if len(sids) > 0 {
				state.kickUser(user)
				for _, sid := range sids {
					emitToSID(sid, "force_logout", map[string]any{})
					fmt.Printf("Sent force_logout command to %s (SID: %s).\n", user, sid)
					disconnectSID(sid)
				}
				kickedCount++
			}
		}
		if kickedCount == 0 {
			return c.Status(404).JSON(fiber.Map{"message": "No active users found with the provided names."})
		}
		return c.JSON(fiber.Map{"message": fmt.Sprintf("Successfully sent logout command to %d user(s).", kickedCount)})
	})

	app.Post("/admin/mute", adminRequired, func(c *fiber.Ctx) error {
		var body struct {
			Users []string `json:"users"`
		}
		if err := c.BodyParser(&body); err != nil || len(body.Users) == 0 {
			return c.Status(400).JSON(fiber.Map{"message": "Invalid request. 'users' key is missing."})
		}
		for _, user := range body.Users {
			sids := state.getSIDs(user)
			if len(sids) > 0 {
				state.muteUser(user)
				for _, sid := range sids {
					emitToSID(sid, "force_mute", map[string]any{})
					fmt.Printf("Sent force_mute command to %s (SID: %s).\n", user, sid)
				}
			}
		}
		return c.JSON(fiber.Map{"message": "Successfully sent mute command"})
	})

	app.Post("/admin/unmute", adminRequired, func(c *fiber.Ctx) error {
		var body struct {
			Users []string `json:"users"`
		}
		if err := c.BodyParser(&body); err != nil || len(body.Users) == 0 {
			return c.Status(400).JSON(fiber.Map{"message": "Invalid request. 'users' key is missing."})
		}
		for _, user := range body.Users {
			state.unmuteUser(user)
			sids := state.getSIDs(user)
			if len(sids) > 0 {
				for _, sid := range sids {
					emitToSID(sid, "force_unmute", map[string]any{})
					fmt.Printf("Sent force_unmute command to %s (SID: %s).\n", user, sid)
				}
			}
		}
		return c.JSON(fiber.Map{"message": "Successfully sent unmute command"})
	})

	banHandler := func(c *fiber.Ctx) error {
		var body struct {
			Users    []string `json:"users"`
			Duration string   `json:"duration"`
		}
		if err := c.BodyParser(&body); err != nil || len(body.Users) == 0 {
			return c.Status(400).JSON(fiber.Map{"message": "Invalid request. 'users' key is missing."})
		}
		usersWithIPs := [][2]string{}
		for _, user := range body.Users {
			if ip, ok := state.usersWithIPOf(user); ok {
				usersWithIPs = append(usersWithIPs, [2]string{user, ip})
			}
		}
		if len(usersWithIPs) == 0 {
			return c.Status(404).JSON(fiber.Map{"message": "Could not find IPs for any of the selected users."})
		}
		ipsToBan := map[string]bool{}
		for _, up := range usersWithIPs {
			ipsToBan[up[1]] = true
		}
		fmt.Printf("--- IP BAN ACTION: Banning IPs %s for %s ---\n", strings.Join(keysOf(ipsToBan), ", "), body.Duration)

		expiry := getBanExpiry(body.Duration)
		banData := make([][]any, 0, len(usersWithIPs))
		for _, up := range usersWithIPs {
			banData = append(banData, []any{up[1], up[0], expiry})
		}
		if err := insertBans(banData); err != nil {
			fmt.Printf("Database error during ban: %v\n", err)
			return c.Status(500).JSON(fiber.Map{"message": "A database error occurred."})
		}
		for _, up := range usersWithIPs {
			state.addBan(up[0], up[1], expiry)
		}
		for _, up := range usersWithIPs {
			sids := state.getSIDs(up[0])
			if len(sids) > 0 {
				state.kickUser(up[0])
				for _, sid := range sids {
					emitToSID(sid, "force_logout", map[string]any{})
					fmt.Printf("Sent force_logout command to %s (SID: %s).\n", up[0], sid)
					disconnectSID(sid)
				}
			}
		}
		return c.JSON(fiber.Map{"message": fmt.Sprintf("Successfully banned and kicked users from IPs: %s", strings.Join(keysOf(ipsToBan), ", "))})
	}
	app.Post("/admin/ban", adminRequired, banHandler)
	app.Post("/admin/ip-ban", adminRequired, banHandler)

	app.Get("/banned", func(c *fiber.Ctx) error {
		expiry := c.Query("expires_at")
		return render(c, "BANNED.html", map[string]any{"expiry": expiry})
	})

	app.Post("/admin/reset-chat", adminRequired, func(c *fiber.Ctx) error {
		clearChatlogs()
		state.resetPromptHistory()
		fmt.Println("In-memory AI prompt history has been cleared.")
		sio.Sockets().Emit("chat_cleared", map[string]any{})
		fmt.Println("Sent 'chat_cleared' event to all clients.")
		return c.JSON(fiber.Map{"message": "Chat has been successfully reset."})
	})

	app.Post("/admin/reload-all", adminRequired, func(c *fiber.Ctx) error {
		sio.Sockets().Emit("force_reload", map[string]any{})
		return c.JSON(fiber.Map{"message": "Everyone has been reloaded."})
	})

	app.Post("/admin/cloak-all", adminRequired, func(c *fiber.Ctx) error {
		sio.Sockets().Emit("force_cloak", map[string]any{})
		return c.JSON(fiber.Map{"message": "Everyone has been cloaked."})
	})

	app.Post("/admin/jumpscare", adminRequired, func(c *fiber.Ctx) error {
		var body struct {
			Users    []string `json:"users"`
			Duration string   `json:"duration"`
		}
		if err := c.BodyParser(&body); err != nil || len(body.Users) == 0 {
			return c.Status(400).JSON(fiber.Map{"message": "Invalid request. 'users' key is missing."})
		}
		_ = body.Duration
		for _, user := range body.Users {
			state.addJumpscare(user)
			for _, sid := range state.getSIDs(user) {
				emitToSID(sid, "force_jumpscare", map[string]any{})
			}
		}
		return c.JSON(fiber.Map{"message": "User(s) have been jumpscared."})
	})

	app.Post("/admin/crash-users", adminRequired, func(c *fiber.Ctx) error {
		var body struct {
			Users []string `json:"users"`
		}
		if err := c.BodyParser(&body); err != nil || len(body.Users) == 0 {
			return c.Status(400).JSON(fiber.Map{"message": "Invalid request. 'users' key is missing."})
		}
		for _, user := range body.Users {
			state.addCrash(user)
			for _, sid := range state.getSIDs(user) {
				emitToSID(sid, "force_crash", map[string]any{})
			}
		}
		return c.JSON(fiber.Map{"message": "User(s) have been crashed."})
	})

	app.Post("/admin/censor-users", adminRequired, func(c *fiber.Ctx) error {
		var body struct {
			Users []string `json:"users"`
		}
		if err := c.BodyParser(&body); err != nil || len(body.Users) == 0 {
			return c.Status(400).JSON(fiber.Map{"message": "Invalid request. 'users' key is missing."})
		}
		for _, user := range body.Users {
			state.addCensor(user)
		}
		return c.JSON(fiber.Map{"message": "User(s) have been crashed."})
	})

	app.Post("/admin/uncensor-users", adminRequired, func(c *fiber.Ctx) error {
		var body struct {
			Users []string `json:"users"`
		}
		if err := c.BodyParser(&body); err != nil || len(body.Users) == 0 {
			return c.Status(400).JSON(fiber.Map{"message": "Invalid request. 'users' key is missing."})
		}
		for _, user := range body.Users {
			state.removeCensor(user)
		}
		return c.JSON(fiber.Map{"message": "User(s) have been crashed."})
	})

	sendMessageHandler := func(c *fiber.Ctx) error {
		var body struct {
			Message  string `json:"message"`
			Username string `json:"username"`
		}
		if err := c.BodyParser(&body); err != nil || body.Message == "" {
			return c.Status(400).JSON(fiber.Map{"message": "Invalid request. 'message' key is missing."})
		}
		if body.Username == "" {
			sio.Sockets().Emit("system_message", map[string]any{"message": body.Message, "highlight": true})
		} else {
			timestamp := isoNow()
			sio.Sockets().Emit("chat_message", map[string]any{"message": body.Message, "nickname": body.Username, "timestamp": timestamp, "system": false})
			addChatlogEntry(body.Message, body.Username, timestamp, "text", "")
		}
		return c.JSON(fiber.Map{"message": "Message sent to chat."})
	}
	app.Post("/admin/system-message", adminRequired, sendMessageHandler)
	app.Post("/admin/user-message", adminRequired, sendMessageHandler)

	app.Post("/admin/update-bans", adminRequired, func(c *fiber.Ctx) error {
		syncBanListFromDB()
		return c.JSON(fiber.Map{"message": "Ban list has been updated from the database."})
	})

	app.Post("/admin/reset-bot-memory", adminRequired, func(c *fiber.Ctx) error {
		state.resetPromptHistory()
		fmt.Println("In-memory AI prompt history has been cleared.")
		return c.JSON(fiber.Map{"message": "Bot's memory has been successfully reset."})
	})

	app.Post("/admin/clear-cache", adminRequired, func(c *fiber.Ctx) error {
		images := clearImageHashCache()
		pending := state.clearPendingVideoSelections()
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		msg := fmt.Sprintf("Cleared %d image-hash cache entries and %d pending video selections. Heap: %.1f MB, System: %.1f MB.",
			images, pending, float64(m.HeapAlloc)/1024/1024, float64(m.Sys)/1024/1024)
		fmt.Println(msg)
		return c.JSON(fiber.Map{"message": msg})
	})

	app.Get("/admin/mem-stats", adminRequired, func(c *fiber.Ctx) error {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		return c.JSON(fiber.Map{
			"heap_mb":    float64(m.HeapAlloc) / 1024 / 1024,
			"inuse_mb":   float64(m.HeapInuse) / 1024 / 1024,
			"sys_mb":     float64(m.Sys) / 1024 / 1024,
			"rss_mb":     processRSSMB(),
			"goroutines": runtime.NumGoroutine(),
		})
	})

	app.Post("/admin/pinned-message", adminRequired, func(c *fiber.Ctx) error {
		var body struct {
			Message string `json:"message"`
		}
		if err := c.BodyParser(&body); err != nil || body.Message == "" {
			return c.Status(400).JSON(fiber.Map{"message": "Invalid request. 'message' key is missing."})
		}
		nickname := getSession(c).Nickname
		if nickname == "" {
			nickname = "Admin"
		}
		sio.Sockets().Emit("add_pinned_msg", map[string]any{"message": body.Message, "nickname": nickname})
		return c.JSON(fiber.Map{"message": "Pinned message updated."})
	})

	app.Get("/jumpscare/*", func(c *fiber.Ctx) error {
		filename := c.Params("*")
		jumpscareDir, err := filepath.Abs("jumpscare")
		if err != nil {
			return c.Status(500).SendString("Server Error")
		}
		safe, err := filepath.Abs(filepath.Join(jumpscareDir, filename))
		if err != nil || !strings.HasPrefix(safe, jumpscareDir) {
			return c.Status(403).SendString("Forbidden")
		}
		if _, err := os.Stat(safe); err != nil {
			return c.Status(404).SendString("Not found")
		}
		return c.SendFile(safe)
	})
}

// fetchSubtitles implements main.py get_subtitle.
func fetchSubtitles(fid string) ([]map[string]string, int, string) {
	fidDir := filepath.Join("subtitles", fid)
	_ = os.MkdirAll(fidDir, 0o755)

	entries, _ := os.ReadDir(fidDir)
	existing := []string{}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".vtt") {
			existing = append(existing, e.Name())
		}
	}
	if len(existing) > 0 {
		sort.Strings(existing)
		subs := []map[string]string{}
		for _, f := range existing {
			labelNum := strings.TrimSuffix(f, ".vtt")
			subs = append(subs, map[string]string{"label": "English " + labelNum, "src": subtitleURL(fid, f)})
		}
		return subs, 200, ""
	}

	imdbResp, err := http.Get(fmt.Sprintf("https://feb.superstudies.site/api/febbox/imdb?fid=%s", fid))
	if err != nil || imdbResp.StatusCode != 200 {
		return nil, 404, "Failed to fetch IMDB ID"
	}
	var imdbData struct {
		Imdb string `json:"imdb"`
	}
	_ = json.NewDecoder(imdbResp.Body).Decode(&imdbData)
	imdbResp.Body.Close()
	if imdbData.Imdb == "" {
		return nil, 404, "No IMDB ID found"
	}

	subResp, err := http.Get(fmt.Sprintf("https://yts-subs.com/movie-imdb/%s", imdbData.Imdb))
	if err != nil || subResp.StatusCode != 200 {
		return nil, 404, "Subtitles not found"
	}
	doc, err := goquery.NewDocumentFromReader(subResp.Body)
	subResp.Body.Close()
	if err != nil {
		return nil, 500, "Server Error"
	}

	downloadLinks := []string{}
	doc.Find("tr").Each(func(i int, sel *goquery.Selection) {
		if i == 0 {
			return
		}
		cols := sel.Find("td")
		if cols.Length() >= 2 {
			language := strings.TrimSpace(cols.Eq(1).Text())
			if strings.Contains(language, "English") {
				if href, ok := cols.Eq(4).Find("a").Attr("href"); ok {
					downloadLinks = append(downloadLinks, href)
				}
			}
		}
	})
	if len(downloadLinks) == 0 {
		return nil, 404, "English subtitles not found"
	}

	top := downloadLinks
	if len(top) > 3 {
		top = top[:3]
	}
	processed := []map[string]string{}
	for index, link := range top {
		pageResp, err := http.Get("https://yts-subs.com" + link)
		if err != nil {
			continue
		}
		pageDoc, err := goquery.NewDocumentFromReader(pageResp.Body)
		pageResp.Body.Close()
		if err != nil {
			continue
		}
		btn := pageDoc.Find("a#btn-download-subtitle")
		if btn.Length() == 0 {
			continue
		}
		encoded, _ := btn.Attr("data-link")
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			continue
		}
		zipResp, err := http.Get(string(decoded))
		if err != nil {
			continue
		}
		zipData, _ := io.ReadAll(zipResp.Body)
		zipResp.Body.Close()
		zr, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
		if err != nil {
			continue
		}
		var srt []byte
		for _, zf := range zr.File {
			if strings.HasSuffix(strings.ToLower(zf.Name), ".srt") {
				if rc, err := zf.Open(); err == nil {
					srt, _ = io.ReadAll(rc)
					rc.Close()
					break
				}
			}
		}
		if len(srt) == 0 {
			continue
		}
		vttLines := []string{"WEBVTT\n"}
		for _, line := range strings.Split(decodeSRT(srt), "\n") {
			if isDigits(strings.TrimSpace(line)) {
				continue
			}
			if strings.Contains(line, "-->") {
				line = strings.ReplaceAll(line, ",", ".")
			}
			vttLines = append(vttLines, line)
		}
		fileName := fmt.Sprintf("%d.vtt", index+1)
		_ = os.WriteFile(filepath.Join(fidDir, fileName), []byte(strings.Join(vttLines, "\n")), 0o644)
		processed = append(processed, map[string]string{
			"label": fmt.Sprintf("English %d", index+1),
			"src":   subtitleURL(fid, fileName),
		})
	}

	if len(processed) == 0 {
		return nil, 500, "Failed to process any subtitles"
	}
	return processed, 200, ""
}

func decodeSRT(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	out, err := charmap.ISO8859_1.NewDecoder().Bytes(b)
	if err != nil {
		return string(b)
	}
	return string(out)
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
