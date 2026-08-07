package main

import (
	"fmt"
	"sync"
	"time"

	genai "google.golang.org/genai"
)

// MuteDetails holds the end time of an active mute.
type MuteDetails struct {
	MuteUntil time.Time
}

// UserOffense tracks how many spam offenses a user has committed.
type UserOffense struct {
	Count       int
	LastOffense time.Time
}

// State is the thread-safe equivalent of utils/globals.py.
type State struct {
	mu sync.Mutex

	connectedUsernames    map[string]bool
	typingUsers           map[string]bool
	aiPromptHistory       []*genai.Content
	usersWithSID          map[string]map[string]bool
	usersWithIP           map[string]string
	usersToJumpscare      map[string]bool
	usersToCrash          map[string]bool
	usersToCensor         map[string]bool
	kickedUsers           map[string]bool
	mutedUsers            map[string]bool
	userMessageTimestamps map[string][]time.Time
	mutedUserDetails      map[string]*MuteDetails
	userOffenses          map[string]*UserOffense
	bannedIPsCache        map[string]*time.Time
	videoChatUsers        map[string]string // sid -> nickname
	screenSharers         map[string]bool   // sid
}

var state = &State{}

func initState() {
	state.mu.Lock()
	defer state.mu.Unlock()
	state.connectedUsernames = map[string]bool{}
	state.typingUsers = map[string]bool{}
	state.aiPromptHistory = []*genai.Content{}
	state.usersWithSID = map[string]map[string]bool{}
	state.usersWithIP = map[string]string{}
	state.usersToJumpscare = map[string]bool{}
	state.usersToCrash = map[string]bool{}
	state.usersToCensor = map[string]bool{}
	state.kickedUsers = map[string]bool{}
	state.mutedUsers = map[string]bool{}
	state.userMessageTimestamps = map[string][]time.Time{}
	state.mutedUserDetails = map[string]*MuteDetails{}
	state.userOffenses = map[string]*UserOffense{}
	state.bannedIPsCache = map[string]*time.Time{}
	state.videoChatUsers = map[string]string{}
	state.screenSharers = map[string]bool{}
}

func (s *State) getOnlineUsers() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	users := make([]string, 0, len(s.connectedUsernames))
	for u := range s.connectedUsernames {
		users = append(users, u)
	}
	return users
}

func (s *State) isConnected(nickname string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.connectedUsernames[nickname]
}

// ensureConnected registers a user/sid mapping, mirroring handle_chat_message.
func (s *State) ensureConnected(nickname, sid, ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.connectedUsernames[nickname] {
		s.connectedUsernames[nickname] = true
		if s.usersWithSID[nickname] == nil {
			s.usersWithSID[nickname] = map[string]bool{}
		}
		s.usersWithSID[nickname][sid] = true
		s.usersWithIP[nickname] = ip
	}
}

func (s *State) getMutedUser(nickname string) (*MuteDetails, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.mutedUserDetails[nickname]
	return d, ok
}

func (s *State) recordSpam(nickname string, now time.Time) (int, time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.userOffenses[nickname]; !ok {
		s.userOffenses[nickname] = &UserOffense{Count: 0, LastOffense: now}
	}
	if now.Sub(s.userOffenses[nickname].LastOffense) > 5*time.Minute {
		s.userOffenses[nickname].Count = 0
	}
	s.userOffenses[nickname].Count++
	s.userOffenses[nickname].LastOffense = now
	duration := 10 + 5*(s.userOffenses[nickname].Count-1)
	muteUntil := now.Add(time.Duration(duration) * time.Second)
	s.mutedUserDetails[nickname] = &MuteDetails{MuteUntil: muteUntil}
	s.mutedUsers[nickname] = true
	return duration, muteUntil
}

// registerMessageStamp returns true if the user exceeded the 5 messages / 2 seconds
// threshold, i.e. spam was detected.
func (s *State) registerMessageStamp(nickname string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.userMessageTimestamps[nickname] == nil {
		s.userMessageTimestamps[nickname] = []time.Time{}
	}
	s.userMessageTimestamps[nickname] = append(s.userMessageTimestamps[nickname], now)
	twoSecondsAgo := now.Add(-2 * time.Second)
	filtered := s.userMessageTimestamps[nickname][:0]
	for _, ts := range s.userMessageTimestamps[nickname] {
		if ts.After(twoSecondsAgo) {
			filtered = append(filtered, ts)
		}
	}
	s.userMessageTimestamps[nickname] = filtered
	return len(filtered) > 5
}

func (s *State) isCensored(nickname string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.usersToCensor[nickname]
}

func (s *State) getBanned(nickname, ip string) (*time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, ok := s.bannedIPsCache[fmt.Sprintf("%s@%s", nickname, ip)]
	return exp, ok
}

func (s *State) setBannedIPs(cache map[string]*time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bannedIPsCache = cache
}

func (s *State) addBan(nickname, ip string, expiresAt *time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bannedIPsCache[fmt.Sprintf("%s@%s", nickname, ip)] = expiresAt
}

func (s *State) isKicked(nickname string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.kickedUsers[nickname]
}

func (s *State) clearKicked(nickname string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.kickedUsers, nickname)
}

func (s *State) kickUser(nickname string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.kickedUsers[nickname] = true
}

func (s *State) getSIDs(nickname string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	sids := make([]string, 0, len(s.usersWithSID[nickname]))
	for sid := range s.usersWithSID[nickname] {
		sids = append(sids, sid)
	}
	return sids
}

func (s *State) addJumpscare(nickname string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.usersToJumpscare[nickname] = true
	return true
}

func (s *State) addCrash(nickname string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.usersToCrash[nickname] = true
}

func (s *State) addCensor(nickname string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.usersToCensor[nickname] = true
}

func (s *State) removeCensor(nickname string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.usersToCensor, nickname)
}

func (s *State) muteUser(nickname string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mutedUsers[nickname] = true
}

func (s *State) unmuteUser(nickname string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.mutedUserDetails, nickname)
	delete(s.mutedUsers, nickname)
}

func (s *State) usersWithIPCopy() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]string, len(s.usersWithIP))
	for k, v := range s.usersWithIP {
		out[k] = v
	}
	return out
}

func (s *State) usersWithIPOf(nickname string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.usersWithIP[nickname]
	return v, ok
}

func (s *State) resetPromptHistory() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.aiPromptHistory = []*genai.Content{}
}

func (s *State) promptHistory() []*genai.Content {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*genai.Content, 0, len(s.aiPromptHistory))
	out = append(out, s.aiPromptHistory...)
	return out
}

func (s *State) promptHistoryLen() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.aiPromptHistory)
}

// image buffer state
var imageBuffersMu sync.Mutex
var imageBuffers = map[string][][]byte{}

func storeImageChunk(id string, chunk []byte) {
	imageBuffersMu.Lock()
	defer imageBuffersMu.Unlock()
	imageBuffers[id] = append(imageBuffers[id], chunk)
}

func takeImageChunks(id string) [][]byte {
	imageBuffersMu.Lock()
	defer imageBuffersMu.Unlock()
	chunks := imageBuffers[id]
	delete(imageBuffers, id)
	return chunks
}
