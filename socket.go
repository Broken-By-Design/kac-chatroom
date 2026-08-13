package main

import (
	"crypto/sha256"
	"fmt"
	"html"
	"net/http"
	"strings"
	"time"

	socketio "github.com/zishang520/socket.io/servers/socket/v3"
	"github.com/zishang520/socket.io/v3/pkg/types"
)

var sio *socketio.Server

func emitChatMessage(payload map[string]any) {
	sio.Sockets().Emit("chat_message", payload)
}

func emitToSID(sid, ev string, payload map[string]any) {
	sio.Sockets().To(socketio.Room(sid)).Emit(ev, payload)
}

func emitToSIDNoPayload(sid, ev string) {
	sio.Sockets().To(socketio.Room(sid)).Emit(ev)
}

func disconnectSID(sid string) {
	if s, ok := sio.Sockets().Sockets().Load(socketio.SocketId(sid)); ok {
		s.Disconnect(true)
	}
}

func requestOf(s *socketio.Socket) *http.Request {
	if s.Request() == nil {
		return nil
	}
	return s.Request().Request()
}

func sessionOf(s *socketio.Socket) *Session {
	if s.Data() != nil {
		if sess, ok := s.Data().(*Session); ok {
			return sess
		}
	}
	return &Session{}
}

func setupSocketIO(opts *socketio.ServerOptions) *socketio.Server {
	io := socketio.NewServer(nil, opts)
	sio = io

	io.On("connection", func(clients ...any) {
		s := clients[0].(*socketio.Socket)
		handleConnect(s)

		s.On("disconnect", func(reason ...any) { handleDisconnect(s) })
		s.On("request_status", func(data ...any) { handleRequestStatus(s) })
		s.On("user_jumpscared", func(data ...any) { removeFromJumpscare(s) })
		s.On("user_crashed", func(data ...any) { removeFromCrash(s) })
		s.On("request_missed_messages", func(data ...any) { handleRequestMissedMessages(s, data) })
		s.On("private_message", func(data ...any) { handlePrivateMessage(s, data) })
		s.On("chat_message", func(data ...any) { handleChatMessage(s, data) })
		s.On("image_chunk", func(data ...any) { handleImageChunk(s, data) })
		s.On("typing", func(data ...any) { handleTyping(s) })
		s.On("stop_typing", func(data ...any) { handleStopTyping(s) })
		s.On("join_video_lounge", func(data ...any) { handleJoinVideoLounge(s) })
		s.On("webrtc_offer", func(data ...any) { handleWebRTCOffer(s, data) })
		s.On("webrtc_answer", func(data ...any) { handleWebRTCAnswer(s, data) })
		s.On("webrtc_candidate", func(data ...any) { handleWebRTCCandidate(s, data) })
		s.On("screen_sharing_started", func(data ...any) { handleScreenSharingStarted(s) })
		s.On("screen_sharing_stopped", func(data ...any) { handleScreenSharingStopped(s) })
	})
	return io
}

func handleConnect(s *socketio.Socket) {
	sess := sessionFromRequest(requestOf(s))
	s.SetData(sess)

	userIP := getRealIP(requestOf(s))
	nickname := sess.Nickname
	sid := string(s.Id())
	fmt.Printf("User connected: %s\n", nickname)

	if !sess.LoggedIn || sess.AcceptanceToken != chatSecretKey {
		s.Disconnect(true)
		return
	}

	if nickname != "" {
		state.mu.Lock()
		state.connectedUsernames[nickname] = true
		if state.usersWithSID[nickname] == nil {
			state.usersWithSID[nickname] = map[string]bool{}
		}
		state.usersWithSID[nickname][sid] = true
		state.usersWithIP[nickname] = userIP
		jumpscare := state.usersToJumpscare[nickname]
		crash := state.usersToCrash[nickname]
		state.mu.Unlock()

		if jumpscare {
			emitToSIDNoPayload(sid, "force_jumpscare")
		}
		if crash {
			emitToSIDNoPayload(sid, "force_crash")
		}
	}
}

func handleRequestStatus(s *socketio.Socket) {
	sess := sessionOf(s)
	nickname := sess.Nickname
	if nickname != "" {
		state.mu.Lock()
		isMuted := state.mutedUsers[nickname]
		state.mu.Unlock()
		s.Emit("user_status", map[string]any{"is_muted": isMuted})
	}
}

func removeFromJumpscare(s *socketio.Socket) {
	nickname := sessionOf(s).Nickname
	state.mu.Lock()
	delete(state.usersToJumpscare, nickname)
	state.mu.Unlock()
}

func removeFromCrash(s *socketio.Socket) {
	nickname := sessionOf(s).Nickname
	state.mu.Lock()
	delete(state.usersToCrash, nickname)
	state.mu.Unlock()
}

func handleDisconnect(s *socketio.Socket) {
	sid := string(s.Id())
	nicknameToRemove := ""

	state.mu.Lock()
	nickname := state.videoChatUsers[sid]
	if nickname != "" {
		fmt.Printf("VIDEO LOUNGE: %s (%s) left.\n", nickname, sid)
		delete(state.videoChatUsers, sid)
		if state.screenSharers[sid] {
			delete(state.screenSharers, sid)
			sio.Sockets().Emit("screen_sharing_stopped", map[string]any{"sid": sid})
		}
		sio.Sockets().Emit("user_left_lounge", sid)
	}

	for n, sids := range state.usersWithSID {
		if sids[sid] {
			nicknameToRemove = n
			break
		}
	}

	if nicknameToRemove != "" {
		fmt.Printf("User disconnected: %s\n", nicknameToRemove)
		sids := state.usersWithSID[nicknameToRemove]
		delete(sids, sid)
		if len(sids) == 0 {
			delete(state.usersWithSID, nicknameToRemove)
			if state.typingUsers[nicknameToRemove] {
				delete(state.typingUsers, nicknameToRemove)
				sio.Sockets().Emit("typing_update", map[string]any{"users": keysOf(state.typingUsers)})
			}
			if state.connectedUsernames[nicknameToRemove] {
				delete(state.connectedUsernames, nicknameToRemove)
			}
			delete(state.usersWithIP, nicknameToRemove)
		}
	}
	state.mu.Unlock()
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func handleRequestMissedMessages(s *socketio.Socket, data []any) {
	if len(data) < 1 {
		return
	}
	msg, ok := data[0].(map[string]any)
	if !ok {
		return
	}
	afterStr, _ := msg["after"].(string)
	if afterStr == "" {
		return
	}
	after, ok := parseTimestamp(afterStr)
	if !ok {
		fmt.Printf("Invalid timestamp format received: %s\n", afterStr)
		return
	}

	var missed []ChatlogEntry
	for _, log := range readChatlogs() {
		if log.Timestamp == "" {
			continue
		}
		ts, ok := parseTimestamp(log.Timestamp)
		if !ok {
			continue
		}
		if ts.After(after) {
			missed = append(missed, log)
		}
	}

	if len(missed) > 0 {
		s.Emit("missed_messages", missed)
	}
}

func parseTimestamp(s string) (time.Time, bool) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, true
	}
	if t, err := time.Parse("2006-01-02T15:04:05.999999", s); err == nil {
		return t, true
	}
	return time.Time{}, false
}

func handlePrivateMessage(s *socketio.Socket, data []any) {
	if len(data) < 1 {
		return
	}
	msg, ok := data[0].(map[string]any)
	if !ok {
		return
	}
	message, _ := msg["message"].(string)
	recipient, _ := msg["to"].(string)
	timestamp, _ := msg["timestamp"].(string)
	sender := sessionOf(s).Nickname

	now := time.Now()

	if info, ok := state.getMutedUser(sender); ok {
		if now.Before(info.MuteUntil) {
			remaining := info.MuteUntil.Sub(now).Seconds()
			s.Emit("system_message", map[string]any{"message": fmt.Sprintf("You are still muted for %.1f seconds.", remaining)})
			return
		}
	}

	if state.registerMessageStamp(sender, now) {
		duration, _ := state.recordSpam(sender, now)
		s.Emit("system_message", map[string]any{"message": fmt.Sprintf("You are muted for %d seconds due to spamming.", duration)})
		s.Emit("force_mute", map[string]any{})
		return
	}

	state.mu.Lock()
	senderConnected := state.connectedUsernames[sender]
	recipientConnected := state.connectedUsernames[recipient]
	state.mu.Unlock()

	if !senderConnected {
		fmt.Printf("Private message rejected: sender %s not connected\n", sender)
		return
	}
	if !recipientConnected {
		fmt.Printf("Private message rejected: recipient %s not connected\n", recipient)
		s.Emit("private_message_error", map[string]any{"error": fmt.Sprintf("User %s is not online", recipient)})
		return
	}

	fmt.Printf("Private message from %s to %s: %s\n", sender, recipient, message)

	addChatlogEntry(message, sender, timestamp, "dm", recipient)

	dmPayload := map[string]any{
		"message":   message,
		"from":      sender,
		"to":        recipient,
		"timestamp": timestamp,
	}

	state.mu.Lock()
	for recipientSID := range state.usersWithSID[recipient] {
		emitToSID(recipientSID, "private_message", dmPayload)
	}
	for senderSID := range state.usersWithSID[sender] {
		emitToSID(senderSID, "private_message", dmPayload)
	}
	state.mu.Unlock()
}

func handleChatMessage(s *socketio.Socket, data []any) {
	if len(data) < 1 {
		return
	}
	msg, ok := data[0].(map[string]any)
	if !ok {
		return
	}
	message, _ := msg["message"].(string)
	nickname := sessionOf(s).Nickname
	timestamp, _ := msg["timestamp"].(string)
	sid := string(s.Id())
	fmt.Println("Message received:", message, "from", nickname)

	now := time.Now()

	if info, ok := state.getMutedUser(nickname); ok {
		if now.Before(info.MuteUntil) {
			remaining := info.MuteUntil.Sub(now).Seconds()
			s.Emit("system_message", map[string]any{"message": fmt.Sprintf("You are still muted for %.1f seconds.", remaining)})
			return
		}
	}

	if state.registerMessageStamp(nickname, now) {
		duration, _ := state.recordSpam(nickname, now)
		s.Emit("system_message", map[string]any{"message": fmt.Sprintf("You are muted for %d seconds due to spamming.", duration)})
		s.Emit("force_mute", map[string]any{})
		return
	}

	if state.isCensored(nickname) {
		message = censorMessage(message, nil, 0.85)
	}

	if !state.isConnected(nickname) {
		state.ensureConnected(nickname, sid, getRealIP(requestOf(s)))
	}

	if message == "/clear" {
		s.Emit("clear_chat")
		return
	}

	if strings.HasPrefix(message, "/highlight") {
		message = strings.TrimPrefix(message, "/highlight ")
		if message != "" {
			emitChatMessage(map[string]any{"message": message, "nickname": nickname, "timestamp": timestamp, "highlight": true})
			addChatlogEntry(message, nickname, timestamp, "highlight", "")
			return
		}
	}

	emitChatMessage(map[string]any{"message": message, "nickname": nickname, "timestamp": timestamp})
	addChatlogEntry(message, nickname, timestamp, "text", "")
	addToPromptHistorySafe("user", fmt.Sprintf("%s: %s", nickname, message))

	if strings.HasPrefix(strings.ToLower(message), "!bot ") {
		fmt.Printf("Asking bot: `%s`\n", message)
		resp := generateResponse(message, nickname, true, false, "")
		timestamp = isoNow()
		if strings.HasPrefix(resp, "/highlight ") {
			resp = strings.TrimPrefix(resp, "/highlight ")
			emitChatMessage(map[string]any{"message": resp, "nickname": "KAC-Bot", "timestamp": timestamp, "highlight": true})
			addChatlogEntry(resp, "KAC-Bot", timestamp, "highlight", "")
		} else {
			emitChatMessage(map[string]any{"message": resp, "nickname": "KAC-Bot", "timestamp": timestamp})
			addChatlogEntry(resp, "KAC-Bot", timestamp, "text", "")
		}
	} else if strings.HasPrefix(message, "/online") {
		online := state.getOnlineUsers()
		msgText := formatOnlineUsers(nickname, online)
		emitChatMessage(map[string]any{"message": msgText, "nickname": "KAC-Bot", "timestamp": timestamp, "system": true})
		addChatlogEntry(msgText, "KAC-Bot", timestamp, "system", "")
	} else if strings.HasPrefix(message, "/help") {
		msgText := html.EscapeString(fmt.Sprintf("%s, The commands are: !bot <message>, /clear, /online, /highlight <message>, /cloak, /lyrics <song name>, /whitman, /gamble, /yt, and /clock", nickname))
		emitChatMessage(map[string]any{"message": msgText, "nickname": "KAC-Bot", "timestamp": timestamp, "system": true})
		addChatlogEntry(html.EscapeString(fmt.Sprintf("%s, The commands are: !bot <message>, /clear, /online, /highlight <message>, /cloak, /lyrics <song name>, /whitman, /gamble, /yt, and /clock", nickname)), "KAC-Bot", timestamp, "system", "")
	} else {
		parseCommand(message, nickname, timestamp)
	}
}

func formatOnlineUsers(nickname string, online []string) string {
	var sb strings.Builder
	sb.WriteString(nickname)
	sb.WriteString(", The users online are: ")
	if len(online) > 0 {
		last := online[len(online)-1]
		for _, u := range online[:len(online)-1] {
			sb.WriteString(u)
			sb.WriteString(", ")
		}
		if len(online) > 1 {
			sb.WriteString("and ")
		}
		sb.WriteString(last)
	}
	return sb.String()
}

func handleImageChunk(s *socketio.Socket, data []any) {
	sess := sessionOf(s)
	if !sess.LoggedIn || sess.AcceptanceToken != chatSecretKey {
		s.Disconnect(true)
		return
	}
	if len(data) < 1 {
		return
	}
	msg, ok := data[0].(map[string]any)
	if !ok {
		return
	}
	tempID, _ := msg["id"].(string)
	isLast, _ := msg["is_last"].(bool)

	var chunk []byte
	if buf, ok := msg["chunk"].(types.BufferInterface); ok {
		chunk = buf.Bytes()
	}

	storeImageChunk(tempID, chunk)

	if isLast {
		metadata, _ := msg["metadata"].(map[string]any)
		if metadata == nil {
			metadata = map[string]any{}
		}
		metadata["nickname"] = sess.Nickname
		go assembleAndEmitImage(tempID, metadata)
	}
}

func assembleAndEmitImage(tempID string, metadata map[string]any) {
	chunks := takeImageChunks(tempID)
	var full []byte
	for _, c := range chunks {
		full = append(full, c...)
	}

	sum := sha256.Sum256(full)

	existingID := findImageIDByHash(sum)
	finalID := existingID
	if finalID == "" {
		finalID = generateRandomString()
		name, _ := metadata["name"].(string)
		if err := saveImage(finalID, name, full); err != nil {
			fmt.Printf("Failed to save image: %v\n", err)
			return
		}
		cacheImageID(sum, finalID)
	}

	nickname, _ := metadata["nickname"].(string)
	timestamp, _ := metadata["timestamp"].(string)

	sio.Sockets().Emit("add_image", map[string]any{"id": finalID, "nickname": nickname, "timestamp": timestamp})
	addChatlogEntry(finalID, nickname, timestamp, "image", "")

	if question, _ := metadata["question"].(string); question != "" {
		msg := "!bot " + question
		sio.Sockets().Emit("chat_message", map[string]any{"message": msg, "nickname": nickname, "timestamp": timestamp})
		addChatlogEntry(msg, nickname, timestamp, "text", "")
		resp := generateResponse(question, nickname, true, true, finalID)
		ts := isoNow()
		sio.Sockets().Emit("chat_message", map[string]any{"message": resp, "nickname": "KAC-Bot", "timestamp": ts})
		addChatlogEntry(resp, "KAC-Bot", ts, "text", "")
	} else {
		addToPromptHistorySafe("user", fmt.Sprintf("%s: sent an image.", nickname))
	}
}

func handleTyping(s *socketio.Socket) {
	nickname := sessionOf(s).Nickname
	if nickname != "" {
		state.mu.Lock()
		state.typingUsers[nickname] = true
		sio.Sockets().Emit("typing_update", map[string]any{"users": keysOf(state.typingUsers)})
		state.mu.Unlock()
	}
}

func handleStopTyping(s *socketio.Socket) {
	nickname := sessionOf(s).Nickname
	if nickname != "" {
		state.mu.Lock()
		if state.typingUsers[nickname] {
			delete(state.typingUsers, nickname)
			sio.Sockets().Emit("typing_update", map[string]any{"users": keysOf(state.typingUsers)})
		}
		state.mu.Unlock()
	}
}

func handleJoinVideoLounge(s *socketio.Socket) {
	sid := string(s.Id())
	nickname := sessionOf(s).Nickname
	if nickname == "" {
		nickname = "Anonymous"
	}

	state.mu.Lock()
	oldSid := ""
	for ss, n := range state.videoChatUsers {
		if n == nickname {
			oldSid = ss
			break
		}
	}
	usersInLounge := make([]map[string]string, 0, len(state.videoChatUsers))
	for ss, n := range state.videoChatUsers {
		usersInLounge = append(usersInLounge, map[string]string{"sid": ss, "nickname": n})
	}
	state.videoChatUsers[sid] = nickname
	sharers := make([]string, 0, len(state.screenSharers))
	for ss := range state.screenSharers {
		sharers = append(sharers, ss)
	}
	state.mu.Unlock()

	if oldSid != "" {
		fmt.Printf("VIDEO LOUNGE: Detected refresh for %s. Cleaning up old SID: %s\n", nickname, oldSid)
		sio.Sockets().Emit("user_left_lounge", oldSid)
	}

	s.Emit("all_users", usersInLounge)
	fmt.Printf("VIDEO LOUNGE: %s (%s) joined.\n", nickname, sid)
	sio.Sockets().Except(socketio.Room(sid)).Emit("user_joined_lounge", map[string]any{"sid": sid, "nickname": nickname})

	for _, sharerSid := range sharers {
		s.Emit("screen_sharing_started", map[string]any{"sid": sharerSid})
	}
}

func handleWebRTCOffer(s *socketio.Socket, data []any) {
	if len(data) < 1 {
		return
	}
	msg, ok := data[0].(map[string]any)
	if !ok {
		return
	}
	targetSid, _ := msg["targetSid"].(string)
	offer := msg["offer"]
	sid := string(s.Id())

	state.mu.Lock()
	senderNickname := state.videoChatUsers[sid]
	state.mu.Unlock()
	if senderNickname == "" {
		senderNickname = "Anonymous"
	}

	emitToSID(targetSid, "webrtc_offer", map[string]any{"offer": offer, "senderSid": sid, "senderNickname": senderNickname})
}

func handleWebRTCAnswer(s *socketio.Socket, data []any) {
	if len(data) < 1 {
		return
	}
	msg, ok := data[0].(map[string]any)
	if !ok {
		return
	}
	targetSid, _ := msg["targetSid"].(string)
	answer := msg["answer"]
	sid := string(s.Id())

	state.mu.Lock()
	senderNickname := state.videoChatUsers[sid]
	state.mu.Unlock()
	if senderNickname == "" {
		senderNickname = "Anonymous"
	}

	emitToSID(targetSid, "webrtc_answer", map[string]any{"answer": answer, "senderSid": sid, "senderNickname": senderNickname})
}

func handleWebRTCCandidate(s *socketio.Socket, data []any) {
	if len(data) < 1 {
		return
	}
	msg, ok := data[0].(map[string]any)
	if !ok {
		return
	}
	targetSid, _ := msg["targetSid"].(string)
	candidate := msg["candidate"]
	sid := string(s.Id())

	emitToSID(targetSid, "webrtc_candidate", map[string]any{"candidate": candidate, "senderSid": sid})
}

func handleScreenSharingStarted(s *socketio.Socket) {
	sid := string(s.Id())
	state.mu.Lock()
	state.screenSharers[sid] = true
	state.mu.Unlock()
	sio.Sockets().Except(socketio.Room(sid)).Emit("screen_sharing_started", map[string]any{"sid": sid})
}

func handleScreenSharingStopped(s *socketio.Socket) {
	sid := string(s.Id())
	state.mu.Lock()
	delete(state.screenSharers, sid)
	state.mu.Unlock()
	sio.Sockets().Except(socketio.Room(sid)).Emit("screen_sharing_stopped", map[string]any{"sid": sid})
}

func runPeriodicBanSync() {
	for {
		time.Sleep(300 * time.Second)
		syncBanListFromDB()
	}
}

func checkExpiredMutes() {
	for {
		time.Sleep(time.Second)
		now := time.Now()
		type unmute struct {
			sids []string
		}
		var toUnmute []unmute
		state.mu.Lock()
		for nickname, details := range state.mutedUserDetails {
			if !now.Before(details.MuteUntil) {
				fmt.Printf("Mute expired for %s. Unmuting.\n", nickname)
				delete(state.mutedUsers, nickname)
				delete(state.mutedUserDetails, nickname)
				sids := make([]string, 0, len(state.usersWithSID[nickname]))
				for sid := range state.usersWithSID[nickname] {
					sids = append(sids, sid)
				}
				toUnmute = append(toUnmute, unmute{sids})
			}
		}
		state.mu.Unlock()
		for _, u := range toUnmute {
			for _, sid := range u.sids {
				emitToSID(sid, "force_unmute", map[string]any{})
				emitToSID(sid, "system_message", map[string]any{"message": "You are no longer muted."})
			}
		}
	}
}

func dailyChatlogRotation() {
	chicago, err := time.LoadLocation("America/Chicago")
	if err != nil {
		fmt.Printf("Failed to load America/Chicago timezone: %v. Falling back to system timezone.\n", err)
		chicago = time.Local
	}
	for {
		now := time.Now().In(chicago)
		next := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 1, 0, chicago).Add(24 * time.Hour)
		time.Sleep(next.Sub(now))
		clearChatlogs()
		state.resetPromptHistory()
		fmt.Println("Daily chat reset: logs rotated and AI prompt history cleared.")
		sio.Sockets().Emit("chat_cleared", map[string]any{})
	}
}
